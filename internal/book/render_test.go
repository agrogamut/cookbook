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
	for _, tc := range []struct {
		kind      Kind
		className string
	}{
		{Kind1, "book1"},
		{Kind2, "book2"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			var buf bytes.Buffer
			if err := RenderHTML(&buf, tc.kind, Metadata{Language: "en"}, nil); err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(buf.String(), `class="`+tc.className+`"`) {
				t.Fatalf("%s must carry the %s palette class", tc.kind, tc.className)
			}
		})
	}
}

// The contract sets two floors -- 9.5pt for body text, 8.5pt for table content -- and names
// no caption exemption. --small-size is the table floor, so anything but a table rule using
// it is prose rendered below the floor. Counted rather than described, because the last
// regression put the smallest type in the document on the provisional banner.
func TestTableSizingDoesNotLeakOntoProse(t *testing.T) {
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	if got := strings.Count(string(css), "--small-size"); got != 2 {
		t.Fatalf("--small-size appears %d times, want 2 (its declaration and the th rule); "+
			"a third use means table sizing reached non-table text, which the contract's "+
			"minimum_body_pt of 9.5 forbids", got)
	}
}
