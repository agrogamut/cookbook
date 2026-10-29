package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/bookdocx"
	"github.com/madamgy/recipie/internal/profile"
)

func TestReaderPreservesSharedSnapshotAndDocumentContract(t *testing.T) {
	pool := testPool(t)
	// The reader adapter must not require the old converter executable at initialization.
	t.Setenv("PATH", t.TempDir())
	for _, language := range []string{"en", "bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			h := New(pool, aidraft.Disabled)
			if h.reader == nil {
				t.Fatal("reader not initialized")
			}
			s := profile.Stored{
				ChildID: "SYNTHETIC-READER", DisplayName: "Synthetic reader child",
				DateOfBirth: time.Now().UTC().AddDate(0, -36, 0), LanguageID: language,
			}
			photo, err := book.ParsePhoto("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGOUC+hhYGBgYmBgYGBgAAALdAD+3y8XygAAAABJRU5ErkJggg==", "Synthetic portrait")
			if err != nil {
				t.Fatal(err)
			}
			type contextKey struct{}
			ctx := context.WithValue(context.Background(), contextKey{}, language)
			var options book.ReaderOptions
			var results [2]book.ReaderResult
			calls := 0
			h.reader = func(gotCtx context.Context, request bookdocx.Request, got book.ReaderOptions) (book.ReaderResult, error) {
				calls++
				if gotCtx.Value(contextKey{}) != language || request.Book != calls || request.Language != language {
					t.Fatalf("reader context/order/language changed: book=%d language=%s", request.Book, request.Language)
				}
				if got.Book1 == nil || got.Book2 == nil {
					t.Fatal("shared assembly missing")
				}
				if calls == 1 {
					options = got
				} else if options.Book1 != got.Book1 || options.Book2 != got.Book2 {
					t.Fatal("reader received a different assembly for Book2")
				}
				if got.Book1.Child.DisplayName != s.DisplayName || got.Book1.Child.Photo != photo || got.Book1.Metadata.ParentsPhoto != photo {
					t.Fatal("recorded identity or uploads changed")
				}
				wantModules := 63
				if request.Book == 2 {
					wantModules = 56
				}
				if len(request.Modules) != wantModules {
					t.Fatalf("book%d modules=%d want=%d", request.Book, len(request.Modules), wantModules)
				}
				if request.Book == 1 {
					for _, module := range []string{"B1-01", "B1-63"} {
						found := false
						for _, block := range request.Modules[module].Blocks {
							if block.Image != nil && len(block.Image.Data) > 0 && block.Image.Alt == photo.Caption {
								found = true
							}
						}
						if !found {
							t.Fatalf("full uploaded image missing from %s", module)
						}
					}
				}
				result, err := bindingReader(gotCtx, request, got)
				if err != nil {
					return result, err
				}
				result.PDF = []byte(fmt.Sprintf("%%PDF-1.4\n%% synthetic reader book%d %s\n%%%%EOF", request.Book, language))
				result.Omissions = append(result.Omissions, bookdocx.Omission{Module: fmt.Sprintf("B%d-01", request.Book), Reason: "Synthetic reader omission"})
				results[request.Book-1] = result
				return result, nil
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/books/generate", nil).WithContext(ctx)
			response, _, ok := h.renderSetWithPhotos(rec, req, s, photo, photo, nil)
			if !ok || calls != 2 {
				t.Fatalf("reader calls=%d success=%t response=%s", calls, ok, rec.Body.String())
			}
			for i, result := range results {
				if !bytes.Equal(response.pdfs[i], result.PDF) || !bytes.Equal(response.docx[i], result.DOCX) {
					t.Fatalf("book%d PDF or DOCX changed after rendering", i+1)
				}
				preview := response.Book1
				omissions := response.Book1Omissions
				if i == 1 {
					preview, omissions = response.Book2, response.Book2Omissions
				}
				if !bytes.Equal(previewPDF(t, preview), result.PDF) {
					t.Fatalf("book%d preview does not contain final PDF bytes", i+1)
				}
				if !slices.Contains(omissions, fmt.Sprintf("B%d-01: Synthetic reader omission", i+1)) {
					t.Fatalf("book%d reader omission lost", i+1)
				}
			}
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			for i, result := range results {
				for suffix, want := range map[string]string{"layout_version": book.ReaderLayoutVersion, "template_sha256": result.TemplateSHA256} {
					var got string
					if err := json.Unmarshal(wire[fmt.Sprintf("book%d_%s", i+1, suffix)], &got); err != nil || got != want || got == "" {
						t.Fatalf("book%d %s=%q want=%q error=%v", i+1, suffix, got, want, err)
					}
				}
			}
		})
	}
}

func TestReaderCancellationDoesNotReturnPartialSet(t *testing.T) {
	pool := testPool(t)
	for _, failBook := range []int{1, 2} {
		t.Run(fmt.Sprintf("book%d", failBook), func(t *testing.T) {
			h := New(pool, aidraft.Disabled)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			h.reader = func(gotCtx context.Context, request bookdocx.Request, options book.ReaderOptions) (book.ReaderResult, error) {
				calls++
				if request.Book == failBook {
					cancel()
					return book.ReaderResult{}, gotCtx.Err()
				}
				return bindingReader(gotCtx, request, options)
			}
			s := profile.Stored{ChildID: "SYNTHETIC-READER-CANCEL", DisplayName: "Synthetic cancellation", DateOfBirth: time.Now().UTC().AddDate(0, -36, 0)}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/books/generate", nil).WithContext(ctx)
			response, set, ok := h.renderSet(rec, req, s)
			if ok || rec.Code != http.StatusInternalServerError || calls != failBook || set.ChildID != "" {
				t.Fatalf("partial render accepted: success=%t status=%d calls=%d child=%q", ok, rec.Code, calls, set.ChildID)
			}
			if !strings.Contains(rec.Body.String(), context.Canceled.Error()) || strings.Contains(rec.Body.String(), "data:application/pdf") {
				t.Fatalf("unexpected render error response: %s", rec.Body.String())
			}
			for i := range response.pdfs {
				if len(response.pdfs[i]) != 0 || len(response.docx[i]) != 0 {
					t.Fatal("partial document returned after cancellation")
				}
			}
		})
	}
}
