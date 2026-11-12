package portal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestPublicIntakeRateLimit(t *testing.T) {
	service := New(nil, Options{})
	router := chi.NewRouter()
	service.Routes(router)
	submit := func(address string) *httptest.ResponseRecorder {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.RemoteAddr = address
			router.ServeHTTP(w, r)
		})
		// Invalid input exercises the real route and limiter without creating records.
		return portalRequest(handler, "POST", "/api/public/registrations", map[string]string{}, nil, "")
	}

	started := time.Now()
	for attempt := 1; attempt <= 2000; attempt++ {
		w := submit(fmt.Sprintf("192.0.2.10:%d", 10000+attempt))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: HTTP %d, want input validation 400: %s", attempt, w.Code, w.Body.String())
		}
	}
	const key = "intake:192.0.2.10"
	entry := service.limits.entries[key]
	if entry.until.Before(started.Add(time.Minute)) || entry.until.After(time.Now().Add(time.Minute)) {
		t.Fatalf("unexpected window expiry: %s", entry.until)
	}
	w := submit("192.0.2.10:30001")
	expectCode(t, w, http.StatusTooManyRequests)
	if w.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", w.Header().Get("Retry-After"))
	}
	if !service.limits.entries[key].until.Equal(entry.until) {
		t.Fatal("rejected request extended the fixed window")
	}
	expectCode(t, submit("192.0.2.11:30001"), http.StatusBadRequest)

	// Advance only this test's window instead of sleeping for a minute.
	service.limits.mu.Lock()
	entry.until = time.Now().Add(-time.Second)
	service.limits.entries[key] = entry
	service.limits.mu.Unlock()
	expectCode(t, submit("192.0.2.10:30002"), http.StatusBadRequest)
	if service.limits.entries[key].count != 1 {
		t.Fatal("expired window did not reset the request count")
	}
}
