package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/madamgy/recipie/internal/db"
)

type testIdentity struct {
	mu        sync.Mutex
	users     map[string]Identity
	passwords map[string]string
}

func newTestIdentity() *testIdentity {
	return &testIdentity{users: map[string]Identity{}, passwords: map[string]string{}}
}
func (*testIdentity) Enabled() bool { return true }
func (f *testIdentity) SignIn(_ context.Context, email, password string) (Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.passwords[email] != password || password == "" {
		return Identity{}, ErrCredentials
	}
	return f.users[email], nil
}
func testUUID() string {
	v := token()[:32]
	return v[:8] + "-" + v[8:12] + "-" + v[12:16] + "-" + v[16:20] + "-" + v[20:]
}
func (f *testIdentity) Create(_ context.Context, email, password string) (Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := Identity{ID: testUUID(), Email: email}
	f.users[email] = u
	f.passwords[email] = password
	return u, nil
}
func (f *testIdentity) Password(_ context.Context, id, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for email, u := range f.users {
		if u.ID == id {
			f.passwords[email] = password
			return nil
		}
	}
	return fmt.Errorf("test identity not found")
}
func (f *testIdentity) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for email, u := range f.users {
		if u.ID == id {
			delete(f.users, email)
			delete(f.passwords, email)
		}
	}
	return nil
}

type testGateway struct {
	*Razorpay
	mu      sync.Mutex
	order   GatewayOrder
	creates int
	payment Payment
}

func newTestGateway() *testGateway {
	return &testGateway{Razorpay: NewRazorpay("test_key", "test_checkout_secret", "test_webhook_secret")}
}
func (g *testGateway) CreateOrder(_ context.Context, _ string, amount int, currency string) (GatewayOrder, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.creates++
	g.order = GatewayOrder{ID: "order_" + token()[:16], Amount: amount, Currency: currency}
	return g.order, nil
}
func (g *testGateway) FetchPayment(_ context.Context, id string) (Payment, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.payment.ID != id {
		return Payment{}, fmt.Errorf("test payment not found")
	}
	return g.payment, nil
}
func (g *testGateway) set(p Payment) { g.mu.Lock(); defer g.mu.Unlock(); g.payment = p }

func portalRequest(handler http.Handler, method, path string, body any, cookie *http.Cookie, registrationToken string) *httptest.ResponseRecorder {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(payload))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Madamgy-Request", "1")
	r.Header.Set("Origin", "http://localhost:3000")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if registrationToken != "" {
		r.Header.Set("X-Registration-Token", registrationToken)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func expectCode(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("HTTP %d, expected %d: %s", w.Code, code, w.Body.String())
	}
}
func decodeResponse[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode response: %v: %s", err, w.Body.String())
	}
	return v
}

