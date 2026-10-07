package portal

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func (s *Server) RefundAppointment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, 404, "Appointment not found.")
		return
	}
	tx, err := s.beginBookingTx(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var orderID, paymentID, key, providerID string
	var amount int
	err = tx.QueryRow(r.Context(), `SELECT o.id,o.payment_id,o.amount_paise-o.refunded_paise
		FROM app_private.appointment a JOIN app_private.consultation_order o ON o.appointment_id=a.id
		WHERE a.id=$1 AND a.status='refund_required' AND o.status IN ('paid','partially_refunded')`, id).Scan(&orderID, &paymentID, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "This appointment does not require a refund.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO app_private.consultation_refund(order_id,amount_paise,requested_by) VALUES ($1,$2,$3)
		ON CONFLICT(order_id) DO UPDATE SET order_id=EXCLUDED.order_id
		RETURNING id::text,amount_paise,coalesce(provider_id,'')`, orderID, amount, Current(r.Context()).ID).Scan(&key, &amount, &providerID)
	if err != nil {
		serverError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	// Persist the exact retry payload before contacting the provider.
	if providerID == "" {
		providerID, err = s.options.Gateway.RefundPayment(r.Context(), paymentID, amount, key)
		if err != nil {
			fail(w, 502, "Refund confirmation is pending. Retry uses the same refund request.")
			return
		}
		if _, err = s.pool.Exec(r.Context(), `UPDATE app_private.consultation_refund SET provider_id=$2,submitted_at=coalesce(submitted_at,now()) WHERE id=$1`, key, providerID); err != nil {
			serverError(w, err)
			return
		}
	}
	if payment, fetchErr := s.options.Gateway.FetchPayment(r.Context(), paymentID); fetchErr == nil {
		if err = s.applyPayment(r.Context(), payment, ""); err != nil {
			serverError(w, err)
			return
		}
	} else {
		slog.Warn("refund submitted; payment reconciliation deferred to webhook", "error", fetchErr)
	}
	respond(w, 200, map[string]string{"refund_id": providerID, "message": "Refund submitted. The payment status updates when the provider processes it."})
}
