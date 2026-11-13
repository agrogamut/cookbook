package portal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func applyTestRefund(t *testing.T, f *paidFixture, p Payment, cumulative int) (Payment, []RefundCase) {
	t.Helper()
	p.AmountRefunded = cumulative
	f.gateway.set(p)
	if err := f.server.applyPayment(context.Background(), p, "refund-"+token()); err != nil {
		t.Fatal(err)
	}
	reviews := decodeResponse[[]RefundReview](t, portalRequest(f.router, "GET", "/api/admin/refund-reviews", nil, f.adminCookie, ""))
	for _, v := range reviews {
		if v.OrderID == p.OrderID {
			return p, v.Cases
		}
	}
	t.Fatal("missing refund review")
	return p, nil
}
func refundBody(amount, remaining int64, reason string) map[string]any {
	return map[string]any{"adjustment_paise": amount, "expected_remaining_paise": remaining, "explanation": reason, "confirmed": true}
}
func refundDecision(f *paidFixture, id string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	return portalRequest(f.router, "POST", "/api/admin/refund-reviews/"+id+"/decision", body, cookie, "")
}
func requireHold(t *testing.T, f *paidFixture, want bool) {
	t.Helper()
	v, err := loadWallet(context.Background(), f.pool, f.doctors[0].ID)
	if err != nil || v.ReconciliationRequired != want {
		t.Fatalf("hold=%+v, want %v: %v", v, want, err)
	}
}
func TestRefundDecisionZeroAuthorizationAndReplay(t *testing.T) {
	f, doctor := walletFixture(t)
	_, p := walletCredit(t, f, 0)
	before := walletLedgerSnapshot(t, f)
	_, cases := applyTestRefund(t, f, p, 1)
	if len(cases) != 1 {
		t.Fatal(cases)
	}
	id := cases[0].ID
	body := refundBody(0, 500000, "Retain the full earning for this one-paise refund.")
	_, secret := f.register(t, "Refund privacy family")
	family := childCookie(t, f, "Refund privacy family", secret)
	for _, v := range []struct {
		cookie *http.Cookie
		code   int
	}{{nil, 401}, {doctor, 403}, {family, 401}} {
		expectCode(t, portalRequest(f.router, "GET", "/api/admin/refund-reviews", nil, v.cookie, ""), v.code)
		expectCode(t, refundDecision(f, id, body, v.cookie), v.code)
		expectCode(t, portalRequest(f.router, "POST", "/api/admin/refund-reviews/"+p.OrderID+"/sync", map[string]any{}, v.cookie, ""), v.code)
	}
	for _, invalid := range []map[string]any{
		{"expected_remaining_paise": 500000, "explanation": "Missing explicit amount", "confirmed": true},
		refundBody(-1, 500000, "Negative"),
		refundBody(0, 500000, " "),
		{"adjustment_paise": 0, "expected_remaining_paise": 500000, "explanation": "No confirmation", "confirmed": false},
	} {
		expectCode(t, refundDecision(f, id, invalid, f.adminCookie), 400)
	}
	expectCode(t, refundDecision(f, id, refundBody(500001, 500000, "Too much"), f.adminCookie), 409)
	requireHold(t, f, true)
	expectCode(t, refundDecision(f, id, body, f.adminCookie), 200)
	requireHold(t, f, false)
	if walletLedgerSnapshot(t, f) != before {
		t.Fatal("zero decision rewrote earning history")
	}
	var original string
	query := `SELECT to_jsonb(d)::text FROM app_private.refund_decision d WHERE refund_id=$1`
	if err := f.pool.QueryRow(context.Background(), query, id).Scan(&original); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		expectCode(t, refundDecision(f, id, body, f.adminCookie), 200)
	}
	expectCode(t, refundDecision(f, id, refundBody(1, 500000, "Changed"), f.adminCookie), 409)
	var replay string
	if err := f.pool.QueryRow(context.Background(), query, id).Scan(&replay); err != nil {
		t.Fatal(err)
	}
	if original != replay {
		t.Fatal("replay changed decision actor or timestamp")
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE app_private.refund_decision SET explanation='rewritten' WHERE refund_id=$1`, id); err == nil {
		t.Fatal("immutable decision updated")
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE app_private.payment_refund SET amount_paise=2 WHERE id=$1`, id); err == nil {
		t.Fatal("immutable refund updated")
	}
}
func TestRefundConcurrentAdminsSequentialCasesAndFullRemainder(t *testing.T) {
	f, _ := walletFixture(t)
	_, p := walletCredit(t, f, 0)
	p, first := applyTestRefund(t, f, p, 1)
	p, cases := applyTestRefund(t, f, p, 2)
	if len(cases) != 2 {
		t.Fatal(cases)
	}
	second := cases[0].ID
	if second == first[0].ID {
		second = cases[1].ID
	}
	actor, err := f.server.Provision(context.Background(), "Second test admin", "refund-admin-"+token()[:12]+"@example.invalid", "test-password-only-123", "admin")
	if err != nil {
		t.Fatal(err)
	}
	f.doctors = append(f.doctors, actor)
	login := portalRequest(f.router, "POST", "/api/auth/login", map[string]string{"email": actor.Email, "password": "test-password-only-123"}, nil, "")
	expectCode(t, login, 200)
	cookies := []*http.Cookie{f.adminCookie, login.Result().Cookies()[0]}
	responses := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			responses <- refundDecision(f, first[0].ID, refundBody(100000, 500000, "Decision by "+cookies[i].Value[:5]), cookies[i])
		})
	}
	wg.Wait()
	close(responses)
	successes, conflicts := 0, 0
	for r := range responses {
		if r.Code == 200 {
			successes++
		} else if r.Code == 409 {
			conflicts++
		} else {
			t.Fatalf("decision %d %s", r.Code, r.Body)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	walletBalance(t, f, 400000, 0)
	requireHold(t, f, true)
	expectCode(t, refundDecision(f, second, refundBody(50000, 500000, "Stale screen"), f.adminCookie), 409)
	expectCode(t, refundDecision(f, second, refundBody(50000, 400000, "Second refund adjustment"), f.adminCookie), 200)
	walletBalance(t, f, 350000, 0)
	requireHold(t, f, false)
	before := walletLedgerSnapshot(t, f)
	if err = f.server.applyPayment(context.Background(), p, "replay-new-event-"+token()); err != nil {
		t.Fatal(err)
	}
	older := p
	older.AmountRefunded = 1
	if err = f.server.applyPayment(context.Background(), older, "stale-"+token()); err != nil {
		t.Fatal(err)
	}
	if before != walletLedgerSnapshot(t, f) {
		t.Fatal("callback changed decided ledger")
	}
	var count, total int64
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*),sum(amount_paise) FROM app_private.payment_refund WHERE order_id=$1`, p.OrderID).Scan(&count, &total); err != nil || count != 2 || total != 2 {
		t.Fatalf("identities=%d total=%d %v", count, total, err)
	}
	p, _ = applyTestRefund(t, f, p, p.Amount)
	walletBalance(t, f, 0, 0)
	if err = f.server.applyPayment(context.Background(), p, "full-replay-"+token()); err != nil {
		t.Fatal(err)
	}
	var reversal int64
	if err = f.pool.QueryRow(context.Background(), `SELECT amount_paise FROM app_private.doctor_ledger WHERE order_id=$1 AND kind='reversal'`, p.OrderID).Scan(&reversal); err != nil || reversal != -350000 {
		t.Fatalf("remainder=%d %v", reversal, err)
	}
	walletBalance(t, f, 0, 0)
}
func TestRefundReservationHoldAndPastPayout(t *testing.T) {
	for _, paidAlready := range []bool{false, true} {
		t.Run(map[bool]string{false: "reservation", true: "paid"}[paidAlready], func(t *testing.T) {
			f, doctor := walletFixture(t)
			_, p := walletCredit(t, f, 0)
			saveTestDestination(t, f, doctor)
			w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 400000, "request_key": testUUID()}, doctor, "")
			expectCode(t, w, 201)
			withdrawal := decodeResponse[Withdrawal](t, w)
			paid := map[string]any{"action": "paid", "transferred_paise": withdrawal.Net, "transfer_reference": "SYNTHETIC-TRANSFER", "transferred_at": time.Now().UTC().Format(time.RFC3339Nano), "transfer_completed": true}
			path := "/api/admin/withdrawals/" + withdrawal.ID + "/decision"
			if paidAlready {
				expectCode(t, portalRequest(f.router, "POST", path, paid, f.adminCookie, ""), 200)
			}
			_, cases := applyTestRefund(t, f, p, 1)
			expectCode(t, refundDecision(f, cases[0].ID, refundBody(200000, 500000, "Explicit partial refund deduction"), f.adminCookie), 200)
			if paidAlready {
				walletBalance(t, f, -100000, 0)
			} else {
				walletBalance(t, f, 300000, 400000)
			}
			expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 100, "request_key": testUUID()}, doctor, ""), 409)
			if !paidAlready {
				expectCode(t, portalRequest(f.router, "POST", path, paid, f.adminCookie, ""), 409)
				expectCode(t, portalRequest(f.router, "POST", path, map[string]string{"action": "rejected", "note": "Release reservation exceeding adjusted balance"}, f.adminCookie, ""), 200)
				walletBalance(t, f, 300000, 0)
				expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 100, "request_key": testUUID()}, doctor, ""), 201)
			}
		})
	}
}
func TestRefundResolutionPreservesOtherHolds(t *testing.T) {
	f, _ := walletFixture(t)
	_, p1 := walletCredit(t, f, 0)
	a2, p2 := walletCredit(t, f, time.Hour)
	p1, c1 := applyTestRefund(t, f, p1, 1)
	p2, c2 := applyTestRefund(t, f, p2, 1)
	f.gateway.set(p1)
	expectCode(t, refundDecision(f, c1[0].ID, refundBody(0, 500000, "Keep first earning"), f.adminCookie), 200)
	requireHold(t, f, true)
	f.gateway.set(p2)
	if _, err := f.pool.Exec(context.Background(), `UPDATE app_private.wallet_reconciliation SET reason='dispute' WHERE appointment_id=$1`, a2); err != nil {
		t.Fatal(err)
	}
	expectCode(t, refundDecision(f, c2[0].ID, refundBody(0, 500000, "Keep second earning; dispute still open"), f.adminCookie), 200)
	requireHold(t, f, true)
	var reason string
	if err := f.pool.QueryRow(context.Background(), `SELECT reason FROM app_private.wallet_reconciliation WHERE appointment_id=$1`, a2).Scan(&reason); err != nil || reason != "dispute" {
		t.Fatalf("other hold lost: %s %v", reason, err)
	}
	other, err := loadWallet(context.Background(), f.pool, f.doctors[1].ID)
	if err != nil || other.ReconciliationRequired {
		t.Fatalf("unrelated wallet held: %+v %v", other, err)
	}
}
func TestRefundIdentityFailurePersistsHoldAndAllowsRetry(t *testing.T) {
	f, _ := walletFixture(t)
	_, p := walletCredit(t, f, 0)
	p.AmountRefunded = 1
	f.gateway.set(p)
	f.gateway.refundError = errors.New("synthetic unavailable")
	event := "refund-error-" + token()
	if err := f.server.applyPayment(context.Background(), p, event); err == nil {
		t.Fatal("unavailable refund identities accepted")
	}
	requireHold(t, f, true)
	walletBalance(t, f, 500000, 0)
	var consumed bool
	if err := f.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM app_private.payment_event WHERE id=$1)`, event).Scan(&consumed); err != nil || consumed {
		t.Fatalf("event consumed before identity retry: %v", err)
	}
	f.gateway.refundError = nil
	if err := f.server.applyPayment(context.Background(), p, event); err != nil {
		t.Fatal(err)
	}
	reviews := decodeResponse[[]RefundReview](t, portalRequest(f.router, "GET", "/api/admin/refund-reviews", nil, f.adminCookie, ""))
	var id string
	for _, r := range reviews {
		if r.OrderID == p.OrderID {
			id = r.Cases[0].ID
		}
	}
	f.gateway.refundOverride = []PaymentRefund{{ID: "rfnd_" + token()[:16], PaymentID: p.ID, Amount: 1, Currency: p.Currency, Status: "processed", CreatedAt: time.Now().Unix()}}
	if err := f.server.applyPayment(context.Background(), p, "bad-union-"+token()); err == nil {
		t.Fatal("combined refund identities exceeded cumulative amount")
	}
	requireHold(t, f, true)
	f.gateway.refundOverride = nil
	expectCode(t, refundDecision(f, id, refundBody(0, 500000, "Verified after retry"), f.adminCookie), 200)
	requireHold(t, f, false)
	f.gateway.refundError = errors.New("synthetic unavailable")
	expectCode(t, refundDecision(f, id, refundBody(0, 500000, "Verified after retry"), f.adminCookie), 500)
	requireHold(t, f, true)
}
func TestActiveTaxRolloutOverrideRoundingAndOldOrder(t *testing.T) {
	for _, v := range []struct{ base, tax int }{{1, 0}, {2, 0}, {3, 1}, {25, 5}, {12345, 2222}, {200000, 36000}} {
		active := (TaxPolicy{}).price(v.base)
		if active.TaxPaise != v.tax || active.TotalPaise != v.base+v.tax || active.Policy != "owner-18-v1" {
			t.Fatalf("%+v", active)
		}
		off := (TaxPolicy{PatientPolicy: "inactive"}).price(v.base)
		if off.TaxPaise != 0 || off.TotalPaise != v.base || off.Policy != "inactive" {
			t.Fatalf("%+v", off)
		}
		deduction, _, policy := (TaxPolicy{}).withdrawal(int64(v.base))
		if deduction != int64(v.tax) || policy != "owner-18-v1" {
			t.Fatal(deduction, policy)
		}
	}
	f, _ := walletFixture(t)
	id, secret := f.register(t, "Legacy tax-off order")
	f.book(t, id, secret, f.doctors[0].ID, f.start)
	old := f.order(t, id, secret)
	if old.Amount != 25000 {
		t.Fatal(old)
	}
	f.server.options.TaxPolicy.PatientPolicy = ""
	if again := f.order(t, id, secret); again != old {
		t.Fatalf("old order repriced: %+v %+v", old, again)
	}
	id2, secret2 := f.register(t, "New active order")
	f.book(t, id2, secret2, f.doctors[0].ID, f.start.Add(time.Hour))
	active := f.order(t, id2, secret2)
	if active.Amount != 29500 {
		t.Fatal(active)
	}
	response := portalRequest(f.router, "POST", "/api/public/registrations/"+id2+"/order/itemized-v1", map[string]any{"amount_paise": 1}, nil, secret2)
	if response.Code == 200 && strings.Contains(response.Body.String(), `"amount_paise":1,`) {
		t.Fatal("client chose amount")
	}
}

