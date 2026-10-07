package portal

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/db"
)

type paidFixture struct {
	pool          *pgxpool.Pool
	server        *Server
	router        http.Handler
	gateway       *testGateway
	admin         Actor
	doctors       []Actor
	adminCookie   *http.Cookie
	start         time.Time
	registrations []string
}

func newPaidFixture(t *testing.T) *paidFixture {
	t.Helper()
	dsn := os.Getenv("PORTAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PORTAL_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &paidFixture{pool: pool, gateway: newTestGateway()}
	f.server = New(pool, Options{Identity: newTestIdentity(), Gateway: f.gateway})
	router := chi.NewRouter()
	f.server.Routes(router)
	f.router = router
	f.admin, err = f.server.Provision(ctx, "Test administrator", "paid-admin-"+token()[:12]+"@example.invalid", "test-password-only-123", "admin")
	if err != nil {
		t.Fatal(err)
	}
	w := portalRequest(router, "POST", "/api/auth/login", map[string]string{"email": f.admin.Email, "password": "test-password-only-123"}, nil, "")
	expectCode(t, w, 200)
	f.adminCookie = w.Result().Cookies()[0]
	day := time.Now().In(indiaTime).AddDate(0, 0, 3)
	f.start = time.Date(day.Year(), day.Month(), day.Day(), 10, 0, 0, 0, indiaTime)
	old, err := f.server.settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `UPDATE app_private.consultation_settings SET amount_paise=$1,payments_enabled=$2,updated_by=NULL WHERE singleton`, old.AmountPaise, old.PaymentsEnabled)
		for _, id := range f.registrations {
			pool.Exec(ctx, `DELETE FROM app_private.payment_event WHERE order_id IN(SELECT id FROM app_private.consultation_order WHERE registration_id=$1)`, id)
			pool.Exec(ctx, `DELETE FROM app_private.consultation_order WHERE registration_id=$1`, id)
			pool.Exec(ctx, `DELETE FROM app_private.consultation_registration WHERE id=$1`, id)
			pool.Exec(ctx, `DELETE FROM public.child_profile WHERE child_id=$1`, id)
			pool.Exec(ctx, `DELETE FROM app_private.guardian_account WHERE email=$1`, "access+"+id+"@family.invalid")
		}
		for _, a := range append(f.doctors, f.admin) {
			pool.Exec(ctx, `DELETE FROM app_private.doctor_availability WHERE doctor_id=$1`, a.ID)
			pool.Exec(ctx, `DELETE FROM app_private.staff_account WHERE id=$1`, a.ID)
		}
		pool.Close()
	})
	for _, name := range []string{"Test Doctor A", "Test Doctor B"} {
		d, err := f.server.Provision(ctx, name, "paid-doctor-"+token()[:12]+"@example.invalid", "test-password-only-123", "doctor")
		if err != nil {
			t.Fatal(err)
		}
		f.doctors = append(f.doctors, d)
		expectCode(t, portalRequest(router, "POST", "/api/availability", map[string]string{"doctor_id": d.ID, "starts_at": f.start.Format(time.RFC3339), "ends_at": f.start.Add(5 * time.Hour).Format(time.RFC3339)}, f.adminCookie, ""), 200)
	}
	expectCode(t, portalRequest(router, "PUT", "/api/admin/settings", map[string]any{"amount_paise": 25000, "payments_enabled": true}, f.adminCookie, ""), 200)
	return f
}

func (f *paidFixture) register(t *testing.T, name string) (string, string) {
	t.Helper()
	secret := token()
	w := portalRequest(f.router, "POST", "/api/public/registrations", Intake{GuardianName: "Test Guardian", ChildName: name, DateOfBirth: "2022-05-01", Phone: "9876543210", Token: secret}, nil, "")
	expectCode(t, w, 201)
	id := decodeResponse[map[string]string](t, w)["id"]
	f.registrations = append(f.registrations, id)
	return id, secret
}

func (f *paidFixture) book(t *testing.T, id, secret, doctor string, start time.Time) string {
	t.Helper()
	w := portalRequest(f.router, "POST", "/api/public/registrations/"+id+"/appointment", map[string]string{"doctor_id": doctor, "starts_at": start.Format(time.RFC3339), "ends_at": start.Add(30 * time.Minute).Format(time.RFC3339)}, nil, secret)
	expectCode(t, w, 201)
	return decodeResponse[map[string]string](t, w)["id"]
}

