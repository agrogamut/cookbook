package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var errRefundHistoryIncomplete = errors.New("processed refund identities do not yet account for the verified cumulative total")

type RefundCase struct {
	ID                string     `json:"id"`
	Amount            int64      `json:"amount_paise"`
	ProviderCreatedAt time.Time  `json:"provider_created_at"`
	Adjustment        *int64     `json:"adjustment_paise"`
	EarningBefore     *int64     `json:"earning_before_paise"`
	ActorID           *string    `json:"actor_id"`
	ActorName         *string    `json:"actor_name"`
	Explanation       *string    `json:"explanation"`
	DecidedAt         *time.Time `json:"decided_at"`
}
type RefundReview struct {
	OrderID          string       `json:"order_id"`
	PaymentID        string       `json:"payment_id"`
	AppointmentID    string       `json:"appointment_id"`
	StartsAt         time.Time    `json:"starts_at"`
	DoctorID         string       `json:"doctor_id"`
	DoctorName       string       `json:"doctor_name"`
	PaymentAmount    int64        `json:"payment_amount_paise"`
	Refunded         int64        `json:"refunded_paise"`
	KnownRefunded    int64        `json:"known_refunded_paise"`
	OriginalEarning  int64        `json:"original_earning_paise"`
	RemainingEarning int64        `json:"remaining_earning_paise"`
	Status           string       `json:"status"`
	Cases            []RefundCase `json:"refunds"`
}

