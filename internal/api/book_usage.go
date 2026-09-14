package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/madamgy/recipie/internal/aidraft"
)

// bookUsage includes assembly, translation and PDF printing in elapsed_ms. Usage
// call_ms is additive across concurrent requests and is not wall-clock time.
func bookUsage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := slog.Default()
		ctx, usage := aidraft.WithUsage(r.Context(), logger)
		r = r.WithContext(ctx)
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		started := time.Now()
		defer func() {
			status := wrapped.Status()
			if status == 0 {
				status = http.StatusInternalServerError
			}
			logger.InfoContext(ctx, "book_generation", "route", chi.RouteContext(ctx).RoutePattern(),
				"status", status, "elapsed_ms", time.Since(started).Milliseconds(),
				"usage", usage.Snapshot())
		}()
		next.ServeHTTP(wrapped, r)
	})
}
