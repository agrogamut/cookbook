package book

import (
	"bytes"
	"html/template"
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

// The prototype's own rule, on page 5: "It must not fabricate vaccine dates or reactions."
// The template has no branch that prints a date, and this is what pins that.
func TestVaccinationTrackerNeverPrintsADate(t *testing.T) {
	b := Book1{
		Metadata: Metadata{Language: "en", ReviewStatus: "Draft"},
		Sections: []Section{{
			BlockID: "B1-009", TemplateID: "B1-VAX-01", Title: "Vaccination Tracker",
			Rows: []Row{{Label: "6 weeks", Reference: "DTwP-1"}},
		}},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "DTwP-1") {
		t.Fatal("the approved schedule must be rendered")
	}
	// Four writing lines per row: given date, brand/batch, reaction, next due.
	if strings.Count(out, "write-line") < 4 {
		t.Fatal("administration columns must be blank writing lines, never populated")
	}
}

// A nil measurement renders as a writing line, never as a number.
func TestMissingActualRendersAsAWritingLine(t *testing.T) {
	b := Book1{
		Metadata: Metadata{Language: "en", ReviewStatus: "Draft"},
		Sections: []Section{{
			TemplateID: "B1-COMPARE-01", Title: "Growth",
			Rows: []Row{{Label: "Weight", Reference: "Use approved age/sex growth reference"}},
		}},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "write-line") {
		t.Fatal("an unrecorded measurement must render as a writing line")
	}
	if strings.Contains(buf.String(), ">0<") {
		t.Fatal("an unrecorded measurement must never render as zero")
	}
}

// Every template id the assembler can emit must have a template defined for it. Silence is
// the right render for an unknown id -- a fallback would give clinical content the wrong
// visual treatment -- which is exactly why the mismatch has to fail here instead.
func TestEveryMappedBlockHasATemplate(t *testing.T) {
	tmpl, err := template.ParseFS(templateFS, "templates/base.html", "templates/book1/*.html")
	if err != nil {
		t.Fatalf("parse book1 templates: %v", err)
	}
	seen := map[string]bool{}
	for blockID, templateID := range blockTemplate {
		if seen[templateID] {
			continue
		}
		seen[templateID] = true
		if tmpl.Lookup(templateID) == nil {
			t.Errorf("blockTemplate[%q] = %q, but no template is defined with that name",
				blockID, templateID)
		}
	}
}
