package portal

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAppointmentDuration(t *testing.T) {
	start := time.Date(2026, 10, 8, 9, 0, 0, 0, indiaTime)
	for _, test := range []struct {
		name     string
		duration time.Duration
		valid    bool
	}{
		{"negative", -time.Minute, false},
		{"zero", 0, false},
		{"shorter consultation", 15 * time.Minute, true},
		{"thirty minutes", 30 * time.Minute, true},
		{"over boundary", 30*time.Minute + time.Nanosecond, false},
		{"whole working block", 8 * time.Hour, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validAppointmentInterval(start, start.Add(test.duration))
			if (err == nil) != test.valid {
				t.Fatalf("duration %s: %v", test.duration, err)
			}
		})
	}
	if err := validInterval(start, start.Add(8*time.Hour)); err != nil {
		t.Fatalf("doctor availability must still allow a working block: %v", err)
	}
}

func TestBookingRoutesLimitConsultationsToThirtyMinutes(t *testing.T) {
	f := newPaidFixture(t)
	for _, route := range []string{"public", "family", "admin"} {
		t.Run(route, func(t *testing.T) {
			name := "Duration " + route
			id, secret := f.register(t, name)
			path := "/api/public/registrations/" + id + "/appointment"
			var cookie *http.Cookie
			if route == "family" {
				login := portalRequest(f.router, "POST", "/api/family/auth/access", map[string]string{
					"child_name": name, "date_of_birth": "2022-05-01", "token": secret,
				}, nil, "")
				expectCode(t, login, 200)
				cookie = login.Result().Cookies()[0]
				path = "/api/family/appointments"
			} else if route == "admin" {
				cookie = f.adminCookie
				path = "/api/admin/appointments"
			}
			body := map[string]string{
				"registration_id": id, "doctor_id": f.doctors[0].ID,
				"starts_at": f.start.Format(time.RFC3339),
				"ends_at":   f.start.Add(30*time.Minute + time.Second).Format(time.RFC3339),
			}
			rejected := portalRequest(f.router, "POST", path, body, cookie, secret)
			expectCode(t, rejected, 400)
			if !strings.Contains(rejected.Body.String(), "cannot exceed 30 minutes") {
				t.Fatalf("unexpected duration error: %s", rejected.Body.String())
			}
			body["ends_at"] = f.start.Add(30 * time.Minute).Format(time.RFC3339)
			accepted := portalRequest(f.router, "POST", path, body, cookie, secret)
			expectCode(t, accepted, 201)
			appointmentID := decodeResponse[map[string]any](t, accepted)["id"].(string)
			expectCode(t, portalRequest(f.router, "POST", "/api/admin/appointments/"+appointmentID+"/decision", map[string]string{"action": "cancel"}, f.adminCookie, ""), 200)
		})
	}
}

func TestAdminCannotExtendConfirmedConsultationPastThirtyMinutes(t *testing.T) {
	f := newPaidFixture(t)
	id, secret := f.register(t, "Duration decision")
	appointmentID := f.book(t, id, secret, f.doctors[0].ID, f.start)
	order := f.order(t, id, secret)
	if err := f.server.applyPayment(context.Background(), Payment{
		ID: "pay_" + token()[:16], OrderID: order.ID, Amount: order.Amount,
		Currency: "INR", Status: "captured", Captured: true,
	}, "duration-"+id); err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/appointments/" + appointmentID + "/decision"
	for _, action := range []string{"confirm", "reschedule"} {
		body := map[string]string{
			"action": action, "starts_at": f.start.Format(time.RFC3339),
			"ends_at": f.start.Add(time.Hour).Format(time.RFC3339),
		}
		rejected := portalRequest(f.router, "POST", path, body, f.adminCookie, "")
		expectCode(t, rejected, 400)
		if !strings.Contains(rejected.Body.String(), "cannot exceed 30 minutes") {
			t.Fatalf("unexpected decision error: %s", rejected.Body.String())
		}
		appointments := decodeResponse[[]Appointment](t, portalRequest(f.router, "GET", "/api/appointments", nil, f.adminCookie, ""))
		found := false
		for _, appointment := range appointments {
			if appointment.ID == appointmentID {
				found = true
				if appointment.EndsAt.Sub(appointment.StartsAt) != 30*time.Minute {
					t.Fatal("rejected extension changed the appointment")
				}
			}
		}
		if !found {
			t.Fatal("appointment disappeared after rejected extension")
		}
		body["ends_at"] = f.start.Add(30 * time.Minute).Format(time.RFC3339)
		expectCode(t, portalRequest(f.router, "POST", path, body, f.adminCookie, ""), 200)
		f.status(t, appointmentID, "confirmed")
	}
}
