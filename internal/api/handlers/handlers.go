// Package handlers holds one file per API resource. Every handler reads from the pool
// directly or calls internal/engine -- there is no service layer in between, since
// every query here is either a single view read or the engine pipeline, and an extra
// layer would not do anything a handler function can't do itself (YAGNI).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/bookdocx"
)

type Handlers struct {
	pool    *pgxpool.Pool
	drafter aidraft.Drafter
	reader  readerRenderer
}

type readerRenderer func(context.Context, bookdocx.Request, book.ReaderOptions) (book.ReaderResult, error)

func New(pool *pgxpool.Pool, drafter aidraft.Drafter) *Handlers {
	return &Handlers{pool: pool, drafter: drafter, reader: book.RenderReader}
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
