package portal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNoAvailableSlot = errors.New("no available doctor slot")
	ErrAppointmentHeld = errors.New("registration already has an active appointment")
)

var indiaTime = time.FixedZone("Asia/Kolkata", 5*60*60+30*60)

type Availability struct {
	ID         string    `json:"id"`
	DoctorID   string    `json:"doctor_id"`
	DoctorName string    `json:"doctor_name"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
}

type FreeInterval struct {
	DoctorID   string    `json:"doctor_id"`
	DoctorName string    `json:"doctor_name"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
}

type Appointment struct {
	ID             string    `json:"id"`
	RegistrationID string    `json:"registration_id"`
	ChildName      string    `json:"child_name"`
	DoctorID       string    `json:"doctor_id"`
	DoctorName     string    `json:"doctor_name"`
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
	Mode           string    `json:"mode"`
	Status         string    `json:"status"`
	RequestedBy    string    `json:"requested_by"`
	DecidedBy      string    `json:"decided_by"`
	CreatedAt      time.Time `json:"created_at"`
}

type appointmentInput struct {
	RegistrationID string `json:"registration_id"`
	DoctorID       string `json:"doctor_id"`
	StartsAt       string `json:"starts_at"`
	EndsAt         string `json:"ends_at"`
}

func parseInstant(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, errors.New("use an RFC3339 appointment time")
	}
	return parsed.UTC(), nil
}

func validInterval(startsAt, endsAt time.Time) error {
	if startsAt.IsZero() || endsAt.IsZero() || !endsAt.After(startsAt) {
		return errors.New("end time must be after start time")
	}
	return nil
}

func weekBounds(value time.Time) (time.Time, time.Time) {
	local := value.In(indiaTime)
	dayOffset := (int(local.Weekday()) + 6) % 7
	start := time.Date(local.Year(), local.Month(), local.Day()-dayOffset, 0, 0, 0, 0, indiaTime)
	return start.UTC(), start.AddDate(0, 0, 7).UTC()
}

func dayBounds(value string) (time.Time, time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), indiaTime)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("use a YYYY-MM-DD date")
	}
	return day.UTC(), day.AddDate(0, 0, 1).UTC(), nil
}

func isPgError(err error, code string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == code
}

