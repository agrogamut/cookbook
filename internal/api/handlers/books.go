package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/profile"
)

// bookOmissionHeader carries the skipped-facts list every book assembler returns, so a
// reviewer sees what a book does not contain without reading the whole document. Task 8's
// frontend parses this header by name and by the "; " separator between entries -- both are
// part of the wire contract fixed here, not an implementation detail.
const bookOmissionHeader = "X-Book-Omissions"

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

// renderBookHTML assembles and renders one book ("book1" or "book2") for the given stored
// profile, as of now. It is the single source of the HTML both BookPreview and BookDownload
// serve -- BookDownload feeds htmlDoc straight into book.PrintPDF, unmodified -- so there is
// exactly one rendering of a book and no way for the preview and the print to disagree.
//
// On any failure it writes the response itself and returns ok=false: a blocked engine gets
// its own 409 via writeBlocked, everything else gets a 500. There is deliberately one
// failure path shared by both endpoints rather than two that could drift apart.
func (h *Handlers) renderBookHTML(w http.ResponseWriter, r *http.Request, s profile.Stored, kind string) (htmlDoc []byte, meta book.Metadata, omissions []string, ok bool) {
	ctx := r.Context()
	asOf := time.Now().UTC()

	var data any
	var bookKind book.Kind
	switch kind {
	case "book1":
		b1, dropped, err := book.AssembleBook1(ctx, h.pool, s, asOf)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "book1 assembly failed: "+err.Error())
			return nil, book.Metadata{}, nil, false
		}
		data, meta, omissions, bookKind = b1, b1.Metadata, dropped, book.Kind1

	case "book2":
		b2, dropped, err := book.AssembleBook2(ctx, h.pool, s, asOf)
		if err != nil {
			if errors.Is(err, book.ErrBlocked) {
				h.writeBlocked(w, r, s, asOf, err)
				return nil, book.Metadata{}, nil, false
			}
			writeError(w, http.StatusInternalServerError, "book2 assembly failed: "+err.Error())
			return nil, book.Metadata{}, nil, false
		}
		data, meta, omissions, bookKind = b2, b2.Metadata, dropped, book.Kind2

	default:
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown book %q: must be book1 or book2", kind))
		return nil, book.Metadata{}, nil, false
	}

	var buf bytes.Buffer
	if err := book.RenderHTML(&buf, bookKind, meta, data); err != nil {
		writeError(w, http.StatusInternalServerError, "render failed: "+err.Error())
		return nil, book.Metadata{}, nil, false
	}
	return buf.Bytes(), meta, omissions, true
}

// writeBlocked returns 409 for a clinician's stop gate, distinct from the 503 an unavailable
// PDF renderer gets: an operator who reads a clinical stop as a service fault will retry it,
// and a stop is not a thing to retry.
//
// The reviewer name comes from a fresh, direct read of special_care_condition_gate rather
// than from parsing err's text -- the assembler's message is prose for a human, and the hard
// rule against inventing data applies just as much to a field pulled out of that prose by
// string matching as to one guessed outright. A block can also come from the clinical-rule
// filter rather than the special-care stop gate; that child carries no special-care
// condition id, there is nothing to look up, and the field is omitted rather than filled
// with a guess.
func (h *Handlers) writeBlocked(w http.ResponseWriter, r *http.Request, s profile.Stored, asOf time.Time, err error) {
	reason := strings.TrimPrefix(err.Error(), book.ErrBlocked.Error()+": ")
	body := map[string]string{"error": reason}

	if cp, _, cerr := s.ToChildProfile(asOf); cerr == nil && cp.SpecialCareCondition != "" {
		var reviewer string
		qerr := h.pool.QueryRow(r.Context(),
			`SELECT coalesce(mandatory_reviewer, '') FROM special_care_condition_gate WHERE condition_id = $1`,
			cp.SpecialCareCondition).Scan(&reviewer)
		if qerr == nil && reviewer != "" {
			body["reviewer"] = reviewer
		}
	}

	writeJSON(w, http.StatusConflict, body)
}

// BookPreview returns the rendered book as HTML -- the same document BookDownload prints
// from, so a reviewer approves the artifact that ships rather than a second rendering of it.
func (h *Handlers) BookPreview(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadBookProfile(w, r, chi.URLParam(r, "childID"))
	if !ok {
		return
	}
	htmlDoc, _, omissions, ok := h.renderBookHTML(w, r, s, chi.URLParam(r, "book"))
	if !ok {
		return
	}

	w.Header().Set(bookOmissionHeader, strings.Join(omissions, "; "))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(htmlDoc)
}

// BookDownload prints the same HTML BookPreview returns to PDF and serves it. 503, not 500,
// when Chromium itself is unavailable: that is a broken dependency, not anything about this
// child's data, and an operator must not read it as a reason to change the request.
func (h *Handlers) BookDownload(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadBookProfile(w, r, chi.URLParam(r, "childID"))
	if !ok {
		return
	}
	kind := chi.URLParam(r, "book")
	htmlDoc, meta, omissions, ok := h.renderBookHTML(w, r, s, kind)
	if !ok {
		return
	}

	pdf, err := book.PrintPDF(r.Context(), htmlDoc, meta)
	if err != nil {
		if errors.Is(err, book.ErrChromiumUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "pdf renderer unavailable: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "pdf render failed: "+err.Error())
		return
	}

	w.Header().Set(bookOmissionHeader, strings.Join(omissions, "; "))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", chi.URLParam(r, "childID")+"-"+kind+".pdf"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}
