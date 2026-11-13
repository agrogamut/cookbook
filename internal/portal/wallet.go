package portal

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func DoctorOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Current(r.Context()).Role != "doctor" {
			fail(w, 403, "Doctor access is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) SaveEarningSettings(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Amount *int `json:"doctor_earning_paise"`
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Amount != nil && (*b.Amount < 0 || *b.Amount > 100000000) {
		fail(w, 400, "Enter an earning amount from ₹0 to ₹10,00,000.")
		return
	}
	_, err := s.pool.Exec(r.Context(), `UPDATE app_private.consultation_settings SET doctor_earning_paise=$1,updated_by=$2,updated_at=now() WHERE singleton`, b.Amount, Current(r.Context()).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	s.AdminSettings(w, r)
}

type Destination struct {
	Kind           string `json:"kind"`
	Holder         string `json:"holder"`
	Bank           string `json:"bank"`
	Account        string `json:"account"`
	ConfirmAccount string `json:"confirm_account,omitempty"`
	IFSC           string `json:"ifsc"`
	UPI            string `json:"upi"`
}

var accountPattern = regexp.MustCompile(`^[0-9]{6,34}$`)
var ifscPattern = regexp.MustCompile(`^[A-Z]{4}0[A-Z0-9]{6}$`)
var upiPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{2,128}@[a-zA-Z0-9.-]{2,64}$`)

func (d *Destination) validate() bool {
	d.Holder = strings.TrimSpace(d.Holder)
	d.Bank = strings.TrimSpace(d.Bank)
	d.Account = strings.TrimSpace(d.Account)
	d.ConfirmAccount = strings.TrimSpace(d.ConfirmAccount)
	d.IFSC = strings.ToUpper(strings.TrimSpace(d.IFSC))
	d.UPI = strings.TrimSpace(d.UPI)
	if !validName(d.Holder) {
		return false
	}
	if d.Kind == "bank" {
		if !validName(d.Bank) || !accountPattern.MatchString(d.Account) || d.Account != d.ConfirmAccount || !ifscPattern.MatchString(d.IFSC) || d.UPI != "" {
			return false
		}
		d.ConfirmAccount = ""
		return true
	}
	return d.Kind == "upi" && upiPattern.MatchString(d.UPI) && d.Bank == "" && d.Account == "" && d.ConfirmAccount == "" && d.IFSC == ""
}
func (d Destination) mask() string {
	if d.Kind == "bank" {
		return "Bank account ending " + d.Account[len(d.Account)-4:]
	}
	parts := strings.SplitN(d.UPI, "@", 2)
	return "UPI " + parts[0][:1] + "***@" + parts[1]
}
func walletCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("wallet encryption is not configured")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("wallet encryption is unavailable")
	}
	return cipher.NewGCM(block)
}
func (s *Server) encryptDestination(doctor string, d Destination) ([]byte, error) {
	a, err := walletCipher(s.options.WalletKey)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(d)
	if err != nil {
		return nil, errors.New("encode payout destination")
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, errors.New("create payout nonce")
	}
	return a.Seal(append([]byte{1}, nonce...), nonce, plain, []byte("doctor-destination:"+doctor)), nil
}
func (s *Server) decryptDestination(doctor string, encrypted []byte) (Destination, error) {
	var d Destination
	a, err := walletCipher(s.options.WalletKey)
	if err != nil {
		return d, err
	}
	if len(encrypted) < 1+a.NonceSize()+a.Overhead() || encrypted[0] != 1 {
		return d, errors.New("invalid encrypted payout destination")
	}
	plain, err := a.Open(nil, encrypted[1:1+a.NonceSize()], encrypted[1+a.NonceSize():], []byte("doctor-destination:"+doctor))
	if err != nil {
		return d, errors.New("payout destination could not be decrypted")
	}
	if err = json.Unmarshal(plain, &d); err != nil {
		return d, errors.New("invalid payout destination")
	}
	return d, nil
}
func (s *Server) SaveDestination(w http.ResponseWriter, r *http.Request) {
	if len(s.options.WalletKey) != 32 {
		fail(w, 503, "Payout details are not configured. Contact the administrator.")
		return
	}
	var d Destination
	if !decode(w, r, &d) {
		return
	}
	if !d.validate() {
		fail(w, 400, "Enter the account holder and valid bank details with matching account numbers, or a UPI ID.")
		return
	}
	actor := Current(r.Context())
	encrypted, err := s.encryptDestination(actor.ID, d)
	if err != nil {
		serverError(w, err)
		return
	}
	tx, err := s.beginBookingTx(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `INSERT INTO app_private.doctor_destination(doctor_id,encrypted_details,masked_details)
        VALUES ($1,$2,$3) ON CONFLICT(doctor_id) DO UPDATE SET encrypted_details=$2,masked_details=$3,updated_at=now()`, actor.ID, encrypted, d.mask())
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO app_private.wallet_audit(actor_id,action) VALUES ($1,'destination_saved')`, actor.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]string{"masked_destination": d.mask()})
}

