package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type pausedPaymentGateway struct {
	PaymentGateway
	snapshot Payment
	fetched  chan struct{}
	resume   chan struct{}
}

func (g *pausedPaymentGateway) FetchPayment(ctx context.Context, id string) (Payment, error) {
	close(g.fetched)
	select {
	case <-g.resume:
		return g.snapshot, nil
	case <-ctx.Done():
		return Payment{}, ctx.Err()
	}
}
func TestDisputeSnapshotOvertakenByRefund(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial", true: "full"}[full], func(t *testing.T) {
			f, _ := walletFixture(t)
			appointment, p := walletCredit(t, f, 0)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			gateway := &pausedPaymentGateway{PaymentGateway: f.gateway, snapshot: p, fetched: make(chan struct{}), resume: make(chan struct{})}
			f.server.options.Gateway = gateway
			payload, _ := json.Marshal(map[string]any{"event": "payment.dispute.created", "payload": map[string]any{"dispute": map[string]any{"entity": map[string]string{"payment_id": p.ID}}}})
			event := "dispute-race-" + p.ID
			invoke := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest("POST", "/api/payments/webhook", bytes.NewReader(payload)).WithContext(ctx)
				req.Header.Set("X-Razorpay-Signature", sign(f.gateway.WebhookSecret, payload))
				req.Header.Set("X-Razorpay-Event-Id", event)
				w := httptest.NewRecorder()
				f.router.ServeHTTP(w, req)
				return w
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- invoke() }()
			select {
			case <-gateway.fetched:
			case <-ctx.Done():
				t.Fatal("dispute fetch did not start")
			}
			newer := p
			newer.AmountRefunded = 1
			if full {
				newer.AmountRefunded = p.Amount
			}
			if err := f.server.applyPayment(ctx, newer, "refund-race-"+p.ID); err != nil {
				close(gateway.resume)
				t.Fatal(err)
			}
			close(gateway.resume)
			select {
			case w := <-done:
				expectCode(t, w, 200)
			case <-ctx.Done():
				t.Fatal("dispute did not finish")
			}
			walletBalance(t, f, 0, 0)
			var reason string
			if err := f.pool.QueryRow(ctx, "SELECT reason FROM app_private.wallet_reconciliation WHERE appointment_id=$1", appointment).Scan(&reason); err != nil || reason != "dispute" {
				t.Fatalf("dispute hold missing: %q %v", reason, err)
			}
			var refunded, events int
			if err := f.pool.QueryRow(ctx, "SELECT refunded_paise FROM app_private.consultation_order WHERE id=$1", p.OrderID).Scan(&refunded); err != nil || refunded != newer.AmountRefunded {
				t.Fatalf("refund regressed: %d %v", refunded, err)
			}
			if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM app_private.payment_event WHERE id=$1", event).Scan(&events); err != nil || events != 1 {
				t.Fatalf("event not recorded: %d %v", events, err)
			}
			before := walletLedgerSnapshot(t, f)
			expectCode(t, invoke(), 200)
			if walletLedgerSnapshot(t, f) != before {
				t.Fatal("duplicate dispute changed ledger")
			}
		})
	}
}
func TestFirstObservedCaptureAlreadyPartiallyRefunded(t *testing.T) {
	for _, mode := range []string{"eligible", "expired_hold", "expired", "cancelled", "historical"} {
		t.Run(mode, func(t *testing.T) {
			f, _ := walletFixture(t)
			ctx := context.Background()
			id, secret := f.register(t, "Synthetic delayed capture child")
			appointment := f.book(t, id, secret, f.doctors[0].ID, f.start)
			order := f.order(t, id, secret)
			var err error
			switch mode {
			case "expired_hold":
				_, err = f.pool.Exec(ctx, "UPDATE app_private.appointment SET hold_expires_at=now()-interval '1 second' WHERE id=$1", appointment)
			case "expired", "cancelled":
				_, err = f.pool.Exec(ctx, "UPDATE app_private.appointment SET status=$2 WHERE id=$1", appointment, mode)
			case "historical":
				_, err = f.pool.Exec(ctx, "UPDATE app_private.appointment SET earning_doctor_id=NULL,earning_paise=NULL,earning_policy=NULL WHERE id=$1", appointment)
			}
			if err != nil {
				t.Fatal(err)
			}
			p := Payment{ID: "pay_" + token()[:16], OrderID: order.ID, Amount: order.Amount, Currency: "INR", Status: "captured", Captured: true, AmountRefunded: 1}
			f.gateway.set(p)
			for i := 0; i < 3; i++ {
				if err = f.server.applyPayment(ctx, p, "delayed-"+token()); err != nil {
					t.Fatal(err)
				}
			}
			want := int64(0)
			if mode == "eligible" {
				want = 500000
			}
			walletBalance(t, f, want, 0)
			if mode == "eligible" {
				requireHold(t, f, true)
				reviews := decodeResponse[[]RefundReview](t, portalRequest(f.router, "GET", "/api/admin/refund-reviews", nil, f.adminCookie, ""))
				var refundID string
				for _, r := range reviews {
					if r.OrderID == p.OrderID && len(r.Cases) == 1 {
						refundID = r.Cases[0].ID
					}
				}
				if refundID == "" {
					t.Fatal("original entitlement not reviewable")
				}
				expectCode(t, refundDecision(f, refundID, refundBody(100000, 500000, "Synthetic delayed capture allocation"), f.adminCookie), 200)
				walletBalance(t, f, 400000, 0)
				requireHold(t, f, false)
				stale := p
				stale.AmountRefunded = 0
				if err = f.server.applyPayment(ctx, stale, "late-capture-"+token()); err != nil {
					t.Fatal(err)
				}
				walletBalance(t, f, 400000, 0)
			}
		})
	}
}
func TestWithdrawalQueueIncludesOlderPendingBeyond500(t *testing.T) {
	f, doctor := walletFixture(t)
	walletCredit(t, f, 0)
	saveTestDestination(t, f, doctor)
	w := portalRequest(f.router, "POST", "/api/doctor/wallet/withdrawals", map[string]any{"amount_paise": 100, "request_key": testUUID()}, doctor, "")
	expectCode(t, w, 201)
	pending := decodeResponse[Withdrawal](t, w)
	_, err := f.pool.Exec(context.Background(), `INSERT INTO app_private.doctor_withdrawal(doctor_id,request_key,requested_paise,deduction_paise,net_paise,deduction_bps,policy,encrypted_destination,masked_destination,status,created_at)
 SELECT doctor_id,gen_random_uuid(),requested_paise,deduction_paise,net_paise,deduction_bps,policy,encrypted_destination,masked_destination,'cancelled',created_at+g*interval '1 second'
 FROM app_private.doctor_withdrawal CROSS JOIN generate_series(1,501) g WHERE id=$1`, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"requested", "processing"} {
		if _, err = f.pool.Exec(context.Background(), "UPDATE app_private.doctor_withdrawal SET status=$2 WHERE id=$1", pending.ID, status); err != nil {
			t.Fatal(err)
		}
		rows := decodeResponse[[]Withdrawal](t, portalRequest(f.router, "GET", "/api/admin/withdrawals", nil, f.adminCookie, ""))
		found := false
		for _, v := range rows {
			if v.ID == pending.ID && v.Status == status {
				found = true
			}
		}
		if !found {
			t.Fatalf("older %s withdrawal hidden behind closed history", status)
		}
		walletBalance(t, f, 500000, 100)
	}
	if _, err = f.pool.Exec(context.Background(), "UPDATE app_private.doctor_withdrawal SET status='requested' WHERE doctor_id=$1", f.doctors[0].ID); err != nil {
		t.Fatal(err)
	}
	all := decodeResponse[[]Withdrawal](t, portalRequest(f.router, "GET", "/api/admin/withdrawals", nil, f.adminCookie, ""))
	if len(all) != 502 {
		t.Fatalf("pending queue truncated at %d", len(all))
	}
	walletBalance(t, f, 500000, 50200)
	if _, err = f.pool.Exec(context.Background(), "UPDATE app_private.doctor_withdrawal SET status='cancelled' WHERE doctor_id=$1 AND id<>$2", f.doctors[0].ID, pending.ID); err != nil {
		t.Fatal(err)
	}
	expectCode(t, portalRequest(f.router, "GET", "/api/admin/withdrawals", nil, doctor, ""), http.StatusForbidden)
	expectCode(t, portalRequest(f.router, "POST", "/api/admin/withdrawals/"+pending.ID+"/decision", map[string]string{"action": "rejected", "note": "Synthetic queue review"}, f.adminCookie, ""), 200)
	walletBalance(t, f, 500000, 0)
}
