package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madamgy/recipie/internal/aidraft"
)

func TestExistingRoutesRequireStaff(t *testing.T) {
	router := NewRouter(nil, aidraft.Disabled)
	for _, request := range []struct{ method, path string }{
		{"GET", "/api/recipes/test"}, {"GET", "/api/ingredients"}, {"GET", "/api/audit/nutrition"}, {"GET", "/api/gaps"}, {"GET", "/api/runs"},
		{"GET", "/api/reference/regions"}, {"GET", "/api/profiles/test"}, {"PUT", "/api/profiles/test"}, {"GET", "/api/profiles/test/engine-input"},
		{"GET", "/api/profile-matches?case_id=test"}, {"POST", "/api/search"}, {"POST", "/api/books/generate"},
		{"POST", "/api/books/generate.zip"}, {"POST", "/api/books/generate/book1.pdf"}, {"GET", "/api/books/test/preview"},
		{"GET", "/api/books/test/book1/preview"}, {"GET", "/api/books/test/books.zip"}, {"GET", "/api/books/test/book1.pdf"},
		{"GET", "/api/admin/staff"}, {"GET", "/api/registrations"},
	} {
		t.Run(request.path, func(t *testing.T) {
			r := httptest.NewRequest(request.method, request.path, nil)
			r.Header.Set("X-Madamgy-Request", "1")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Fatal("health check is not public")
	}
}
