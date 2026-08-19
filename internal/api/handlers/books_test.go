package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/profile"
)

func booksRouter(t *testing.T) (*chi.Mux, *Handlers) {
	t.Helper()
	h := New(testPool(t))
	r := chi.NewRouter()
	r.Get("/api/books/{childID}/preview", h.BookSetPreview)
	r.Get("/api/books/{childID}/books.zip", h.BookSetDownload)
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

// bookBrowserOnPath decides whether to skip a PDF-producing test rather than fail it on a
// machine with no Chromium, the same contract TEST_DATABASE_URL has with the integrity
// suite. A skipped test is not a passing test; run it with a browser before calling this
// task done.
//
// It delegates rather than keeping its own name list. The copy it used to keep listed four
// names where chromedp resolves twelve, so this would have skipped on a machine whose
// browser is installed as chrome or headless-shell -- hiding the print path on exactly the
// machines most likely to run it in CI.
func bookBrowserOnPath() bool { return book.BrowserAvailable() }

// specialCareConditionIDs reads every condition the provider's gate covers. Hardcoding one
// id would leave the other five unexercised, and the gate is only as good as its least
// covered row.
func specialCareConditionIDs(t *testing.T, h *Handlers) []string {
	t.Helper()
	rows, err := h.pool.Query(context.Background(),
		`SELECT condition_id FROM special_care_condition_gate ORDER BY condition_id`)
	if err != nil {
		t.Fatalf("load special-care condition ids: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan condition id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("condition id rows: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("special_care_condition_gate is empty; the stop gate cannot be exercised")
	}
	return ids
}

// A clinical stop and a broken renderer must not look alike to an operator. A child with a
// declared special-care condition must never get a book, blocked or otherwise -- the
// special-care stop gate exists because the feeding decision for these children is a
// clinician's, and this test pins that the HTTP layer carries the block through as 409
// rather than papering over it with an empty 200.
//
// Both books and both routes, over every condition the gate defines. Book 1 runs no engine
// of its own, so it was the branch where a STOP-REVIEW child got a 200 and a full book of
// general-population milestone tables in their own name; asserting only book2 is what let
// that stand while this test's own doc comment claimed otherwise.
func TestBlockedChildGets409NotAnEmptyBook(t *testing.T) {
	r, h := booksRouter(t)
	const childID = "BOOK-TEST-BLOCKED-001"
	cleanupChild(t, h, childID)

	for _, conditionID := range specialCareConditionIDs(t, h) {
		s := profile.Stored{
			ChildID:     childID,
			DisplayName: "Blocked Test Child",
			DateOfBirth: time.Now().AddDate(0, -36, 0),
			CreatedBy:   "books_test",
			Conditions: []profile.ClinicalCondition{
				// Routing the condition through TriggerField "Special_Care_Condition" is
				// what internal/profile.ToChildProfile requires to reach
				// models.ChildProfile.SpecialCareCondition rather than ClinicalFlags.
				{TriggerField: "Special_Care_Condition", FlagValue: conditionID, Class: "congenital"},
			},
		}
		if err := profile.Save(context.Background(), h.pool, s); err != nil {
			t.Fatalf("profile.Save: %v", err)
		}

		// Read from special_care_condition_gate.mandatory_reviewer directly, the same
		// source the handler is supposed to use, rather than hardcoding the provider's
		// current text -- this pins the join, not the workbook's current wording.
		var wantReviewer string
		if err := h.pool.QueryRow(context.Background(),
			`SELECT mandatory_reviewer FROM special_care_condition_gate WHERE condition_id = $1`,
			conditionID).Scan(&wantReviewer); err != nil {
			t.Fatalf("reviewer lookup: %v", err)
		}

		for _, path := range []string{
			"/api/books/" + childID + "/book1/preview",
			"/api/books/" + childID + "/book1.pdf",
			"/api/books/" + childID + "/book2/preview",
			"/api/books/" + childID + "/book2.pdf",
			// The set is the surface the console actually calls, so a stop that held on
			// the per-book routes and leaked through this one would leak in the only
			// place it matters.
			"/api/books/" + childID + "/preview",
			"/api/books/" + childID + "/books.zip",
		} {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

			// 409 and nothing else. 503 in particular is reserved for a broken renderer:
			// an operator who reads a clinical stop as a service fault will retry it, and
			// this stop is not a thing to retry.
			if rec.Code != 409 {
				t.Fatalf("%s (%s): a special-care child must get 409, got %d: %s",
					path, conditionID, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("%s (%s): a 409 body must be JSON, got Content-Type %q",
					path, conditionID, ct)
			}
			// No book may leak through the block, in either binding.
			if bytes.Contains(rec.Body.Bytes(), []byte("<html")) {
				t.Fatalf("%s (%s): a blocked child must not receive a rendered document",
					path, conditionID)
			}

			var body struct {
				Error    string `json:"error"`
				Reviewer string `json:"reviewer"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s (%s): decode: %v", path, conditionID, err)
			}
			if body.Error == "" {
				t.Fatalf("%s (%s): a 409 body with no reason leaves the operator no next step",
					path, conditionID)
			}
			if body.Reviewer != wantReviewer {
				t.Fatalf("%s (%s): reviewer = %q, want %q from special_care_condition_gate",
					path, conditionID, body.Reviewer, wantReviewer)
			}
		}
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

// One request, both books. This is the endpoint the console calls, and the product rule it
// carries is that a run produces two books -- so a 200 that returns one populated book and
// one empty string has failed even though nothing errored.
func TestBookSetPreviewReturnsBothBooks(t *testing.T) {
	r, h := booksRouter(t)
	const childID = "BOOK-TEST-SET-HTTP"
	cleanupChild(t, h, childID)

	s := profile.Stored{
		ChildID:       childID,
		DisplayName:   "Set Endpoint Child",
		DateOfBirth:   time.Now().AddDate(-4, 0, 0),
		RegionCulture: "West Bengal / East India",
		DietType:      "Vegetarian",
	}
	if err := profile.Save(context.Background(), h.pool, s); err != nil {
		t.Fatalf("profile.Save: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/books/"+childID+"/preview", nil))
	if rec.Code != 200 {
		t.Fatalf("set preview: got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		ChildID          string   `json:"child_id"`
		Book1            string   `json:"book1_html"`
		Book2            string   `json:"book2_html"`
		ProfileOmissions []string `json:"profile_omissions"`
		Book1Omissions   []string `json:"book1_omissions"`
		Book2Omissions   []string `json:"book2_omissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.ChildID != childID {
		t.Fatalf("child_id = %q, want %q", body.ChildID, childID)
	}
	for name, doc := range map[string]string{"book1_html": body.Book1, "book2_html": body.Book2} {
		if !strings.Contains(doc, "<html") {
			t.Fatalf("%s is not a rendered document: %.80q", name, doc)
		}
		// The provisional banner is on every page of every book. Its absence here would
		// mean the set rendered through a path that skips it.
		if !strings.Contains(doc, "clinical prescription") {
			t.Fatalf("%s is missing the provisional banner", name)
		}
	}
	if body.Book1 == body.Book2 {
		t.Fatal("both books rendered identically; the set must produce two different books")
	}

	// Every omission list is present and non-null, so the console renders them without a
	// null check.
	for name, list := range map[string][]string{
		"profile_omissions": body.ProfileOmissions,
		"book1_omissions":   body.Book1Omissions,
		"book2_omissions":   body.Book2Omissions,
	} {
		if list == nil {
			t.Fatalf("%s must serialise as [] rather than null", name)
		}
	}
}

// The download is a zip holding both books, named by child and book. One merged PDF would
// renumber Book 2's pages behind Book 1's, so a page reference printed inside either book
// would stop matching the page it is on.
func TestBookSetDownloadCarriesBothPDFs(t *testing.T) {
	if !bookBrowserOnPath() {
		t.Skip("no chromium on PATH")
	}
	r, h := booksRouter(t)
	const childID = "BOOK-TEST-SET-ZIP"
	cleanupChild(t, h, childID)

	s := profile.Stored{
		ChildID:       childID,
		DisplayName:   "Zip Child",
		DateOfBirth:   time.Now().AddDate(-4, 0, 0),
		RegionCulture: "West Bengal / East India",
		DietType:      "Vegetarian",
	}
	if err := profile.Save(context.Background(), h.pool, s); err != nil {
		t.Fatalf("profile.Save: %v", err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/books/"+childID+"/books.zip", nil))
	if rec.Code != 200 {
		t.Fatalf("set download: got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", ct)
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	want := []string{childID + "-book1.pdf", childID + "-book2.pdf"}
	var got []string
	for _, f := range zr.File {
		got = append(got, f.Name)

		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		// A real PDF, not an error page or an empty entry.
		if !bytes.HasPrefix(content, []byte("%PDF-")) {
			t.Fatalf("%s is not a PDF: %.16q", f.Name, content)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("zip holds %v, want %v", got, want)
	}
}
