package portal

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Model an unexpired session created before password entry was retired.
func legacyGuardianCookie(t *testing.T, s *Server, g Guardian) *http.Cookie {
	t.Helper()
	value, err := s.createGuardianSession(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: guardianSessionCookie, Value: value}
}

func issueAccess(t *testing.T, f *paidFixture, id string) string {
	t.Helper()
	w := portalRequest(f.router, "POST", "/api/admin/registrations/"+id+"/family-access", map[string]bool{"guardian_verified": true}, f.adminCookie, "")
	expectCode(t, w, 201)
	var value struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires_at"`
	}
	value = decodeResponse[struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires_at"`
	}](t, w)
	if !validToken(value.Token) || time.Until(value.Expires) > familyAccessLifetime || time.Until(value.Expires) < 14*time.Minute {
		t.Fatal("invalid grant or lifetime")
	}
	return value.Token
}

func TestFamilyRecoveryAuthorizationAndSingleUse(t *testing.T) {
	f := newPaidFixture(t)
	id, old := f.register(t, "Recovery Child")
	path := "/api/admin/registrations/" + id + "/family-access"
	body := map[string]bool{"guardian_verified": true}
	expectCode(t, portalRequest(f.router, "POST", path, body, nil, ""), 401)
	w := portalRequest(f.router, "POST", "/api/auth/login", map[string]string{"email": f.doctors[0].Email, "password": "test-password-only-123"}, nil, "")
	expectCode(t, w, 200)
	expectCode(t, portalRequest(f.router, "POST", path, body, w.Result().Cookies()[0], ""), 403)
	expectCode(t, portalRequest(f.router, "POST", path, map[string]bool{}, f.adminCookie, ""), 400)
	wrongOrigin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Origin", "https://other.example.invalid")
		f.router.ServeHTTP(w, r)
	})
	expectCode(t, portalRequest(wrongOrigin, "POST", path, body, f.adminCookie, ""), 403)
	grant := issueAccess(t, f, id)
	var hash string
	if err := f.pool.QueryRow(context.Background(), `SELECT token_hash FROM app_private.family_access_grant WHERE registration_id=$1`, id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != tokenHash(grant) || hash == grant {
		t.Fatal("grant must be stored only as a hash")
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/family/auth/recover", nil, nil, ""), 405)
	input := map[string]string{"guardian_name": "Test Guardian", "child_name": "Recovery Child", "date_of_birth": "2022-05-01", "token": grant}
	expectCode(t, portalRequest(wrongOrigin, "POST", "/api/family/auth/recover", input, nil, ""), 403)
	for _, wrong := range []map[string]string{
		{"child_name": "Other Child", "date_of_birth": "2022-05-01", "token": grant},
		{"child_name": "Recovery Child", "date_of_birth": "2022-05-02", "token": grant},
		{"child_name": "Recovery Child", "date_of_birth": "2022-05-01", "token": token()},
		{"child_name": "Recovery Child", "date_of_birth": "2022-05-01"},
	} {
		expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", wrong, nil, ""), 401)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() { codes <- portalRequest(f.router, "POST", "/api/family/auth/recover", input, nil, "").Code })
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[401] != 1 {
		t.Fatalf("concurrent redemption: %v", counts)
	}
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", input, nil, ""), 401)
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/access", map[string]string{"guardian_name": "Test Guardian", "child_name": "Recovery Child", "date_of_birth": "2022-05-01", "token": old}, nil, ""), 401)
}

func TestFamilyRecoveryPreservesOwnedPaymentAndRevokesOldAccess(t *testing.T) {
	f := newPaidFixture(t)
	ctx := context.Background()
	id, old := f.register(t, "Recovery Paid Child")
	sibling, _ := f.register(t, "Private Sibling")
	appointment := f.book(t, id, old, f.doctors[0].ID, f.start)
	order := f.order(t, id, old)
	if err := f.server.applyPayment(ctx, Payment{ID: "pay_" + token()[:16], OrderID: order.ID, Amount: order.Amount, Currency: "INR", Status: "captured", Captured: true}, "recovery-"+id); err != nil {
		t.Fatal(err)
	}
	child := map[string]string{"guardian_name": "Test Guardian", "child_name": "Recovery Paid Child", "date_of_birth": "2022-05-01", "token": old}
	w := portalRequest(f.router, "POST", "/api/family/auth/access", child, nil, "")
	expectCode(t, w, 200)
	previous := w.Result().Cookies()[0]
	g := decodeResponse[Guardian](t, w)
	if _, err := f.pool.Exec(ctx, `UPDATE app_private.consultation_registration SET guardian_id=$1 WHERE id=$2`, g.ID, sibling); err != nil {
		t.Fatal(err)
	}
	legacy := legacyGuardianCookie(t, f.server, g)
	grant := issueAccess(t, f, id)
	child["token"] = grant
	w = portalRequest(f.router, "POST", "/api/family/auth/recover", child, nil, "")
	expectCode(t, w, 200)
	receipt := decodeResponse[map[string]string](t, w)
	if receipt["id"] != id || !validToken(receipt["token"]) || receipt["token"] == old {
		t.Fatal("invalid replacement credential")
	}
	child["token"] = receipt["token"]
	w = portalRequest(f.router, "POST", "/api/family/auth/access", child, nil, "")
	expectCode(t, w, 200)
	current := w.Result().Cookies()[0]
	if got := decodeResponse[Guardian](t, w); got.ID != g.ID || got.RegistrationID != id {
		t.Fatal("ownership changed")
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/family/registrations", nil, previous, ""), 401)
	w = portalRequest(f.router, "GET", "/api/family/registrations", nil, current, "")
	expectCode(t, w, 200)
	items := decodeResponse[[]FamilyRegistration](t, w)
	if len(items) != 1 || items[0].ID != id || items[0].PaymentStatus != "paid" || items[0].AppointmentID == nil || *items[0].AppointmentID != appointment {
		t.Fatalf("wrong visible registration/payment: %+v", items)
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/family/registrations/"+sibling, nil, current, ""), 404)
	expectCode(t, portalRequest(f.router, "POST", "/api/family/registrations/"+sibling+"/order", map[string]string{}, current, ""), 404)
	items = decodeResponse[[]FamilyRegistration](t, portalRequest(f.router, "GET", "/api/family/registrations", nil, legacy, ""))
	if len(items) != 2 {
		t.Fatal("existing full family session changed")
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/public/registrations/"+id, nil, nil, old), 404)
}

func TestFamilyRecoveryExpiredReplacedAndInactive(t *testing.T) {
	f := newPaidFixture(t)
	id, secret := f.register(t, "Expiry Child")
	first := issueAccess(t, f, id)
	second := issueAccess(t, f, id)
	input := map[string]string{"guardian_name": "Test Guardian", "child_name": "Expiry Child", "date_of_birth": "2022-05-01", "token": first}
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", input, nil, ""), 401)
	if _, err := f.pool.Exec(context.Background(), `UPDATE app_private.family_access_grant SET expires_at=now()-interval '1 second' WHERE registration_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	input["token"] = second
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", input, nil, ""), 401)
	third := issueAccess(t, f, id)
	input["token"] = secret
	w := portalRequest(f.router, "POST", "/api/family/auth/access", input, nil, "")
	expectCode(t, w, 200)
	g := decodeResponse[Guardian](t, w)
	if _, err := f.pool.Exec(context.Background(), `UPDATE app_private.guardian_account SET active=false WHERE id=$1`, g.ID); err != nil {
		t.Fatal(err)
	}
	input["token"] = third
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", input, nil, ""), 401)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/registrations/"+id+"/family-access", map[string]bool{"guardian_verified": true}, f.adminCookie, ""), 404)
}

func TestFamilyPasswordEndpointsRetired(t *testing.T) {
	f := newPaidFixture(t)
	g, err := f.server.ProvisionGuardian(context.Background(), "Legacy Guardian", "legacy-"+token()[:12]+"@example.invalid", "test-password-only-123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM app_private.guardian_account WHERE id=$1`, g.ID)
	})
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/login", map[string]string{"email": g.Email, "password": "test-password-only-123"}, nil, ""), 410)
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/register", map[string]string{"name": "New Guardian", "email": "unused-" + token()[:12] + "@example.invalid", "password": "test-password-only-123"}, nil, ""), 410)
	var sessions int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM app_private.guardian_session WHERE guardian_id=$1`, g.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatal("retired password login created a session")
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/auth/me", nil, f.adminCookie, ""), 200)
}
