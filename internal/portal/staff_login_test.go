package portal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/db"
)

func loginTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PORTAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PORTAL_TEST_DATABASE_URL not set; use an isolated local database")
	}
	pool, err := db.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestStaffLoginNotRateLimited(t *testing.T) {
	pool := loginTestPool(t)
	ctx := context.Background()
	const password = "test-password-only-123"
	for _, test := range []struct {
		name, role                             string
		inactive, wrongPassword, wrongIdentity bool
		want                                   int
		message                                string
	}{
		{name: "admin", role: "admin", want: 200},
		{name: "doctor", role: "doctor", want: 200},
		{name: "admin wrong password", role: "admin", wrongPassword: true, want: 401, message: "Email or password is incorrect."},
		{name: "doctor wrong password", role: "doctor", wrongPassword: true, want: 401, message: "Email or password is incorrect."},
		{name: "inactive admin", role: "admin", inactive: true, want: 401, message: "Email or password is incorrect, or staff access is inactive."},
		{name: "inactive doctor", role: "doctor", inactive: true, want: 401, message: "Email or password is incorrect, or staff access is inactive."},
		{name: "unknown account", want: 401, message: "Email or password is incorrect, or staff access is inactive."},
		{name: "identity mismatch", role: "admin", wrongIdentity: true, want: 401, message: "Email or password is incorrect."},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := newTestIdentity()
			service := New(pool, Options{Identity: identity})
			router := chi.NewRouter()
			service.Routes(router)
			email := "login-test-" + token()[:12] + "@example.invalid"
			if test.role != "" {
				actor, err := service.Provision(ctx, "Login Test Staff", email, password, test.role)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := pool.Exec(ctx, `DELETE FROM app_private.staff_account WHERE id=$1`, actor.ID); err != nil {
						t.Error(err)
					}
				})
				if test.inactive {
					if _, err := pool.Exec(ctx, `UPDATE app_private.staff_account SET active=false WHERE id=$1`, actor.ID); err != nil {
						t.Fatal(err)
					}
				}
				if test.wrongIdentity {
					identity.users[email] = Identity{ID: testUUID(), Email: email}
				}
			}
			input := map[string]string{"email": email, "password": password}
			if test.wrongPassword {
				input["password"] = "wrong-password"
			}
			for attempt := 1; attempt <= 32; attempt++ {
				w := portalRequest(router, "POST", "/api/auth/login", input, nil, "")
				if w.Code != test.want {
					t.Fatalf("attempt %d: HTTP %d, want %d: %s", attempt, w.Code, test.want, w.Body.String())
				}
				if test.want == 200 {
					actor := decodeResponse[Actor](t, w)
					if actor.Email != email || actor.Role != test.role || !actor.Active {
						t.Fatalf("unexpected staff identity: %+v", actor)
					}
					cookies := w.Result().Cookies()
					if len(cookies) != 1 || cookies[0].Name != sessionCookie || !cookies[0].HttpOnly {
						t.Fatal("successful login did not set the staff session cookie")
					}
					expectCode(t, portalRequest(router, "GET", "/api/auth/me", nil, cookies[0], ""), 200)
				} else {
					if got := decodeResponse[map[string]string](t, w)["error"]; got != test.message {
						t.Fatalf("error %q, want %q", got, test.message)
					}
					if len(w.Result().Cookies()) != 0 {
						t.Fatal("rejected login set a session cookie")
					}
				}
			}
		})
	}
}

func TestStaffLoginOriginProtection(t *testing.T) {
	service := New(nil, Options{Identity: newTestIdentity()})
	router := chi.NewRouter()
	service.Routes(router)
	for _, test := range []struct{ name, origin, header string }{
		{"foreign origin", "https://other.example", "1"},
		{"missing request header", service.Origin(), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			for attempt := 0; attempt < 32; attempt++ {
				r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", test.origin)
				r.Header.Set("X-Madamgy-Request", test.header)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				expectCode(t, w, 403)
				if got := decodeResponse[map[string]string](t, w)["error"]; got != "Request origin is not allowed." {
					t.Fatalf("unexpected error: %q", got)
				}
			}
		})
	}
}

func TestStaffLoginInvalidInputNotRateLimited(t *testing.T) {
	service := New(nil, Options{Identity: newTestIdentity()})
	router := chi.NewRouter()
	service.Routes(router)
	for attempt := 1; attempt <= 32; attempt++ {
		w := portalRequest(router, "POST", "/api/auth/login", map[string]string{}, nil, "")
		if w.Code != 400 {
			t.Fatalf("attempt %d: HTTP %d, want 400: %s", attempt, w.Code, w.Body.String())
		}
	}
}

func TestFamilyRouteRateLimitsRemain(t *testing.T) {
	for _, test := range []struct {
		path                   string
		maximum, initialStatus int
	}{
		{"/api/family/auth/login", 10, 400},
		{"/api/family/auth/register", 5, 400},
		{"/api/family/auth/access", 10, 401},
	} {
		t.Run(test.path, func(t *testing.T) {
			service := New(nil, Options{Identity: newTestIdentity()})
			router := chi.NewRouter()
			service.Routes(router)
			for attempt := 0; attempt < test.maximum; attempt++ {
				expectCode(t, portalRequest(router, "POST", test.path, map[string]string{}, nil, ""), test.initialStatus)
			}
			w := portalRequest(router, "POST", test.path, map[string]string{}, nil, "")
			expectCode(t, w, 429)
			if w.Header().Get("Retry-After") != "60" {
				t.Fatalf("missing rate-limit retry header: %v", w.Header())
			}
		})
	}
}

func TestFamilyLoginEmailRateLimitRemains(t *testing.T) {
	service := New(loginTestPool(t), Options{Identity: newTestIdentity()})
	router := chi.NewRouter()
	service.Routes(router)
	input := map[string]string{"email": "unknown-" + token()[:12] + "@example.invalid", "password": "wrong-password"}
	for attempt := 1; attempt <= 11; attempt++ {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.RemoteAddr = fmt.Sprintf("192.0.2.%d:12345", attempt)
			router.ServeHTTP(w, r)
		})
		w := portalRequest(handler, "POST", "/api/family/auth/login", input, nil, "")
		if attempt <= 10 {
			expectCode(t, w, 401)
		} else {
			expectCode(t, w, 429)
			if got := decodeResponse[map[string]string](t, w)["error"]; got != "Too many sign-in attempts. Try again in a minute." {
				t.Fatalf("unexpected email rate-limit error: %q", got)
			}
		}
	}
}
