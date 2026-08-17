// Package handlers holds one file per API resource. Every handler reads from the pool
// directly or calls internal/engine -- there is no service layer in between, since
// every query here is either a single view read or the engine pipeline, and an extra
// layer would not do anything a handler function can't do itself (YAGNI).
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handlers struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Handlers {
	return &Handlers{pool: pool}
}

func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
