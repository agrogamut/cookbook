package portal

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type Intake struct {
	GuardianName string `json:"guardian_name"`
	ChildName    string `json:"child_name"`
	DateOfBirth  string `json:"date_of_birth"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	Token        string `json:"token"`
}

func validName(v string) bool {
	if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > 100 {
		return false
	}
	for _, c := range v {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func validEmail(v string) bool {
	a, err := mail.ParseAddress(v)
	return err == nil && a.Address == v && len(v) <= 254 && strings.Contains(v, ".")
}
func normalizePhone(v string) string {
	v = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(v))
	if len(v) == 10 && v[0] >= '6' && v[0] <= '9' {
		v = "+91" + v
	}
	return v
}
func (i *Intake) validate(now time.Time) string {
	i.GuardianName = strings.TrimSpace(i.GuardianName)
	i.ChildName = strings.TrimSpace(i.ChildName)
	i.Email = strings.TrimSpace(i.Email)
	i.Phone = normalizePhone(i.Phone)
	if !validName(i.GuardianName) || !validName(i.ChildName) {
		return "Enter both names, using at most 100 characters each."
	}
	dob, err := time.Parse("2006-01-02", i.DateOfBirth)
	// A calendar date has no time zone. Use the service's Indian calendar day.
	date := now.In(time.FixedZone("IST", 19800)).Format("2006-01-02")
	if err != nil || dob.Year() < 1900 || i.DateOfBirth > date {
		return "Enter a valid date of birth that is not in the future."
	}
	if len(i.Phone) < 9 || len(i.Phone) > 16 || i.Phone[0] != '+' || i.Phone[1] == '0' {
		return "Enter a phone number with country code, for example +91 followed by your number."
	}
	for _, c := range i.Phone[1:] {
		if c < '0' || c > '9' {
			return "Enter a valid phone number."
		}
	}
	if i.Email != "" && !validEmail(i.Email) {
		return "Enter a valid email address or leave it blank."
	}
	if !validToken(i.Token) {
		return "The form expired. Refresh the page and try again."
	}
	return ""
}
func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	var input Intake
	if !decode(w, r, &input) {
		return
	}
	if message := input.validate(time.Now()); message != "" {
		fail(w, 400, message)
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
		(token_hash,guardian_name,child_name,date_of_birth,phone,email)
		VALUES ($1,$2,$3,$4,$5,nullif($6,'')) ON CONFLICT(token_hash) DO NOTHING RETURNING id`,
		tokenHash(input.Token), input.GuardianName, input.ChildName, input.DateOfBirth, input.Phone, input.Email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Repeated submission of the same form is safe; the private token never lets
		// a later request silently replace the first request's information.
		err = tx.QueryRow(r.Context(), `SELECT id FROM app_private.consultation_registration WHERE token_hash=$1
			AND guardian_name=$2 AND child_name=$3 AND date_of_birth=$4 AND phone=$5 AND coalesce(email,'')=$6`,
			tokenHash(input.Token), input.GuardianName, input.ChildName, input.DateOfBirth, input.Phone, input.Email).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 409, "This form was already submitted with different details. Start a new registration.")
			return
		}
	} else if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO public.child_profile(child_id,display_name,date_of_birth,created_by) VALUES ($1,$2,$3,'guardian_intake')`, id, input.ChildName, input.DateOfBirth)
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE app_private.consultation_registration SET child_id=$1 WHERE id=$1::uuid`, id)
		}
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	respond(w, 201, map[string]string{"id": id})
}

type Registration struct {
	ID               string    `json:"id"`
	GuardianName     string    `json:"guardian_name"`
	ChildName        string    `json:"child_name"`
	DateOfBirth      string    `json:"date_of_birth"`
	Phone            string    `json:"phone"`
	Email            string    `json:"email"`
	ChildID          string    `json:"child_id"`
	AssignedDoctorID string    `json:"assigned_doctor_id"`
	DoctorName       string    `json:"doctor_name"`
	Status           string    `json:"status"`
	Notes            string    `json:"notes"`
	CreatedAt        time.Time `json:"created_at"`
	PaymentStatus    string    `json:"payment_status"`
	AmountPaise      *int      `json:"amount_paise"`
	Currency         string    `json:"currency"`
	PaymentID        string    `json:"payment_id"`
	OrderID          string    `json:"order_id"`
	RefundedPaise    int       `json:"refunded_paise"`
}

