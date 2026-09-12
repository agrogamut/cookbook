package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {
	id, secret := chi.URLParam(r, "id"), r.Header.Get("X-Registration-Token")
	if !validUUID(id) || !validToken(secret) {
		fail(w, 404, "Registration not found.")
		return
	}
	settings, err := s.settings(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if !settings.CheckoutAvailable {
		fail(w, 503, "Online payment is currently unavailable. Your registration is saved.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var registrationID string
	err = tx.QueryRow(r.Context(), `SELECT id FROM app_private.consultation_registration WHERE id=$1 AND token_hash=$2 FOR UPDATE`, id, tokenHash(secret)).Scan(&registrationID)
	if notFound(w, err) {
		return
	}
	var order GatewayOrder
	var status string
	err = tx.QueryRow(r.Context(), `SELECT id,amount_paise,currency,status FROM app_private.consultation_order WHERE registration_id=$1`, id).Scan(&order.ID, &order.Amount, &order.Currency, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		order, err = s.options.Gateway.CreateOrder(r.Context(), id, *settings.AmountPaise, settings.Currency)
		if err != nil {
			slog.Error("create checkout order", "error", err)
			fail(w, 502, "Checkout could not be opened. Your registration is saved; please try again.")
			return
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO app_private.consultation_order(id,registration_id,amount_paise,currency) VALUES ($1,$2,$3,$4)`, order.ID, id, order.Amount, order.Currency)
	} else if err == nil && (status == "paid" || status == "partially_refunded" || status == "refunded" || status == "authorized") {
		fail(w, 409, "A payment already exists for this consultation. Refresh its payment status.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]any{"order_id": order.ID, "amount_paise": order.Amount, "currency": order.Currency, "key_id": s.options.Gateway.PublicKey()})
}

func (s *Server) VerifyPayment(w http.ResponseWriter, r *http.Request) {
	id, secret := chi.URLParam(r, "id"), r.Header.Get("X-Registration-Token")
	if !validUUID(id) || !validToken(secret) {
		fail(w, 404, "Registration not found.")
		return
	}
	var b struct {
		OrderID   string `json:"razorpay_order_id"`
		PaymentID string `json:"razorpay_payment_id"`
		Signature string `json:"razorpay_signature"`
	}
	if !decode(w, r, &b) {
		return
	}
	var orderID string
	err := s.pool.QueryRow(r.Context(), `SELECT o.id FROM app_private.consultation_order o JOIN app_private.consultation_registration r ON r.id=o.registration_id WHERE r.id=$1 AND r.token_hash=$2`, id, tokenHash(secret)).Scan(&orderID)
	if notFound(w, err) {
		return
	}
	// Sign the stored order identifier, never an order supplied solely by a browser.
	if b.OrderID != orderID || !validProviderID(b.PaymentID, "pay_") || !s.options.Gateway.VerifyCheckout(orderID, b.PaymentID, b.Signature) {
		fail(w, 400, "Payment confirmation could not be verified.")
		return
	}
	p, err := s.options.Gateway.FetchPayment(r.Context(), b.PaymentID)
	if err != nil {
		slog.Error("fetch checkout payment", "error", err)
		fail(w, 502, "Payment confirmation is pending. Check the status again shortly.")
		return
	}
	if p.OrderID != orderID {
		fail(w, 400, "Payment does not belong to this consultation.")
		return
	}
	if err = s.applyPayment(r.Context(), p, ""); err != nil {
		serverError(w, err)
		return
	}
	s.PublicRegistration(w, r)
}

func paymentState(p Payment) (string, error) {
	if p.Amount <= 0 || p.AmountRefunded < 0 || p.AmountRefunded > p.Amount {
		return "", errors.New("invalid payment amounts")
	}
	if p.AmountRefunded > 0 {
		if !p.Captured && p.Status != "refunded" {
			return "", errors.New("refund without a captured payment")
		}
		if p.AmountRefunded == p.Amount {
			return "refunded", nil
		}
		return "partially_refunded", nil
	}
	switch p.Status {
	case "captured":
		if !p.Captured {
			return "", errors.New("captured flag does not match status")
		}
		return "paid", nil
	case "authorized":
		return "authorized", nil
	case "failed":
		return "failed", nil
	case "created":
		return "pending", nil
	default:
		return "", errors.New("unsupported payment status")
	}
}
func settled(status string) bool {
	return status == "paid" || status == "partially_refunded" || status == "refunded"
}

// A signed callback alone cannot make a registration paid. Amount, currency and
// capture state come from a fresh server-to-server payment fetch.
func (s *Server) applyPayment(ctx context.Context, p Payment, eventID string) error {
	state, err := paymentState(p)
	if err != nil {
		return err
	}
	if !validProviderID(p.ID, "pay_") || !validProviderID(p.OrderID, "order_") {
		return errors.New("invalid payment identifiers")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin payment update: %w", err)
	}
	defer tx.Rollback(ctx)
	var amount, refunded int
	var currency, oldState, oldPaymentID string
	err = tx.QueryRow(ctx, `SELECT amount_paise,currency,status,coalesce(payment_id,''),refunded_paise FROM app_private.consultation_order WHERE id=$1 FOR UPDATE`, p.OrderID).Scan(&amount, &currency, &oldState, &oldPaymentID, &refunded)
	if err != nil {
		return fmt.Errorf("find payment order: %w", err)
	}
	if amount != p.Amount || currency != p.Currency {
		return errors.New("payment amount or currency does not match stored order")
	}
	if eventID != "" {
		result, err := tx.Exec(ctx, `INSERT INTO app_private.payment_event(id,order_id) VALUES ($1,$2) ON CONFLICT(id) DO NOTHING`, eventID, p.OrderID)
		if err != nil {
			return fmt.Errorf("record payment event: %w", err)
		}
		if result.RowsAffected() == 0 {
			return tx.Commit(ctx)
		}
	}
	// Failed attempts can arrive after capture. Refund totals and settled status
	// never move backwards, even with delayed webhooks or simultaneous callbacks.
	if settled(oldState) && (!settled(state) || oldPaymentID != p.ID || p.AmountRefunded < refunded) {
		return tx.Commit(ctx)
	}
	if oldState == "authorized" && (state == "pending" || (state == "failed" && oldPaymentID != p.ID)) {
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE app_private.consultation_order SET payment_id=$2,status=$3,refunded_paise=$4,
		paid_at=CASE WHEN $3 IN ('paid','partially_refunded','refunded') THEN coalesce(paid_at,now()) ELSE paid_at END,updated_at=now() WHERE id=$1`, p.OrderID, p.ID, state, p.AmountRefunded)
	if err != nil {
		return fmt.Errorf("update payment state: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Server) Webhook(w http.ResponseWriter, r *http.Request) {
	if !s.options.Gateway.Enabled() {
		fail(w, 503, "Payments are not configured.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		fail(w, 400, "Invalid webhook body.")
		return
	}
	if !s.options.Gateway.VerifyWebhook(body, r.Header.Get("X-Razorpay-Signature")) {
		fail(w, 400, "Invalid webhook signature.")
		return
	}
	eventID := r.Header.Get("X-Razorpay-Event-Id")
	if eventID == "" || len(eventID) > 200 {
		fail(w, 400, "Missing or invalid event identifier.")
		return
	}
	var event struct {
		Event   string `json:"event"`
		Payload struct {
			Payment struct {
				Entity struct {
					ID string `json:"id"`
				} `json:"entity"`
			} `json:"payment"`
			Refund struct {
				Entity struct {
					PaymentID string `json:"payment_id"`
				} `json:"entity"`
			} `json:"refund"`
		} `json:"payload"`
	}
	if json.Unmarshal(body, &event) != nil {
		fail(w, 400, "Invalid webhook payload.")
		return
	}
	var paymentID string
	switch event.Event {
	case "payment.captured", "payment.authorized", "payment.failed", "order.paid":
		paymentID = event.Payload.Payment.Entity.ID
	case "refund.processed":
		paymentID = event.Payload.Refund.Entity.PaymentID
	default:
		respond(w, 200, map[string]bool{"received": true})
		return
	}
	if !validProviderID(paymentID, "pay_") {
		fail(w, 400, "Invalid payment identifier.")
		return
	}
	var duplicate bool
	if err = s.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM app_private.payment_event WHERE id=$1)`, eventID).Scan(&duplicate); err != nil {
		serverError(w, err)
		return
	}
	if duplicate {
		respond(w, 200, map[string]bool{"received": true})
		return
	}
	p, err := s.options.Gateway.FetchPayment(r.Context(), paymentID)
	if err != nil {
		slog.Error("fetch webhook payment", "error", err)
		fail(w, 502, "Payment service unavailable; retry this event.")
		return
	}
	if err = s.applyPayment(r.Context(), p, eventID); err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"received": true})
}