func (f *paidFixture) order(t *testing.T, id, secret string) GatewayOrder {
	t.Helper()
	w := portalRequest(f.router, "POST", "/api/public/registrations/"+id+"/order", map[string]string{}, nil, secret)
	expectCode(t, w, 200)
	var value struct {
		ID       string `json:"order_id"`
		Amount   int    `json:"amount_paise"`
		Currency string `json:"currency"`
	}
	value = decodeResponse[struct {
		ID       string `json:"order_id"`
		Amount   int    `json:"amount_paise"`
		Currency string `json:"currency"`
	}](t, w)
	return GatewayOrder{ID: value.ID, Amount: value.Amount, Currency: value.Currency}
}

func (f *paidFixture) status(t *testing.T, id, want string) {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM app_private.appointment WHERE id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("appointment status %s, expected %s", status, want)
	}
}

func TestPaidBookingExpiryRefundAndRebooking(t *testing.T) {
	f := newPaidFixture(t)
	ctx := context.Background()
	id, secret := f.register(t, "Test Child")
	first := f.book(t, id, secret, f.doctors[0].ID, f.start)
	order := f.order(t, id, secret)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+first+"/decision", map[string]string{"action": "confirm"}, f.adminCookie, ""), 409)
	if _, err := f.pool.Exec(ctx, `UPDATE app_private.appointment SET hold_expires_at=now()-interval '1 second' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/public/registrations/"+id, nil, nil, secret), 200)
	f.status(t, first, "expired")
	expectCode(t, portalRequest(f.router, "POST", "/api/public/registrations/"+id+"/order", map[string]string{}, nil, secret), 409)
	second := f.book(t, id, secret, "", f.start)
	secondOrder := f.order(t, id, secret)
	if order.ID == secondOrder.ID {
		t.Fatal("expired order was reused")
	}
	p := Payment{ID: "pay_" + token()[:16], OrderID: order.ID, Amount: order.Amount, Currency: "INR", Status: "captured", Captured: true}
	if err := f.server.applyPayment(ctx, p, "late-"+id); err != nil {
		t.Fatal(err)
	}
	f.status(t, first, "refund_required")
	f.status(t, second, "awaiting_payment")
	p.ID = "pay_" + token()[:16]
	p.OrderID = secondOrder.ID
	if err := f.server.applyPayment(ctx, p, "capture-"+id); err != nil {
		t.Fatal(err)
	}
	f.status(t, second, "paid_pending_admin")
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+second+"/decision", map[string]string{"action": "confirm"}, f.adminCookie, ""), 200)
	f.status(t, second, "confirmed")
	expectCode(t, portalRequest(f.router, "GET", "/api/appointments", nil, f.adminCookie, ""), 200)
	expectCode(t, portalRequest(f.router, "GET", "/api/book-releases", nil, f.adminCookie, ""), 200)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+second+"/decision", map[string]string{"action": "cancel"}, f.adminCookie, ""), 200)
	f.status(t, second, "refund_required")
	f.gateway.set(p)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+second+"/refund", map[string]string{}, nil, ""), 401)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+second+"/refund", map[string]string{}, f.adminCookie, ""), 200)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+second+"/refund", map[string]string{}, f.adminCookie, ""), 409)
	p.AmountRefunded = p.Amount
	p.Status = "refunded"
	if err := f.server.applyPayment(ctx, p, "refund-"+id); err != nil {
		t.Fatal(err)
	}
	f.status(t, second, "refunded")
	third := f.book(t, id, secret, f.doctors[0].ID, f.start)
	f.status(t, third, "awaiting_payment")
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+third+"/decision", map[string]string{"action": "confirm"}, f.adminCookie, ""), 409)
	if got := f.order(t, id, secret); got.ID == secondOrder.ID {
		t.Fatal("refunded order was reused")
	}
}

func TestChildAccessIsLimitedToVerifiedRegistration(t *testing.T) {
	f := newPaidFixture(t)
	ctx := context.Background()
	id, secret := f.register(t, "Test Child")
	sibling, siblingSecret := f.register(t, "Sibling Child")
	access := func(name, dob, credential string) *http.Cookie {
		t.Helper()
		w := portalRequest(f.router, "POST", "/api/family/auth/access", map[string]string{"child_name": name, "date_of_birth": dob, "token": credential}, nil, "")
		expectCode(t, w, 200)
		return w.Result().Cookies()[0]
	}
	for _, fields := range []map[string]string{
		{"child_name": "Unknown Child", "date_of_birth": "2022-05-01", "token": secret},
		{"child_name": "Test Child", "date_of_birth": "2022-05-02", "token": secret},
		{"child_name": "Test Child", "date_of_birth": "2022-05-01", "token": token()},
	} {
		expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/access", fields, nil, ""), 401)
	}
	cookie := access("  test   child ", "2022-05-01", secret)
	var guardianID string
	if err := f.pool.QueryRow(ctx, `SELECT guardian_id FROM app_private.consultation_registration WHERE id=$1`, id).Scan(&guardianID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE app_private.consultation_registration SET guardian_id=$1 WHERE id=$2`, guardianID, sibling); err != nil {
		t.Fatal(err)
	}
	w := portalRequest(f.router, "GET", "/api/family/registrations", nil, cookie, "")
	expectCode(t, w, 200)
	items := decodeResponse[[]FamilyRegistration](t, w)
	if len(items) != 1 || items[0].ID != id {
		t.Fatal("child token exposed a sibling")
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/family/registrations/"+sibling, nil, cookie, ""), 404)
	expectCode(t, portalRequest(f.router, "POST", "/api/family/registrations/"+sibling+"/order", map[string]string{}, cookie, ""), 404)
	expectCode(t, portalRequest(f.router, "POST", "/api/family/registrations", map[string]string{}, cookie, ""), 403)
	expectCode(t, portalRequest(f.router, "GET", "/api/registrations", nil, cookie, ""), 401)
	other := access("Sibling Child", "2022-05-01", siblingSecret)
	var releaseID string
	if err := f.pool.QueryRow(ctx, `INSERT INTO app_private.book_release(registration_id,child_id,book,pdf,generated_by) VALUES ($1,$1,'book1',$2,$3) RETURNING id`, sibling, []byte("%PDF-test"), f.admin.ID).Scan(&releaseID); err != nil {
		t.Fatal(err)
	}
	path := "/api/family/book-releases/" + releaseID + "/download"
	expectCode(t, portalRequest(f.router, "GET", path, nil, other, ""), 404)
	expectCode(t, portalRequest(f.router, "PATCH", "/api/admin/book-releases/"+releaseID, map[string]string{"action": "approve"}, f.adminCookie, ""), 200)
	expectCode(t, portalRequest(f.router, "GET", path, nil, cookie, ""), 404)
	expectCode(t, portalRequest(f.router, "GET", path, nil, other, ""), 200)
	if _, err := f.pool.Exec(ctx, `UPDATE app_private.guardian_session SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, tokenHash(cookie.Value)); err != nil {
		t.Fatal(err)
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/family/registrations", nil, cookie, ""), 401)
}

func TestConcurrentPublicSlotRequests(t *testing.T) {
	f := newPaidFixture(t)
	id1, key1 := f.register(t, "Race One")
	id2, key2 := f.register(t, "Race Two")
	var group sync.WaitGroup
	codes := make(chan int, 2)
	for _, value := range [][2]string{{id1, key1}, {id2, key2}} {
		group.Go(func() {
			w := portalRequest(f.router, "POST", "/api/public/registrations/"+value[0]+"/appointment", map[string]string{"doctor_id": f.doctors[0].ID, "starts_at": f.start.Format(time.RFC3339), "ends_at": f.start.Add(30 * time.Minute).Format(time.RFC3339)}, nil, value[1])
			codes <- w.Code
		})
	}
	group.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatalf("slot race returned %v", counts)
	}
}

func TestBookingMigrationPreservesLegacyPayment(t *testing.T) {
	f := newPaidFixture(t)
	ctx := context.Background()
	id, _ := f.register(t, "Legacy Child")
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, name := range []string{"0044_consultation_refunds.down.sql", "0043_paid_booking_and_family_access.down.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var appointmentID string
	if err = tx.QueryRow(ctx, `INSERT INTO app_private.appointment(registration_id,doctor_id,starts_at,ends_at,mode,status,requested_by) VALUES ($1,$2,$3,$4,'specific_doctor','pending_admin',$5) RETURNING id`, id, f.doctors[0].ID, f.start, f.start.Add(time.Hour), f.admin.ID).Scan(&appointmentID); err != nil {
		t.Fatal(err)
	}
	orderID := "order_" + token()[:16]
	if _, err = tx.Exec(ctx, `INSERT INTO app_private.consultation_order(id,registration_id,amount_paise,currency,status) VALUES($1,$2,25000,'INR','paid')`, orderID, id); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0043_paid_booking_and_family_access.up.sql", "0044_consultation_refunds.up.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var linked string
	if err = tx.QueryRow(ctx, `SELECT appointment_id FROM app_private.consultation_order WHERE id=$1`, orderID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != appointmentID {
		t.Fatal("legacy payment was not preserved on its original appointment")
	}
}