type WalletBalance struct {
	DoctorID               string `json:"doctor_id"`
	DoctorName             string `json:"doctor_name"`
	Balance                int64  `json:"balance_paise"`
	Reserved               int64  `json:"reserved_paise"`
	Available              int64  `json:"available_paise"`
	ReconciliationRequired bool   `json:"reconciliation_required"`
	MaskedDestination      string `json:"masked_destination"`
}
type walletQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadWallet(ctx context.Context, q walletQuerier, doctor string) (WalletBalance, error) {
	v := WalletBalance{DoctorID: doctor}
	err := q.QueryRow(ctx, `SELECT a.name,
        coalesce((SELECT sum(l.amount_paise) FROM app_private.doctor_ledger l WHERE l.doctor_id=a.id),0)::bigint,
        coalesce((SELECT sum(w.requested_paise) FROM app_private.doctor_withdrawal w WHERE w.doctor_id=a.id AND w.status IN ('requested','processing')),0)::bigint,
        EXISTS(SELECT 1 FROM app_private.wallet_reconciliation x WHERE x.doctor_id=a.id) OR
        EXISTS(SELECT 1 FROM app_private.doctor_ledger l JOIN app_private.appointment p ON p.id=l.appointment_id WHERE l.doctor_id=a.id AND l.kind='earning' AND p.status='refund_required'),
        coalesce((SELECT masked_details FROM app_private.doctor_destination WHERE doctor_id=a.id),'')
        FROM app_private.staff_account a WHERE a.id=$1 AND a.role='doctor'`, doctor).Scan(&v.DoctorName, &v.Balance, &v.Reserved, &v.ReconciliationRequired, &v.MaskedDestination)
	v.Available = v.Balance - v.Reserved
	v.ReconciliationRequired = v.ReconciliationRequired || v.Available < 0
	return v, err
}