func TestPortalPersistenceAndAuthorization(t *testing.T) {
	databaseURL := os.Getenv("PORTAL_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PORTAL_TEST_DATABASE_URL not set; use an isolated local database")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	identity, gateway := newTestIdentity(), newTestGateway()
	service := New(pool, Options{Identity: identity, Gateway: gateway})
	router := chi.NewRouter()
	service.Routes(router)
	router.With(service.RequireStaff, service.RequireProfile).Get("/test/profiles/{childID}", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]bool{"ok": true}) })
	const password = "test-password-only-123"
	var actors []Actor
	for _, role := range []string{"admin", "doctor", "doctor"} {
		a, err := service.Provision(ctx, "Test "+role, "portal-test-"+token()[:12]+"@example.invalid", password, role)
		if err != nil {
			t.Fatal(err)
		}
		actors = append(actors, a)
	}
	var registrationID string
	oldSettings, err := service.settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(ctx, `UPDATE app_private.consultation_settings SET amount_paise=$1,payments_enabled=$2,updated_by=NULL WHERE singleton`, oldSettings.AmountPaise, oldSettings.PaymentsEnabled)
		if registrationID != "" {
			pool.Exec(ctx, `DELETE FROM app_private.payment_event WHERE order_id IN(SELECT id FROM app_private.consultation_order WHERE registration_id=$1)`, registrationID)
			pool.Exec(ctx, `DELETE FROM app_private.consultation_order WHERE registration_id=$1`, registrationID)
			pool.Exec(ctx, `DELETE FROM app_private.consultation_registration WHERE id=$1`, registrationID)
			pool.Exec(ctx, `DELETE FROM public.child_profile WHERE child_id=$1`, registrationID)
		}
		for _, a := range actors {
			pool.Exec(ctx, `DELETE FROM app_private.staff_account WHERE id=$1`, a.ID)
		}
	}()
	login := func(a Actor) *http.Cookie {
		t.Helper()
		w := portalRequest(router, "POST", "/api/auth/login", map[string]string{"email": a.Email, "password": password}, nil, "")
		expectCode(t, w, 200)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
			t.Fatalf("invalid session cookie: %v", cookies)
		}
		if bytes.Contains(w.Body.Bytes(), []byte("token")) {
			t.Fatal("session token leaked in JSON")
		}
		return cookies[0]
	}
	admin, doctor, otherDoctor := login(actors[0]), login(actors[1]), login(actors[2])
	expectCode(t, portalRequest(router, "POST", "/api/auth/login", map[string]string{"email": actors[0].Email, "password": "wrong"}, nil, ""), 401)
	expectCode(t, portalRequest(router, "GET", "/api/admin/staff", nil, doctor, ""), 403)
	expectCode(t, portalRequest(router, "GET", "/api/registrations", nil, nil, ""), 401)
	forged := *doctor
	forged.Value = token()
	expectCode(t, portalRequest(router, "GET", "/api/registrations", nil, &forged, ""), 401)

	input := Intake{GuardianName: "Test Guardian", ChildName: "Test Child", DateOfBirth: "2023-02-28", Phone: "9876543210", Token: token()}
	w := portalRequest(router, "POST", "/api/public/registrations", input, nil, "")
	expectCode(t, w, 201)
	registrationID = decodeResponse[map[string]string](t, w)["id"]
	if !validUUID(registrationID) {
		t.Fatal("no registration identifier")
	}
	w = portalRequest(router, "POST", "/api/public/registrations", input, nil, "")
	expectCode(t, w, 201)
	if decodeResponse[map[string]string](t, w)["id"] != registrationID {
		t.Fatal("repeated form created another child")
	}
	changed := input
	changed.ChildName = "Different Test Child"
	expectCode(t, portalRequest(router, "POST", "/api/public/registrations", changed, nil, ""), 409)
	var storedPhone string
	var storedEmail, storedMother *string
	if err := pool.QueryRow(ctx, `SELECT r.phone,r.email,p.mother_name FROM app_private.consultation_registration r JOIN public.child_profile p ON p.child_id=r.child_id WHERE r.id=$1`, registrationID).Scan(&storedPhone, &storedEmail, &storedMother); err != nil {
		t.Fatal(err)
	}
	if storedPhone != "+919876543210" || storedEmail != nil || storedMother != nil {
		t.Fatalf("invented or incorrect intake values: %q %v %v", storedPhone, storedEmail, storedMother)
	}
	path := "/api/registrations/" + registrationID
	expectCode(t, portalRequest(router, "GET", path, nil, doctor, ""), 404)
	expectCode(t, portalRequest(router, "PATCH", path, map[string]string{"assigned_doctor_id": actors[1].ID}, admin, ""), 200)
	expectCode(t, portalRequest(router, "GET", path, nil, doctor, ""), 200)
	expectCode(t, portalRequest(router, "GET", path, nil, otherDoctor, ""), 404)
	list := decodeResponse[RegistrationListTest](t, portalRequest(router, "GET", "/api/registrations", nil, doctor, ""))
	if len(list.Items) != 1 || list.Items[0].ID != registrationID {
		t.Fatal("assigned child missing")
	}
	otherList := decodeResponse[RegistrationListTest](t, portalRequest(router, "GET", "/api/registrations", nil, otherDoctor, ""))
	if len(otherList.Items) != 0 {
		t.Fatal("another doctor's child leaked")
	}
	expectCode(t, portalRequest(router, "GET", "/test/profiles/"+registrationID, nil, doctor, ""), 200)
	expectCode(t, portalRequest(router, "GET", "/test/profiles/"+registrationID, nil, otherDoctor, ""), 404)
	expectCode(t, portalRequest(router, "PATCH", path, map[string]string{"assigned_doctor_id": actors[2].ID}, doctor, ""), 403)
	expectCode(t, portalRequest(router, "PATCH", path, map[string]string{"payment_status": "paid"}, admin, ""), 400)
	expectCode(t, portalRequest(router, "PATCH", path, map[string]string{"status": "contacted", "notes": "Test follow-up note"}, doctor, ""), 200)

	request := httptest.NewRequest("PATCH", path, bytes.NewBufferString(`{"notes":"forged"}`))
	request.AddCookie(admin)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://other.example")
	request.Header.Set("X-Madamgy-Request", "1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	expectCode(t, recorder, 403)
	expectCode(t, portalRequest(router, "PUT", "/api/admin/settings", map[string]any{"amount_paise": 12345, "payments_enabled": true}, admin, ""), 200)
	publicPath := "/api/public/registrations/" + registrationID
	expectCode(t, portalRequest(router, "POST", publicPath+"/order", map[string]string{}, nil, token()), 404)
	var wait sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wait.Go(func() {
			responses <- portalRequest(router, "POST", publicPath+"/order", map[string]string{}, nil, input.Token)
		})
	}
	wait.Wait()
	close(responses)
	for response := range responses {
		expectCode(t, response, 200)
	}
	if gateway.creates != 1 {
		t.Fatalf("created %d orders, expected one", gateway.creates)
	}
	order := gateway.order
	p := Payment{ID: "pay_" + token()[:16], OrderID: order.ID, Amount: order.Amount, Currency: order.Currency, Status: "authorized"}
	gateway.set(p)
	confirmation := map[string]string{"razorpay_order_id": order.ID, "razorpay_payment_id": p.ID, "razorpay_signature": sign(gateway.Secret, []byte(order.ID+"|"+p.ID))}
	w = portalRequest(router, "POST", publicPath+"/verify", confirmation, nil, input.Token)
	expectCode(t, w, 200)
	if decodeResponse[map[string]string](t, w)["payment_status"] != "authorized" {
		t.Fatal("authorization incorrectly counted as paid")
	}
	confirmation["razorpay_signature"] = "invalid"
	expectCode(t, portalRequest(router, "POST", publicPath+"/verify", confirmation, nil, input.Token), 400)
	webhook := func(eventID string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"event": "payment.captured", "payload": map[string]any{"payment": map[string]any{"entity": map[string]string{"id": p.ID}}}})
		r := httptest.NewRequest("POST", "/api/payments/webhook", bytes.NewReader(body))
		r.Header.Set("X-Razorpay-Signature", sign(gateway.WebhookSecret, body))
		r.Header.Set("X-Razorpay-Event-Id", eventID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	p.Status = "captured"
	p.Captured = true
	p.Amount++
	gateway.set(p)
	expectCode(t, webhook("mismatch-"+registrationID), 500)
	p.Amount--
	gateway.set(p)
	eventID := "capture-" + registrationID
	expectCode(t, webhook(eventID), 200)
	expectCode(t, webhook(eventID), 200)
	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app_private.payment_event WHERE id=$1`, eventID).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("event count: %d %v", eventCount, err)
	}
	p.Status = "failed"
	p.Captured = false
	gateway.set(p)
	expectCode(t, webhook("late-"+registrationID), 200)
	paid := decodeResponse[Registration](t, portalRequest(router, "GET", path, nil, admin, ""))
	if paid.PaymentStatus != "paid" {
		t.Fatalf("capture regressed: %s", paid.PaymentStatus)
	}
	p.Status = "captured"
	p.Captured = true
	p.AmountRefunded = 500
	gateway.set(p)
	expectCode(t, webhook("partial-"+registrationID), 200)
	p.AmountRefunded = 0
	gateway.set(p)
	expectCode(t, webhook("stale-"+registrationID), 200)
	partial := decodeResponse[Registration](t, portalRequest(router, "GET", path, nil, admin, ""))
	if partial.PaymentStatus != "partially_refunded" || partial.RefundedPaise != 500 {
		t.Fatal("refund regressed")
	}
	p.AmountRefunded = p.Amount
	p.Status = "refunded"
	gateway.set(p)
	expectCode(t, webhook("refund-"+registrationID), 200)
	expectCode(t, portalRequest(router, "POST", publicPath+"/order", map[string]string{}, nil, input.Token), 409)
	public := portalRequest(router, "GET", publicPath, nil, nil, input.Token)
	expectCode(t, public, 200)
	if bytes.Contains(public.Body.Bytes(), []byte("guardian")) || bytes.Contains(public.Body.Bytes(), []byte("phone")) {
		t.Fatal("public status leaked identity")
	}

	// Even an accidental browser SELECT grant cannot bypass RLS on private tables.
	role := "portal_browser_" + token()[:12]
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN"); err != nil {
		t.Fatal(err)
	}
	defer func() { pool.Exec(ctx, "DROP OWNED BY "+quoted); pool.Exec(ctx, "DROP ROLE "+quoted) }()
	if _, err := pool.Exec(ctx, "GRANT USAGE ON SCHEMA app_private TO "+quoted); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "GRANT SELECT ON app_private.consultation_registration TO "+quoted); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+quoted); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM app_private.consultation_registration`).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	tx.Rollback(ctx)
	if visible != 0 {
		t.Fatal("RLS exposed registrations")
	}

	expectCode(t, portalRequest(router, "PATCH", "/api/admin/staff/"+actors[1].ID, map[string]any{"name": actors[1].Name, "active": false}, admin, ""), 200)
	expectCode(t, portalRequest(router, "GET", "/api/auth/me", nil, doctor, ""), 401)
	unassigned := decodeResponse[Registration](t, portalRequest(router, "GET", path, nil, admin, ""))
	if unassigned.AssignedDoctorID != "" {
		t.Fatal("deactivated doctor retained assignment")
	}
	expectCode(t, portalRequest(router, "POST", "/api/auth/logout", map[string]string{}, otherDoctor, ""), 200)
	expectCode(t, portalRequest(router, "GET", "/api/auth/me", nil, otherDoctor, ""), 401)
}

