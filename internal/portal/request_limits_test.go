package portal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestPortalRequestsBeyondFormerLimits(t *testing.T) {
	for _, test := range []struct {
		name, method, path string
		body               map[string]string
		attempts, want     int
	}{
		{"intake", "POST", "/api/public/registrations", map[string]string{}, 2001, 400},
		{"child access without credential", "POST", "/api/family/auth/access", map[string]string{"guardian_name": "Test Guardian", "child_name": "Private Child", "date_of_birth": "2022-05-01"}, 11, 401},
		{"recovery without credential", "POST", "/api/family/auth/recover", map[string]string{"guardian_name": "Test Guardian", "child_name": "Private Child", "date_of_birth": "2022-05-01"}, 11, 401},
		{"retired signup", "POST", "/api/family/auth/register", map[string]string{"name": "Test Guardian", "email": "test@example.invalid", "password": "test-password-only-123"}, 6, 410},
		{"retired password login", "POST", "/api/family/auth/login", map[string]string{"email": "test@example.invalid", "password": "test-password-only-123"}, 11, 410},
		{"checkout without credential", "POST", "/api/public/registrations/00000000-0000-0000-0000-000000000001/appointment", map[string]string{}, 121, 404},
		{"admin issuance without session", "POST", "/api/admin/registrations/00000000-0000-0000-0000-000000000001/family-access", map[string]string{}, 11, 401},
		{"family records without session", "GET", "/api/family/registrations", nil, 11, 401},
		{"staff records without session", "GET", "/api/registrations", nil, 11, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := New(nil, Options{})
			router := chi.NewRouter()
			service.Routes(router)
			for attempt := 1; attempt <= test.attempts; attempt++ {
				w := portalRequest(router, test.method, test.path, test.body, nil, "")
				if w.Code != test.want || w.Header().Get("Retry-After") != "" {
					t.Fatalf("attempt %d: HTTP %d, want %d; retry=%q", attempt, w.Code, test.want, w.Header().Get("Retry-After"))
				}
				if len(w.Result().Cookies()) != 0 {
					t.Fatal("rejected request created a session")
				}
			}
		})
	}
}

func TestRetiredFamilyEndpointsBeyondFormerEmailLimits(t *testing.T) {
	for _, path := range []string{"/api/family/auth/login", "/api/family/auth/register"} {
		t.Run(path, func(t *testing.T) {
			service := New(nil, Options{})
			router := chi.NewRouter()
			service.Routes(router)
			body := map[string]string{"email": "same@example.invalid", "password": "test-password-only-123"}
			if strings.HasSuffix(path, "/register") {
				body["name"] = "Test Guardian"
			}
			for attempt := 1; attempt <= 12; attempt++ {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.RemoteAddr = fmt.Sprintf("192.0.2.%d:12345", attempt)
					router.ServeHTTP(w, r)
				})
				expectCode(t, portalRequest(handler, "POST", path, body, nil, ""), 410)
			}
		})
	}
}

func TestRepeatedPortalRequestsKeepOriginAndBodyProtection(t *testing.T) {
	service := New(nil, Options{})
	router := chi.NewRouter()
	service.Routes(router)
	for _, test := range []struct {
		name, origin, header, payload string
		want                          int
	}{
		{"foreign origin", "https://other.example.invalid", "1", `{}`, 403},
		{"missing request header", service.Origin(), "", `{}`, 403},
		{"oversized body", service.Origin(), "1", `{"child_name":"` + strings.Repeat("x", 33<<10) + `"}`, 400},
		{"unknown field", service.Origin(), "1", `{"unexpected":true}`, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			for attempt := 0; attempt < 12; attempt++ {
				r := httptest.NewRequest("POST", "/api/family/auth/access", strings.NewReader(test.payload))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", test.origin)
				r.Header.Set("X-Madamgy-Request", test.header)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				expectCode(t, w, test.want)
			}
		})
	}
}

func TestRepeatedFamilyAccessAndCheckoutKeepCredentials(t *testing.T) {
	f := newPaidFixture(t)
	id, secret := f.register(t, "Repeated Access Child")
	input := map[string]string{"guardian_name": "Test Guardian", "child_name": "Repeated Access Child", "date_of_birth": "2022-05-01", "token": token()}
	for attempt := 0; attempt < 12; attempt++ {
		expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/access", input, nil, ""), 401)
		expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", input, nil, ""), 401)
	}
	input["token"] = secret
	for attempt := 0; attempt < 12; attempt++ {
		w := portalRequest(f.router, "POST", "/api/family/auth/access", input, nil, "")
		expectCode(t, w, 200)
		if got := decodeResponse[Guardian](t, w); got.RegistrationID != id {
			t.Fatal("child session escaped its registration")
		}
	}
	path := "/api/public/registrations/" + id
	for attempt := 0; attempt < 121; attempt++ {
		expectCode(t, portalRequest(f.router, "GET", path, nil, nil, ""), 404)
		expectCode(t, portalRequest(f.router, "GET", path, nil, nil, token()), 404)
		expectCode(t, portalRequest(f.router, "GET", path, nil, nil, secret), 200)
		for _, action := range []string{"order", "verify"} {
			expectCode(t, portalRequest(f.router, "POST", path+"/"+action, map[string]string{}, nil, ""), 404)
		}
	}
	if f.gateway.creates != 0 {
		t.Fatal("unauthenticated checkout created a provider order")
	}
}

func TestRepeatedAdminRecoveryIssuanceKeepsSingleUse(t *testing.T) {
	f := newPaidFixture(t)
	id, _ := f.register(t, "Repeated Recovery Child")
	var grant string
	for attempt := 0; attempt < 12; attempt++ {
		grant = issueAccess(t, f, id)
	}
	input := map[string]string{"guardian_name": "Test Guardian", "child_name": "Repeated Recovery Child", "date_of_birth": "2022-05-01", "token": grant}
	expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", input, nil, ""), 200)
	for attempt := 0; attempt < 12; attempt++ {
		expectCode(t, portalRequest(f.router, "POST", "/api/family/auth/recover", input, nil, ""), 401)
	}
}
