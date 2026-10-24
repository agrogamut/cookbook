package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
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
	pdfs                [2][]byte
	docx                [2][]byte
	ChildID             string    `json:"child_id"`
	AsOf                time.Time `json:"as_of"`
	Book1               string    `json:"book1_html"`
	Book2               string    `json:"book2_html"`
	Book1TemplateSHA256 string    `json:"book1_template_sha256"`
	Book2TemplateSHA256 string    `json:"book2_template_sha256"`

	ProfileOmissions []string `json:"profile_omissions"`
	Book1Omissions   []string `json:"book1_omissions"`
	Book2Omissions   []string `json:"book2_omissions"`
}

// renderSet assembles one shared snapshot and converts both native masters.
func (h *Handlers) renderSet(w http.ResponseWriter, r *http.Request, s profile.Stored) (bookSetResponse, book.Set, bool) {
	return h.renderSetWithPhotos(w, r, s, nil, nil, nil)
}

// renderSetWithPhotos is renderSet with three optional images: the front cover's child
// photo, the back cover's parents' photo, and a photograph of an already-written
// prescription. All three are attached after assembly rather than carried through it --
// none changes a recipe, a filter or an omission, so threading any of them through the
// assemblers would put a decoration in the path of the clinical logic.
func (h *Handlers) renderSetWithPhotos(w http.ResponseWriter, r *http.Request, s profile.Stored, photo, parentsPhoto, prescriptionPhoto *book.ChildPhoto) (bookSetResponse, book.Set, bool) {
	ctx := r.Context()
	asOf := time.Now().UTC()

	set, err := book.AssembleSet(ctx, h.pool, s, asOf, book.WithDrafter(h.drafter))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "book set assembly failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}

	// Book 1 only, all three photos -- Book 2 is a working recipe document, its cover
	// carries no photo upload of any kind.
	if photo != nil {
		set.Book1.Child.Photo = photo
	}
	if parentsPhoto != nil {
		set.Book1.Metadata.ParentsPhoto = parentsPhoto
	}
	if prescriptionPhoto != nil {
		set.Book1.Metadata.PrescriptionPhoto = prescriptionPhoto
	}

	if h.converterErr != nil {
		writeError(w, http.StatusServiceUnavailable, "native document renderer unavailable: "+h.converterErr.Error())
		return bookSetResponse{}, book.Set{}, false
	}
	bindings := book.NativeBindings(s, set)
	if err := book.NativePhotos(&bindings, set); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return bookSetResponse{}, book.Set{}, false
	}
	if err := book.NativeIndices(ctx, h.pool, set, &bindings); err != nil {
		writeError(w, http.StatusInternalServerError, "source indices failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}
	if err := book.LocalizeNative(ctx, &bindings, h.drafter); err != nil {
		writeError(w, http.StatusInternalServerError, "dynamic localization failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}
	resp := bookSetResponse{ChildID: set.ChildID, AsOf: set.AsOf,
		ProfileOmissions: set.ProfileOmissions,
		Book1Omissions:   append(set.Book1Omissions, bindings.Omissions[0]...),
		Book2Omissions:   append(set.Book2Omissions, bindings.Omissions[1]...)}
	for i, request := range bindings.Books {
		result, err := h.converter.Render(ctx, request)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("book%d native render failed: %v", i+1, err))
			return bookSetResponse{}, book.Set{}, false
		}
		resp.pdfs[i], resp.docx[i] = result.PDF, result.DOCX
		if i == 0 {
			resp.Book1TemplateSHA256 = result.TemplateSHA256
		} else {
			resp.Book2TemplateSHA256 = result.TemplateSHA256
		}
		for _, omission := range result.Omissions {
			message := omission.Module + ": " + omission.Reason
			if i == 0 {
				resp.Book1Omissions = append(resp.Book1Omissions, message)
			} else {
				resp.Book2Omissions = append(resp.Book2Omissions, message)
			}
		}
	}
	resp.pdfs[0], err = book.AppendPrescription(ctx, resp.pdfs[0], set.Book1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "supplied prescription could not be retained: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}
	resp.Book1 = nativePDFPreview(resp.pdfs[0])
	resp.Book2 = nativePDFPreview(resp.pdfs[1])
	return resp, set, true
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
	h.writeBookZip(w, r, childID, resp, set)
}

// writeBookZip packages the completed PDFs from the same generation run.
func (h *Handlers) writeBookZip(w http.ResponseWriter, r *http.Request, name string, resp bookSetResponse, set book.Set) {

	pdfs := resp.pdfs

	// Built in memory and written whole, so a print failure on the second book cannot leave
	// a half-written archive already streamed to the operator with a 200 on it.
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for i, which := range []string{"book1", "book2"} {
		f, err := zw.Create(fmt.Sprintf("%s-%s.pdf", name, which))
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
		fmt.Sprintf("attachment; filename=%q", name+"-books.zip"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive.Bytes())
}

func nativePDFPreview(pdf []byte) string {
	return `<!doctype html><html><head><meta charset="utf-8"><title>Book PDF</title></head><body style="margin:0"><iframe title="Generated book PDF" style="border:0;width:100%;height:100vh" src="data:application/pdf;base64,` + base64.StdEncoding.EncodeToString(pdf) + `"></iframe></body></html>`
}