func (s *Server) availabilityValues(ctx context.Context, rows pgx.Rows) ([]Availability, error) {
	defer rows.Close()
	values := make([]Availability, 0)
	for rows.Next() {
		var value Availability
		if err := rows.Scan(&value.ID, &value.DoctorID, &value.DoctorName, &value.StartsAt, &value.EndsAt, &value.Active, &value.CreatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Server) ListAvailability(w http.ResponseWriter, r *http.Request) {
	a := Current(r.Context())
	doctorID := strings.TrimSpace(r.URL.Query().Get("doctor_id"))
	if a.Role == "doctor" {
		doctorID = a.ID
	}
	query := `SELECT v.id,v.doctor_id,d.name,v.starts_at,v.ends_at,v.active,v.created_at
		FROM app_private.doctor_availability v JOIN app_private.staff_account d ON d.id=v.doctor_id
		WHERE ($1='' OR v.doctor_id::text=$1) ORDER BY v.starts_at,v.id`
	rows, err := s.pool.Query(r.Context(), query, doctorID)
	if err != nil {
		serverError(w, err)
		return
	}
	values, err := s.availabilityValues(r.Context(), rows)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, values)
}

func (s *Server) SaveAvailability(w http.ResponseWriter, r *http.Request) {
	a := Current(r.Context())
	var body struct {
		ID       string `json:"id"`
		DoctorID string `json:"doctor_id"`
		StartsAt string `json:"starts_at"`
		EndsAt   string `json:"ends_at"`
		Active   *bool  `json:"active"`
	}
	if !decode(w, r, &body) {
		return
	}
	if a.Role == "doctor" {
		body.DoctorID = a.ID
	}
	if !validUUID(body.DoctorID) {
		fail(w, http.StatusBadRequest, "Choose an active doctor.")
		return
	}
	startsAt, err := parseInstant(body.StartsAt)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	endsAt, err := parseInstant(body.EndsAt)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validInterval(startsAt, endsAt); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	var result pgconn.CommandTag
	if body.ID == "" {
		result, err = s.pool.Exec(r.Context(), `INSERT INTO app_private.doctor_availability(doctor_id,starts_at,ends_at,created_by,active)
			SELECT $1,$2,$3,$4,$5 WHERE EXISTS (SELECT 1 FROM app_private.staff_account WHERE id=$1 AND role='doctor' AND active)`, body.DoctorID, startsAt, endsAt, a.ID, active)
	} else {
		if !validUUID(body.ID) {
			fail(w, http.StatusNotFound, "Availability block not found.")
			return
		}
		if a.Role == "admin" {
			result, err = s.pool.Exec(r.Context(), `UPDATE app_private.doctor_availability v SET doctor_id=$2,starts_at=$3,ends_at=$4,active=$5,updated_at=now()
				WHERE v.id=$1 AND EXISTS (SELECT 1 FROM app_private.staff_account WHERE id=$2 AND role='doctor' AND active)`, body.ID, body.DoctorID, startsAt, endsAt, active)
		} else {
			result, err = s.pool.Exec(r.Context(), `UPDATE app_private.doctor_availability v SET starts_at=$2,ends_at=$3,active=$4,updated_at=now()
				WHERE v.id=$1 AND v.doctor_id=$5 AND EXISTS (SELECT 1 FROM app_private.staff_account WHERE id=v.doctor_id AND role='doctor' AND active)`, body.ID, startsAt, endsAt, active, a.ID)
		}
	}
	if err != nil {
		if isPgError(err, "23P01") {
			fail(w, http.StatusConflict, "This availability overlaps an existing block for that doctor.")
			return
		}
		serverError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		fail(w, http.StatusNotFound, "Doctor or availability block not found.")
		return
	}
	respond(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) RevokeAvailability(w http.ResponseWriter, r *http.Request) {
	a := Current(r.Context())
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Availability block not found.")
		return
	}
	result, err := s.pool.Exec(r.Context(), `UPDATE app_private.doctor_availability SET active=false,updated_at=now()
		WHERE id=$1 AND ($2='admin' OR doctor_id=$3)`, id, a.Role, a.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		fail(w, http.StatusNotFound, "Availability block not found.")
		return
	}
	respond(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) ListFamilyAvailability(w http.ResponseWriter, r *http.Request) {
	start, end, err := dayBounds(r.URL.Query().Get("date"))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	doctorID := strings.TrimSpace(r.URL.Query().Get("doctor_id"))
	rows, err := s.pool.Query(r.Context(), `SELECT v.doctor_id,d.name,v.starts_at,v.ends_at
		FROM app_private.doctor_availability v JOIN app_private.staff_account d ON d.id=v.doctor_id
		WHERE v.active AND d.active AND d.role='doctor' AND v.ends_at>$1 AND v.starts_at<$2
			AND ($3='' OR v.doctor_id::text=$3) ORDER BY v.starts_at,v.doctor_id`, start, end, doctorID)
	if err != nil {
		serverError(w, err)
		return
	}
	type block struct {
		doctorID, doctorName string
		startsAt, endsAt     time.Time
	}
	blocks := make([]block, 0)
	for rows.Next() {
		var value block
		if err := rows.Scan(&value.doctorID, &value.doctorName, &value.startsAt, &value.endsAt); err != nil {
			rows.Close()
			serverError(w, err)
			return
		}
		blocks = append(blocks, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		serverError(w, err)
		return
	}
	rows.Close()
	appointments, err := s.loadBookedIntervals(r.Context(), s.pool, start, end, doctorID)
	if err != nil {
		serverError(w, err)
		return
	}
	free := make([]FreeInterval, 0)
	for _, block := range blocks {
		cursor := block.startsAt
		if cursor.Before(start) {
			cursor = start
		}
		blockEnd := block.endsAt
		if blockEnd.After(end) {
			blockEnd = end
		}
		for _, appointment := range appointments[block.doctorID] {
			if !appointment.endsAt.After(cursor) {
				continue
			}
			if !appointment.startsAt.Before(blockEnd) {
				break
			}
			if appointment.startsAt.After(cursor) {
				free = append(free, FreeInterval{DoctorID: block.doctorID, DoctorName: block.doctorName, StartsAt: cursor, EndsAt: minTime(appointment.startsAt, blockEnd)})
			}
			if appointment.endsAt.After(cursor) {
				cursor = appointment.endsAt
			}
			if !cursor.Before(blockEnd) {
				break
			}
		}
		if cursor.Before(blockEnd) {
			free = append(free, FreeInterval{DoctorID: block.doctorID, DoctorName: block.doctorName, StartsAt: cursor, EndsAt: blockEnd})
		}
	}
	respond(w, http.StatusOK, free)
}

type bookedInterval struct {
	startsAt time.Time
	endsAt   time.Time
}

func (s *Server) loadBookedIntervals(ctx context.Context, source interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, start, end time.Time, doctorID string) (map[string][]bookedInterval, error) {
	rows, err := source.Query(ctx, `SELECT doctor_id,starts_at,ends_at
		FROM app_private.appointment WHERE status IN ('pending_admin','confirmed')
		AND ends_at>$1 AND starts_at<$2 AND ($3='' OR doctor_id::text=$3) ORDER BY doctor_id,starts_at`, start, end, doctorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	booked := make(map[string][]bookedInterval)
	for rows.Next() {
		var doctorID string
		var value bookedInterval
		if err := rows.Scan(&doctorID, &value.startsAt, &value.endsAt); err != nil {
			return nil, err
		}
		booked[doctorID] = append(booked[doctorID], value)
	}
	return booked, rows.Err()
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *Server) lockEligibleDoctors(ctx context.Context, tx pgx.Tx, startsAt, endsAt time.Time, requestedDoctor string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT v.id,d.id
		FROM app_private.doctor_availability v JOIN app_private.staff_account d ON d.id=v.doctor_id
		WHERE v.active AND d.active AND d.role='doctor' AND v.starts_at <= $1 AND v.ends_at >= $2
			AND ($3='' OR d.id::text=$3) ORDER BY d.id,v.starts_at,v.id FOR UPDATE OF v,d`, startsAt, endsAt, requestedDoctor)
	if err != nil {
		return nil, fmt.Errorf("lock eligible doctors: %w", err)
	}
	defer rows.Close()
	seen := make(map[string]bool)
	ids := make([]string, 0)
	for rows.Next() {
		var availabilityID, doctorID string
		if err := rows.Scan(&availabilityID, &doctorID); err != nil {
			return nil, fmt.Errorf("scan eligible doctor: %w", err)
		}
		if !seen[doctorID] {
			seen[doctorID] = true
			ids = append(ids, doctorID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read eligible doctors: %w", err)
	}
	return ids, nil
}

func (s *Server) chooseDoctor(ctx context.Context, tx pgx.Tx, startsAt, endsAt time.Time, requestedDoctor, excludeAppointment string) (string, error) {
	ids, err := s.lockEligibleDoctors(ctx, tx, startsAt, endsAt, requestedDoctor)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNoAvailableSlot
	}
	weekStart, weekEnd := weekBounds(startsAt)
	excludeArg := excludeAppointment
	type score struct {
		id    string
		count int
	}
	scores := make([]score, 0, len(ids))
	for _, id := range ids {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM app_private.appointment
			WHERE doctor_id=$1 AND status='confirmed' AND starts_at >= $2 AND starts_at < $3
				AND ($4='' OR id::text<>$4)`, id, weekStart, weekEnd, excludeArg).Scan(&count); err != nil {
			return "", fmt.Errorf("count doctor appointments: %w", err)
		}
		var occupied bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_private.appointment
			WHERE doctor_id=$1 AND status IN ('pending_admin','confirmed')
			AND tstzrange(starts_at,ends_at,'[)') && tstzrange($2,$3,'[)')
			AND ($4='' OR id::text<>$4))`, id, startsAt, endsAt, excludeArg).Scan(&occupied); err != nil {
			return "", fmt.Errorf("check doctor overlap: %w", err)
		}
		if !occupied {
			scores = append(scores, score{id: id, count: count})
		}
	}
	if len(scores) == 0 {
		return "", ErrNoAvailableSlot
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].count != scores[j].count {
			return scores[i].count < scores[j].count
		}
		return scores[i].id < scores[j].id
	})
	return scores[0].id, nil
}

func (s *Server) insertAppointment(ctx context.Context, tx pgx.Tx, registrationID, requestedBy, requestedDoctor string, startsAt, endsAt time.Time, mode, status string) (string, error) {
	if err := validInterval(startsAt, endsAt); err != nil {
		return "", err
	}
	doctorID, err := s.chooseDoctor(ctx, tx, startsAt, endsAt, requestedDoctor, "")
	if err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO app_private.appointment(registration_id,doctor_id,starts_at,ends_at,mode,status,requested_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, registrationID, doctorID, startsAt, endsAt, mode, status, requestedBy).Scan(&id)
	if isPgError(err, "23P01") || isPgError(err, "23505") {
		return "", ErrNoAvailableSlot
	}
	return id, err
}

func (s *Server) familyRegistrationTx(ctx context.Context, tx pgx.Tx, registrationID, guardianID string) error {
	var found string
	err := tx.QueryRow(ctx, `SELECT id FROM app_private.consultation_registration WHERE id=$1 AND guardian_id=$2 FOR UPDATE`, registrationID, guardianID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgx.ErrNoRows
	}
	return err
}

func (s *Server) createAppointment(w http.ResponseWriter, r *http.Request, family bool) {
	var body appointmentInput
	if !decode(w, r, &body) {
		return
	}
	if !validUUID(body.RegistrationID) {
		fail(w, http.StatusNotFound, "Registration not found.")
		return
	}
	startsAt, err := parseInstant(body.StartsAt)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	endsAt, err := parseInstant(body.EndsAt)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.DoctorID != "" && !validUUID(body.DoctorID) {
		fail(w, http.StatusBadRequest, "Choose a doctor from the available intervals.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if family {
		if err := s.familyRegistrationTx(r.Context(), tx, body.RegistrationID, CurrentGuardian(r.Context()).ID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				fail(w, http.StatusNotFound, "Registration not found.")
			} else {
				serverError(w, err)
			}
			return
		}
	} else {
		a := Current(r.Context())
		var assigned string
		err = tx.QueryRow(r.Context(), `SELECT coalesce(assigned_doctor_id::text,'') FROM app_private.consultation_registration WHERE id=$1 FOR UPDATE`, body.RegistrationID).Scan(&assigned)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, http.StatusNotFound, "Registration not found.")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		if a.Role == "doctor" && assigned != a.ID {
			fail(w, http.StatusNotFound, "Registration not found.")
			return
		}
	}
	var active bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM app_private.appointment WHERE registration_id=$1 AND status IN ('pending_admin','confirmed'))`, body.RegistrationID).Scan(&active)
	if err != nil {
		serverError(w, err)
		return
	}
	if active {
		fail(w, http.StatusConflict, "This registration already has an active appointment request.")
		return
	}
	mode := "time_range"
	if body.DoctorID != "" {
		mode = "specific_doctor"
	}
	requestedBy := Current(r.Context()).ID
	if family {
		requestedBy = CurrentGuardian(r.Context()).ID
	}
	id, err := s.insertAppointment(r.Context(), tx, body.RegistrationID, requestedBy, body.DoctorID, startsAt, endsAt, mode, "pending_admin")
	if err != nil {
		if errors.Is(err, ErrNoAvailableSlot) {
			fail(w, http.StatusConflict, "No doctor is free for that entire interval.")
			return
		}
		serverError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusCreated, map[string]string{"id": id, "status": "pending_admin"})
}

func (s *Server) CreateFamilyAppointment(w http.ResponseWriter, r *http.Request) {
	s.createAppointment(w, r, true)
}

func (s *Server) CreateAdminAppointment(w http.ResponseWriter, r *http.Request) {
	s.createAppointment(w, r, false)
}

func (s *Server) ListAppointments(w http.ResponseWriter, r *http.Request) {
	a := Current(r.Context())
	where := ""
	args := []any{}
	if a.Role == "doctor" {
		where = " WHERE a.doctor_id=$1"
		args = append(args, a.ID)
	}
	rows, err := s.pool.Query(r.Context(), `SELECT a.id,a.registration_id,r.child_name,a.doctor_id,d.name,a.starts_at,a.ends_at,a.mode,a.status,
			a.requested_by::text,coalesce(a.decided_by::text,''),a.created_at
		FROM app_private.appointment a JOIN app_private.consultation_registration r ON r.id=a.registration_id
		JOIN app_private.staff_account d ON d.id=a.doctor_id`+where+` ORDER BY a.starts_at DESC,a.id DESC`, args...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	items := make([]Appointment, 0)
	for rows.Next() {
		var value Appointment
		if err := rows.Scan(&value.ID, &value.RegistrationID, &value.ChildName, &value.DoctorID, &value.DoctorName, &value.StartsAt, &value.EndsAt, &value.Mode, &value.Status, &value.RequestedBy, &value.DecidedBy, &value.CreatedAt); err != nil {
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

func (s *Server) DecideAppointment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Appointment not found.")
		return
	}
	var body struct {
		Action   string `json:"action"`
		DoctorID string `json:"doctor_id"`
		StartsAt string `json:"starts_at"`
		EndsAt   string `json:"ends_at"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Action != "confirm" && body.Action != "reject" && body.Action != "cancel" && body.Action != "reschedule" {
		fail(w, http.StatusBadRequest, "Choose confirm, reject, cancel or reschedule.")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var appointment Appointment
	err = tx.QueryRow(r.Context(), `SELECT a.id,a.registration_id,r.child_name,a.doctor_id,d.name,a.starts_at,a.ends_at,a.mode,a.status,
		a.requested_by::text,coalesce(a.decided_by::text,''),a.created_at
		FROM app_private.appointment a JOIN app_private.consultation_registration r ON r.id=a.registration_id
		JOIN app_private.staff_account d ON d.id=a.doctor_id WHERE a.id=$1 FOR UPDATE OF a,r`, id).Scan(
		&appointment.ID, &appointment.RegistrationID, &appointment.ChildName, &appointment.DoctorID, &appointment.DoctorName,
		&appointment.StartsAt, &appointment.EndsAt, &appointment.Mode, &appointment.Status, &appointment.RequestedBy, &appointment.DecidedBy, &appointment.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "Appointment not found.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if (body.Action == "confirm" || body.Action == "reject") && appointment.Status != "pending_admin" {
		fail(w, http.StatusConflict, "Only pending appointments can be confirmed or rejected.")
		return
	}
	if body.Action == "reschedule" && appointment.Status != "confirmed" {
		fail(w, http.StatusConflict, "Only confirmed appointments can be rescheduled.")
		return
	}
	if body.Action == "cancel" && appointment.Status != "pending_admin" && appointment.Status != "confirmed" {
		fail(w, http.StatusConflict, "This appointment has already been decided.")
		return
	}
	if body.Action == "reject" || body.Action == "cancel" {
		status := "rejected"
		if body.Action == "cancel" {
			status = "cancelled"
		}
		_, err = tx.Exec(r.Context(), `UPDATE app_private.appointment SET status=$2,decided_by=$3,updated_at=now() WHERE id=$1`, id, status, Current(r.Context()).ID)
		if err == nil && body.Action == "cancel" && appointment.Status == "confirmed" {
			_, err = tx.Exec(r.Context(), `UPDATE app_private.consultation_registration
				SET assigned_doctor_id=NULL,status='cancelled',updated_at=now() WHERE id=$1`, appointment.RegistrationID)
		}
	} else {
		if (body.StartsAt == "") != (body.EndsAt == "") {
			fail(w, http.StatusBadRequest, "Provide both start and end times.")
			return
		}
		startsAt, endsAt := appointment.StartsAt, appointment.EndsAt
		if body.StartsAt != "" {
			startsAt, err = parseInstant(body.StartsAt)
			if err == nil {
				endsAt, err = parseInstant(body.EndsAt)
			}
		}
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if err = validInterval(startsAt, endsAt); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		doctorID := appointment.DoctorID
		if body.DoctorID != "" {
			if !validUUID(body.DoctorID) {
				fail(w, http.StatusBadRequest, "Choose an active doctor.")
				return
			}
			doctorID = body.DoctorID
		}
		chosen, chooseErr := s.chooseDoctor(r.Context(), tx, startsAt, endsAt, doctorID, id)
		if chooseErr != nil {
			if errors.Is(chooseErr, ErrNoAvailableSlot) {
				fail(w, http.StatusConflict, "That doctor is not free for the entire interval.")
				return
			}
			serverError(w, chooseErr)
			return
		}
		_, err = tx.Exec(r.Context(), `UPDATE app_private.appointment SET doctor_id=$2,starts_at=$3,ends_at=$4,status='confirmed',decided_by=$5,updated_at=now() WHERE id=$1`, id, chosen, startsAt, endsAt, Current(r.Context()).ID)
		if err == nil {
			_, err = tx.Exec(r.Context(), `UPDATE app_private.consultation_registration SET assigned_doctor_id=$2,status='scheduled',updated_at=now() WHERE id=$1`, appointment.RegistrationID, chosen)
		}
	}
	if isPgError(err, "23P01") || isPgError(err, "23505") {
		fail(w, http.StatusConflict, "That doctor is no longer free for the entire interval.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) CancelFamilyAppointment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "Appointment not found.")
		return
	}
	result, err := s.pool.Exec(r.Context(), `UPDATE app_private.appointment a SET status='cancelled',updated_at=now()
		FROM app_private.consultation_registration r WHERE a.id=$1 AND a.registration_id=r.id AND r.guardian_id=$2 AND a.status='pending_admin'`, id, CurrentGuardian(r.Context()).ID)
	if err != nil {
		serverError(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		fail(w, http.StatusNotFound, "Pending appointment not found.")
		return
	}
	respond(w, http.StatusOK, map[string]bool{"ok": true})
}
