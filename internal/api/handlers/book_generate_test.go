package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"github.com/madamgy/recipie/internal/aidraft"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func generateRouter(t *testing.T) *chi.Mux {
	t.Helper()
	h := New(testPool(t), aidraft.Disabled)
	r := chi.NewRouter()
	r.Post("/api/books/generate", h.BookGenerate)
	r.Post("/api/books/generate.zip", h.BookGenerateZip)
	return r
}

func postGenerate(t *testing.T, r *chi.Mux, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return rec
}

const generateBody = `{
  "display_name":"Inline Child","date_of_birth":"2022-05-01","sex":"female",
  "region_culture":"West Bengal / East India","diet_type":"Vegetarian","budget_band":"Low",
  "allergens":[{"group":"Peanut","status":"suspected","source":"parent_reported"}],
  "growth":[{"measured_on":"2026-07-01","weight_kg":14.2,"height_cm":96.5,"measured_by":"Dr Sen"}]
}`

// One request, no stored child, both books. This is the console's whole flow: an operator
// types the inputs and gets the two books, with nothing written down about the child.
func TestGenerateProducesBothBooksWithoutAStoredProfile(t *testing.T) {
	r := generateRouter(t)
	rec := postGenerate(t, r, "/api/books/generate", generateBody)
	if rec.Code != 200 {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Book1            string   `json:"book1_html"`
		Book2            string   `json:"book2_html"`
		ProfileOmissions []string `json:"profile_omissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for name, doc := range map[string]string{"book1": body.Book1, "book2": body.Book2} {
		if !strings.Contains(doc, "<html") {
			t.Fatalf("%s is not a rendered document", name)
		}
	}
	if !strings.Contains(body.Book1, "Inline Child") {
		t.Fatal("the book must carry the name that was typed in")
	}
	// The inline growth measurement has to reach the monitoring table, or supplying it was
	// pointless -- this is the input with no stored counterpart to fall back on.
	if !strings.Contains(body.Book1, "14.2 kg") || !strings.Contains(body.Book1, "Dr Sen") {
		t.Fatal("an inline growth measurement must print in the growth table")
	}
	// The same allergy disclosure the stored path makes.
	found := false
	for _, o := range body.ProfileOmissions {
		if strings.Contains(o, "Peanut") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a suspected allergen must be reported, got %v", body.ProfileOmissions)
	}
}

// A profile the write path would reject must be rejected here too. The failure it prevents --
// a region the corpus does not carry, producing a book ranked against nothing -- is about the
// book, so it cannot depend on whether the profile was saved first.
func TestGenerateRejectsWhatTheWritePathRejects(t *testing.T) {
	r := generateRouter(t)
	for _, c := range []struct{ name, field, body string }{
		{"unknown region", "region_culture",
			`{"date_of_birth":"2022-05-01","region_culture":"Atlantis"}`},
		{"diet in the wrong case", "diet_type",
			`{"date_of_birth":"2022-05-01","diet_type":"vegetarian"}`},
		{"sex outside the constraint", "sex",
			`{"date_of_birth":"2022-05-01","sex":"F"}`},
		{"allergen status outside the constraint", "status",
			`{"date_of_birth":"2022-05-01","allergens":[{"group":"Peanut","status":"maybe","source":"parent_reported"}]}`},
		{"an image type that cannot be printed", "printable image",
			`{"date_of_birth":"2022-05-01","photo_data_uri":"data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="}`},
		// The parents' photo is validated by the same book.ParsePhoto as the child's own, and
		// nothing before this fix-wave pass exercised that path with a malformed upload.
		{"a parents' photo image type that cannot be printed", "printable image",
			`{"date_of_birth":"2022-05-01","parents_photo_data_uri":"data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="}`},
		{"a date that is not a date", "date", `{"date_of_birth":"01-05-2022"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := postGenerate(t, r, "/api/books/generate", c.body)
			if rec.Code != 400 {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.field) {
				t.Fatalf("the 400 must name what was wrong (%q), got %s", c.field, rec.Body.String())
			}
		})
	}
}

