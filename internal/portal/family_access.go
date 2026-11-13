package portal

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const familyAccessLifetime = 15 * time.Minute

func (s *Server) IssueFamilyAccess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	var body struct {
		GuardianVerified bool `json:"guardian_verified"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !body.GuardianVerified {
		fail(w, http.StatusBadRequest, "Verify the guardian's identity before creating an access link.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var found string
	err = tx.QueryRow(r.Context(), `SELECT r.id FROM app_private.consultation_registration r
		WHERE r.id=$1 AND (r.guardian_id IS NULL OR EXISTS (
			SELECT 1 FROM app_private.guardian_account g WHERE g.id=r.guardian_id AND g.active))
		FOR UPDATE`, id).Scan(&found)
	if notFound(w, err) {
		return
	}
	secret := token()
	expires := time.Now().Add(familyAccessLifetime)
	_, err = tx.Exec(r.Context(), `INSERT INTO app_private.family_access_grant(registration_id,token_hash,expires_at,issued_by)
		VALUES ($1,$2,$3,$4) ON CONFLICT (registration_id) DO UPDATE
		SET token_hash=EXCLUDED.token_hash,expires_at=EXCLUDED.expires_at,issued_by=EXCLUDED.issued_by,created_at=now()`, id, tokenHash(secret), expires, Current(r.Context()).ID)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"token": secret, "expires_at": expires})
}

func (s *Server) RedeemFamilyAccess(w http.ResponseWriter, r *http.Request) {
	const accessError = "This access link or the child details could not be verified. Check the details or ask the clinic for a new link."
	var body struct {
		GuardianName string `json:"guardian_name"`
		ChildName    string `json:"child_name"`
		DateOfBirth  string `json:"date_of_birth"`
		Token        string `json:"token"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.GuardianName = strings.Join(strings.Fields(body.GuardianName), " ")
	body.ChildName = strings.Join(strings.Fields(body.ChildName), " ")
	body.DateOfBirth = strings.TrimSpace(body.DateOfBirth)
	body.Token = strings.TrimSpace(body.Token)
	_, dateErr := time.Parse("2006-01-02", body.DateOfBirth)
	if !validName(body.GuardianName) || !validName(body.ChildName) || !validToken(body.Token) || dateErr != nil {
		fail(w, http.StatusUnauthorized, accessError)
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	// Issuance and redemption lock the registration first, then the grant.
	err = tx.QueryRow(r.Context(), `SELECT r.id FROM app_private.consultation_registration r
		JOIN app_private.family_access_grant a ON a.registration_id=r.id
		WHERE a.token_hash=$1 AND a.expires_at>clock_timestamp()
		AND lower(regexp_replace(btrim(r.child_name),'\s+',' ','g'))=lower($2) AND r.date_of_birth=$3 AND lower(regexp_replace(btrim(r.guardian_name),'\s+',' ','g'))=lower($4)
		AND (r.guardian_id IS NULL OR EXISTS (SELECT 1 FROM app_private.guardian_account g WHERE g.id=r.guardian_id AND g.active))
		FOR UPDATE OF r`, tokenHash(body.Token), body.ChildName, body.DateOfBirth, body.GuardianName).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusUnauthorized, accessError)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	result, err := tx.Exec(r.Context(), `DELETE FROM app_private.family_access_grant
		WHERE registration_id=$1 AND token_hash=$2 AND expires_at>clock_timestamp()`, id, tokenHash(body.Token))
	if err != nil {
		serverError(w, err)
		return
	}
	if result.RowsAffected() != 1 {
		fail(w, http.StatusUnauthorized, accessError)
		return
	}
	secret := token()
	if _, err = tx.Exec(r.Context(), `UPDATE app_private.consultation_registration SET token_hash=$2,updated_at=now() WHERE id=$1`, id, tokenHash(secret)); err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM app_private.guardian_session WHERE registration_id=$1`, id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"id": id, "token": secret})
}
