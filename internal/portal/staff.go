package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func validPassword(p string) bool { return len(p) >= 12 && len(p) <= 256 }

func (s *Server) Provision(ctx context.Context, name, email, password, role string) (Actor, error) {
	a := Actor{Name: strings.TrimSpace(name), Email: strings.ToLower(strings.TrimSpace(email)), Role: role, Active: true}
	if !validName(a.Name) || !validEmail(a.Email) || !validPassword(password) || (role != "doctor" && role != "admin") {
		return a, errors.New("use a name, email and password of 12 to 256 characters")
	}
	identity, err := s.options.Identity.Create(ctx, a.Email, password)
	if err != nil {
		return a, fmt.Errorf("create staff identity: %w", err)
	}
	a.ID = identity.ID
	_, err = s.pool.Exec(ctx, `INSERT INTO app_private.staff_account(id,name,email,role) VALUES ($1,$2,$3,$4)`, a.ID, a.Name, a.Email, a.Role)
	if err != nil {
		if cleanupErr := s.options.Identity.Delete(ctx, a.ID); cleanupErr != nil {
			slog.Error("remove incomplete staff identity", "user_id", a.ID, "error", cleanupErr)
		}
		return a, fmt.Errorf("save staff account: %w", err)
	}
	return a, nil
}
func (s *Server) ListStaff(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT id,name,email,role,active FROM app_private.staff_account ORDER BY active DESC,role,name,id`)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	items := []Actor{}
	for rows.Next() {
		var a Actor
		if err := rows.Scan(&a.ID, &a.Name, &a.Email, &a.Role, &a.Active); err != nil {
			serverError(w, err)
			return
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, items)
}
func (s *Server) CreateStaff(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &b) {
		return
	}
	if !validName(strings.TrimSpace(b.Name)) || !validEmail(strings.TrimSpace(b.Email)) || !validPassword(b.Password) {
		fail(w, 400, "Enter a name, email and password of 12 to 256 characters.")
		return
	}
	a, err := s.Provision(r.Context(), b.Name, b.Email, b.Password, "doctor")
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			fail(w, 409, "A staff account already uses that email.")
			return
		}
		slog.Error("create doctor", "error", err)
		fail(w, 502, "Could not create this account. Check whether the email already exists in Supabase.")
		return
	}
	respond(w, 201, a)
}
func (s *Server) UpdateStaff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, 404, "Doctor not found.")
		return
	}
	var b struct {
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	if !decode(w, r, &b) {
		return
	}
	if !validName(strings.TrimSpace(b.Name)) {
		fail(w, 400, "Enter a name of at most 100 characters.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), `UPDATE app_private.staff_account SET name=$2,active=$3 WHERE id=$1 AND role='doctor'`, id, strings.TrimSpace(b.Name), b.Active)
	if err != nil {
		serverError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		fail(w, 404, "Doctor not found.")
		return
	}
	if !b.Active {
		if _, err = tx.Exec(r.Context(), `DELETE FROM app_private.staff_session WHERE staff_id=$1`, id); err != nil {
			serverError(w, err)
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE app_private.consultation_registration SET assigned_doctor_id=NULL,updated_at=now() WHERE assigned_doctor_id=$1`, id); err != nil {
			serverError(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, 404, "Doctor not found.")
		return
	}
	var b struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &b) {
		return
	}
	if !validPassword(b.Password) {
		fail(w, 400, "Use a password of 12 to 256 characters.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var found string
	if err := tx.QueryRow(r.Context(), `SELECT id FROM app_private.staff_account WHERE id=$1 AND role='doctor' FOR UPDATE`, id).Scan(&found); notFound(w, err) {
		return
	}
	if err := s.options.Identity.Password(r.Context(), id, b.Password); err != nil {
		slog.Error("reset doctor password", "error", err)
		fail(w, 502, "Password could not be updated.")
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM app_private.staff_session WHERE staff_id=$1`, id); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var b struct {
		CurrentPassword string `json:"current_password"`
		Password        string `json:"password"`
	}
	if !decode(w, r, &b) {
		return
	}
	if !validPassword(b.Password) || len(b.CurrentPassword) > 256 {
		fail(w, 400, "Use a new password of 12 to 256 characters.")
		return
	}
	a := Current(r.Context())
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var found string
	if err := tx.QueryRow(r.Context(), `SELECT id FROM app_private.staff_account WHERE id=$1 AND active FOR UPDATE`, a.ID).Scan(&found); notFound(w, err) {
		return
	}
	identity, err := s.options.Identity.SignIn(r.Context(), a.Email, b.CurrentPassword)
	if err != nil || identity.ID != a.ID {
		fail(w, 400, "Current password could not be verified.")
		return
	}
	if err := s.options.Identity.Password(r.Context(), a.ID, b.Password); err != nil {
		slog.Error("change password", "error", err)
		fail(w, 502, "Password could not be updated.")
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM app_private.staff_session WHERE staff_id=$1`, a.ID); err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	s.setCookie(w, "", -1)
	respond(w, 200, map[string]bool{"ok": true})
}

type Settings struct {
	AmountPaise       *int   `json:"amount_paise"`
	Currency          string `json:"currency"`
	PaymentsEnabled   bool   `json:"payments_enabled"`
	CheckoutAvailable bool   `json:"checkout_available"`
	GatewayConfigured bool   `json:"gateway_configured,omitempty"`
}

func (s *Server) settings(ctx context.Context) (Settings, error) {
	var v Settings
	err := s.pool.QueryRow(ctx, `SELECT amount_paise,currency,payments_enabled FROM app_private.consultation_settings WHERE singleton`).Scan(&v.AmountPaise, &v.Currency, &v.PaymentsEnabled)
	v.GatewayConfigured = s.options.Gateway.Enabled()
	v.CheckoutAvailable = v.PaymentsEnabled && v.AmountPaise != nil && v.GatewayConfigured
	return v, err
}
func (s *Server) PublicSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.settings(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]any{"amount_paise": v.AmountPaise, "currency": v.Currency, "checkout_available": v.CheckoutAvailable})
}
func (s *Server) AdminSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.settings(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, v)
}
func (s *Server) SaveSettings(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AmountPaise     *int `json:"amount_paise"`
		PaymentsEnabled bool `json:"payments_enabled"`
	}
	if !decode(w, r, &b) {
		return
	}
	if (b.PaymentsEnabled && b.AmountPaise == nil) || (b.AmountPaise != nil && (*b.AmountPaise < 100 || *b.AmountPaise > 100000000)) {
		fail(w, 400, "Enter a consultation fee between ₹1 and ₹10,00,000, or leave payments disabled.")
		return
	}
	result, err := s.pool.Exec(r.Context(), `UPDATE app_private.consultation_settings SET amount_paise=$1,payments_enabled=$2,updated_by=$3,updated_at=now() WHERE singleton`, b.AmountPaise, b.PaymentsEnabled, Current(r.Context()).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		serverError(w, pgx.ErrNoRows)
		return
	}
	s.AdminSettings(w, r)
}
