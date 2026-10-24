package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/profile"
)

// bookOmissionHeader carries the skipped-facts list every book assembler returns, so a
// reviewer sees what a book does not contain without reading the whole document. Task 8's
// frontend parses this header by name and by the "; " separator between entries -- both are
// part of the wire contract fixed here, not an implementation detail.
const bookOmissionHeader = "X-Book-Omissions"

// joinOmissions renders an omission list into the header value. The "; " separator is part
// of the wire contract the console parses on, so it is written once here rather than at each
// call site where one of them could drift.
func joinOmissions(omissions []string) string {
	return strings.Join(omissions, "; ")
}

// loadBookProfile loads the child's stored profile, writing a 404 the same way GetProfile
// does when none exists. ok is false once the response has already been written, so the
// caller can just return.
func (h *Handlers) loadBookProfile(w http.ResponseWriter, r *http.Request, childID string) (profile.Stored, bool) {
	s, err := profile.Load(r.Context(), h.pool, childID)
	if errors.Is(err, profile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no profile for that child id")
		return profile.Stored{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile load failed: "+err.Error())
		return profile.Stored{}, false
	}
	return s, true
}

// renderBookHTML wraps the native PDF selected from one complete book set.
func (h *Handlers) renderBookHTML(w http.ResponseWriter, r *http.Request, s profile.Stored, kind string) (htmlDoc []byte, meta book.Metadata, omissions []string, ok bool) {
	if kind != "book1" && kind != "book2" {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown book %q: must be book1 or book2", kind))
		return nil, book.Metadata{}, nil, false
	}
	resp, set, ok := h.renderSet(w, r, s)
	if !ok {
		return nil, book.Metadata{}, nil, false
	}
	if kind == "book1" {
		return []byte(resp.Book1), set.Book1.Metadata, omissionsFor(kind, resp), true
	}
	return []byte(resp.Book2), set.Book2.Metadata, omissionsFor(kind, resp), true
}

// BookPreview embeds the completed native PDF in a standalone preview.
func (h *Handlers) BookPreview(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadBookProfile(w, r, chi.URLParam(r, "childID"))
	if !ok {
		return
	}
	htmlDoc, _, omissions, ok := h.renderBookHTML(w, r, s, chi.URLParam(r, "book"))
	if !ok {
		return
	}

	w.Header().Set(bookOmissionHeader, joinOmissions(omissions))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(htmlDoc)
}

// BookDownload serves the completed native PDF without another print pass.
func (h *Handlers) BookDownload(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadBookProfile(w, r, chi.URLParam(r, "childID"))
	if !ok {
		return
	}
	kind := chi.URLParam(r, "book")
	if kind != "book1" && kind != "book2" {
		writeError(w, http.StatusNotFound, "unknown book: must be book1 or book2")
		return
	}
	resp, _, ok := h.renderSet(w, r, s)
	if !ok {
		return
	}
	pdf := resp.pdfs[0]
	if kind == "book2" {
		pdf = resp.pdfs[1]
	}
	omissions := omissionsFor(kind, resp)

	w.Header().Set(bookOmissionHeader, joinOmissions(omissions))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", chi.URLParam(r, "childID")+"-"+kind+".pdf"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}
