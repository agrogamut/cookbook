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

// renderBookHTML assembles and renders one book ("book1" or "book2") for the given stored
// profile, as of now. It is the single source of the HTML both BookPreview and BookDownload
// serve -- BookDownload feeds htmlDoc straight into book.PrintPDF, unmodified -- so there is
// exactly one rendering of a book and no way for the preview and the print to disagree.
//
// On any failure it writes the response itself and returns ok=false, always a 500. There is
// deliberately one failure path shared by both endpoints rather than two that could drift
// apart.
//
// There is no 409 here any more. It used to be the clinician stop gate's status, and that
// gate is gone (SP1: see docs/superpowers/specs/2026-09-05-direct-generation-design.md);
// nothing about this child's clinical state refuses to produce a document. 503 for an
// unavailable renderer and 500 for a failed print are untouched and still have to read
// differently from each other.
func (h *Handlers) renderBookHTML(w http.ResponseWriter, r *http.Request, s profile.Stored, kind string) (htmlDoc []byte, meta book.Metadata, omissions []string, ok bool) {
	ctx := r.Context()
	asOf := time.Now().UTC()

	var data any
	var bookKind book.Kind
	switch kind {
	case "book1":
		b1, dropped, err := book.AssembleBook1(ctx, h.pool, s, asOf, book.WithDrafter(h.drafter))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "book1 assembly failed: "+err.Error())
			return nil, book.Metadata{}, nil, false
		}
		data, meta, omissions, bookKind = b1, b1.Metadata, dropped, book.Kind1

	case "book2":
		b2, dropped, err := book.AssembleBook2(ctx, h.pool, s, asOf, book.WithDrafter(h.drafter))
		if err != nil {
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

	w.Header().Set(bookOmissionHeader, joinOmissions(omissions))
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

	w.Header().Set(bookOmissionHeader, joinOmissions(omissions))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", chi.URLParam(r, "childID")+"-"+kind+".pdf"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}
