package portal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const guardianSessionCookie = "madamgy_guardian"

type Guardian struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	Active         bool   `json:"active"`
	RegistrationID string `json:"registration_id,omitempty"`
}

func (s *Server) ListGuardians(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT id,name,email,active FROM app_private.guardian_account ORDER BY name,id`)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	items := make([]Guardian, 0)
	for rows.Next() {
		var item Guardian
		if err := rows.Scan(&item.ID, &item.Name, &item.Email, &item.Active); err != nil {
			serverError(w, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, items)
}

type guardianKey struct{}

func CurrentGuardian(ctx context.Context) Guardian {
	g, _ := ctx.Value(guardianKey{}).(Guardian)
	return g
}

func guardianRequestToken(r *http.Request) string {
	c, err := r.Cookie(guardianSessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Server) setGuardianCookie(w http.ResponseWriter, value string, maxAge int) {
	c := &http.Cookie{
		Name: guardianSessionCookie, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.options.SecureCookies, SameSite: http.SameSiteLaxMode,
	}
	if maxAge < 0 {
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}

func (s *Server) RequireGuardian(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		value := guardianRequestToken(r)
		if !validToken(value) {
			fail(w, http.StatusUnauthorized, "Sign in to continue.")
			return
		}
		var g Guardian
		err := s.pool.QueryRow(r.Context(), `
			SELECT a.id, a.name, CASE WHEN s.registration_id IS NULL THEN a.email ELSE coalesce(r.email,'') END, a.active,coalesce(s.registration_id::text,'')
			FROM app_private.guardian_session s
			JOIN app_private.guardian_account a ON a.id=s.guardian_id
			LEFT JOIN app_private.consultation_registration r ON r.id=s.registration_id AND r.guardian_id=a.id
			WHERE s.token_hash=$1 AND s.expires_at > now() AND a.active AND (s.registration_id IS NULL OR r.id IS NOT NULL)`, tokenHash(value)).
			Scan(&g.ID, &g.Name, &g.Email, &g.Active, &g.RegistrationID)
		if errors.Is(err, pgx.ErrNoRows) {
			s.setGuardianCookie(w, "", -1)
			fail(w, http.StatusUnauthorized, "Your family session ended. Sign in again.")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), guardianKey{}, g)))
	})
}

func (s *Server) ProvisionGuardian(ctx context.Context, name, email, password string) (Guardian, error) {
	g := Guardian{Name: strings.TrimSpace(name), Email: strings.ToLower(strings.TrimSpace(email)), Active: true}
	if !validName(g.Name) || !validEmail(g.Email) || !validPassword(password) {
		return g, errors.New("use a name, email and password of 12 to 256 characters")
	}
	identity, err := s.options.Identity.Create(ctx, g.Email, password)
	if err != nil {
		return g, fmt.Errorf("create guardian identity: %w", err)
	}
	g.ID = identity.ID
	_, err = s.pool.Exec(ctx, `INSERT INTO app_private.guardian_account(id,name,email) VALUES ($1,$2,$3)`, g.ID, g.Name, g.Email)
	if err != nil {
		if cleanupErr := s.options.Identity.Delete(ctx, g.ID); cleanupErr != nil {
			slog.Error("remove incomplete guardian identity", "user_id", g.ID, "error", cleanupErr)
		}
		return g, fmt.Errorf("save guardian account: %w", err)
	}
	return g, nil
}

func (s *Server) createGuardianSession(ctx context.Context, g Guardian) (string, error) {
	v := token()
	_, err := s.pool.Exec(ctx, `INSERT INTO app_private.guardian_session(token_hash,guardian_id,expires_at) VALUES ($1,$2,$3)`, tokenHash(v), g.ID, time.Now().Add(sessionLifetime))
	if err != nil {
		return "", err
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM app_private.guardian_session WHERE expires_at < now()`)
	return v, nil
}

func (s *Server) GuardianSignup(w http.ResponseWriter, r *http.Request) {
	if !s.options.Identity.Enabled() {
		fail(w, http.StatusServiceUnavailable, "Family sign-in is not configured yet.")
		return
	}
	var body struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if !validName(body.Name) || !validEmail(body.Email) || !validPassword(body.Password) {
		fail(w, http.StatusBadRequest, "Enter a name, email and password of 12 to 256 characters.")
		return
	}
	if !s.limits.allow("guardian-signup:"+tokenHash(body.Email), 5) {
		fail(w, http.StatusTooManyRequests, "Too many account attempts. Try again in a minute.")
		return
	}
	g, err := s.ProvisionGuardian(r.Context(), body.Name, body.Email, body.Password)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			fail(w, http.StatusConflict, "A family account already uses that email.")
			return
		}
		fail(w, http.StatusBadGateway, "Family account could not be created. Try again shortly.")
		return
	}
	v, err := s.createGuardianSession(r.Context(), g)
	if err != nil {
		serverError(w, err)
		return
	}
	s.setGuardianCookie(w, v, int(sessionLifetime.Seconds()))
	respond(w, http.StatusCreated, g)
}

