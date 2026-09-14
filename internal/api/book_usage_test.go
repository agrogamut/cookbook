package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func TestBookUsageLogsStatusAndDeadlineWithoutProfileIdentifiers(t *testing.T) {
	for _, status := range []int{200, 500, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			r := chi.NewRouter()
			r.Use(bookUsage)
			r.Use(middleware.Timeout(time.Millisecond))
			r.Get("/books/{childID}", func(w http.ResponseWriter, r *http.Request) {
				if status == 504 {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(status)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/books/PRIVATE_CHILD?name=PRIVATE_NAME", nil))
			var record struct {
				Status  int    `json:"status"`
				Route   string `json:"route"`
				Elapsed int64  `json:"elapsed_ms"`
			}
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if w.Code != status || record.Status != status || record.Route != "/books/{childID}" || strings.Contains(logs.String(), "PRIVATE_") {
				t.Fatalf("incorrect or identifying log: %s", logs.String())
			}
			if status == 504 && record.Elapsed < 1 {
				t.Fatal("request duration omitted the timeout wait")
			}
		})
	}
}
