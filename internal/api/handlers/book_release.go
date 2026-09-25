package handlers

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/portal"
)

var releaseUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (h *Handlers) GenerateBookReleases(w http.ResponseWriter, r *http.Request) {
	registrationID := chi.URLParam(r, "registrationID")
	if !releaseUUIDPattern.MatchString(registrationID) {
		writeError(w, http.StatusNotFound, "Registration not found.")
		return
	}
	actor := portal.Current(r.Context())
	var childID, childName string
	var dateOfBirth time.Time
	err := h.pool.QueryRow(r.Context(), `SELECT coalesce(r.child_id,''),r.child_name,r.date_of_birth
		FROM app_private.consultation_registration r
		WHERE r.id=$1 AND ($2='admin' OR r.assigned_doctor_id=$3)`, registrationID, actor.Role, actor.ID).
		Scan(&childID, &childName, &dateOfBirth)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Registration not found.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registration lookup failed: "+err.Error())
		return
	}
	if childID == "" {
		writeError(w, http.StatusConflict, "This registration has no child record yet.")
		return
	}
	s, photo, parentsPhoto, prescriptionPhoto, ok := h.decodeGenerate(w, r)
	if !ok {
		return
	}
	if s.ChildID != "" && s.ChildID != childID {
		writeError(w, http.StatusBadRequest, "The generated book must use this registration's child record.")
		return
	}
	if s.DisplayName != "" && s.DisplayName != childName {
		writeError(w, http.StatusBadRequest, "The generated book name must match the registration.")
		return
	}
	if s.DateOfBirth.UTC().Format(dateLayout) != dateOfBirth.UTC().Format(dateLayout) {
		writeError(w, http.StatusBadRequest, "The generated book date of birth must match the registration.")
		return
	}
	s.ChildID = childID
	s.DisplayName = childName
	s.DateOfBirth = dateOfBirth
	resp, set, ok := h.renderSetWithPhotos(w, r, s, photo, parentsPhoto, prescriptionPhoto)
	if !ok {
		return
	}
	pdfs, err := book.PrintPDFAll(r.Context(), []book.PrintJob{
		{Name: "book1", HTML: []byte(resp.Book1), Metadata: set.Book1.Metadata},
		{Name: "book2", HTML: []byte(resp.Book2), Metadata: set.Book2.Metadata},
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, book.ErrChromiumUnavailable) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, "the books could not be printed: "+err.Error())
		return
	}
	if len(pdfs) != 2 {
		writeError(w, http.StatusInternalServerError, "the books could not be printed")
		return
	}
	if err := portal.New(h.pool, portal.Options{}).SaveBookReleases(r.Context(), registrationID, childID, actor.ID, pdfs[0], pdfs[1]); err != nil {
		writeError(w, http.StatusInternalServerError, "the book release could not be saved: "+err.Error())
		return
	}
	out := printedSetResponse{bookSetResponse: resp}
	out.Book1PDF = printedBook{PDF: pdfs[0]}
	out.Book2PDF = printedBook{PDF: pdfs[1]}
	writeJSON(w, http.StatusCreated, out)
}