func (s *Server) GuardianLogin(w http.ResponseWriter, r *http.Request) {
	if !s.options.Identity.Enabled() {
		fail(w, http.StatusServiceUnavailable, "Family sign-in is not configured yet.")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if !validEmail(body.Email) || len(body.Password) == 0 || len(body.Password) > 256 {
		fail(w, http.StatusBadRequest, "Enter an email address and password.")
		return
	}
	if !s.limits.allow("guardian-login:"+tokenHash(body.Email), 10) {
		fail(w, http.StatusTooManyRequests, "Too many sign-in attempts. Try again in a minute.")
		return
	}
	var g Guardian
	err := s.pool.QueryRow(r.Context(), `SELECT id,name,email,active FROM app_private.guardian_account WHERE email=$1 AND active`, body.Email).Scan(&g.ID, &g.Name, &g.Email, &g.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusUnauthorized, "Email or password is incorrect.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	identity, err := s.options.Identity.SignIn(r.Context(), body.Email, body.Password)
	if errors.Is(err, ErrCredentials) || (err == nil && identity.ID != g.ID) {
		fail(w, http.StatusUnauthorized, "Email or password is incorrect.")
		return
	}
	if err != nil {
		fail(w, http.StatusBadGateway, "Sign-in is temporarily unavailable.")
		return
	}
	v, err := s.createGuardianSession(r.Context(), g)
	if err != nil {
		serverError(w, err)
		return
	}
	s.setGuardianCookie(w, v, int(sessionLifetime.Seconds()))
	respond(w, http.StatusOK, g)
}

// GuardianChildAccess is the account-free family entry point. The registration
// token is the private second factor, so a name and birth date alone can never
// open a child's records.
func (s *Server) GuardianChildAccess(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChildName   string `json:"child_name"`
		DateOfBirth string `json:"date_of_birth"`
		Token       string `json:"token"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.ChildName = strings.Join(strings.Fields(body.ChildName), " ")
	body.DateOfBirth = strings.TrimSpace(body.DateOfBirth)
	body.Token = strings.TrimSpace(body.Token)
	if !validName(body.ChildName) || !validToken(body.Token) {
		fail(w, http.StatusUnauthorized, "The child details or private token are incorrect.")
		return
	}
	if _, err := time.Parse("2006-01-02", body.DateOfBirth); err != nil {
		fail(w, http.StatusUnauthorized, "The child details or private token are incorrect.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var registrationID string
	var guardianID sql.NullString
	var guardianName, guardianEmail sql.NullString
	err = tx.QueryRow(r.Context(), `SELECT id,guardian_id::text,guardian_name,coalesce(email,'')
		FROM app_private.consultation_registration
		WHERE lower(regexp_replace(btrim(child_name),'\s+',' ','g'))=lower($1) AND date_of_birth=$2 AND token_hash=$3
		FOR UPDATE`, body.ChildName, body.DateOfBirth, tokenHash(body.Token)).Scan(&registrationID, &guardianID, &guardianName, &guardianEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusUnauthorized, "The child details or private token are incorrect.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if !guardianID.Valid {
		guardianID.String = ""
		err = tx.QueryRow(r.Context(), `INSERT INTO app_private.guardian_account(id,name,email)
			VALUES (gen_random_uuid(),$1,'access+' || $2 || '@family.invalid')
			RETURNING id::text`, guardianName.String, registrationID).Scan(&guardianID.String)
		guardianID.Valid = err == nil
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE app_private.consultation_registration SET guardian_id=$2,updated_at=now() WHERE id=$1`, registrationID, guardianID.String)
		}
	}
	if err != nil {
		serverError(w, err)
		return
	}
	var active bool
	if err = tx.QueryRow(r.Context(), `SELECT active FROM app_private.guardian_account WHERE id=$1`, guardianID.String).Scan(&active); err != nil {
		serverError(w, err)
		return
	}
	if !active {
		fail(w, 401, "The child details or private token are incorrect.")
		return
	}
	g := Guardian{ID: guardianID.String, Name: guardianName.String, Email: guardianEmail.String, Active: true, RegistrationID: registrationID}
	v := token()
	if _, err = tx.Exec(r.Context(), `INSERT INTO app_private.guardian_session(token_hash,guardian_id,registration_id,expires_at) VALUES ($1,$2,$3,$4)`, tokenHash(v), g.ID, registrationID, time.Now().Add(sessionLifetime)); err != nil {
		serverError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	s.setGuardianCookie(w, v, int(sessionLifetime.Seconds()))
	respond(w, http.StatusOK, g)
}