func recordRefunds(ctx context.Context, tx pgx.Tx, p Payment, refunds []PaymentRefund) error {
	total := 0
	seen := map[string]bool{}
	for _, v := range refunds {
		if seen[v.ID] || !validProviderID(v.ID, "rfnd_") || v.PaymentID != p.ID || v.Currency != p.Currency || v.Status != "processed" || v.Amount <= 0 || v.Amount > p.Amount || v.CreatedAt <= 0 {
			return errors.New("invalid processed refund identity")
		}
		seen[v.ID] = true
		total += v.Amount
	}
	if total > p.AmountRefunded {
		return errors.New("refund identities exceed verified payment total; refresh payment")
	}
	for _, v := range refunds {
		_, err := tx.Exec(ctx, `INSERT INTO app_private.payment_refund(id,order_id,payment_id,amount_paise,provider_created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO NOTHING`, v.ID, p.OrderID, p.ID, v.Amount, time.Unix(v.CreatedAt, 0))
		if err != nil {
			return fmt.Errorf("record refund identity: %w", err)
		}
		var same bool
		err = tx.QueryRow(ctx, `SELECT order_id=$2 AND payment_id=$3 AND amount_paise=$4 AND provider_created_at=$5 FROM app_private.payment_refund WHERE id=$1`, v.ID, p.OrderID, p.ID, v.Amount, time.Unix(v.CreatedAt, 0)).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return errors.New("refund identity changed")
		}
	}
	var known int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(sum(amount_paise),0) FROM app_private.payment_refund WHERE order_id=$1`, p.OrderID).Scan(&known); err != nil {
		return fmt.Errorf("sum recorded refunds: %w", err)
	}
	if known > int64(p.AmountRefunded) {
		return errors.New("recorded refund identities exceed verified total")
	}
	if known < int64(p.AmountRefunded) {
		return errRefundHistoryIncomplete
	}
	return nil
}
func refreshPartialHold(ctx context.Context, tx pgx.Tx, appointment string) error {
	var pending bool
	var doctor *string
	err := tx.QueryRow(ctx, `SELECT a.earning_doctor_id::text,
        (o.refunded_paise<>(SELECT coalesce(sum(f.amount_paise),0) FROM app_private.payment_refund f WHERE f.order_id=o.id)
        OR (o.status='partially_refunded' AND EXISTS(SELECT 1 FROM app_private.payment_refund f LEFT JOIN app_private.refund_decision d ON d.refund_id=f.id WHERE f.order_id=o.id AND d.refund_id IS NULL)))
        FROM app_private.appointment a JOIN app_private.consultation_order o ON o.appointment_id=a.id WHERE a.id=$1 AND o.status IN ('partially_refunded','refunded')`, appointment).Scan(&doctor, &pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if doctor == nil {
		return nil
	}
	if pending {
		_, err = tx.Exec(ctx, `INSERT INTO app_private.wallet_reconciliation(appointment_id,doctor_id,reason) VALUES($1,$2,'partial_refund') ON CONFLICT DO NOTHING`, appointment, *doctor)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM app_private.wallet_reconciliation WHERE appointment_id=$1 AND reason='partial_refund'`, appointment)
	}
	return err
}
func (s *Server) ListRefundReviews(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT o.id,o.payment_id,a.id,a.starts_at,a.earning_doctor_id,d.name,o.amount_paise,o.refunded_paise,
        (SELECT coalesce(sum(f.amount_paise),0) FROM app_private.payment_refund f WHERE f.order_id=o.id),
        (SELECT coalesce(sum(l.amount_paise),0) FROM app_private.doctor_ledger l WHERE l.appointment_id=a.id AND l.kind='earning'),
        (SELECT coalesce(sum(l.amount_paise),0) FROM app_private.doctor_ledger l WHERE l.appointment_id=a.id),
        o.status
        FROM app_private.consultation_order o JOIN app_private.appointment a ON a.id=o.appointment_id JOIN app_private.staff_account d ON d.id=a.earning_doctor_id
        WHERE o.refunded_paise>0 ORDER BY o.created_at DESC,o.id`)
	if err != nil {
		serverError(w, err)
		return
	}
	items := []RefundReview{}
	for rows.Next() {
		var v RefundReview
		err = rows.Scan(&v.OrderID, &v.PaymentID, &v.AppointmentID, &v.StartsAt, &v.DoctorID, &v.DoctorName, &v.PaymentAmount, &v.Refunded, &v.KnownRefunded, &v.OriginalEarning, &v.RemainingEarning, &v.Status)
		if err != nil {
			rows.Close()
			serverError(w, err)
			return
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		serverError(w, err)
		return
	}
	for i := range items {
		cases, err := s.pool.Query(r.Context(), `SELECT f.id,f.amount_paise,f.provider_created_at,d.adjustment_paise,d.earning_before_paise,d.actor_id::text,a.name,d.explanation,d.created_at
            FROM app_private.payment_refund f LEFT JOIN app_private.refund_decision d ON d.refund_id=f.id LEFT JOIN app_private.staff_account a ON a.id=d.actor_id
            WHERE f.order_id=$1 ORDER BY f.provider_created_at,f.id`, items[i].OrderID)
		if err != nil {
			serverError(w, err)
			return
		}
		items[i].Cases = []RefundCase{}
		for cases.Next() {
			var c RefundCase
			if err = cases.Scan(&c.ID, &c.Amount, &c.ProviderCreatedAt, &c.Adjustment, &c.EarningBefore, &c.ActorID, &c.ActorName, &c.Explanation, &c.DecidedAt); err != nil {
				cases.Close()
				serverError(w, err)
				return
			}
			items[i].Cases = append(items[i].Cases, c)
		}
		err = cases.Err()
		cases.Close()
		if err != nil {
			serverError(w, err)
			return
		}
	}
	respond(w, 200, items)
}
func (s *Server) SyncRefundReview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validProviderID(id, "order_") {
		fail(w, 404, "Payment not found.")
		return
	}
	var paymentID string
	if notFound(w, s.pool.QueryRow(r.Context(), `SELECT payment_id FROM app_private.consultation_order WHERE id=$1 AND refunded_paise>0`, id).Scan(&paymentID)) {
		return
	}
	p, err := s.options.Gateway.FetchPayment(r.Context(), paymentID)
	if err != nil {
		fail(w, 502, "Refund details could not be refreshed. The hold remains.")
		return
	}
	if p.OrderID != id {
		fail(w, 409, "Payment identity changed.")
		return
	}
	if err = s.applyPayment(r.Context(), p, ""); err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) DecideRefund(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validProviderID(id, "rfnd_") {
		fail(w, 404, "Refund not found.")
		return
	}
	var b struct {
		Adjustment        *int64 `json:"adjustment_paise"`
		ExpectedRemaining *int64 `json:"expected_remaining_paise"`
		Explanation       string `json:"explanation"`
		Confirmed         bool   `json:"confirmed"`
	}
	if !decode(w, r, &b) {
		return
	}
	b.Explanation = strings.TrimSpace(b.Explanation)
	if b.Adjustment == nil || *b.Adjustment < 0 || b.ExpectedRemaining == nil || !b.Confirmed || len(b.Explanation) == 0 || len(b.Explanation) > 500 || strings.ContainsRune(b.Explanation, 0) {
		fail(w, 400, "Enter an explicit deduction, explanation and confirmation.")
		return
	}
	var order, payment string
	if notFound(w, s.pool.QueryRow(r.Context(), `SELECT order_id,payment_id FROM app_private.payment_refund WHERE id=$1`, id).Scan(&order, &payment)) {
		return
	}
	p, err := s.options.Gateway.FetchPayment(r.Context(), payment)
	if err != nil {
		fail(w, 502, "Current payment could not be verified. The hold remains.")
		return
	}
	if p.OrderID != order {
		fail(w, 409, "Payment identity changed.")
		return
	}
	if err = s.applyPayment(r.Context(), p, ""); err != nil {
		serverError(w, err)
		return
	}
	tx, err := s.beginBookingTx(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var adjustment int64
	var explanation string
	err = tx.QueryRow(r.Context(), `SELECT adjustment_paise,explanation FROM app_private.refund_decision WHERE refund_id=$1`, id).Scan(&adjustment, &explanation)
	if err == nil {
		if adjustment != *b.Adjustment || explanation != b.Explanation {
			fail(w, 409, "This refund already has an immutable decision.")
			return
		}
		respond(w, 200, map[string]bool{"ok": true, "replayed": true})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		serverError(w, err)
		return
	}
	var appointment, doctor, status string
	var remaining, refunded, known int64
	err = tx.QueryRow(r.Context(), `SELECT a.id,a.earning_doctor_id,o.status,o.refunded_paise,
        (SELECT coalesce(sum(amount_paise),0) FROM app_private.payment_refund WHERE order_id=o.id),
        (SELECT coalesce(sum(amount_paise),0) FROM app_private.doctor_ledger WHERE appointment_id=a.id)
        FROM app_private.consultation_order o JOIN app_private.appointment a ON a.id=o.appointment_id WHERE o.id=$1`, order).Scan(&appointment, &doctor, &status, &refunded, &known, &remaining)
	if err != nil {
		serverError(w, err)
		return
	}
	if status != "partially_refunded" || known != refunded || int64(p.AmountRefunded) < refunded {
		fail(w, 409, "Refresh the complete current refund history before deciding. Fully refunded payments need no further earning deduction.")
		return
	}
	if remaining != *b.ExpectedRemaining || *b.Adjustment > remaining {
		fail(w, 409, "Earnings changed. Refresh and choose between zero and the remaining appointment earning.")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO app_private.refund_decision(refund_id,actor_id,adjustment_paise,earning_before_paise,explanation) VALUES($1,$2,$3,$4,$5)`, id, Current(r.Context()).ID, *b.Adjustment, remaining, b.Explanation)
	if err == nil && *b.Adjustment > 0 {
		_, err = tx.Exec(r.Context(), `INSERT INTO app_private.doctor_ledger(doctor_id,kind,amount_paise,appointment_id,order_id,payment_id,refund_id,reason) VALUES($1,'refund_adjustment',$2,$3,$4,$5,$6,'admin_partial_refund')`, doctor, -*b.Adjustment, appointment, order, payment, id)
	}
	if err == nil {
		err = refreshPartialHold(r.Context(), tx, appointment)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