const registrationSelect = `SELECT r.id, r.guardian_name, r.child_name, to_char(r.date_of_birth,'YYYY-MM-DD'), r.phone,
	coalesce(r.email,''), coalesce(r.child_id,''), coalesce(r.assigned_doctor_id::text,''), coalesce(d.name,''), r.status, r.notes,
	r.created_at, coalesce(o.status,'unpaid'), o.amount_paise, coalesce(o.currency,'INR'), coalesce(o.payment_id,''), coalesce(o.id,''), coalesce(o.refunded_paise,0)`
const registrationFrom = ` FROM app_private.consultation_registration r
	LEFT JOIN app_private.staff_account d ON d.id=r.assigned_doctor_id
	LEFT JOIN app_private.consultation_order o ON o.registration_id=r.id `

func scanRegistration(row interface{ Scan(...any) error }) (Registration, error) {
	var v Registration
	err := row.Scan(&v.ID, &v.GuardianName, &v.ChildName, &v.DateOfBirth, &v.Phone, &v.Email, &v.ChildID,
		&v.AssignedDoctorID, &v.DoctorName, &v.Status, &v.Notes, &v.CreatedAt, &v.PaymentStatus, &v.AmountPaise, &v.Currency, &v.PaymentID, &v.OrderID, &v.RefundedPaise)
	return v, err
}
func (s *Server) GetRegistration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, 404, "Record not found.")
		return
	}
	a := Current(r.Context())
	v, err := scanRegistration(s.pool.QueryRow(r.Context(), registrationSelect+registrationFrom+`WHERE r.id=$1 AND ($2='admin' OR r.assigned_doctor_id=$3)`, id, a.Role, a.ID))
	if notFound(w, err) {
		return
	}
	respond(w, 200, v)
}
func (s *Server) ListRegistrations(w http.ResponseWriter, r *http.Request) {
	a := Current(r.Context())
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		fail(w, 400, "Page is out of range.")
		return
	}
	search := strings.TrimSpace(q.Get("q"))
	if len(search) > 200 {
		fail(w, 400, "Search is too long.")
		return
	}
	status, payment, assignment := q.Get("status"), q.Get("payment"), q.Get("assignment")
	where := `WHERE ($1='admin' OR r.assigned_doctor_id=$2)
		AND ($3='' OR concat_ws(' ',r.guardian_name,r.child_name,r.phone,r.email) ILIKE '%' || $3 || '%')
		AND ($4='' OR r.status=$4) AND ($5='' OR coalesce(o.status,'unpaid')=$5)
		AND ($6<>'unassigned' OR r.assigned_doctor_id IS NULL)`
	args := []any{a.Role, a.ID, search, status, payment, assignment}
	var total int
	if err := s.pool.QueryRow(r.Context(), `SELECT count(*)`+registrationFrom+where, args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}
	rows, err := s.pool.Query(r.Context(), registrationSelect+registrationFrom+where+` ORDER BY r.created_at DESC,r.id DESC LIMIT 30 OFFSET $7`, append(args, (page-1)*30)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	items := []Registration{}
	for rows.Next() {
		v, err := scanRegistration(rows)
		if err != nil {
			serverError(w, err)
			return
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	rows.Close()
	var summary struct {
		Total      int `json:"total"`
		New        int `json:"new"`
		Unassigned int `json:"unassigned"`
		Paid       int `json:"paid"`
	}
	err = s.pool.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE r.status='new'),count(*) FILTER(WHERE r.assigned_doctor_id IS NULL),count(*) FILTER(WHERE o.status IN ('paid','partially_refunded'))`+registrationFrom+`WHERE ($1='admin' OR r.assigned_doctor_id=$2)`, a.Role, a.ID).Scan(&summary.Total, &summary.New, &summary.Unassigned, &summary.Paid)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": 30, "summary": summary})
}

type registrationUpdate struct {
	GuardianName     *string         `json:"guardian_name"`
	ChildName        *string         `json:"child_name"`
	DateOfBirth      *string         `json:"date_of_birth"`
	Phone            *string         `json:"phone"`
	Email            *string         `json:"email"`
	AssignedDoctorID json.RawMessage `json:"assigned_doctor_id"`
	Status           *string         `json:"status"`
	Notes            *string         `json:"notes"`
}

func (s *Server) UpdateRegistration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, 404, "Record not found.")
		return
	}
	var u registrationUpdate
	if !decode(w, r, &u) {
		return
	}
	a := Current(r.Context())
	if a.Role != "admin" && (u.GuardianName != nil || u.ChildName != nil || u.DateOfBirth != nil || u.Phone != nil || u.Email != nil || len(u.AssignedDoctorID) > 0) {
		fail(w, 403, "Only an administrator can change registration details or assignments.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	v, err := scanRegistration(tx.QueryRow(r.Context(), registrationSelect+registrationFrom+`WHERE r.id=$1 AND ($2='admin' OR r.assigned_doctor_id=$3) FOR UPDATE OF r`, id, a.Role, a.ID))
	if notFound(w, err) {
		return
	}
	if u.GuardianName != nil {
		v.GuardianName = *u.GuardianName
	}
	if u.ChildName != nil {
		v.ChildName = *u.ChildName
	}
	if u.DateOfBirth != nil {
		v.DateOfBirth = *u.DateOfBirth
	}
	if u.Phone != nil {
		v.Phone = *u.Phone
	}
	if u.Email != nil {
		v.Email = *u.Email
	}
	input := Intake{GuardianName: v.GuardianName, ChildName: v.ChildName, DateOfBirth: v.DateOfBirth, Phone: v.Phone, Email: v.Email, Token: token()}
	if message := input.validate(time.Now()); message != "" {
		fail(w, 400, message)
		return
	}
	if u.Status != nil {
		v.Status = *u.Status
	}
	if u.Notes != nil {
		v.Notes = strings.TrimSpace(*u.Notes)
	}
	if !strings.Contains("|new|contacted|scheduled|completed|cancelled|", "|"+v.Status+"|") || v.Status == "" || utf8.RuneCountInString(v.Notes) > 5000 {
		fail(w, 400, "Choose a valid consultation status and keep notes within 5,000 characters.")
		return
	}
	if len(u.AssignedDoctorID) > 0 {
		var doctorID *string
		if json.Unmarshal(u.AssignedDoctorID, &doctorID) != nil {
			fail(w, 400, "Choose a doctor or leave unassigned.")
			return
		}
		v.AssignedDoctorID = ""
		if doctorID != nil && *doctorID != "" {
			if !validUUID(*doctorID) {
				fail(w, 400, "Choose an active doctor.")
				return
			}
			var found string
			err := tx.QueryRow(r.Context(), `SELECT id FROM app_private.staff_account WHERE id=$1 AND role='doctor' AND active FOR SHARE`, *doctorID).Scan(&found)
			if errors.Is(err, pgx.ErrNoRows) {
				fail(w, 400, "Choose an active doctor.")
				return
			}
			if err != nil {
				serverError(w, err)
				return
			}
			v.AssignedDoctorID = *doctorID
		}
	}
	_, err = tx.Exec(r.Context(), `UPDATE app_private.consultation_registration SET guardian_name=$2,child_name=$3,date_of_birth=$4,phone=$5,email=nullif($6,''),assigned_doctor_id=nullif($7,'')::uuid,status=$8,notes=$9,updated_at=now() WHERE id=$1`, id, input.GuardianName, input.ChildName, input.DateOfBirth, input.Phone, input.Email, v.AssignedDoctorID, v.Status, v.Notes)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE public.child_profile SET display_name=$2,date_of_birth=$3,updated_by=$4,updated_at=now() WHERE child_id=$1`, v.ChildID, input.ChildName, input.DateOfBirth, a.ID)
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	s.GetRegistration(w, r)
}

func (s *Server) PublicRegistration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	secret := r.Header.Get("X-Registration-Token")
	if !validUUID(id) || !validToken(secret) {
		fail(w, 404, "Registration not found.")
		return
	}
	var status string
	err := s.pool.QueryRow(r.Context(), `SELECT coalesce(o.status,'unpaid') FROM app_private.consultation_registration r LEFT JOIN app_private.consultation_order o ON o.registration_id=r.id WHERE r.id=$1 AND r.token_hash=$2`, id, tokenHash(secret)).Scan(&status)
	if notFound(w, err) {
		return
	}
	respond(w, 200, map[string]string{"id": id, "payment_status": status})
}