type LedgerEntry struct {
	ID            int64     `json:"id"`
	Kind          string    `json:"kind"`
	Amount        int64     `json:"amount_paise"`
	AppointmentID *string   `json:"appointment_id"`
	WithdrawalID  *string   `json:"withdrawal_id"`
	Reason        string    `json:"reason"`
	CreatedAt     time.Time `json:"created_at"`
}
type Withdrawal struct {
	Policy            string     `json:"-"`
	PolicyPending     bool       `json:"policy_pending"`
	ID                string     `json:"id"`
	DoctorID          string     `json:"doctor_id"`
	DoctorName        string     `json:"doctor_name"`
	Requested         int64      `json:"requested_paise"`
	Deduction         int64      `json:"deduction_paise"`
	Net               int64      `json:"net_paise"`
	DeductionBPS      int        `json:"deduction_bps"`
	MaskedDestination string     `json:"masked_destination"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
	Transferred       *int64     `json:"transferred_paise"`
	Reference         *string    `json:"transfer_reference"`
	TransferredAt     *time.Time `json:"transferred_at"`
	Note              string     `json:"decision_note"`
}

const withdrawalSelect = `SELECT w.id,w.doctor_id,a.name,w.requested_paise,w.deduction_paise,w.net_paise,w.deduction_bps,w.masked_destination,w.status,w.created_at,w.transferred_paise,w.transfer_reference,w.transferred_at,w.decision_note,(w.policy IN ('pending_review','inactive')),w.policy
    FROM app_private.doctor_withdrawal w JOIN app_private.staff_account a ON a.id=w.doctor_id `

func scanWithdrawal(row interface{ Scan(...any) error }) (Withdrawal, error) {
	var v Withdrawal
	err := row.Scan(&v.ID, &v.DoctorID, &v.DoctorName, &v.Requested, &v.Deduction, &v.Net, &v.DeductionBPS, &v.MaskedDestination, &v.Status, &v.CreatedAt, &v.Transferred, &v.Reference, &v.TransferredAt, &v.Note, &v.PolicyPending, &v.Policy)
	return v, err
}
func (s *Server) withdrawals(ctx context.Context, doctor string) ([]Withdrawal, error) {
	rows, err := s.pool.Query(ctx, withdrawalSelect+`WHERE ($1='' OR w.doctor_id::text=$1) ORDER BY w.created_at DESC,w.id DESC LIMIT 500`, doctor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Withdrawal{}
	for rows.Next() {
		v, err := scanWithdrawal(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, s.withdrawalForDisplay(v))
	}
	return items, rows.Err()
}
func (s *Server) withdrawalForDisplay(v Withdrawal) Withdrawal {
	if v.Status == "requested" || v.Status == "processing" {
		v.PolicyPending = v.PolicyPending || s.options.TaxPolicy.WithdrawalApproval == "" || v.Policy != s.options.TaxPolicy.WithdrawalApproval
	}
	return v
}
func (s *Server) DoctorWallet(w http.ResponseWriter, r *http.Request) {
	id := Current(r.Context()).ID
	balance, err := loadWallet(r.Context(), s.pool, id)
	if err != nil {
		serverError(w, err)
		return
	}
	withdrawals, err := s.withdrawals(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	rows, err := s.pool.Query(r.Context(), `SELECT id,kind,amount_paise,appointment_id,withdrawal_id,reason,created_at FROM app_private.doctor_ledger WHERE doctor_id=$1 ORDER BY id DESC LIMIT 200`, id)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	entries := []LedgerEntry{}
	for rows.Next() {
		var v LedgerEntry
		if err = rows.Scan(&v.ID, &v.Kind, &v.Amount, &v.AppointmentID, &v.WithdrawalID, &v.Reason, &v.CreatedAt); err != nil {
			serverError(w, err)
			return
		}
		entries = append(entries, v)
	}
	if err = rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	_, bps, _ := s.options.TaxPolicy.withdrawal(0)
	respond(w, 200, map[string]any{"balance": balance, "withdrawals": withdrawals, "ledger": entries, "configured": len(s.options.WalletKey) == 32, "deduction_bps": bps, "deduction_active": s.options.TaxPolicy.WithdrawalApproval != ""})
}
func (s *Server) AdminWallets(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT id::text FROM app_private.staff_account WHERE role='doctor' ORDER BY name,id`)
	if err != nil {
		serverError(w, err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			serverError(w, err)
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		serverError(w, err)
		return
	}
	items := []WalletBalance{}
	for _, id := range ids {
		v, err := loadWallet(r.Context(), s.pool, id)
		if err != nil {
			serverError(w, err)
			return
		}
		items = append(items, v)
	}
	respond(w, 200, items)
}
func (s *Server) ListWithdrawals(w http.ResponseWriter, r *http.Request) {
	items, err := s.withdrawals(r.Context(), "")
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, items)
}
func (s *Server) RequestWithdrawal(w http.ResponseWriter, r *http.Request) {
	if len(s.options.WalletKey) != 32 {
		fail(w, 503, "Withdrawals are not configured. Contact the administrator.")
		return
	}
	var b struct {
		Amount int64  `json:"amount_paise"`
		Key    string `json:"request_key"`
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Amount < 100 || b.Amount > 100000000 || !validUUID(b.Key) {
		fail(w, 400, "Enter an amount from ₹1 to ₹10,00,000 and a valid request identifier.")
		return
	}
	tx, err := s.beginBookingTx(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	doctor := Current(r.Context()).ID
	previous, err := scanWithdrawal(tx.QueryRow(r.Context(), withdrawalSelect+`WHERE w.doctor_id=$1 AND w.request_key=$2`, doctor, b.Key))
	if err == nil {
		if previous.Requested != b.Amount {
			fail(w, 409, "This request identifier already belongs to a different amount.")
			return
		}
		respond(w, 200, s.withdrawalForDisplay(previous))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		serverError(w, err)
		return
	}
	balance, err := loadWallet(r.Context(), tx, doctor)
	if err != nil {
		serverError(w, err)
		return
	}
	if balance.ReconciliationRequired {
		fail(w, 409, "A refund, dispute or negative balance needs administrator reconciliation before another withdrawal.")
		return
	}
	if b.Amount > balance.Available {
		fail(w, 409, "The requested amount exceeds the available balance.")
		return
	}
	var encrypted []byte
	var masked string
	err = tx.QueryRow(r.Context(), `SELECT encrypted_details,masked_details FROM app_private.doctor_destination WHERE doctor_id=$1`, doctor).Scan(&encrypted, &masked)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "Save your payout destination first.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err = s.decryptDestination(doctor, encrypted); err != nil {
		serverError(w, err)
		return
	}
	deduction, bps, policy := s.options.TaxPolicy.withdrawal(b.Amount)
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO app_private.doctor_withdrawal(doctor_id,request_key,requested_paise,deduction_paise,net_paise,deduction_bps,policy,encrypted_destination,masked_destination)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, doctor, b.Key, b.Amount, deduction, b.Amount-deduction, bps, policy, encrypted, masked).Scan(&id)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO app_private.wallet_audit(actor_id,withdrawal_id,action) VALUES ($1,$2,'requested')`, doctor, id)
	}
	if err != nil {
		serverError(w, err)
		return
	}
	value, err := scanWithdrawal(tx.QueryRow(r.Context(), withdrawalSelect+`WHERE w.id=$1`, id))
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 201, value)
}
func (s *Server) CancelWithdrawal(w http.ResponseWriter, r *http.Request) {
	s.changeWithdrawal(w, r, true)
}
func (s *Server) DecideWithdrawal(w http.ResponseWriter, r *http.Request) {
	s.changeWithdrawal(w, r, false)
}
func (s *Server) changeWithdrawal(w http.ResponseWriter, r *http.Request, cancel bool) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, 404, "Withdrawal not found.")
		return
	}
	var b struct {
		Action        string `json:"action"`
		Transferred   int64  `json:"transferred_paise"`
		Reference     string `json:"transfer_reference"`
		TransferredAt string `json:"transferred_at"`
		Confirmed     bool   `json:"transfer_completed"`
		Note          string `json:"note"`
	}
	if !decode(w, r, &b) {
		return
	}
	if cancel {
		b.Action = "cancelled"
	}
	if b.Action != "cancelled" && b.Action != "processing" && b.Action != "paid" && b.Action != "rejected" {
		fail(w, 400, "Choose processing, paid or rejected.")
		return
	}
	if !cancel && b.Action == "cancelled" {
		fail(w, 400, "Use reject to release the request.")
		return
	}
	b.Reference = strings.TrimSpace(b.Reference)
	b.Note = strings.TrimSpace(b.Note)
	if len(b.Note) > 500 || len(b.Reference) > 120 || strings.ContainsFunc(b.Reference, unicode.IsControl) || strings.ContainsRune(b.Note, 0) {
		fail(w, 400, "Use a short transfer reference and note.")
		return
	}
	var at time.Time
	var err error
	if b.Action == "paid" {
		at, err = time.Parse(time.RFC3339, b.TransferredAt)
		at = at.Truncate(time.Microsecond)
		if !b.Confirmed || b.Reference == "" || err != nil || at.After(time.Now().Add(time.Minute)) {
			fail(w, 400, "Confirm the transfer is already complete and enter its reference, amount and actual time.")
			return
		}
	}
	tx, err := s.beginBookingTx(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	value, err := scanWithdrawal(tx.QueryRow(r.Context(), withdrawalSelect+`WHERE w.id=$1 FOR UPDATE OF w`, id))
	if notFound(w, err) {
		return
	}
	actor := Current(r.Context())
	if cancel && value.DoctorID != actor.ID {
		fail(w, 404, "Withdrawal not found.")
		return
	}
	if value.Status == b.Action {
		if b.Action == "paid" && (value.Transferred == nil || *value.Transferred != b.Transferred || value.Reference == nil || *value.Reference != b.Reference || value.TransferredAt == nil || !value.TransferredAt.Equal(at)) {
			fail(w, 409, "This withdrawal was already marked paid with different transfer details.")
			return
		}
		respond(w, 200, value)
		return
	}
	if value.Status != "requested" && value.Status != "processing" {
		fail(w, 409, "This withdrawal has already been decided.")
		return
	}
	if cancel && value.Status != "requested" {
		fail(w, 409, "The administrator is processing this request. Contact the clinic before cancelling.")
		return
	}
	if b.Action == "paid" {
		if value.PolicyPending || s.options.TaxPolicy.WithdrawalApproval == "" || value.Policy != s.options.TaxPolicy.WithdrawalApproval {
			fail(w, 409, "Final payout is pending policy review. Reject this reservation and create a new request under the approved policy; this request cannot be marked paid.")
			return
		}
		// The transfer form records receipt times to the minute.
		if b.Transferred != value.Net || at.Before(value.CreatedAt.Truncate(time.Minute)) {
			fail(w, 400, "The transfer must match the final withdrawable amount and occur after the request.")
			return
		}
		balance, err := loadWallet(r.Context(), tx, value.DoctorID)
		if err != nil {
			serverError(w, err)
			return
		}
		if balance.ReconciliationRequired {
			fail(w, 409, "Reconciliation is required. Do not mark this request paid; contact the clinic's finance administrator.")
			return
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO app_private.doctor_ledger(doctor_id,kind,amount_paise,withdrawal_id,reason) VALUES ($1,'withdrawal',$2,$3,'manual_transfer')`, value.DoctorID, -value.Requested, id)
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE app_private.doctor_withdrawal SET transferred_paise=$2,transfer_reference=$3,transferred_at=$4 WHERE id=$1`, id, b.Transferred, b.Reference, at)
		}
		if err != nil {
			serverError(w, err)
			return
		}
	}
	_, err = tx.Exec(r.Context(), `UPDATE app_private.doctor_withdrawal SET status=$2,decided_by=$3,decision_note=$4,updated_at=now() WHERE id=$1`, id, b.Action, actor.ID, b.Note)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO app_private.wallet_audit(actor_id,withdrawal_id,action) VALUES ($1,$2,$3)`, actor.ID, id, b.Action)
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
func (s *Server) RevealWithdrawalDestination(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, 404, "Withdrawal not found.")
		return
	}
	var b struct {
		Confirmed bool `json:"manual_transfer_review"`
	}
	if !decode(w, r, &b) {
		return
	}
	if !b.Confirmed {
		fail(w, 400, "Confirm you are reviewing the destination for a manual transfer.")
		return
	}
	tx, err := s.beginBookingTx(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var doctor, status string
	var encrypted []byte
	err = tx.QueryRow(r.Context(), `SELECT doctor_id,encrypted_destination,status FROM app_private.doctor_withdrawal WHERE id=$1 FOR UPDATE`, id).Scan(&doctor, &encrypted, &status)
	if notFound(w, err) {
		return
	}
	if status != "requested" && status != "processing" {
		fail(w, 409, "Only pending manual transfers allow destination review.")
		return
	}
	d, err := s.decryptDestination(doctor, encrypted)
	if err != nil {
		serverError(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO app_private.wallet_audit(actor_id,withdrawal_id,action) VALUES ($1,$2,'destination_revealed')`, Current(r.Context()).ID, id)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, d)
}

// The caller already holds the booking lock, so capture, refund and withdrawals
// cannot race the balance transition. Unique source identities also guard replay.
func (s *Server) applyEarning(ctx context.Context, tx pgx.Tx, p Payment, state, appointment string) error {
	if state == "paid" {
		_, err := tx.Exec(ctx, `INSERT INTO app_private.doctor_ledger(doctor_id,kind,amount_paise,appointment_id,order_id,payment_id,reason)
            SELECT a.earning_doctor_id,'earning',a.earning_paise,a.id,$2,$3,'verified_capture'
            FROM app_private.appointment a JOIN app_private.consultation_order o ON o.id=$2 AND o.appointment_id=a.id
            WHERE a.id=$1 AND a.earning_policy='fixed-v1' AND a.earning_paise>0
                AND a.earning_doctor_id=a.doctor_id AND a.status IN ('paid_pending_admin','confirmed')
                AND o.created_at>=a.created_at AND NOT EXISTS(SELECT 1 FROM app_private.wallet_reconciliation WHERE appointment_id=a.id)
            ON CONFLICT DO NOTHING`, appointment, p.OrderID, p.ID)
		if err != nil {
			return fmt.Errorf("record captured earning: %w", err)
		}
	}
	if state == "partially_refunded" {
		// Allocation of a partial refund requires a business decision, not a ledger guess.
		_, err := tx.Exec(ctx, `INSERT INTO app_private.wallet_reconciliation(appointment_id,doctor_id,reason)
            SELECT id,earning_doctor_id,'partial_refund' FROM app_private.appointment
            WHERE id=$1 AND earning_doctor_id IS NOT NULL AND earning_paise>0
            ON CONFLICT DO NOTHING`, appointment)
		return err
	}
	if state == "refunded" {
		if err := reverseEarning(ctx, tx, appointment, "patient_refund"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM app_private.wallet_reconciliation WHERE appointment_id=$1 AND reason='partial_refund'`, appointment)
		return err
	}
	return nil
}
func reverseEarning(ctx context.Context, tx pgx.Tx, appointment, reason string) error {
	_, err := tx.Exec(ctx, `INSERT INTO app_private.doctor_ledger(doctor_id,kind,amount_paise,appointment_id,order_id,payment_id,reason)
        SELECT doctor_id,'reversal',-amount_paise,appointment_id,order_id,payment_id,$2
        FROM app_private.doctor_ledger WHERE appointment_id=$1 AND kind='earning'
        ON CONFLICT(appointment_id,kind) DO NOTHING`, appointment, reason)
	if err != nil {
		return fmt.Errorf("reverse consultation earning: %w", err)
	}
	return nil
}
