package portal

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
)

func validMeetingURL(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 2048 || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Opaque == "" && !strings.Contains(u.Host, "\\")
}
func (s *Server) SaveMeeting(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, 404, "Appointment not found.")
		return
	}
	var b struct {
		URL            string  `json:"meeting_url"`
		ExpectedDoctor string  `json:"expected_doctor_id"`
		ExpectedURL    *string `json:"expected_meeting_url"`
	}
	if !decode(w, r, &b) {
		return
	}
	b.URL = strings.TrimSpace(b.URL)
	if !validMeetingURL(b.URL) {
		fail(w, 400, "Use a full HTTPS meeting link without embedded credentials, or leave it blank.")
		return
	}
	tx, err := s.beginBookingTx(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a := Current(r.Context())
	var status, doctor, currentURL string
	err = tx.QueryRow(r.Context(), `SELECT status,doctor_id,meeting_url FROM app_private.appointment WHERE id=$1 AND ($2='admin' OR doctor_id=$3) FOR UPDATE`, id, a.Role, a.ID).Scan(&status, &doctor, &currentURL)
	if notFound(w, err) {
		return
	}
	if status != "awaiting_payment" && status != "confirmed" && status != "paid_pending_admin" && status != "pending_admin" {
		fail(w, 409, "Meeting links can be changed for pending or confirmed appointments.")
		return
	}
	if b.ExpectedURL == nil || b.ExpectedDoctor != doctor || *b.ExpectedURL != currentURL {
		fail(w, 409, "The appointment or meeting link changed. Refresh and review it before saving.")
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE app_private.appointment SET meeting_url=$2,meeting_updated_by=$3,meeting_updated_at=now(),updated_at=now() WHERE id=$1`, id, b.URL, a.ID)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
