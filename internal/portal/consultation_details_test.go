package portal

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func childCookie(t *testing.T, f *paidFixture, name, secret string) *http.Cookie {
	t.Helper()
	w := portalRequest(f.router, "POST", "/api/family/auth/access", map[string]string{"guardian_name": "Test Guardian", "child_name": name, "date_of_birth": "2022-05-01", "token": secret}, nil, "")
	expectCode(t, w, 200)
	return w.Result().Cookies()[0]
}
func TestGuardianIdentityAndDetailsStayPrivate(t *testing.T) {
	f := newPaidFixture(t)
	secret := token()
	in := Intake{GuardianName: "Test Guardian", ChildName: "Details Child", DateOfBirth: "2022-05-01", Phone: "9876543210", Token: secret, GuardianDescription: "Guardian note\nবাংলা যুক্তাক্ষর: ক্ষ\n<script>not executable</script>"}
	w := portalRequest(f.router, "POST", "/api/public/registrations", in, nil, "")
	expectCode(t, w, 201)
	id := decodeResponse[map[string]string](t, w)["id"]
	f.registrations = append(f.registrations, id)
	expectCode(t, portalRequest(f.router, "POST", "/api/public/registrations", in, nil, ""), 201)
	in.GuardianDescription += " changed"
	expectCode(t, portalRequest(f.router, "POST", "/api/public/registrations", in, nil, ""), 409)
	in.Token = token()
	in.GuardianDescription = strings.Repeat("x", 4001)
	expectCode(t, portalRequest(f.router, "POST", "/api/public/registrations", in, nil, ""), 400)
	credentials := map[string]string{"guardian_name": "wrong guardian", "child_name": "Details Child", "date_of_birth": "2022-05-01", "token": secret}
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/access", credentials, nil, ""), 401)
	credentials["guardian_name"] = "  TEST   guardian  "
	w = portalRequest(f.router, "POST", "/api/family/auth/access", credentials, nil, "")
	expectCode(t, w, 200)
	credentials["token"] = token()
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/access", credentials, nil, ""), 401)
	delete(credentials, "token")
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/access", credentials, nil, ""), 401)
	credentials["token"] = secret
	delete(credentials, "guardian_name")
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/access", credentials, nil, ""), 401)
	pub := portalRequest(f.router, "GET", "/api/public/registrations/"+id, nil, nil, secret)
	expectCode(t, pub, 200)
	if strings.Contains(pub.Body.String(), "Guardian note") {
		t.Fatal("description leaked publicly")
	}
	staff := portalRequest(f.router, "GET", "/api/registrations/"+id, nil, f.adminCookie, "")
	expectCode(t, staff, 200)
	if !strings.Contains(decodeResponse[Registration](t, staff).GuardianDescription, "বাংলা") {
		t.Fatal("description lost")
	}
	grantResponse := portalRequest(f.router, "POST", "/api/admin/registrations/"+id+"/family-access", map[string]bool{"guardian_verified": true}, f.adminCookie, "")
	expectCode(t, grantResponse, 201)
	var grant struct {
		Token string `json:"token"`
	}
	grant = decodeResponse[struct {
		Token string `json:"token"`
	}](t, grantResponse)
	credentials["token"] = grant.Token
	credentials["guardian_name"] = "other"
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", credentials, nil, ""), 401)
	credentials["guardian_name"] = "test guardian"
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", credentials, nil, ""), 200)
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", credentials, nil, ""), 401)
}
func TestMeetingAuthorizationAndPendingFamilyVisibility(t *testing.T) {
	f, doctor := walletFixture(t)
	id, secret := f.register(t, "Meeting Child")
	otherID, otherSecret := f.register(t, "Sibling Child")
	appointment := f.book(t, id, secret, f.doctors[0].ID, f.start)
	family := childCookie(t, f, "Meeting Child", secret)
	sibling := childCookie(t, f, "Sibling Child", otherSecret)
	w := portalRequest(f.router, "POST", "/api/auth/login", map[string]string{"email": f.doctors[1].Email, "password": "test-password-only-123"}, nil, "")
	expectCode(t, w, 200)
	unrelated := w.Result().Cookies()[0]
	path := "/api/appointments/" + appointment + "/meeting"
	body := map[string]string{"meeting_url": "https://meet.example.invalid/test-call"}
	expectCode(t, portalRequest(f.router, "PUT", path, body, nil, ""), 401)
	expectCode(t, portalRequest(f.router, "PUT", path, body, family, ""), 401)
	expectCode(t, portalRequest(f.router, "PUT", path, body, unrelated, ""), 404)
	for _, url := range []string{"http://example.invalid", "javascript:alert(1)", "https://user:secret@example.invalid", "https://example.invalid/\n"} {
		expectCode(t, portalRequest(f.router, "PUT", path, map[string]string{"meeting_url": url + "bad"}, doctor, ""), 400)
	}
	expectCode(t, portalRequest(f.router, "PUT", path, body, doctor, ""), 200)
	w = portalRequest(f.router, "GET", "/api/family/registrations/"+id, nil, family, "")
	expectCode(t, w, 200)
	value := decodeResponse[FamilyRegistration](t, w)
	if value.MeetingURL != body["meeting_url"] || *value.AppointmentStatus != "awaiting_payment" {
		t.Fatalf("pending link hidden: %+v", value)
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/family/registrations/"+id, nil, sibling, ""), 404)
	expectCode(t, portalRequest(f.router, "GET", "/api/family/registrations/"+otherID, nil, family, ""), 404)
	w = portalRequest(f.router, "GET", "/api/appointments", nil, unrelated, "")
	expectCode(t, w, 200)
	if strings.Contains(w.Body.String(), "test-call") {
		t.Fatal("link leaked to other doctor")
	}
	body["meeting_url"] = "https://zoom.example.invalid/changed"
	expectCode(t, portalRequest(f.router, "PUT", path, body, f.adminCookie, ""), 200)
	expectCode(t, portalRequest(f.router, "POST", "/api/public/registrations/"+id+"/appointment/"+appointment+"/cancel", map[string]any{}, nil, secret), 200)
	w = portalRequest(f.router, "GET", "/api/family/registrations/"+id, nil, family, "")
	expectCode(t, w, 200)
	if decodeResponse[FamilyRegistration](t, w).MeetingURL != "" {
		t.Fatal("cancelled meeting remains visible")
	}
	expectCode(t, portalRequest(f.router, "PUT", path, body, doctor, ""), 409)
}
func TestWalletRefundAfterPaidCreatesNegativeBalance(t *testing.T) {
	f, doctor := walletFixture(t)
	_, payment := walletCredit(t, f, 0)
	saveTestDestination(t, f, doctor)
	w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 200000, "request_key": testUUID()}, doctor, "")
	expectCode(t, w, 201)
	value := decodeResponse[Withdrawal](t, w)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+value.ID+"/decision", map[string]any{"action": "paid", "transferred_paise": 164000, "transfer_reference": "LOCAL-REFUND-TEST", "transferred_at": time.Now().UTC().Format(time.RFC3339Nano), "transfer_completed": true}, f.adminCookie, ""), 200)
	before := walletLedgerSnapshot(t, f)
	payment.AmountRefunded = 1
	if err := f.server.applyPayment(context.Background(), payment, "partial-after-paid-"+payment.ID); err != nil {
		t.Fatal(err)
	}
	walletBalance(t, f, 300000, 0)
	if walletLedgerSnapshot(t, f) != before {
		t.Fatal("partial refund changed paid history")
	}
	balance, err := loadWallet(context.Background(), f.pool, f.doctors[0].ID)
	if err != nil || !balance.ReconciliationRequired {
		t.Fatalf("partial post-payout hold missing: %v", err)
	}
	expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 100, "request_key": testUUID()}, doctor, ""), 409)
	payment.AmountRefunded = payment.Amount
	if err := f.server.applyPayment(context.Background(), payment, "refund-after-paid-"+payment.ID); err != nil {
		t.Fatal(err)
	}
	walletBalance(t, f, -200000, 0)
	expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 100, "request_key": testUUID()}, doctor, ""), 409)
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM app_private.doctor_ledger WHERE doctor_id=$1`, f.doctors[0].ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("history count=%d err=%v", count, err)
	}
}

func TestMeetingChangeKeepsAvailabilityEditingWorking(t *testing.T) {
	f := newPaidFixture(t)
	var id string
	if err := f.pool.QueryRow(context.Background(), `SELECT id FROM app_private.doctor_availability WHERE doctor_id=$1`, f.doctors[0].ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"id": id, "doctor_id": f.doctors[0].ID, "starts_at": f.start.Format(time.RFC3339), "ends_at": f.start.Add(4 * time.Hour).Format(time.RFC3339), "active": true}
	expectCode(t, portalRequest(f.router, "PUT", "/api/availability/"+id, body, f.adminCookie, ""), 200)
}