func TestRefundIncompleteCoalescedListKeepsHoldAndRetries(t *testing.T) {
	f, _ := walletFixture(t)
	_, p := walletCredit(t, f, 0)
	p.AmountRefunded = 30
	f.gateway.set(p)
	first := PaymentRefund{ID: "rfnd_" + token()[:16], PaymentID: p.ID, Amount: 10, Currency: "INR", Status: "processed", CreatedAt: time.Now().Unix()}
	second := first
	second.ID = "rfnd_" + token()[:16]
	second.Amount = 20
	event := "coalesced-" + token()
	f.gateway.refundOverride = []PaymentRefund{first}
	if err := f.server.applyPayment(context.Background(), p, event); !errors.Is(err, errRefundHistoryIncomplete) {
		t.Fatalf("incomplete list: %v", err)
	}
	requireHold(t, f, true)
	expectCode(t, refundDecision(f, first.ID, refundBody(0, 500000, "Not complete yet"), f.adminCookie), 500)
	var consumed bool
	if err := f.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM app_private.payment_event WHERE id=$1)`, event).Scan(&consumed); err != nil || consumed {
		t.Fatalf("incomplete event consumed: %v", err)
	}
	f.gateway.refundOverride = []PaymentRefund{second, first}
	if err := f.server.applyPayment(context.Background(), p, event); err != nil {
		t.Fatal(err)
	}
	expectCode(t, refundDecision(f, second.ID, refundBody(5000, 500000, "Second refund received first"), f.adminCookie), 200)
	walletBalance(t, f, 495000, 0)
	requireHold(t, f, true)
	expectCode(t, refundDecision(f, first.ID, refundBody(0, 495000, "First refund retains earning"), f.adminCookie), 200)
	requireHold(t, f, false)
	for i := 0; i < 2; i++ {
		if err := f.server.applyPayment(context.Background(), p, "replayed-"+token()); err != nil {
			t.Fatal(err)
		}
	}
	walletBalance(t, f, 495000, 0)
	requireHold(t, f, false)
}

func TestFamilyPriceKeepsStoredTaxPolicyAfterRollout(t *testing.T) {
	f, _ := walletFixture(t)
	id, secret := f.register(t, "Stored policy family")
	f.book(t, id, secret, f.doctors[0].ID, f.start)
	f.order(t, id, secret)
	cookie := childCookie(t, f, "Stored policy family", secret)
	f.server.options.TaxPolicy.PatientPolicy = "owner-18-v1"
	w := portalRequest(f.router, "GET", "/api/family/registrations/"+id, nil, cookie, "")
	expectCode(t, w, 200)
	value := decodeResponse[FamilyRegistration](t, w)
	if value.Price.Policy != "inactive" || value.Price.TotalPaise != 25000 || value.Price.TaxPaise != 0 {
		t.Fatalf("family snapshot changed: %+v", value.Price)
	}
	settings, err := f.server.settings(context.Background())
	if err != nil || settings.Price.Policy != "owner-18-v1" || settings.Price.TotalPaise != 29500 {
		t.Fatalf("new quote wrong: %+v %v", settings, err)
	}
}
