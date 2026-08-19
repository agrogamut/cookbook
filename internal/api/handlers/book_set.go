package handlers

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/profile"
)

// bookSetResponse is the wire shape of one generation run. Both books are returned rendered,
// because the set exists so that an operator never has to make two requests and hope they
// describe the same child.
//
// Omissions are split three ways rather than concatenated: an operator needs to know whether
// a missing thing is a fact about the child's stored profile (and so affects both books) or
// about one book's own corpus coverage. Every list is non-nil, so a client can render it
// without a null check.
type bookSetResponse struct {
	ChildID string    `json:"child_id"`
	AsOf    time.Time `json:"as_of"`
	Book1   string    `json:"book1_html"`
	Book2   string    `json:"book2_html"`

	ProfileOmissions []string `json:"profile_omissions"`
	Book1Omissions   []string `json:"book1_omissions"`
	Book2Omissions   []string `json:"book2_omissions"`
}

// renderSet assembles both books and renders both to HTML.
//
// On any failure it writes the response itself and returns ok=false, routing ErrBlocked to
// the same 409 the single-book endpoints use. A set is all-or-nothing: a stop gate withholds
// both books, and an assembly failure in either one fails the run rather than serving half a
// deliverable an operator might mistake for the whole thing.
func (h *Handlers) renderSet(w http.ResponseWriter, r *http.Request, s profile.Stored) (bookSetResponse, book.Set, bool) {
	ctx := r.Context()
	asOf := time.Now().UTC()

	set, err := book.AssembleSet(ctx, h.pool, s, asOf)
	if err != nil {
		if errors.Is(err, book.ErrBlocked) {
			h.writeBlocked(w, r, s, asOf, err)
			return bookSetResponse{}, book.Set{}, false
		}
		writeError(w, http.StatusInternalServerError, "book set assembly failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}

	var buf1, buf2 bytes.Buffer
	if err := book.RenderHTML(&buf1, book.Kind1, set.Book1.Metadata, set.Book1); err != nil {
		writeError(w, http.StatusInternalServerError, "book1 render failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}
	if err := book.RenderHTML(&buf2, book.Kind2, set.Book2.Metadata, set.Book2); err != nil {
		writeError(w, http.StatusInternalServerError, "book2 render failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}

	return bookSetResponse{
		ChildID:          set.ChildID,
		AsOf:             set.AsOf,
		Book1:            buf1.String(),
		Book2:            buf2.String(),
		ProfileOmissions: set.ProfileOmissions,
		Book1Omissions:   set.Book1Omissions,
		Book2Omissions:   set.Book2Omissions,
	}, set, true
}

// BookSetPreview runs one generation and returns both of the child's books.
//
// This is the endpoint the console uses. The per-book endpoints remain for fetching one book
// directly, but a run is a set: both books come from a single stored profile read at a single
// instant, so the two can never disagree about the child they describe.
func (h *Handlers) BookSetPreview(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadBookProfile(w, r, chi.URLParam(r, "childID"))
	if !ok {
		return
	}
	resp, _, ok := h.renderSet(w, r, s)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// BookSetDownload prints both books and serves them as one zip.
//
// A zip of two PDFs rather than one merged PDF: they are two books, with their own covers and
// their own page numbering, and merging them would renumber Book 2's pages behind Book 1's so
// that a page reference printed inside either book stops matching the page it is on.
//
// The omissions header carries every omission from the run, marked by which book reported it,
// because a zip has nowhere else to put them.
func (h *Handlers) BookSetDownload(w http.ResponseWriter, r *http.Request) {
	childID := chi.URLParam(r, "childID")
	s, ok := h.loadBookProfile(w, r, childID)
	if !ok {
		return
	}
	resp, set, ok := h.renderSet(w, r, s)
	if !ok {
		return
	}

	pdfs := make([][]byte, 0, 2)
	for _, b := range []struct {
		name string
		html string
		meta book.Metadata
	}{
		{"book1", resp.Book1, set.Book1.Metadata},
		{"book2", resp.Book2, set.Book2.Metadata},
	} {
		pdf, err := book.PrintPDF(r.Context(), []byte(b.html), b.meta)
		if err != nil {
			// Same split the single-book download makes: a missing browser is an install
			// problem an operator cannot fix by retrying, a failed print may be about this
			// document. Reported against the book that failed, since the two books print
			// separately and only one of them may be at fault.
			if errors.Is(err, book.ErrChromiumUnavailable) {
				writeError(w, http.StatusServiceUnavailable,
					"pdf renderer unavailable: "+err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("pdf render failed for %s: %v", b.name, err))
			return
		}
		pdfs = append(pdfs, pdf)
	}

	// Built in memory and written whole, so a print failure on the second book cannot leave
	// a half-written archive already streamed to the operator with a 200 on it.
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for i, name := range []string{"book1", "book2"} {
		f, err := zw.Create(fmt.Sprintf("%s-%s.pdf", childID, name))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "zip create failed: "+err.Error())
			return
		}
		if _, err := f.Write(pdfs[i]); err != nil {
			writeError(w, http.StatusInternalServerError, "zip write failed: "+err.Error())
			return
		}
	}
	if err := zw.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "zip close failed: "+err.Error())
		return
	}

	all := make([]string, 0,
		len(resp.ProfileOmissions)+len(resp.Book1Omissions)+len(resp.Book2Omissions))
	all = append(all, resp.ProfileOmissions...)
	for _, o := range resp.Book1Omissions {
		all = append(all, "book1: "+o)
	}
	for _, o := range resp.Book2Omissions {
		all = append(all, "book2: "+o)
	}

	w.Header().Set(bookOmissionHeader, joinOmissions(all))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", childID+"-books.zip"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive.Bytes())
}