type RegistrationListTest struct {
	Items []Registration `json:"items"`
}

type pausedIdentity struct {
	*testIdentity
	email   string
	entered chan struct{}
	release chan struct{}
}

func (p *pausedIdentity) SignIn(ctx context.Context, email, password string) (Identity, error) {
	identity, err := p.testIdentity.SignIn(ctx, email, password)
	if err == nil && email == p.email {
		select {
		case p.entered <- struct{}{}:
		case <-ctx.Done():
			return Identity{}, ctx.Err()
		}
		select {
		case <-p.release:
		case <-ctx.Done():
			return Identity{}, ctx.Err()
		}
	}
	return identity, err
}

func TestResetRevokesLoginAlreadyCheckingOldPassword(t *testing.T) {
	databaseURL := os.Getenv("PORTAL_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PORTAL_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	identity := &pausedIdentity{testIdentity: newTestIdentity(), entered: make(chan struct{}, 1), release: make(chan struct{})}
	service := New(pool, Options{Identity: identity})
	routes := chi.NewRouter()
	service.Routes(routes)
	// Give the helper requests the same cancellation boundary as the test.
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { routes.ServeHTTP(w, r.WithContext(ctx)) })
	const password = "test-old-password-123"
	admin, err := service.Provision(ctx, "Test admin", "reset-admin-"+token()[:12]+"@example.invalid", password, "admin")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM app_private.staff_account WHERE id=$1`, admin.ID)
	doctor, err := service.Provision(ctx, "Test doctor", "reset-doctor-"+token()[:12]+"@example.invalid", password, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM app_private.staff_account WHERE id=$1`, doctor.ID)
	identity.email = doctor.Email
	adminLogin := portalRequest(router, "POST", "/api/auth/login", map[string]string{"email": admin.Email, "password": password}, nil, "")
	expectCode(t, adminLogin, 200)
	adminCookie := adminLogin.Result().Cookies()[0]
	loginDone, resetDone := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(identity.release) }) }
	defer release()
	go func() {
		loginDone <- portalRequest(router, "POST", "/api/auth/login", map[string]string{"email": doctor.Email, "password": password}, nil, "")
	}()
	select {
	case <-identity.entered:
	case <-ctx.Done():
		t.Fatal("login did not reach the identity provider")
	}
	go func() {
		resetDone <- portalRequest(router, "POST", "/api/admin/staff/"+doctor.ID+"/password", map[string]string{"password": "test-new-password-123"}, adminCookie, "")
	}()
	select {
	case <-resetDone:
		t.Fatal("password reset passed a login still checking the previous password")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	var login *httptest.ResponseRecorder
	select {
	case login = <-loginDone:
		expectCode(t, login, 200)
	case <-ctx.Done():
		t.Fatal("login did not finish")
	}
	select {
	case reset := <-resetDone:
		expectCode(t, reset, 200)
	case <-ctx.Done():
		t.Fatal("password reset did not finish")
	}
	expectCode(t, portalRequest(router, "GET", "/api/auth/me", nil, login.Result().Cookies()[0], ""), 401)
	expectCode(t, portalRequest(router, "POST", "/api/auth/login", map[string]string{"email": doctor.Email, "password": password}, nil, ""), 401)
}
