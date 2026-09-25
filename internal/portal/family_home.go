package portal

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type FamilyRegistration struct {
	ID                  string     `json:"id"`
	ChildName           string     `json:"child_name"`
	DateOfBirth         string     `json:"date_of_birth"`
	RegistrationStatus  string     `json:"registration_status"`
	PaymentStatus       string     `json:"payment_status"`
	AmountPaise         *int       `json:"amount_paise"`
	Currency            string     `json:"currency"`
	DoctorName          string     `json:"doctor_name"`
	AppointmentID       *string    `json:"appointment_id"`
	AppointmentStatus   *string    `json:"appointment_status"`
	AppointmentStartsAt *time.Time `json:"appointment_starts_at"`
	AppointmentEndsAt   *time.Time `json:"appointment_ends_at"`
	Book1ReleaseID      *string    `json:"book1_release_id"`
	Book1Status         *string    `json:"book1_status"`
	Book2ReleaseID      *string    `json:"book2_release_id"`
	Book2Status         *string    `json:"book2_status"`
	CreatedAt           time.Time  `json:"created_at"`
}

const familyRegistrationQuery = `
	SELECT r.id,r.child_name,to_char(r.date_of_birth,'YYYY-MM-DD'),r.status,
		coalesce(o.status,'unpaid'),o.amount_paise,coalesce(o.currency,'INR'),
		coalesce(d.name,''),
		a.id,a.status,a.starts_at,a.ends_at,
		b1.id,b1.status,b2.id,b2.status,r.created_at
	FROM app_private.consultation_registration r
	LEFT JOIN app_private.consultation_order o ON o.registration_id=r.id
	LEFT JOIN app_private.staff_account d ON d.id=r.assigned_doctor_id
	LEFT JOIN LATERAL (
		SELECT a.id,a.status,a.starts_at,a.ends_at
		FROM app_private.appointment a
		WHERE a.registration_id=r.id
		ORDER BY a.created_at DESC,a.id DESC
		LIMIT 1
	) a ON true
	LEFT JOIN LATERAL (
		SELECT b.id,b.status
		FROM app_private.book_release b
		WHERE b.registration_id=r.id AND b.book='book1'
		ORDER BY b.generated_at DESC,b.id DESC
		LIMIT 1
	) b1 ON true
	LEFT JOIN LATERAL (
		SELECT b.id,b.status
		FROM app_private.book_release b
		WHERE b.registration_id=r.id AND b.book='book2'
		ORDER BY b.generated_at DESC,b.id DESC
		LIMIT 1
	) b2 ON true
	WHERE r.guardian_id=$1`

func scanFamilyRegistration(row interface{ Scan(...any) error }) (FamilyRegistration, error) {
	var value FamilyRegistration
	var appointmentID, appointmentStatus, book1ID, book1Status, book2ID, book2Status sql.NullString
	if err := row.Scan(
		&value.ID, &value.ChildName, &value.DateOfBirth, &value.RegistrationStatus,
		&value.PaymentStatus, &value.AmountPaise, &value.Currency, &value.DoctorName,
		&appointmentID, &appointmentStatus, &value.AppointmentStartsAt, &value.AppointmentEndsAt,
		&book1ID, &book1Status, &book2ID, &book2Status, &value.CreatedAt,
	); err != nil {
		return value, err
	}
	if appointmentID.Valid {
		value.AppointmentID = &appointmentID.String
	}
	if appointmentStatus.Valid {
		value.AppointmentStatus = &appointmentStatus.String
	}
	if book1ID.Valid {
		value.Book1ReleaseID = &book1ID.String
	}
	if book1Status.Valid {
		value.Book1Status = &book1Status.String
	}
	if book2ID.Valid {
		value.Book2ReleaseID = &book2ID.String
	}
	if book2Status.Valid {
		value.Book2Status = &book2Status.String
	}
	return value, nil
}

func (s *Server) ListFamilyRegistrations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), familyRegistrationQuery+` ORDER BY r.created_at DESC,r.id DESC`, CurrentGuardian(r.Context()).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	items := make([]FamilyRegistration, 0)
	for rows.Next() {
		value, err := scanFamilyRegistration(rows)
		if err != nil {
			serverError(w, err)
			return
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, items)
}

func (s *Server) FamilyRegistrationStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	var value FamilyRegistration
	row := s.pool.QueryRow(r.Context(), familyRegistrationQuery+` AND r.id=$2`, CurrentGuardian(r.Context()).ID, id)
	var err error
	value, err = scanFamilyRegistration(row)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, value)
}