func (s *Server) GuardianLogout(w http.ResponseWriter, r *http.Request) {
	if _, err := s.pool.Exec(r.Context(), `DELETE FROM app_private.guardian_session WHERE token_hash=$1`, tokenHash(guardianRequestToken(r))); err != nil {
		serverError(w, err)
		return
	}
	s.setGuardianCookie(w, "", -1)
	respond(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) GuardianMe(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, CurrentGuardian(r.Context()))
}

type familyRegistrationInput struct {
	ChildName   string `json:"child_name"`
	DateOfBirth string `json:"date_of_birth"`
	Phone       string `json:"phone"`
}

func (s *Server) CreateFamilyRegistration(w http.ResponseWriter, r *http.Request) {
	if CurrentGuardian(r.Context()).RegistrationID != "" {
		fail(w, 403, "Sign in with your family account to manage other registrations.")
		return
	}
	var body familyRegistrationInput
	if !decode(w, r, &body) {
		return
	}
	g := CurrentGuardian(r.Context())
	privateToken := token()
	input := Intake{GuardianName: g.Name, ChildName: body.ChildName, DateOfBirth: body.DateOfBirth, Phone: body.Phone, Email: g.Email, Token: privateToken}
	if message := input.validate(time.Now()); message != "" {
		fail(w, http.StatusBadRequest, message)
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO app_private.consultation_registration
		(token_hash,guardian_id,guardian_name,child_name,date_of_birth,phone,email)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, tokenHash(privateToken), g.ID,
		input.GuardianName, input.ChildName, input.DateOfBirth, input.Phone, input.Email).Scan(&id)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO public.child_profile(child_id,display_name,date_of_birth,created_by) VALUES ($1,$2,$3,'guardian_account')`, id, input.ChildName, input.DateOfBirth)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE app_private.consultation_registration SET child_id=$1 WHERE id=$1::uuid`, id)
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) AttachFamilyRegistration(w http.ResponseWriter, r *http.Request) {
	if CurrentGuardian(r.Context()).RegistrationID != "" {
		fail(w, 403, "Sign in with your family account to manage other registrations.")
		return
	}
	id := chi.URLParam(r, "id")
	if id != "" && !validUUID(id) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !validToken(body.Token) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	var attachedID string
	if id != "" {
		err := s.pool.QueryRow(r.Context(), `UPDATE app_private.consultation_registration SET guardian_id=$2,updated_at=now()
			WHERE id=$1 AND token_hash=$3 AND guardian_id IS NULL RETURNING id`, id, CurrentGuardian(r.Context()).ID, tokenHash(body.Token)).Scan(&attachedID)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, http.StatusNotFound, "Registration not found or already attached.")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
	} else {
		err := s.pool.QueryRow(r.Context(), `UPDATE app_private.consultation_registration SET guardian_id=$1,updated_at=now()
			WHERE token_hash=$2 AND guardian_id IS NULL RETURNING id`, CurrentGuardian(r.Context()).ID, tokenHash(body.Token)).Scan(&attachedID)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, http.StatusNotFound, "Registration not found or already attached.")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
	}
	respond(w, http.StatusOK, map[string]string{"id": attachedID})
}

func (s *Server) AttachRegistrationAdmin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	var body struct {
		GuardianID string `json:"guardian_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.GuardianID = strings.TrimSpace(body.GuardianID)
	if body.GuardianID != "" && !validUUID(body.GuardianID) {
		fail(w, http.StatusBadRequest, "Choose a family account.")
		return
	}
	if body.GuardianID != "" {
		var active bool
		err := s.pool.QueryRow(r.Context(), `SELECT active FROM app_private.guardian_account WHERE id=$1`, body.GuardianID).Scan(&active)
		if errors.Is(err, pgx.ErrNoRows) || !active {
			fail(w, http.StatusBadRequest, "Choose an active family account.")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
	}
	result, err := s.pool.Exec(r.Context(), `UPDATE app_private.consultation_registration
		SET guardian_id=NULLIF($2,'')::uuid,updated_at=now() WHERE id=$1`, id, body.GuardianID)
	if err != nil {
		serverError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	respond(w, http.StatusOK, map[string]string{"guardian_id": body.GuardianID})
}
