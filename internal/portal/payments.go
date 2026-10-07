package portal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var (
	errRegistrationNotFound = errors.New("registration not found")
	errCheckoutUnavailable  = errors.New("checkout unavailable")
	errPaymentAlreadyExists = errors.New("payment already exists")
	errBookingRequired      = errors.New("active booking required")
)

func (s *Server) createOrderFor(ctx context.Context, id, guardianID, secret string) (GatewayOrder, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return GatewayOrder{}, err
	}
	if !settings.CheckoutAvailable {
		return GatewayOrder{}, errCheckoutUnavailable
	}
	tx, err := s.beginBookingTx(ctx)
	if err != nil {
		return GatewayOrder{}, err
	}
	defer tx.Rollback(ctx)
	var registrationID string
	if guardianID != "" {
		if !guardianAllows(ctx, id) {
			return GatewayOrder{}, errRegistrationNotFound
		}
		err = tx.QueryRow(ctx, `SELECT id FROM app_private.consultation_registration WHERE id=$1 AND guardian_id=$2 FOR UPDATE`, id, guardianID).Scan(&registrationID)
	} else {
		err = tx.QueryRow(ctx, `SELECT id FROM app_private.consultation_registration WHERE id=$1 AND token_hash=$2 FOR UPDATE`, id, tokenHash(secret)).Scan(&registrationID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return GatewayOrder{}, errRegistrationNotFound
	}
	if err != nil {
		return GatewayOrder{}, err
	}
	var appointmentID sql.NullString
	if err = tx.QueryRow(ctx, `SELECT id::text FROM app_private.appointment
		WHERE registration_id=$1 AND (
			status IN ('pending_admin','paid_pending_admin','confirmed') OR
			(status='awaiting_payment' AND (hold_expires_at IS NULL OR hold_expires_at > now()))
		)
		ORDER BY created_at DESC,id DESC LIMIT 1`, registrationID).Scan(&appointmentID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return GatewayOrder{}, fmt.Errorf("find appointment hold: %w", err)
	}
	if !appointmentID.Valid {
		return GatewayOrder{}, errBookingRequired
	}
	var order GatewayOrder
	var status string
	err = tx.QueryRow(ctx, `SELECT id,amount_paise,currency,status FROM app_private.consultation_order WHERE appointment_id=$1`, appointmentID.String).Scan(&order.ID, &order.Amount, &order.Currency, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		order, err = s.options.Gateway.CreateOrder(ctx, appointmentID.String, *settings.AmountPaise, settings.Currency)
		if err != nil {
			return GatewayOrder{}, fmt.Errorf("create checkout order: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO app_private.consultation_order(id,registration_id,amount_paise,currency,appointment_id) VALUES ($1,$2,$3,$4,$5)`, order.ID, id, order.Amount, order.Currency, appointmentID.String)
	} else if err == nil && (settled(status) || status == "authorized") {
		return GatewayOrder{}, errPaymentAlreadyExists
	}
	if err != nil {
		return GatewayOrder{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return GatewayOrder{}, err
	}
	return order, nil
}

func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {
	id, secret := chi.URLParam(r, "id"), r.Header.Get("X-Registration-Token")
	if !validUUID(id) || !validToken(secret) {
		fail(w, 404, "Registration not found.")
		return
	}
	order, err := s.createOrderFor(r.Context(), id, "", secret)
	if errors.Is(err, errRegistrationNotFound) {
		fail(w, 404, "Registration not found.")
		return
	}
	if errors.Is(err, errCheckoutUnavailable) {
		fail(w, 503, "Online payment is currently unavailable. Your registration is saved.")
		return
	}
	if errors.Is(err, errBookingRequired) {
		fail(w, 409, "Choose an available appointment time before paying. Your previous hold may have expired.")
		return
	}
	if errors.Is(err, errPaymentAlreadyExists) {
		fail(w, 409, "A payment already exists for this consultation. Refresh its payment status.")
		return
	}
	if err != nil {
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
	err := s.pool.QueryRow(r.Context(), `SELECT o.id FROM app_private.consultation_order o JOIN app_private.consultation_registration r ON r.id=o.registration_id WHERE r.id=$1 AND r.token_hash=$2 AND o.id=$3`, id, tokenHash(secret), b.OrderID).Scan(&orderID)
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

func (s *Server) CreateFamilyOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	order, err := s.createOrderFor(r.Context(), id, CurrentGuardian(r.Context()).ID, "")
	if errors.Is(err, errRegistrationNotFound) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	if errors.Is(err, errCheckoutUnavailable) {
		fail(w, http.StatusServiceUnavailable, "Online payment is currently unavailable. Your registration is saved.")
		return
	}
	if errors.Is(err, errBookingRequired) {
		fail(w, 409, "Choose an available appointment time before paying. Your previous hold may have expired.")
		return
	}
	if errors.Is(err, errPaymentAlreadyExists) {
		fail(w, http.StatusConflict, "A payment already exists for this consultation. Refresh its payment status.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"order_id": order.ID, "amount_paise": order.Amount, "currency": order.Currency, "key_id": s.options.Gateway.PublicKey()})
}

func (s *Server) VerifyFamilyPayment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !guardianAllows(r.Context(), id) {
		fail(w, 404, "Registration not found.")
		return
	}
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Registration not found.")
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
	err := s.pool.QueryRow(r.Context(), `SELECT o.id FROM app_private.consultation_order o
		JOIN app_private.consultation_registration r ON r.id=o.registration_id
		WHERE r.id=$1 AND r.guardian_id=$2 AND o.id=$3`, id, CurrentGuardian(r.Context()).ID, b.OrderID).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if b.OrderID != orderID || !validProviderID(b.PaymentID, "pay_") || !s.options.Gateway.VerifyCheckout(orderID, b.PaymentID, b.Signature) {
		fail(w, http.StatusBadRequest, "Payment confirmation could not be verified.")
		return
	}
	p, err := s.options.Gateway.FetchPayment(r.Context(), b.PaymentID)
	if err != nil {
		slog.Error("fetch family checkout payment", "error", err)
		fail(w, http.StatusBadGateway, "Payment confirmation is pending. Check the status again shortly.")
		return
	}
	if p.OrderID != orderID {
		fail(w, http.StatusBadRequest, "Payment does not belong to this consultation.")
		return
	}
	if err = s.applyPayment(r.Context(), p, ""); err != nil {
		serverError(w, err)
		return
	}
	s.FamilyRegistrationStatus(w, r)
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
	tx, err := s.beginBookingTx(ctx)
	if err != nil {
		return fmt.Errorf("begin payment update: %w", err)
	}
	defer tx.Rollback(ctx)
	var amount, refunded int
	var currency, oldState, oldPaymentID string
	var appointmentID sql.NullString
	err = tx.QueryRow(ctx, `SELECT amount_paise,currency,status,coalesce(payment_id,''),refunded_paise,appointment_id::text FROM app_private.consultation_order WHERE id=$1 FOR UPDATE`, p.OrderID).Scan(&amount, &currency, &oldState, &oldPaymentID, &refunded, &appointmentID)
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
	if appointmentID.Valid && state == "paid" {
		if _, err = tx.Exec(ctx, `UPDATE app_private.appointment SET status='paid_pending_admin',hold_expires_at=NULL,updated_at=now()
			WHERE id=$1 AND (status='pending_admin' OR (status='awaiting_payment' AND hold_expires_at>now()))`, appointmentID.String); err != nil {
			return fmt.Errorf("advance appointment after payment: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE app_private.appointment SET status='refund_required',hold_expires_at=NULL,updated_at=now()
			WHERE id=$1 AND status IN ('awaiting_payment','expired','cancelled','rejected')`, appointmentID.String); err != nil {
			return fmt.Errorf("flag expired appointment payment: %w", err)
		}
	}
	if appointmentID.Valid && (state == "refunded" || state == "partially_refunded") {
		bookingStatus := "refunded"
		if state == "partially_refunded" {
			bookingStatus = "refund_required"
		}
		if _, err = tx.Exec(ctx, `UPDATE app_private.appointment SET status=$2,hold_expires_at=NULL,updated_at=now() WHERE id=$1`, appointmentID.String, bookingStatus); err != nil {
			return fmt.Errorf("close appointment after refund: %w", err)
		}
	}
	if appointmentID.Valid && (state == "refunded" || state == "partially_refunded") {
		if _, err = tx.Exec(ctx, `UPDATE app_private.consultation_registration r SET assigned_doctor_id=NULL,status='cancelled',updated_at=now()
            FROM app_private.appointment a WHERE a.id=$1 AND r.id=a.registration_id
            AND NOT EXISTS (SELECT 1 FROM app_private.appointment newer WHERE newer.registration_id=r.id AND newer.status='confirmed')`, appointmentID.String); err != nil {
			return fmt.Errorf("clear refunded appointment assignment: %w", err)
		}
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
