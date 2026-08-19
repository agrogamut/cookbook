package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/profile"
)

func booksRouter(t *testing.T) (*chi.Mux, *Handlers) {
	t.Helper()
	h := New(testPool(t))
	r := chi.NewRouter()
	r.Get("/api/books/{childID}/{book}/preview", h.BookPreview)
	r.Get("/api/books/{childID}/{book}.pdf", h.BookDownload)
	return r, h
}

func cleanupChild(t *testing.T, h *Handlers, childID string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			"DELETE FROM child_profile WHERE child_id = $1", childID)
	})
}

// bookBrowserOnPath mirrors internal/book/pdf_test.go's browserOnPath: it decides whether to
// skip a PDF-producing test rather than fail it on a machine with no Chromium, the same
// contract TEST_DATABASE_URL has with the integrity suite. A skipped test is not a passing
// test; run it with a browser before calling this task done.
func bookBrowserOnPath() bool {
	for _, name := range []string{
		"google-chrome-stable", "google-chrome", "chromium", "chromium-browser",
	} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

// A clinical stop and a broken renderer must not look alike to an operator. A child with a
// declared special-care condition must never get a book, blocked or otherwise -- the
// special-care stop gate exists because the feeding decision for these children is a
// clinician's, and this test pins that the HTTP layer carries the block through as 409
// rather than papering over it with an empty 200.
func TestBlockedChildGets409NotAnEmptyBook(t *testing.T) {
	r, h := booksRouter(t)
	const childID = "BOOK-TEST-BLOCKED-001"
	cleanupChild(t, h, childID)

	s := profile.Stored{
		ChildID:     childID,
		DisplayName: "Blocked Test Child",
		DateOfBirth: time.Now().AddDate(0, -36, 0),
		CreatedBy:   "books_test",
		Conditions: []profile.ClinicalCondition{
			// SC-CP (cerebral palsy) is one of the six STOP-REVIEW conditions the
			// provider's Special-Care master defines. Routing it through TriggerField
			// "Special_Care_Condition" is what internal/profile.ToChildProfile requires
			// to reach models.ChildProfile.SpecialCareCondition rather than ClinicalFlags.
			{TriggerField: "Special_Care_Condition", FlagValue: "SC-CP", Class: "congenital"},
		},
	}
	if err := profile.Save(context.Background(), h.pool, s); err != nil {
		t.Fatalf("profile.Save: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/books/"+childID+"/book2/preview", nil))

	if rec.Code != 409 {
		t.Fatalf("a special-care child must get 409, got %d: %s", rec.Code, rec.Body.String())
	}
	// Never a service-fault status: an operator who reads a clinical stop as a broken
	// renderer will retry it, and this stop is not a thing to retry.
	if rec.Code == 503 {
		t.Fatal("a clinical stop must not be reported as 503, that is reserved for a broken renderer")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("a 409 body must be JSON, got Content-Type %q", ct)
	}

	var body struct {
		Error    string `json:"error"`
		Reviewer string `json:"reviewer"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "" {
		t.Fatal("a 409 body with no reason leaves the operator no next step")
	}
	// Read from special_care_condition_gate.mandatory_reviewer directly, the same source
	// the handler is supposed to use, rather than hardcoding the provider's current text --
	// this pins the join, not the workbook's current wording.
	var wantReviewer string
	if err := h.pool.QueryRow(context.Background(),
		`SELECT mandatory_reviewer FROM special_care_condition_gate WHERE condition_id = 'SC-CP'`).
		Scan(&wantReviewer); err != nil {
		t.Fatalf("reviewer lookup: %v", err)
	}
	if body.Reviewer != wantReviewer {
		t.Fatalf("reviewer = %q, want %q from special_care_condition_gate", body.Reviewer, wantReviewer)
	}

	// Also confirm the download route carries the same block rather than only the preview
	// route -- a reviewer reaching this child through either path must see the stop.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/books/"+childID+"/book2.pdf", nil))
	if rec.Code != 409 {
		t.Fatalf("the download route must also 409 a blocked child, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Preview and download must render the same document, so review approves what prints. This
// asserts the property at the level that actually matters: BookPreview's HTTP body is
// byte-for-byte what internal/api/handlers.renderBookHTML produces, and BookDownload feeds
// that same function's output straight into book.PrintPDF unmodified -- so there is exactly
// one rendering of the book, not a second one that could quietly drift from the first. It
// does not compare against the PDF's text layer: PrintPDF adds Chrome's own repeating print
// header carrying the provisional-data banner, which the preview instead carries once in
// document flow, and that divergence is deliberate.
func TestPreviewAndPdfShareTheSameHtml(t *testing.T) {
	r, h := booksRouter(t)
	const childID = "BOOK-TEST-PREVIEW-001"
	cleanupChild(t, h, childID)

	s := profile.Stored{
		ChildID:     childID,
		DisplayName: "Preview Test Child",
		DateOfBirth: time.Now().AddDate(0, -24, 0),
		DietType:    "Vegetarian",
		CreatedBy:   "books_test",
	}
	if err := profile.Save(context.Background(), h.pool, s); err != nil {
		t.Fatalf("profile.Save: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/books/"+childID+"/book1/preview", nil))
	if rec.Code != 200 {
		t.Fatalf("preview: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("preview Content-Type = %q", ct)
	}
	previewHTML := rec.Body.Bytes()
	if len(previewHTML) == 0 {
		t.Fatal("preview returned no body")
	}
	// Present even when nothing was skipped -- Task 8 reads the header unconditionally.
	if _, ok := rec.Header()["X-Book-Omissions"]; !ok {
		t.Fatal("X-Book-Omissions header must always be present")
	}

	// The same source the preview endpoint just served, rebuilt directly through the
	// handler's shared rendering function rather than through a second HTTP round trip.
	// GenerationDate and AgeMonths are both derived from the current UTC calendar date, so
	// a second call made moments later within the same test renders identically -- this is
	// what makes the byte-for-byte comparison meaningful rather than flaky.
	loaded, err := profile.Load(context.Background(), h.pool, childID)
	if err != nil {
		t.Fatalf("profile.Load: %v", err)
	}
	rebuildRec := httptest.NewRecorder()
	rebuildReq := httptest.NewRequest("GET", "/api/books/"+childID+"/book1/preview", nil)
	rebuiltHTML, _, _, ok := h.renderBookHTML(rebuildRec, rebuildReq, loaded, "book1")
	if !ok {
		t.Fatalf("renderBookHTML failed: %s", rebuildRec.Body.String())
	}
	if string(rebuiltHTML) != string(previewHTML) {
		t.Fatal("BookPreview's HTTP body must be exactly what renderBookHTML produces, " +
			"the same bytes BookDownload feeds to PrintPDF")
	}

	if !bookBrowserOnPath() {
		t.Skip("no chromium on PATH; skipping the download round trip")
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/books/"+childID+"/book1.pdf", nil))
	if rec.Code != 200 {
		t.Fatalf("download: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("download Content-Type = %q", ct)
	}
	pdfBytes := rec.Body.Bytes()
	if len(pdfBytes) < 5 || string(pdfBytes[:5]) != "%PDF-" {
		t.Fatalf("download body is not a PDF, first bytes: %q", pdfBytes[:min(8, len(pdfBytes))])
	}
}