// Date of birth is the one field with no sensible default: every book states the child's age,
// and an age derived from a zero date would print as a confident wrong number.
func TestGenerateRequiresDateOfBirth(t *testing.T) {
	rec := postGenerate(t, generateRouter(t), "/api/books/generate", `{"display_name":"No DOB"}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "date_of_birth") {
		t.Fatalf("expected a 400 naming date_of_birth, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A supplied photograph reaches the cover, and only Book 1's. Both the child's own photo
// (front cover) and the parents' photo (back cover, added later than the child's) are covered
// here, together, since they are validated and embedded the same way and must both stay off
// Book 2, a working recipe document with no photo upload of any kind.
func TestGeneratePutsThePhotoOnBook1Cover(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	// A second, distinct PNG (a real 1x1 red pixel, not a copy of the fixture above) so a
	// bytes.Contains match against it can only mean the parents' photo specifically, never a
	// coincidental match against the child's photo or an embedded illustration.
	const parentsPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"
	body := `{"display_name":"Photo Child","date_of_birth":"2022-05-01",
	          "photo_data_uri":"data:image/png;base64,` + png + `","photo_caption":"With her mother",
	          "parents_photo_data_uri":"data:image/png;base64,` + parentsPNG + `","parents_photo_caption":"Mum and dad"}`

	rec := postGenerate(t, generateRouter(t), "/api/books/generate", body)
	if rec.Code != 200 {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Book1 string `json:"book1_html"`
		Book2 string `json:"book2_html"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(got.Book1, "data:image/png;base64,"+png) {
		t.Fatal("the photograph must be embedded in book 1")
	}
	if !strings.Contains(got.Book1, "With her mother") {
		t.Fatal("the caption must print with the photograph")
	}
	// The parents' photo belongs on Book 1's back cover, the same book as the child's own
	// front-cover photo -- both are the child's own book, just its two different covers.
	// Unlike the front-cover photo, end.html prints no caption line for the parents' photo
	// (see book1/end.html), so the assertion here is scoped to the photo's own bytes, not a
	// caption string -- matching what the back cover actually renders today.
	if !strings.Contains(got.Book1, "data:image/png;base64,"+parentsPNG) {
		t.Fatal("the parents' photograph must be embedded in book 1")
	}
	// Book 2 is a recipe book; a portrait there is decoration on a working document. Book 2's
	// own cover legitimately embeds PNG data URIs now -- the decorative corner illustrations
	// and the kids-cooking hero image, all package-embedded and unrelated to any child (see
	// internal/book/coverart.go) -- so the check has to be specific to the uploaded child
	// photograph's own bytes, not "any PNG at all".
	if strings.Contains(got.Book2, "data:image/png;base64,"+png) {
		t.Fatal("the child's photograph must not appear in book 2")
	}
	if strings.Contains(got.Book2, "data:image/png;base64,"+parentsPNG) {
		t.Fatal("the parents' photograph must not appear in book 2")
	}
}

// The stop gate holds on the inline route. A child whose condition stops generation must not
// be able to get a book by skipping the saved profile.
func TestGenerateIsBlockedByTheStopGate(t *testing.T) {
	h := New(testPool(t), aidraft.Disabled)
	r := chi.NewRouter()
	r.Post("/api/books/generate", h.BookGenerate)

	for _, id := range specialCareConditionIDs(t, h) {
		body := `{"display_name":"SC Child","date_of_birth":"2022-05-01","conditions":[
		           {"trigger_field":"Special_Care_Condition","flag_value":"` + id + `","class":"chronic"}]}`
		rec := postGenerate(t, r, "/api/books/generate", body)
		if rec.Code != 409 {
			t.Fatalf("%s: expected 409, got %d: %s", id, rec.Code, rec.Body.String())
		}
		if bytes.Contains(rec.Body.Bytes(), []byte("<html")) {
			t.Fatalf("%s: a blocked child must not receive a document", id)
		}
	}
}

// The zip carries both printed books, named from the child's name since there is no id.
func TestGenerateZipIsNamedFromTheChildsName(t *testing.T) {
	if !bookBrowserOnPath() {
		t.Skip("no chromium on PATH")
	}
	rec := postGenerate(t, generateRouter(t), "/api/books/generate.zip", generateBody)
	if rec.Code != 200 {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "inline-child-books.zip") {
		t.Fatalf("archive should be named from the child, got %q", cd)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("zip holds %d files, want 2", len(zr.File))
	}
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "inline-child-") {
			t.Fatalf("entry %q should be named from the child", f.Name)
		}
	}
}

// A name that is entirely non-ASCII still produces a saveable file.
func TestSlugOrDefault(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Inline Child", "inline-child"},
		{"  Aarav  Sen ", "aarav-sen"},
		{"রিয়া", "books"}, // Bengali only: the book still carries the name, the filename cannot
		{"", "books"},
		{"///", "books"},
	} {
		if got := slugOrDefault(c.in, "books"); got != c.want {
			t.Fatalf("slugOrDefault(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
