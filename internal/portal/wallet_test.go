package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func walletFixture(t *testing.T) (*paidFixture, *http.Cookie) {
	t.Helper()
	f := newPaidFixture(t)
	f.server.options.WalletKey = bytes.Repeat([]byte{42}, 32)
	f.server.options.TaxPolicy.WithdrawalApproval = "synthetic-adviser-policy-v1"
	expectCode(t, portalRequest(f.router, "PUT", "/api/admin/earnings", map[string]int{"doctor_earning_paise": 500000}, f.adminCookie, ""), 200)
	w := portalRequest(f.router, "POST", "/api/auth/login", map[string]string{"email": f.doctors[0].Email, "password": "test-password-only-123"}, nil, "")
	expectCode(t, w, 200)
	return f, w.Result().Cookies()[0]
}
func walletCredit(t *testing.T, f *paidFixture, offset time.Duration) (string, Payment) {
	t.Helper()
	id, secret := f.register(t, "Wallet test child")
	appointment := f.book(t, id, secret, f.doctors[0].ID, f.start.Add(offset))
	order := f.order(t, id, secret)
	p := Payment{ID: "pay_" + token()[:16], OrderID: order.ID, Amount: order.Amount, Currency: order.Currency, Status: "captured", Captured: true}
	if err := f.server.applyPayment(context.Background(), p, ""); err != nil {
		t.Fatal(err)
	}
	return appointment, p
}
func saveTestDestination(t *testing.T, f *paidFixture, cookie *http.Cookie) {
	t.Helper()
	expectCode(t, portalRequest(f.router, "PUT", "/api/doctor/wallet/destination", Destination{Kind: "bank", Holder: "Synthetic Doctor", Bank: "Example Bank", Account: "001234567890", ConfirmAccount: "001234567890", IFSC: "TEST0123456"}, cookie, ""), 200)
}
func walletBalance(t *testing.T, f *paidFixture, balance, reserved int64) {
	t.Helper()
	v, err := loadWallet(context.Background(), f.pool, f.doctors[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Balance != balance || v.Reserved != reserved || v.Available != balance-reserved {
		t.Fatalf("balance=%+v expected %d reserved %d", v, balance, reserved)
	}
}
func TestWalletCaptureIdentityCutoverAndRefund(t *testing.T) {
	f, _ := walletFixture(t)
	ctx := context.Background()
	id, secret := f.register(t, "Capture child")
	appt := f.book(t, id, secret, f.doctors[0].ID, f.start)
	order := f.order(t, id, secret)
	p := Payment{ID: "pay_" + token()[:16], OrderID: order.ID, Amount: order.Amount, Currency: "INR", Status: "authorized"}
	if err := f.server.applyPayment(ctx, p, "authorized-"+id); err != nil {
		t.Fatal(err)
	}
	walletBalance(t, f, 0, 0)
	expectCode(t, portalRequest(f.router, "PUT", "/api/admin/earnings", map[string]int{"doctor_earning_paise": 900000}, f.adminCookie, ""), 200)
	p.Status = "captured"
	p.Captured = true
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Go(func() { errors <- f.server.applyPayment(ctx, p, "capture-"+token()) })
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	walletBalance(t, f, 500000, 0)
	wrong := p
	wrong.Amount++
	if err := f.server.applyPayment(ctx, wrong, "mismatch-"+id); err == nil {
		t.Fatal("wrong amount accepted")
	}
	wrong = p
	wrong.Currency = "USD"
	if err := f.server.applyPayment(ctx, wrong, "currency-"+id); err == nil {
		t.Fatal("wrong currency accepted")
	}
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+appt+"/decision", map[string]string{"action": "confirm", "doctor_id": f.doctors[1].ID}, f.adminCookie, ""), 409)
	p.AmountRefunded = 1
	if err := f.server.applyPayment(ctx, p, "partial-"+id); err != nil {
		t.Fatal(err)
	}
	walletBalance(t, f, 500000, 0)
	balance, _ := loadWallet(ctx, f.pool, f.doctors[0].ID)
	if !balance.ReconciliationRequired {
		t.Fatal("partial refund not held")
	}
	p.AmountRefunded = p.Amount
	if err := f.server.applyPayment(ctx, p, "full-"+id); err != nil {
		t.Fatal(err)
	}
	if err := f.server.applyPayment(ctx, p, "full-other-"+id); err != nil {
		t.Fatal(err)
	}
	walletBalance(t, f, 0, 0)
	balance, _ = loadWallet(ctx, f.pool, f.doctors[0].ID)
	if balance.ReconciliationRequired {
		t.Fatal("completed refund left partial hold")
	}
	historical, credential := f.register(t, "Historical appointment")
	old := f.book(t, historical, credential, f.doctors[0].ID, f.start.Add(time.Hour))
	if _, err := f.pool.Exec(ctx, `UPDATE app_private.appointment SET earning_doctor_id=NULL,earning_paise=NULL,earning_policy=NULL WHERE id=$1`, old); err != nil {
		t.Fatal(err)
	}
	oldOrder := f.order(t, historical, credential)
	p = Payment{ID: "pay_" + token()[:16], OrderID: oldOrder.ID, Amount: oldOrder.Amount, Currency: "INR", Status: "captured", Captured: true}
	if err := f.server.applyPayment(ctx, p, "historical-"+historical); err != nil {
		t.Fatal(err)
	}
	walletBalance(t, f, 0, 0)
}
func TestWalletWithdrawalRacePrivacyAndManualPaid(t *testing.T) {
	f, doctor := walletFixture(t)
	walletCredit(t, f, 0)
	saveTestDestination(t, f, doctor)
	var encrypted []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT encrypted_details FROM app_private.doctor_destination WHERE doctor_id=$1`, f.doctors[0].ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("001234567890")) {
		t.Fatal("plaintext account stored")
	}
	if _, err := f.server.decryptDestination(f.doctors[1].ID, encrypted); err == nil {
		t.Fatal("cross-doctor cipher accepted")
	}
	damaged := append([]byte(nil), encrypted...)
	damaged[len(damaged)-1] ^= 1
	if _, err := f.server.decryptDestination(f.doctors[0].ID, damaged); err == nil {
		t.Fatal("tampered cipher accepted")
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/doctor/wallet", nil, nil, ""), 401)
	expectCode(t, portalRequest(f.router, "GET", "/api/doctor/wallet", nil, f.adminCookie, ""), 403)
	expectCode(t, portalRequest(f.router, "GET", "/api/admin/withdrawals", nil, doctor, ""), 403)
	type result struct {
		code int
		body []byte
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 300000, "request_key": testUUID()}, doctor, "")
			results <- result{w.Code, w.Body.Bytes()}
		})
	}
	wg.Wait()
	close(results)
	var withdrawal Withdrawal
	success, conflict := 0, 0
	for result := range results {
		if result.code == 201 {
			success++
			if err := json.Unmarshal(result.body, &withdrawal); err != nil {
				t.Fatal(err)
			}
		} else if result.code == 409 {
			conflict++
		} else {
			t.Fatalf("unexpected %d %s", result.code, result.body)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	walletBalance(t, f, 500000, 300000)
	list := portalRequest(f.router, "GET", "/api/doctor/wallet", nil, doctor, "")
	expectCode(t, list, 200)
	if strings.Contains(list.Body.String(), "001234567890") {
		t.Fatal("unmasked account exposed")
	}
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+withdrawal.ID+"/destination", map[string]bool{"manual_transfer_review": true}, doctor, ""), 403)
	reveal := portalRequest(f.router, "POST", "/api/admin/withdrawals/"+withdrawal.ID+"/destination", map[string]bool{"manual_transfer_review": true}, f.adminCookie, "")
	expectCode(t, reveal, 200)
	if decodeResponse[Destination](t, reveal).Account != "001234567890" {
		t.Fatal("leading zero lost")
	}
	other := portalRequest(f.router, "POST", "/api/auth/login", map[string]string{"email": f.doctors[1].Email, "password": "test-password-only-123"}, nil, "")
	expectCode(t, other, 200)
	otherCookie := other.Result().Cookies()[0]
	expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals/"+withdrawal.ID+"/cancel", map[string]any{}, otherCookie, ""), 404)
	otherWallet := portalRequest(f.router, "GET", "/api/doctor/wallet", nil, otherCookie, "")
	expectCode(t, otherWallet, 200)
	if strings.Contains(otherWallet.Body.String(), withdrawal.ID) || strings.Contains(otherWallet.Body.String(), "7890") {
		t.Fatal("another doctor's withdrawal leaked")
	}
	_, familySecret := f.register(t, "Wallet privacy family")
	family := childCookie(t, f, "Wallet privacy family", familySecret)
	expectCode(t, portalRequest(f.router, "GET", "/api/doctor/wallet", nil, family, ""), 401)
	expectCode(t, portalRequest(f.router, "GET", "/api/admin/withdrawals", nil, family, ""), 401)
	paid := map[string]any{"action": "paid", "transferred_paise": withdrawal.Net, "transfer_reference": "LOCAL-TRANSFER-ONLY", "transferred_at": time.Now().UTC().Format(time.RFC3339Nano), "transfer_completed": false}
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+withdrawal.ID+"/decision", paid, f.adminCookie, ""), 400)
	paid["transfer_completed"] = true
	for i := 0; i < 2; i++ {
		expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+withdrawal.ID+"/decision", paid, f.adminCookie, ""), 200)
	}
	walletBalance(t, f, 200000, 0)
	paid["transfer_reference"] = "different"
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+withdrawal.ID+"/decision", paid, f.adminCookie, ""), 409)
	expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals/"+withdrawal.ID+"/cancel", map[string]any{}, doctor, ""), 409)
}
func TestWalletWithdrawalCalculationReplayRejectionAndDispute(t *testing.T) {
	f, doctor := walletFixture(t)
	f.server.options.TaxPolicy.WithdrawalApproval = "synthetic-adviser-policy-v1"
	_, p := walletCredit(t, f, 0)
	saveTestDestination(t, f, doctor)
	body := map[string]any{"amount_paise": 200000, "request_key": testUUID()}
	w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", body, doctor, "")
	expectCode(t, w, 201)
	withdrawal := decodeResponse[Withdrawal](t, w)
	if withdrawal.Requested != 200000 || withdrawal.Deduction != 36000 || withdrawal.Net != 164000 {
		t.Fatalf("wrong withdrawal calculation %+v", withdrawal)
	}
	walletBalance(t, f, 500000, 200000)
	w = portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", body, doctor, "")
	expectCode(t, w, 200)
	if decodeResponse[Withdrawal](t, w).ID != withdrawal.ID {
		t.Fatal("idempotency lost")
	}
	body["amount_paise"] = 100000
	expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", body, doctor, ""), 409)
	for i := 0; i < 2; i++ {
		expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+withdrawal.ID+"/decision", map[string]string{"action": "rejected", "note": "Synthetic destination review"}, f.adminCookie, ""), 200)
	}
	walletBalance(t, f, 500000, 0)
	f.gateway.set(p)
	payload, _ := json.Marshal(map[string]any{"event": "payment.dispute.created", "payload": map[string]any{"dispute": map[string]any{"entity": map[string]string{"payment_id": p.ID}}}})
	webhook := func(signature string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/payments/webhook", bytes.NewReader(payload))
		req.Header.Set("X-Razorpay-Signature", signature)
		req.Header.Set("X-Razorpay-Event-Id", "dispute-"+p.ID)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, req)
		return w
	}
	expectCode(t, webhook("invalid"), 400)
	for i := 0; i < 2; i++ {
		expectCode(t, webhook(sign(f.gateway.WebhookSecret, payload)), 200)
	}
	walletBalance(t, f, 0, 0)
	balance, _ := loadWallet(context.Background(), f.pool, f.doctors[0].ID)
	if !balance.ReconciliationRequired {
		t.Fatal("dispute not held")
	}
	expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 100, "request_key": testUUID()}, doctor, ""), 409)
	if err := f.server.applyPayment(context.Background(), p, "later-capture-"+p.ID); err != nil {
		t.Fatal(err)
	}
	walletBalance(t, f, 0, 0)
}
func TestTaxGatesAndOrderSnapshot(t *testing.T) {
	if got := (TaxPolicy{}).price(100000); got.TotalPaise != 100000 || got.TaxPaise != 0 {
		t.Fatal(got)
	}
	deduction, bps, _ := (TaxPolicy{}).withdrawal(200000)
	if deduction != 36000 || bps != 1800 {
		t.Fatal("pending estimate is incorrect")
	}
	f, _ := walletFixture(t)
	f.server.options.TaxPolicy.PatientApproval = "synthetic-adviser-policy-v1"
	id, secret := f.register(t, "Tax snapshot")
	f.book(t, id, secret, f.doctors[0].ID, f.start)
	order := f.order(t, id, secret)
	if order.Amount != 29500 {
		t.Fatalf("amount %d", order.Amount)
	}
	f.server.options.TaxPolicy.PatientApproval = ""
	again := f.order(t, id, secret)
	if again.ID != order.ID || again.Amount != order.Amount {
		t.Fatal("order repriced")
	}
	var base, tax, bpsStored int
	if err := f.pool.QueryRow(context.Background(), `SELECT base_paise,tax_paise,tax_bps FROM app_private.consultation_order WHERE id=$1`, order.ID).Scan(&base, &tax, &bpsStored); err != nil {
		t.Fatal(err)
	}
	if base != 25000 || tax != 4500 || bpsStored != 1800 {
		t.Fatalf("snapshot %d %d %d", base, tax, bpsStored)
	}
}

func TestWithdrawalCannotBePaidBeforePolicyReview(t *testing.T) {
	f, doctor := walletFixture(t)
	f.server.options.TaxPolicy.WithdrawalApproval = ""
	walletCredit(t, f, 0)
	saveTestDestination(t, f, doctor)
	w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 200000, "request_key": testUUID()}, doctor, "")
	expectCode(t, w, 201)
	v := decodeResponse[Withdrawal](t, w)
	if !v.PolicyPending || v.Deduction != 36000 || v.Net != 164000 {
		t.Fatalf("pending snapshot %+v", v)
	}
	paid := map[string]any{"action": "paid", "transferred_paise": 164000, "transfer_reference": "NOT-TRANSFERRED", "transferred_at": time.Now().UTC().Format(time.RFC3339Nano), "transfer_completed": true}
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+v.ID+"/decision", paid, f.adminCookie, ""), 409)
	f.server.options.TaxPolicy.WithdrawalApproval = "newly-confirmed-local-policy"
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+v.ID+"/decision", paid, f.adminCookie, ""), 409)
	walletBalance(t, f, 500000, 200000)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+v.ID+"/decision", map[string]string{"action": "rejected", "note": "Recreate under confirmed policy"}, f.adminCookie, ""), 200)
	walletBalance(t, f, 500000, 0)
	w = portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 200000, "request_key": testUUID()}, doctor, "")
	expectCode(t, w, 201)
	recreated := decodeResponse[Withdrawal](t, w)
	if recreated.PolicyPending || recreated.Deduction != 36000 || recreated.Net != 164000 {
		t.Fatalf("reviewed snapshot %+v", recreated)
	}
	paid["transferred_at"] = time.Now().UTC().Truncate(time.Minute).Format(time.RFC3339)
	f.server.options.TaxPolicy.WithdrawalApproval = ""
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+recreated.ID+"/decision", paid, f.adminCookie, ""), 409)
	f.server.options.TaxPolicy.WithdrawalApproval = "different-reviewed-policy"
	listed := decodeResponse[[]Withdrawal](t, portalRequest(f.router, "GET", "/api/admin/withdrawals", nil, f.adminCookie, ""))
	for _, item := range listed {
		if item.ID == recreated.ID && !item.PolicyPending {
			t.Fatal("unapproved current policy presented as payable")
		}
	}
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+recreated.ID+"/decision", paid, f.adminCookie, ""), 409)
	f.server.options.TaxPolicy.WithdrawalApproval = "newly-confirmed-local-policy"
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+recreated.ID+"/decision", paid, f.adminCookie, ""), 200)
	walletBalance(t, f, 300000, 0)
	var originalPolicy, originalStatus string
	var originalDeduction, originalNet int64
	if err := f.pool.QueryRow(context.Background(), `SELECT policy,status,deduction_paise,net_paise FROM app_private.doctor_withdrawal WHERE id=$1`, v.ID).Scan(&originalPolicy, &originalStatus, &originalDeduction, &originalNet); err != nil {
		t.Fatal(err)
	}
	if originalPolicy != "pending_review" || originalStatus != "rejected" || originalDeduction != 36000 || originalNet != 164000 {
		t.Fatal("approval changed the original request")
	}
}

func walletLedgerSnapshot(t *testing.T, f *paidFixture) string {
	t.Helper()
	var snapshot string
	if err := f.pool.QueryRow(context.Background(), `SELECT coalesce(jsonb_agg(to_jsonb(l) ORDER BY l.id),'[]'::jsonb)::text FROM app_private.doctor_ledger l WHERE doctor_id=$1`, f.doctors[0].ID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPartialRefundHoldsWithoutChoosingEarningAllocation(t *testing.T) {
	f, doctor := walletFixture(t)
	_, payment := walletCredit(t, f, 0)
	saveTestDestination(t, f, doctor)
	w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 200000, "request_key": testUUID()}, doctor, "")
	expectCode(t, w, 201)
	request := decodeResponse[Withdrawal](t, w)
	before := walletLedgerSnapshot(t, f)
	for i, refunded := range []int{1, 1, 2} {
		payment.AmountRefunded = refunded
		if err := f.server.applyPayment(context.Background(), payment, fmt.Sprintf("partial-%s-%d", payment.ID, i)); err != nil {
			t.Fatal(err)
		}
		walletBalance(t, f, 500000, 200000)
		if walletLedgerSnapshot(t, f) != before {
			t.Fatal("partial refund changed ledger history")
		}
		balance, err := loadWallet(context.Background(), f.pool, f.doctors[0].ID)
		if err != nil || !balance.ReconciliationRequired {
			t.Fatalf("missing reconciliation hold: %v", err)
		}
		expectCode(t, portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 100, "request_key": testUUID()}, doctor, ""), 409)
		expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+request.ID+"/decision", map[string]any{"action": "paid", "transferred_paise": 164000, "transfer_reference": "NOT-TRANSFERRED", "transferred_at": time.Now().UTC().Format(time.RFC3339Nano), "transfer_completed": true}, f.adminCookie, ""), 409)
	}
	payment.AmountRefunded = payment.Amount
	for _, event := range []string{"full-one-", "full-replay-"} {
		if err := f.server.applyPayment(context.Background(), payment, event+payment.ID); err != nil {
			t.Fatal(err)
		}
	}
	walletBalance(t, f, 0, 200000)
	var reversals int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM app_private.doctor_ledger WHERE doctor_id=$1 AND kind='reversal'`, f.doctors[0].ID).Scan(&reversals); err != nil || reversals != 1 {
		t.Fatalf("full refund reversal count=%d: %v", reversals, err)
	}
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+request.ID+"/decision", map[string]string{"action": "rejected", "note": "Full patient refund"}, f.adminCookie, ""), 200)
	walletBalance(t, f, 0, 0)
	balance, err := loadWallet(context.Background(), f.pool, f.doctors[0].ID)
	if err != nil || balance.ReconciliationRequired {
		t.Fatalf("full refund left a partial hold: %v", err)
	}
}
