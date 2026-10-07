package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type BookRelease struct {
	ID             string     `json:"id"`
	RegistrationID string     `json:"registration_id"`
	ChildName      string     `json:"child_name"`
	Book           string     `json:"book"`
	Status         string     `json:"status"`
	GeneratedBy    string     `json:"generated_by"`
	ApprovedBy     string     `json:"approved_by"`
	GeneratedAt    time.Time  `json:"generated_at"`
	DecidedAt      *time.Time `json:"decided_at"`
	SizeBytes      int        `json:"size_bytes"`
}

func (s *Server) SaveBookReleases(ctx context.Context, registrationID, childID, generatedBy string, book1, book2 []byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var storedChild string
	if err := tx.QueryRow(ctx, `SELECT coalesce(child_id,'') FROM app_private.consultation_registration WHERE id=$1 FOR UPDATE`, registrationID).Scan(&storedChild); err != nil {
		return err
	}
	if storedChild == "" || storedChild != childID {
		return fmt.Errorf("registration child does not match generated book")
	}
	for _, item := range []struct {
		book string
		pdf  []byte
	}{
		{book: "book1", pdf: book1},
		{book: "book2", pdf: book2},
	} {
		if len(item.pdf) == 0 {
			return fmt.Errorf("%s PDF is empty", item.book)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app_private.book_release(registration_id,child_id,book,pdf,generated_by)
			VALUES ($1,$2,$3,$4,$5)`, registrationID, childID, item.book, item.pdf, generatedBy); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Server) ListBookReleases(w http.ResponseWriter, r *http.Request) {
	a := Current(r.Context())
	registrationID := r.URL.Query().Get("registration_id")
	if registrationID != "" && !validUUID(registrationID) {
		fail(w, http.StatusBadRequest, "Registration not found.")
		return
	}
	rows, err := s.pool.Query(r.Context(), `SELECT b.id,b.registration_id,r.child_name,b.book,b.status,
		b.generated_by::text,coalesce(b.approved_by::text,''),b.generated_at,b.decided_at,octet_length(b.pdf)
		FROM app_private.book_release b JOIN app_private.consultation_registration r ON r.id=b.registration_id
		WHERE ($1='admin' OR r.assigned_doctor_id=$2) AND ($3='' OR b.registration_id::text=$3)
		ORDER BY b.generated_at DESC,b.id DESC`, a.Role, a.ID, registrationID)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	items := make([]BookRelease, 0)
	for rows.Next() {
		var item BookRelease
		if err := rows.Scan(&item.ID, &item.RegistrationID, &item.ChildName, &item.Book, &item.Status,
			&item.GeneratedBy, &item.ApprovedBy, &item.GeneratedAt, &item.DecidedAt, &item.SizeBytes); err != nil {
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

func (s *Server) DecideBookRelease(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Book release not found.")
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Action != "approve" && body.Action != "reject" {
		fail(w, http.StatusBadRequest, "Choose approve or reject.")
		return
	}
	status := "rejected"
	if body.Action == "approve" {
		status = "approved"
	}
	var approvedBy any
	if body.Action == "approve" {
		approvedBy = Current(r.Context()).ID
	}
	result, err := s.pool.Exec(r.Context(), `UPDATE app_private.book_release
		SET status=$2,approved_by=$3,decided_by=$4,decided_at=now()
		WHERE id=$1 AND status='pending_admin'`, id, status, approvedBy, Current(r.Context()).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		fail(w, http.StatusConflict, "This book release has already been decided.")
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) StaffBookReleaseDownload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Book release not found.")
		return
	}
	a := Current(r.Context())
	var pdf []byte
	var book, childName, status string
	err := s.pool.QueryRow(r.Context(), `SELECT b.pdf,b.book,r.child_name,b.status
		FROM app_private.book_release b JOIN app_private.consultation_registration r ON r.id=b.registration_id
		WHERE b.id=$1 AND ($2='admin' OR r.assigned_doctor_id=$3)`, id, a.Role, a.ID).Scan(&pdf, &book, &childName, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "Book release not found.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	filename := "book-" + book + "-" + strconv.FormatInt(time.Now().Unix(), 10) + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("X-Book-Status", status)
	w.Header().Set("X-Book-Child", childName)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

func (s *Server) FamilyBookDownload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Book release not found.")
		return
	}
	var pdf []byte
	var book, childName string
	err := s.pool.QueryRow(r.Context(), `SELECT b.pdf,b.book,r.child_name
		FROM app_private.book_release b JOIN app_private.consultation_registration r ON r.id=b.registration_id
		WHERE b.id=$1 AND b.status='approved' AND r.guardian_id=$2 AND ($3='' OR r.id::text=$3)`, id, CurrentGuardian(r.Context()).ID, CurrentGuardian(r.Context()).RegistrationID).Scan(&pdf, &book, &childName)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "Book release not found.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	filename := "book-" + book + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("X-Book-Child", childName)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}
