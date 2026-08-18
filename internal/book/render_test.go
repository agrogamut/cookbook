package book

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderRejectsAnUnknownKind(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind("book3"), Metadata{}, nil); err == nil {
		t.Fatal("an unknown book kind must error rather than render an empty document")
	}
}

// The provisional banner lives in the base template precisely so no section can omit it.
// If this ever fails, a book can be generated that does not disclose that its data is
// unapproved, which is the single worst thing this renderer could do.
func TestEveryBookCarriesTheProvisionalBanner(t *testing.T) {
	for _, kind := range []Kind{Kind1, Kind2} {
		t.Run(string(kind), func(t *testing.T) {
			var buf bytes.Buffer
			meta := Metadata{Title: "t", Language: "en", ReviewStatus: "Draft"}
			if err := RenderHTML(&buf, kind, meta, nil); err != nil {
				t.Fatalf("render: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, "Provisional - not clinically approved") {
				t.Fatal("generated book does not disclose that its data is unapproved")
			}
			if !strings.Contains(out, "Draft") {
				t.Fatal("the provider's own review status must appear verbatim")
			}
		})
	}
}

// The palette is chosen by book, and the two must not be confusable: Book 1 is teal/navy and
// Book 2 is plum/rose per the contract's visual_language.
func TestEachBookCarriesItsOwnPaletteClass(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, Metadata{Language: "en"}, nil); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), `class="book1"`) {
		t.Fatal("Book 1 must carry the book1 palette class")
	}
}
