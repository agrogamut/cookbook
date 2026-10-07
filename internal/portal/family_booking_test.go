package portal

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/madamgy/recipie/internal/db"
)

func TestFamilyBookingReleaseAndRoleBoundaries(t *testing.T) {
	databaseURL := os.Getenv("PORTAL_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PORTAL_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	identity := newTestIdentity()
	service := New(pool, Options{Identity: identity, Gateway: newTestGateway()})
	router := chi.NewRouter()
	service.Routes(router)
	admin, err := service.Provision(ctx, "Booking admin", "booking-admin-"+token()[:12]+"@example.invalid", "test-password-only-123", "admin")
	if err != nil {
		t.Fatal(err)
	}
	doctorOne, err := service.Provision(ctx, "Doctor One", "booking-doctor-one-"+token()[:12]+"@example.invalid", "test-password-only-123", "doctor")
	if err != nil {
		t.Fatal(err)
	}
	doctorTwo, err := service.Provision(ctx, "Doctor Two", "booking-doctor-two-"+token()[:12]+"@example.invalid", "test-password-only-123", "doctor")
	if err != nil {
		t.Fatal(err)
	}
	familyOne, err := service.ProvisionGuardian(ctx, "Family One", "family-one-"+token()[:12]+"@example.invalid", "family-password-only-123")
	if err != nil {
		t.Fatal(err)
	}
	familyTwo, err := service.ProvisionGuardian(ctx, "Family Two", "family-two-"+token()[:12]+"@example.invalid", "family-password-only-123")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM app_private.appointment WHERE requested_by IN ($1,$2) OR decided_by IN ($1,$2)`, admin.ID, doctorOne.ID)
		pool.Exec(ctx, `DELETE FROM app_private.payment_event WHERE order_id IN (SELECT id FROM app_private.consultation_order WHERE registration_id IN (SELECT id FROM app_private.consultation_registration WHERE guardian_id IN ($1,$2)))`, familyOne.ID, familyTwo.ID)
		pool.Exec(ctx, `DELETE FROM app_private.consultation_order WHERE registration_id IN (SELECT id FROM app_private.consultation_registration WHERE guardian_id IN ($1,$2))`, familyOne.ID, familyTwo.ID)
		pool.Exec(ctx, `DELETE FROM app_private.doctor_availability WHERE doctor_id IN ($1,$2)`, doctorOne.ID, doctorTwo.ID)
		pool.Exec(ctx, `DELETE FROM public.child_profile WHERE child_id IN (SELECT child_id FROM app_private.consultation_registration WHERE guardian_id IN ($1,$2))`, familyOne.ID, familyTwo.ID)
		pool.Exec(ctx, `DELETE FROM app_private.consultation_registration WHERE guardian_id IN ($1,$2)`, familyOne.ID, familyTwo.ID)
		pool.Exec(ctx, `DELETE FROM app_private.guardian_account WHERE id IN ($1,$2)`, familyOne.ID, familyTwo.ID)
		pool.Exec(ctx, `DELETE FROM app_private.staff_account WHERE id IN ($1,$2,$3)`, admin.ID, doctorOne.ID, doctorTwo.ID)
	}()

	login := func(path, email, password string) *http.Cookie {
		t.Helper()
		w := portalRequest(router, "POST", path, map[string]string{"email": email, "password": password}, nil, "")
		expectCode(t, w, 200)
		return w.Result().Cookies()[0]
	}
	adminCookie := login("/api/auth/login", admin.Email, "test-password-only-123")
	doctorCookie := login("/api/auth/login", doctorTwo.Email, "test-password-only-123")
	familyOneCookie := login("/api/family/auth/login", familyOne.Email, "family-password-only-123")
	familyTwoCookie := login("/api/family/auth/login", familyTwo.Email, "family-password-only-123")

	createRegistration := func(cookie *http.Cookie, child string) string {
		t.Helper()
		w := portalRequest(router, "POST", "/api/family/registrations", map[string]string{
			"child_name": child, "date_of_birth": "2022-05-01", "phone": "9876543210",
		}, cookie, "")
		expectCode(t, w, 201)
		return decodeResponse[map[string]string](t, w)["id"]
	}
	registrationOne := createRegistration(familyOneCookie, "Child One")
	registrationTwo := createRegistration(familyTwoCookie, "Child Two")
	registrationThree := createRegistration(familyOneCookie, "Child Three")
	expectCode(t, portalRequest(router, "PATCH", "/api/admin/registrations/"+registrationThree+"/guardian", map[string]string{"guardian_id": familyTwo.ID}, adminCookie, ""), 200)

	local := time.Now().In(indiaTime).AddDate(0, 0, 2)
	start := time.Date(local.Year(), local.Month(), local.Day(), 10, 0, 0, 0, indiaTime)
	end := start.Add(3 * time.Hour)
	startText, endText := start.Format(time.RFC3339), end.Format(time.RFC3339)
	for _, doctorID := range []string{doctorOne.ID, doctorTwo.ID} {
		expectCode(t, portalRequest(router, "POST", "/api/availability", map[string]string{
			"doctor_id": doctorID, "starts_at": startText, "ends_at": endText,
		}, adminCookie, ""), 200)
	}
	if !isPgError(execOverlapAvailability(ctx, pool, doctorOne.ID, start.Add(time.Hour), end.Add(-time.Hour), admin.ID), "23P01") {
		t.Fatal("database did not reject overlapping availability")
	}
	expectCode(t, portalRequest(router, "POST", "/api/availability", map[string]string{
		"doctor_id": doctorOne.ID, "starts_at": start.Add(time.Hour).Format(time.RFC3339), "ends_at": end.Add(-time.Hour).Format(time.RFC3339),
	}, adminCookie, ""), 409)

	// One confirmed appointment in the week makes doctor one lose the tie-break.
	if _, err := pool.Exec(ctx, `INSERT INTO app_private.appointment(registration_id,doctor_id,starts_at,ends_at,mode,status,requested_by)
		VALUES ($1,$2,$3,$4,'specific_doctor','confirmed',$5)`, registrationThree, doctorOne.ID, start.Add(-2*time.Hour), start.Add(-time.Hour), admin.ID); err != nil {
		t.Fatal(err)
	}
	booking := map[string]string{"registration_id": registrationOne, "starts_at": start.Add(time.Hour).Format(time.RFC3339), "ends_at": start.Add(90 * time.Minute).Format(time.RFC3339)}
	w := portalRequest(router, "POST", "/api/family/appointments", booking, familyOneCookie, "")
	expectCode(t, w, 201)
	appointmentOne := decodeResponse[map[string]string](t, w)["id"]
	var selectedDoctor string
	if err := pool.QueryRow(ctx, `SELECT doctor_id::text FROM app_private.appointment WHERE id=$1`, appointmentOne).Scan(&selectedDoctor); err != nil {
		t.Fatal(err)
	}
	if selectedDoctor != doctorTwo.ID {
		t.Fatalf("time-range assignment chose %s, expected doctor two %s", selectedDoctor, doctorTwo.ID)
	}
	if !isPgError(execOverlapAppointment(ctx, pool, registrationThree, doctorTwo.ID, start.Add(time.Hour), start.Add(2*time.Hour), admin.ID), "23P01") {
		t.Fatal("database did not reject overlapping appointment")
	}
	expectCode(t, portalRequest(router, "POST", "/api/admin/appointments/"+appointmentOne+"/decision", map[string]string{"action": "confirm"}, adminCookie, ""), 409)
	if _, err := pool.Exec(ctx, `INSERT INTO app_private.consultation_order(id,registration_id,amount_paise,currency,status,appointment_id)
		VALUES ($1,$2,1000,'INR','paid',$3)`, "order_"+token()[:20], registrationOne, appointmentOne); err != nil {
		t.Fatal(err)
	}

	// A specific-doctor request uses doctor one, whose earlier confirmed appointment does not overlap.
	specific := map[string]string{"registration_id": registrationTwo, "doctor_id": doctorOne.ID, "starts_at": start.Add(time.Hour).Format(time.RFC3339), "ends_at": start.Add(90 * time.Minute).Format(time.RFC3339)}
	w = portalRequest(router, "POST", "/api/family/appointments", specific, familyTwoCookie, "")
	expectCode(t, w, 201)
	appointmentTwo := decodeResponse[map[string]string](t, w)["id"]
	// Both doctors are now held, so a third request fails honestly.
	registrationFour := createRegistration(familyOneCookie, "Child Four")
	noFree := map[string]string{"registration_id": registrationFour, "starts_at": start.Add(time.Hour).Format(time.RFC3339), "ends_at": start.Add(90 * time.Minute).Format(time.RFC3339)}
	expectCode(t, portalRequest(router, "POST", "/api/family/appointments", noFree, familyOneCookie, ""), 409)

	expectCode(t, portalRequest(router, "POST", "/api/admin/appointments/"+appointmentOne+"/decision", map[string]string{"action": "confirm"}, doctorCookie, ""), 403)
	expectCode(t, portalRequest(router, "POST", "/api/admin/appointments/"+appointmentOne+"/decision", map[string]string{"action": "confirm"}, adminCookie, ""), 200)
	var assigned, status string
	if err := pool.QueryRow(ctx, `SELECT coalesce(assigned_doctor_id::text,''),status FROM app_private.consultation_registration WHERE id=$1`, registrationOne).Scan(&assigned, &status); err != nil {
		t.Fatal(err)
	}
	if assigned != doctorTwo.ID || status != "scheduled" {
		t.Fatalf("confirmation did not assign doctor two: %s %s", assigned, status)
	}
	expectCode(t, portalRequest(router, "POST", "/api/admin/appointments/"+appointmentTwo+"/decision", map[string]string{"action": "reject"}, adminCookie, ""), 200)
	expectCode(t, portalRequest(router, "POST", "/api/admin/appointments/"+appointmentOne+"/decision", map[string]string{
		"action": "reschedule", "doctor_id": doctorOne.ID,
		"starts_at": start.Add(time.Hour).Format(time.RFC3339), "ends_at": start.Add(90 * time.Minute).Format(time.RFC3339),
	}, adminCookie, ""), 200)
	if err := pool.QueryRow(ctx, `SELECT doctor_id::text,status FROM app_private.appointment WHERE id=$1`, appointmentOne).Scan(&assigned, &status); err != nil {
		t.Fatal(err)
	}
	if assigned != doctorOne.ID || status != "confirmed" {
		t.Fatalf("reschedule did not move the confirmed appointment: %s %s", assigned, status)
	}
	expectCode(t, portalRequest(router, "PATCH", "/api/admin/staff/"+doctorOne.ID, map[string]any{"name": "Doctor One", "active": false}, adminCookie, ""), 200)
	var appointmentStatus, assignedAfterDeactivation string
	if err := pool.QueryRow(ctx, `SELECT a.status,coalesce(r.assigned_doctor_id::text,'')
		FROM app_private.appointment a JOIN app_private.consultation_registration r ON r.id=a.registration_id WHERE a.id=$1`, appointmentOne).Scan(&appointmentStatus, &assignedAfterDeactivation); err != nil {
		t.Fatal(err)
	}
	if appointmentStatus != "refund_required" || assignedAfterDeactivation != "" {
		t.Fatalf("doctor deactivation left active booking state: appointment=%s assignment=%s", appointmentStatus, assignedAfterDeactivation)
	}

	// Family access is scoped in SQL, and the two cookie namespaces cannot cross routes.
	items := decodeResponse[[]FamilyRegistration](t, portalRequest(router, "GET", "/api/family/registrations", nil, familyOneCookie, ""))
	if len(items) != 2 {
		t.Fatalf("family one saw %d registrations, expected two", len(items))
	}
	expectCode(t, portalRequest(router, "GET", "/api/family/registrations/"+registrationTwo, nil, familyOneCookie, ""), 404)
	expectCode(t, portalRequest(router, "GET", "/api/registrations", nil, familyOneCookie, ""), 401)
	expectCode(t, portalRequest(router, "GET", "/api/family/registrations", nil, doctorCookie, ""), 401)

	var childID string
	if err := pool.QueryRow(ctx, `SELECT child_id FROM app_private.consultation_registration WHERE id=$1`, registrationOne).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	var releaseID string
	if err := pool.QueryRow(ctx, `INSERT INTO app_private.book_release(registration_id,child_id,book,pdf,generated_by)
		VALUES ($1,$2,'book1',$3,$4) RETURNING id`, registrationOne, childID, []byte("%PDF-test"), doctorTwo.ID).Scan(&releaseID); err != nil {
		t.Fatal(err)
	}
	expectCode(t, portalRequest(router, "GET", "/api/family/book-releases/"+releaseID+"/download", nil, familyOneCookie, ""), 404)
	expectCode(t, portalRequest(router, "PATCH", "/api/admin/book-releases/"+releaseID, map[string]string{"action": "approve"}, doctorCookie, ""), 403)
	expectCode(t, portalRequest(router, "PATCH", "/api/admin/book-releases/"+releaseID, map[string]string{"action": "approve"}, adminCookie, ""), 200)
	approved := portalRequest(router, "GET", "/api/family/book-releases/"+releaseID+"/download", nil, familyOneCookie, "")
	expectCode(t, approved, 200)
	if approved.Body.String() != "%PDF-test" {
		t.Fatal("family did not receive the approved PDF bytes")
	}
}

func execOverlapAvailability(ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, doctorID string, starts, ends time.Time, createdBy string) error {
	_, err := pool.Exec(ctx, `INSERT INTO app_private.doctor_availability(doctor_id,starts_at,ends_at,created_by) VALUES ($1,$2,$3,$4)`, doctorID, starts, ends, createdBy)
	return err
}

func execOverlapAppointment(ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, registrationID, doctorID string, starts, ends time.Time, requestedBy string) error {
	_, err := pool.Exec(ctx, `INSERT INTO app_private.appointment(registration_id,doctor_id,starts_at,ends_at,mode,status,requested_by) VALUES ($1,$2,$3,$4,'specific_doctor','pending_admin',$5)`, registrationID, doctorID, starts, ends, requestedBy)
	return err
}
