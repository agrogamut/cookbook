package book

import (
	"bytes"
	"context"
	"errors"
	"html"
	"html/template"
	"strings"
	"testing"
	"time"
)

func TestRenderRejectsAnUnknownKind(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind("book3"), Metadata{}, nil); err == nil {
		t.Fatal("an unknown book kind must error rather than render an empty document")
	}
}

// The provisional banner used to live in the base template precisely so no section could omit
// it. It is gone now: approval is a physical signature on the signature page (director and
// dietitian), decided the same day this test was rewritten, and the banner's claim that
// nothing has been approved stopped being true the moment that page exists to be signed. This
// asserts the opposite of what it used to -- the banner must never reappear, on either book.
func TestNoBookCarriesTheProvisionalBanner(t *testing.T) {
	for _, kind := range []Kind{Kind1, Kind2} {
		t.Run(string(kind), func(t *testing.T) {
			var buf bytes.Buffer
			meta := Metadata{Title: "t", Language: "en"}
			if err := RenderHTML(&buf, kind, meta, nil); err != nil {
				t.Fatalf("render: %v", err)
			}
			out := buf.String()
			if strings.Contains(out, "Provisional - not clinically approved") ||
				strings.Contains(out, "not clinically approved") {
				t.Fatal("a generated book must not print the provisional disclaimer -- " +
					"approval is now a physical signature on the signature page, and the " +
					"data-level Draft/Review_Status flags on individual rows are untouched " +
					"and still surfaced elsewhere, but this document-level claim is gone")
			}
		})
	}
}

func TestRenderedDocumentEmbedsThePoppinsFontFaces(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, Metadata{Language: "en"}, nil); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "@font-face") {
		t.Fatal("rendered document must embed @font-face rules for Poppins")
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
// no caption exemption. --small-size is the table floor, so any rule using it must be a rule
// that only ever applies inside a table; anywhere else it is prose rendered below the floor.
//
// The selectors are listed rather than counted. The count version ("want 2") was the first
// thing to fail when a legitimate in-table rule was added, and the tempting fix -- bump 2 to
// 3 -- would have passed just as readily for an illegitimate one. Naming them means adding a
// rule is a decision someone writes down here, with the reason it is table content.
func TestTableSizingDoesNotLeakOntoProse(t *testing.T) {
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}

	// Every rule permitted to use the table floor, and why it is table content.
	allowed := map[string]string{
		"th":          "table header cells",
		".ref-detail": "the citation string inside a reference-table cell, never used outside one",
		".tracker th": "a tracker's column headings, which can run to eight columns on a " +
			"170mm text block and need the table floor to break between words rather than " +
			"through them",
	}

	var offenders []string
	for _, block := range strings.Split(string(css), "}") {
		if !strings.Contains(block, "var(--small-size)") {
			continue
		}
		// The selector is whatever precedes the opening brace, last line only.
		head := block
		if i := strings.LastIndex(block, "{"); i >= 0 {
			head = block[:i]
		}
		lines := strings.Split(strings.TrimSpace(head), "\n")
		selector := strings.TrimSpace(lines[len(lines)-1])
		if _, ok := allowed[selector]; !ok {
			offenders = append(offenders, selector)
		}
	}
	if len(offenders) != 0 {
		t.Fatalf("%v use --small-size but are not listed as table content. The contract's "+
			"minimum_body_pt is 9.5 and it names no caption exemption: either the rule only "+
			"ever applies inside a table, in which case add it to allowed with that reason, "+
			"or it is prose and must use --caption-size or larger.", offenders)
	}
}

// The prototype's own rule, on page 5: "It must not fabricate vaccine dates or reactions."
// The template has no branch that prints a date, and this is what pins that.
func TestVaccinationTrackerNeverPrintsADate(t *testing.T) {
	b := Book1{
		Metadata: Metadata{Language: "en"},
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

// An unrecorded value renders as a writing line, never as a number or a dash.
//
// B1-PROFILE-01 rather than B1-DEV-01: the development table's writing lines are unconditional
// literals in the template, so a test pointed at it passes whatever the data says and can
// never fail. The profile table branches on the value, which is the behaviour worth pinning,
// so this asserts both directions -- a blank renders a line, and a present value renders
// itself and no line.
//
// The element, not the class name: tokens.css defines a .write-line rule and is inlined into
// every render, so a substring test for "write-line" matches the stylesheet on any input and
// proves nothing about the row.
func TestUnrecordedValueRendersAsAWritingLine(t *testing.T) {
	render := func(t *testing.T, rows []Row) string {
		t.Helper()
		b := Book1{
			Metadata: Metadata{Language: "en"},
			Sections: []Section{{
				TemplateID: "B1-PROFILE-01", Title: "Child profile", Rows: rows,
			}},
		}
		var buf bytes.Buffer
		if err := RenderHTML(&buf, Kind1, b.Metadata, b); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}

	const line = `<span class="write-line">`

	blank := render(t, []Row{{Label: "Feeding stage"}})
	if !strings.Contains(blank, line) {
		t.Fatal("an unrecorded value must render as a writing line")
	}
	// Scoped to the row's own <tr>...</tr>, not the whole document. Book1's body.html always
	// renders B1-COVER-01 ahead of every section, including in this test's minimal fixture with
	// no photo -- and that cover's no-photo state now carries embedded base64 PNG illustrations
	// (a fix-wave addition, see book1/cover.html) plus the base64 woff2 font-face payloads in
	// the inlined stylesheet. A base64 alphabet can and does contain "n/a" or ">0<"-shaped
	// runs by pure chance across that much encoded binary data, which is not this row rendering
	// a zero or a dash. Anchoring on the row's own label finds the one <tr> under test.
	row := blank
	if i := strings.Index(blank, "Feeding stage"); i >= 0 {
		row = blank[i:]
		if j := strings.Index(row, "</tr>"); j >= 0 {
			row = row[:j]
		}
	}
	if strings.Contains(row, ">0<") || strings.Contains(row, "n/a") {
		t.Fatal("an unrecorded value must never render as zero or as a dash")
	}

	// The other direction, without which the assertion above would pass on a template that
	// prints a writing line unconditionally.
	filled := render(t, []Row{{Label: "Age", Note: "4 years 3 months"}})
	if !strings.Contains(filled, "4 years 3 months") {
		t.Fatal("a recorded value must render itself")
	}
	// Truncated before Book 1's own signature and back pages, which always render write-lines
	// of their own -- three blank signature lines and an unset release id -- unrelated to the
	// profile row under test here. A blanket substring search across the whole document would
	// wrongly catch them now that every Book 1 render carries both.
	beforeBackPage := filled
	if i := strings.Index(filled, "Sign-off"); i >= 0 {
		beforeBackPage = filled[:i]
	}
	if strings.Contains(beforeBackPage, line) {
		t.Fatal("a recorded value must not also render a writing line")
	}
}

// Every template id the assembler can emit must have a template defined for it. Silence is
// the right render for an unknown id -- a fallback would give clinical content the wrong
// visual treatment -- which is exactly why the mismatch has to fail here instead.
func TestEveryMappedBlockHasATemplate(t *testing.T) {
	// Funcs before ParseFS, matching RenderHTML. Without it the parse fails on the first
	// template that calls one, which reports as "function not defined" and looks like a
	// missing template rather than a test that builds its templates differently from the
	// renderer it is checking.
	tmpl, err := template.New("base.html").Funcs(templateFuncs).
		ParseFS(templateFS, "templates/base.html", "templates/book1/*.html")
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

// TestEveryContentKindReachesThePage is the counterpart to SectionHasContent, and it exists
// because that guard has a blind side.
//
// SectionHasContent asks whether the model carries something. It cannot ask whether the
// template draws it. B1-016 populated a tracker, passed the guard, and printed a section band
// over an empty page, because B1-ILLNESS-01 rendered only .Illness and silently dropped
// .Trackers. Both halves passed their own check and the page was still blank.
//
// So: for each template, build a section carrying every content kind the assembler can put on
// it, render it, and require a distinctive string from each kind to appear. A template that
// forgets a field fails here rather than in a printed book.
func TestEveryContentKindReachesThePage(t *testing.T) {
	grid := TrackerSpec{
		Title: "TRACKERTITLE", Reference: "TRACKERREF", Frequency: "Selected week",
		Columns: []string{"COLONE", "COLTWO"}, Rows: 3,
		Alarm: "TRACKERALARM", Review: "TRACKERREVIEW",
	}

	for _, tc := range []struct {
		templateID string
		section    Section
		want       []string
		// notWant is for a field that is deliberately populated on the struct and
		// deliberately absent from the page. Dropping such a field from want would let a
		// change that reintroduces it pass silently, which is the whole failure mode this
		// test exists for, only inverted.
		notWant []string
	}{
		{
			templateID: "B1-DAILY-01",
			section: Section{Domains: []DailyDomain{{
				ID: "DL-X", Domain: "DOMAINNAME", AgeContext: "AGECONTEXT",
				Reference: "DOMAINREF", Goal: "DOMAINGOAL", RedFlag: "DOMAINREDFLAG",
				Referral: "DOMAINREFERRAL", AILimit: "DOMAINLIMIT", Tracker: &grid,
			}}},
			want: []string{"DOMAINNAME", "AGECONTEXT", "DOMAINREF", "DOMAINGOAL",
				"DOMAINREDFLAG", "DOMAINREFERRAL",
				"TRACKERTITLE", "COLONE", "COLTWO", "TRACKERALARM", "TRACKERREVIEW"},
			// ai_limit is a constraint on this generator, addressed to this generator. The
			// page obeying it is the evidence; a line of small print at the foot of thirteen
			// consecutive domains is not. Still on the struct and in the JSON.
			notWant: []string{"DOMAINLIMIT"},
		},
		{
			templateID: "B1-ILLNESS-01",
			section: Section{
				Illness: []IllnessBlock{{
					ID: "IF-X", Situation: "SITUATIONNAME",
					SupportiveMessage: "ILLNESSMESSAGE", WhatToMonitor: "ILLNESSMONITOR",
					RedFlags: "ILLNESSREDFLAG", EngineLimit: "ILLNESSLIMIT",
				}},
				// The field whose absence produced the blank page.
				Trackers: []TrackerSpec{grid},
			},
			want: []string{"SITUATIONNAME", "ILLNESSMESSAGE", "ILLNESSMONITOR",
				"ILLNESSREDFLAG", "TRACKERTITLE", "COLONE"},
			// book_engine_limit said, at every one of five situations, that the guidance
			// beside it was general rather than specific to this child -- on a page a doctor
			// has entered this child's conditions into and will sign.
			notWant: []string{"ILLNESSLIMIT"},
		},
		{
			templateID: "B1-TRACKER-01",
			section:    Section{Trackers: []TrackerSpec{grid}},
			want:       []string{"TRACKERTITLE", "TRACKERREF", "COLONE", "COLTWO", "TRACKERALARM"},
		},
		{
			templateID: "B1-SAFETY-01",
			section: Section{Safety: &SafetyCard{
				Confirmed: []string{"CONFIRMEDALLERGEN"},
				Suspected: []string{"SUSPECTEDALLERGEN"},
				Rules:     []Row{{Label: "RULELABEL", Reference: "RULEREF", Note: "RULENOTE"}},
				Choking: []ChokingRule{{
					Food: "CHOKINGFOOD", Risk: "CHOKINGRISK",
					Rule: "CHOKINGRULE", AgeFor: "CHOKINGAGE",
				}},
				ReactionLog: &grid,
			}},
			want: []string{"CONFIRMEDALLERGEN", "SUSPECTEDALLERGEN", "RULELABEL", "RULEREF",
				"RULENOTE", "CHOKINGFOOD", "CHOKINGRISK", "CHOKINGRULE", "CHOKINGAGE",
				"TRACKERTITLE", "COLONE"},
		},
		{
			templateID: "B1-REFS-01",
			section: Section{Refs: []EvidenceSource{{
				SourceID: "SRCID", Authority: "SRCAUTHORITY", Topic: "SRCTOPIC",
				Reference: "SRCREFERENCE", HowUsed: "SRCHOWUSED", Limitation: "SRCLIMITATION",
			}}},
			// SRCLIMITATION stays on the page, unlike the two caveats above, and the
			// distinction is CLAUDE.md's 2026-08-25 amendment: important_limitation states
			// what each cited source can and cannot support ("Does not replace clinical
			// assessment"), which is a fact about the source rather than about this book's
			// review status. A references table that drops it claims more than the sources do.
			want: []string{"SRCID", "SRCAUTHORITY", "SRCTOPIC", "SRCREFERENCE",
				"SRCHOWUSED", "SRCLIMITATION"},
		},
	} {
		t.Run(tc.templateID, func(t *testing.T) {
			tc.section.TemplateID = tc.templateID
			html := renderOneSection(t, tc.section)
			for _, want := range tc.want {
				if !strings.Contains(html, want) {
					t.Errorf("%s: %q is in the model and not on the page", tc.templateID, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(html, notWant) {
					t.Errorf("%s: %q is deliberately off the page and printed anyway",
						tc.templateID, notWant)
				}
			}
		})
	}
}

// TestEveryStageFacetPrintsSomething holds the four feeding facets to each drawing a page. A
// facet name with no branch in stage.html renders an empty section, which is the same defect
// as a missing field with a different cause.
func TestEveryStageFacetPrintsSomething(t *testing.T) {
	stage := &AgeStage{
		StageCode: "AFX", StageName: "STAGENAME", DisplayAge: "STAGEAGE",
		Phase: "PHASETEXT", MilkContext: "MILKTEXT", ComplementaryFood: "CFTEXT",
		BreastfedMeals: "BFMEALS", NonBreastfedMeals: "NBFMEALS",
		TextureMinimum: "TEXTUREMIN", ResponsiveFeeding: "RESPONSIVETEXT",
		QuantityRule: "QUANTITYTEXT", VarietyRule: "VARIETYTEXT",
		SelfFeeding: "SELFFEEDTEXT", ChokingControl: "CHOKINGTEXT",
		HardExclusion: "EXCLUSIONTEXT", HoneyRule: "HONEYTEXT",
	}
	for facet, want := range map[string][]string{
		"target":     {"QUANTITYTEXT", "VARIETYTEXT", "HONEYTEXT", "CHOKINGTEXT", "EXCLUSIONTEXT"},
		"schedule":   {"BFMEALS", "NBFMEALS", "TEXTUREMIN"},
		"approach":   {"RESPONSIVETEXT", "SELFFEEDTEXT"},
		"comparison": {"STAGEAGE", "NBFMEALS"},
	} {
		t.Run(facet, func(t *testing.T) {
			html := renderOneSection(t, Section{
				TemplateID: "B1-STAGE-01",
				Stage:      &StagePage{Current: stage, Facet: facet},
			})
			for _, w := range want {
				if !strings.Contains(html, w) {
					t.Errorf("facet %q does not print %q", facet, w)
				}
			}
		})
	}
}

// renderOneSection renders a single Book 1 section through the real template set, so a test
// cannot pass against templates the renderer does not use.
func renderOneSection(t *testing.T, sec Section) string {
	t.Helper()
	var buf bytes.Buffer
	err := RenderHTML(&buf, Kind1, Metadata{Language: "en", GenerationDate: time.Now()},
		Book1{Sections: []Section{sec}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

// Pictures were removed from the recipe page a second time on 2026-09-04 -- neither the
// drawn dish-format mark nor a stored photograph prints, regardless of whether
// RecipeCard.Mark/.Photo are set. The struct fields stay (the API still resolves them),
// only the template stopped printing. Replaces
// TestARecipePagePrefersThePhotoOverTheMarkWhenBothAreSet and
// TestARecipePagePrintsTheMarkWhenThereIsNoPhoto, which pinned the brief Photo-else-Mark
// interval between the two removals.
func TestARecipePagePrintsNoPictureEvenWhenMarkAndPhotoAreSet(t *testing.T) {
	card := RecipeCard{
		Number: 1,
		Title:  "Test Recipe",
		Mark:   &DishMark{FormatLabel: "Test format", SVG: template.HTML("<svg></svg>")},
		Photo:  &RecipePhoto{DataURI: template.URL("data:image/jpeg;base64,/9k="), SourceLabel: "test archetype"},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, Book2{
		MealSections: []MealSection{{Number: 1, Title: "Breakfast", Recipes: []RecipeCard{card}}},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, string(card.Photo.DataURI)) {
		t.Fatal("the photo must not print")
	}
	if strings.Contains(out, "<svg></svg>") {
		t.Fatal("the mark's SVG must not print")
	}
}

// A recipe with no mark and no photo prints without either -- no placeholder, no empty frame.
func TestARecipeWithNoMarkPrintsNoFrame(t *testing.T) {
	b := Book2{MealSections: []MealSection{{
		MealCategoryID: "MC-01", Title: "Breakfast", Number: 1,
		Recipes: []RecipeCard{{RecipeID: "MG-R-00002", Title: "Untagged dish", Number: 1}},
	}}}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	// The class name appears in the inlined stylesheet on every page; what must be absent is
	// the element.
	if strings.Contains(buf.String(), `<figure class="recipe-mark"`) {
		t.Error("a recipe with no mark must print no mark frame")
	}
}

// The stylesheet has a screen half.
//
// Without one the console's iframe lays the document out at the iframe's own width -- about
// 1700 CSS pixels against a 170mm text block -- so every grid spreads, prose capped at the
// measure sits in a column four times too wide, and no page boundary is visible. Most of what
// an operator reported as misalignment was this, not the printed page.
func TestTheStylesheetHasAScreenPresentation(t *testing.T) {
	css := stylesheetSource(t)
	if !strings.Contains(css, "@media screen") {
		t.Fatal("the preview has no screen styling and renders at iframe width")
	}
	for _, want := range []string{"width: 170mm", "box-shadow"} {
		if !strings.Contains(css, want) {
			t.Errorf("the screen half must constrain the page and show sheet boundaries; %q is absent", want)
		}
	}
}

// The provisional banner and its CSS are gone, on screen and in print alike -- there is no
// state left in which either should reappear. This replaces the old test, which asserted the
// banner was present on screen and suppressed only in print; approval is a physical signature
// now, and a banner that came back in one rendering path and not the other would be a worse
// bug than one that never left, because it would look deliberate.
func TestNoProvisionalBannerAnywhere(t *testing.T) {
	css := stylesheetSource(t)
	if strings.Contains(css, ".provisional") {
		t.Error("tokens.css must not carry any .provisional rule; the banner is removed, not hidden")
	}

	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, Metadata{Language: "en"}, Book1{}); err != nil {
		t.Fatalf("render: %v", err)
	}
	// The banner element and its visible text, not a bare substring match: tokens.css keeps a
	// couple of historical comments that mention the old banner by name (this project layers
	// history rather than scrubbing it, the same way CLAUDE.md keeps a dated ruling rather
	// than deleting it), and those comments are inlined verbatim into every <style> block.
	// They are invisible CSS comments, never rendered text, so they are not the defect this
	// guards against.
	out := buf.String()
	if strings.Contains(out, `class="provisional"`) {
		t.Error("no rendered book may carry the banner element")
	}
	if strings.Contains(out, "not clinically approved") {
		t.Error("no rendered book may carry the banner's disclaimer text")
	}
}

// The physical approval page: a director and a dietitian sign the printed copy before it
// reaches a family (see CLAUDE.md's sign-off amendment -- there is no stored record and no
// system gate, this page is the entire mechanism). Second-to-last in both books, immediately
// before the imprint/back page, so a signature always has a sheet to itself and is never lost
// among the content pages.
func TestSignoffPagePrecedesTheImprint(t *testing.T) {
	for _, tc := range []struct {
		kind  Kind
		data  any
		roles []string
	}{
		// Book 1's sign-off page carries the author/editor/clinical four-role set (see
		// signoffCredits in book1.go), sourced from Book1.SignoffCredits -- populated here the
		// way AssembleBook1 populates it, since this test builds the render model by hand
		// rather than through the assembler.
		{Kind1, Book1{Child: ChildSummary{DisplayName: "Test Child"}, SignoffCredits: signoffCredits()},
			[]string{"Author", "Pediatrician", "Dietician", "Editor"}},
		// Book 2 now carries the same four-role set as Book 1 (see SignoffCredits's own doc
		// comment in types.go): one consultation's paperwork for one child, one card set.
		{Kind2, Book2{Child: ChildSummary{DisplayName: "Test Child"}, SignoffCredits: signoffCredits()},
			[]string{"Author", "Pediatrician", "Dietician", "Editor"}},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			var buf bytes.Buffer
			meta := Metadata{Language: "en", GenerationDate: time.Now()}
			if err := RenderHTML(&buf, tc.kind, meta, tc.data); err != nil {
				t.Fatalf("render: %v", err)
			}
			out := buf.String()
			signoffIdx := strings.Index(out, "Sign-off")
			imprintIdx := strings.Index(out, "About this copy")
			if signoffIdx < 0 {
				t.Fatal("no signature page rendered")
			}
			for _, role := range tc.roles {
				if !strings.Contains(out, role) {
					t.Fatalf("signature page must name %q", role)
				}
			}
			if imprintIdx < 0 {
				t.Fatal("no imprint/back page rendered")
			}
			if signoffIdx > imprintIdx {
				t.Fatal("the signature page must come before the imprint, not after")
			}
		})
	}
}

// Book 1 carries the single centered brand watermark (water1.jpeg); Book 2 carries four
// scattered food illustrations instead, never the brand mark -- see tokens.css's .watermark
// and .food-scatter notes for why the two books diverge here. Verified by a real
// headless-Chrome print spike (2026-08-24, not committed here) that a position: fixed img
// with a negative z-index repeats identically on every physical page with no tiling bug --
// unlike the in-document banner that was tried and rejected, this is a non-flow
// pseudo-element-like box, not a block competing with document layout, which is what made
// the difference. That property holds for the four Book 2 elements as much as for Book 1's
// one.
func TestWatermarkAppearsOnEveryBook(t *testing.T) {
	t.Run("book1", func(t *testing.T) {
		var buf bytes.Buffer
		if err := RenderHTML(&buf, Kind1, Metadata{Language: "en"}, nil); err != nil {
			t.Fatalf("render: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, `class="watermark"`) {
			t.Fatal("book1 must render the single brand watermark")
		}
		if strings.Contains(out, `class="food-scatter`) {
			t.Fatal("book1 must not render Book 2's food-scatter illustrations")
		}
		if !strings.Contains(out, `src="data:image/jpeg;base64,`) {
			t.Fatal("watermark must be embedded as a data URI, not fetched at print time")
		}
	})
	t.Run("book2", func(t *testing.T) {
		var buf bytes.Buffer
		if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, nil); err != nil {
			t.Fatalf("render: %v", err)
		}
		out := buf.String()
		if strings.Contains(out, `class="watermark"`) {
			t.Fatal("book2 must not render Book 1's brand watermark")
		}
		if got := strings.Count(out, `class="food-scatter `); got != 4 {
			t.Fatalf("want 4 scattered food-illustration elements, got %d", got)
		}
		if !strings.Contains(out, `src="data:image/png;base64,`) {
			t.Fatal("food-scatter images must be embedded as data URIs, not fetched at print time")
		}
	})
}

// The kitchen-safety chapter renders every SOP row's rule text, grouped under its two
// subsections, appearing exactly once. Book 1 never gets this chapter -- it is scoped to
// Book 2 only, per the original ask.
func TestBook2PrintsTheKitchenSafetyChapter(t *testing.T) {
	data := Book2{
		Child: ChildSummary{DisplayName: "Test Child"},
		SafetySOP: []SafetyGuideline{
			{SOPID: "FS-001", Area: "Raw animal foods", Rule: "No raw or undercooked egg, meat, poultry, fish or shellfish.", Status: "Draft"},
			{SOPID: "FS-004", Area: "Hand hygiene", Rule: "Begin from clean hands and a clean surface.", Status: "Draft",
				EvidenceTitle: "Five Keys to Safer Food", EvidenceAuthority: "WHO"},
		},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en", GenerationDate: time.Now()}, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		"No raw or undercooked egg, meat, poultry, fish or shellfish.",
		"Begin from clean hands and a clean surface.",
		"Five Keys to Safer Food",
	} {
		if strings.Count(out, want) != 1 {
			t.Errorf("want %q to appear exactly once, appeared %d times", want, strings.Count(out, want))
		}
	}
}

// Book 1 carries no SafetySOP field at all -- confirming this via Book 1's own render output
// makes sure the chapter is genuinely Book2-only, not merely unused by Book1's test fixtures.
func TestBook1NeverPrintsTheKitchenSafetyChapter(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, Metadata{Language: "en", GenerationDate: time.Now()},
		Book1{Child: ChildSummary{DisplayName: "Test Child"}}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), "Kitchen safety") {
		t.Fatal("Book 1 must never carry the Book 2 kitchen-safety chapter")
	}
}

// TestBook1CoverPrintsTheChildPhotoAsAFullBleedBackground and
// TestBook1CoverWithNoPhotoPrintsNoBackgroundLayer check for a `background-image` set inline
// on the cover box, not a `class="cover-bg"` layer.
//
// A separate `.cover-bg` div (`position: absolute; inset: 0`) with `.cover-card` given
// `position: relative; z-index: 1` to stack above it was the first construction and it printed
// wrong: against the real cover's full content, Chromium's print path rendered the card and
// the photo as though they belonged to two unrelated boxes, with nothing behind the card at
// all. Confirmed with plain `google-chrome --print-to-pdf` against the real generated HTML
// (not this project's own PDF pipeline), and confirmed fixed by painting the photo as the
// cover box's own CSS background instead -- a background needs no stacking context to sit
// behind its box's content, so there is nothing for the print fragmenter to get wrong. See the
// comment above `.cover.with-photo` in tokens.css and above `B1-COVER-01` in cover.html.
func TestBook1CoverPrintsTheChildPhotoAsAFullBleedBackground(t *testing.T) {
	photo := &ChildPhoto{DataURI: template.URL("data:image/png;base64,iVBORw0KGgo=")}
	b := Book1{
		Metadata: Metadata{Language: "en"},
		Child:    ChildSummary{DisplayName: "Test Child", Photo: photo},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="cover with-photo"`) {
		t.Fatal("with a photo present, the cover must carry the with-photo variant class")
	}
	if !strings.Contains(coverOpeningTag(t, out), "background-image:") {
		t.Fatal("with a photo present, the cover must carry an inline full-bleed background-image")
	}
	if !strings.Contains(out, string(photo.DataURI)) {
		t.Fatal("the child's own photo data URI must appear in the rendered cover")
	}
}

// coverOpeningTag returns just the `<div class="cover ...">` opening tag's own text -- from
// its `<div class="cover ` start through the first `>` that closes it -- rather than the whole
// rendered document.
//
// TestBook1CoverWithNoPhotoPrintsNoBackgroundLayer used to search the entire document for
// "background-image:", which inlines the whole stylesheet (tokens.css, via base.html's
// {{ .CSS }}) into every rendered page. Any future @font-face, .corner-art, or other CSS rule
// anywhere in tokens.css that happened to use a background-image property would fail that test
// for a reason with nothing to do with the cover's own inline style attribute. Scoping the
// search to the cover element's own opening tag is what the test actually means to assert.
func coverOpeningTag(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, `<div class="cover `)
	if start == -1 {
		t.Fatal("no <div class=\"cover ...\"> element found in the rendered document")
	}
	end := strings.Index(html[start:], ">")
	if end == -1 {
		t.Fatal("the cover element's opening tag is never closed")
	}
	return html[start : start+end+1]
}

func TestBook1CoverWithNoPhotoPrintsNoBackgroundLayer(t *testing.T) {
	b := Book1{
		Metadata: Metadata{Language: "en"},
		Child:    ChildSummary{DisplayName: "Test Child"},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(coverOpeningTag(t, buf.String()), "background-image:") {
		t.Fatal("with no photo, the cover must not print an empty background-image")
	}
}

// Book 2's cover carries no child photo -- it is a working recipe document, not the child's
// identification page (see book1/cover.html and book2/cover.html's own comments). Its
// full-bleed background is decorative illustration instead, corner-scattered, with the
// supplied kids-cooking illustration as a hero image inside the identity card.
func TestBook2CoverPrintsTheDecorativeIllustrationsAndTheKidsCookingHero(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, Book2{
		Child: ChildSummary{DisplayName: "Test Child"},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	// html/template always HTML-entity-escapes an attribute value, even one typed
	// template.URL -- the trusted-type marker only bypasses the URL-safety scheme filter, not
	// the ordinary attribute escaper, so a base64 data URI's "+" bytes print as the literal
	// text "&#43;" in the src="" attribute. A real HTML parser (Chromium, at print time; any
	// browser, on screen) decodes that back to "+" per spec, so the printed page is correct --
	// unescaping here checks the same thing a parser would see, rather than routing around a
	// defect. Confirmed against html/template's actual output before writing this, not
	// assumed: an isolated `<img src="{{.}}">` render of a template.URL containing "+"
	// reproduces the identical "&#43;" substitution.
	out := html.UnescapeString(buf.String())
	for _, key := range []string{"bibimbap", "hotpot", "mooncake", "kids-cooking"} {
		if !strings.Contains(out, string(coverArt[key])) {
			t.Errorf("expected the %q illustration's data URI on Book 2's cover", key)
		}
	}
}

// stylesheetSource reads the embedded stylesheet the renderer inlines.
func stylesheetSource(t *testing.T) string {
	t.Helper()
	b, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	return string(b)
}

func TestBook1BackCoverPrintsTheParentsPhotoAsAFullBleedBackground(t *testing.T) {
	photo := &ChildPhoto{DataURI: template.URL("data:image/png;base64,iVBORw0KGgo=")}
	meta := Metadata{Language: "en", ParentsPhoto: photo}
	b := Book1{Metadata: meta, Child: ChildSummary{DisplayName: "Test Child"}}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, meta, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="backcover-bg"`) {
		t.Fatal("with a parents' photo present, the back cover must carry a full-bleed background layer")
	}
	if !strings.Contains(out, string(photo.DataURI)) {
		t.Fatal("the parents' photo data URI must appear on the back cover")
	}
	// The real imprint facts still print -- this page invents nothing new.
	if !strings.Contains(out, "Book version") || !strings.Contains(out, "Generation date") {
		t.Fatal("the back cover must still print the real version/release/date facts end.html always has")
	}
}

func TestBook1BackCoverWithNoParentsPhotoPrintsThePlainImprintPage(t *testing.T) {
	meta := Metadata{Language: "en"}
	b := Book1{Metadata: meta, Child: ChildSummary{DisplayName: "Test Child"}}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, meta, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), `class="backcover-bg"`) {
		t.Fatal("with no parents' photo, the back cover must not print an empty background layer")
	}
}

func TestPrescriptionPagePrintsTheUploadedPhotoInPlaceOfTheBlankForm(t *testing.T) {
	photo := &ChildPhoto{DataURI: template.URL("data:image/png;base64,iVBORw0KGgo="), Caption: "Dr Sen, 4 Sept"}
	meta := Metadata{Language: "en", PrescriptionPhoto: photo}
	b := Book1{Metadata: meta, Child: ChildSummary{DisplayName: "Test Child"}}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, meta, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="prescription-photo"`) {
		t.Fatal("with a prescription photo present, the page must print the photo frame")
	}
	if !strings.Contains(out, string(photo.DataURI)) {
		t.Fatal("the prescription photo data URI must appear on the page")
	}
	if !strings.Contains(out, "Dr Sen, 4 Sept") {
		t.Fatal("the prescription photo's caption must print")
	}
	// The blank form this page prints by default must not also print -- a real, already
	// signed document does not need a second, empty copy of the same fields underneath it.
	if strings.Contains(out, "To be completed by the examining doctor") {
		t.Fatal("with a prescription photo present, the blank form must not also print")
	}
	if strings.Contains(out, "Priority recommendations") {
		t.Fatal("with a prescription photo present, the blank priority-recommendations table must not print")
	}
}

func TestPrescriptionPageWithNoPhotoPrintsTheBlankForm(t *testing.T) {
	meta := Metadata{Language: "en"}
	b := Book1{Metadata: meta, Child: ChildSummary{DisplayName: "Test Child"}}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, meta, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `class="prescription-photo"`) {
		t.Fatal("with no prescription photo, the page must not print an empty photo frame")
	}
	if !strings.Contains(out, "To be completed by the examining doctor") {
		t.Fatal("with no prescription photo, the blank form must print, unchanged")
	}
	if !strings.Contains(out, "Priority recommendations") {
		t.Fatal("with no prescription photo, the blank priority-recommendations table must print")
	}
}

func TestChapterOpenerPrintsADecorativeIllustration(t *testing.T) {
	var buf bytes.Buffer
	b2 := Book2{
		Child:        ChildSummary{DisplayName: "Test Child"},
		MealSections: []MealSection{{Number: 1, Title: "Breakfast", Recipes: nil}},
	}
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, b2); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), `class="chapter-art"`) {
		t.Fatal("a chapter opener must print a decorative corner illustration")
	}
}

// TestChapterOpenerStaysOnOnePageAtMaxRecipes pins the single-page guarantee
// B2-SECTION-01's own comment claims ("The opener is a whole page on purpose") against the
// real worst case, not the easy one.
//
// A first pass of this task's manual verification checked only 1-3 recipe chapters and
// concluded no CSS change was needed -- the wrong end of the range to stress. book2.go's
// maxRecipesPerSection is 10, and real corpus titles are long combinatorial names (see
// pdf_test.go's widestBook2 fixture, "Himalayan India Ragi & Sattu (roasted gram flour)
// Stuffed flatbread", for a real example of the length this draws on). Printed against the
// original 62mm .chapter margin-top plus a 70mm-wide .chapter-art, a ten-recipe chapter with
// titles this long genuinely overflowed: the tenth list item spilled onto what should have
// been the first recipe page, confirmed by rendering and reading the actual PDF before this
// test existed. Fixed by shrinking both (.chapter's margin-top to 30mm, .chapter-art's width
// to 50mm -- see tokens.css). This test pins that fix so a future change to either value, or
// a longer real recipe title, cannot silently reintroduce the split.
func TestChapterOpenerStaysOnOnePageAtMaxRecipes(t *testing.T) {
	if !browserOnPath() {
		t.Skip("no chromium on PATH")
	}
	// Real combinatorial-length titles, not short placeholders -- the defect this test guards
	// against does not reproduce on short titles.
	titles := []string{
		"Himalayan India Ragi & Sattu (roasted gram flour) Stuffed Flatbread",
		"West Bengal Hilsa & Mustard (kasundi) Steamed Curry",
		"South India Toor Dal & Drumstick (moringa) Sambar Curry",
		"North India Paneer & Spinach (palak) Simmered Curry",
		"Bangladesh Ilish & Green Chili (kacha morich) Steamed Bhapa",
		"Northeast India Bamboo Shoot & Pork (soji ekthum) Fermented Curry",
		"Central Tribal India Mahua & Millet (ragi) Roasted Porridge",
		"West India Bajra & Methi (fenugreek leaves) Flatbread Thepla",
		"Nepal Gundruk & Soybean (bhatmas) Fermented Curry",
		"Himalayan India Buckwheat & Potato (aloo) Stuffed Flatbread",
	}
	recipes := make([]RecipeCard, len(titles))
	for i, title := range titles {
		recipes[i] = RecipeCard{RecipeID: "MG-R-TEST", Title: title, Number: i + 1}
	}
	b2 := Book2{
		Child: ChildSummary{DisplayName: "Test Child"},
		MealSections: []MealSection{
			{MealCategoryID: "MC-01", Title: "Breakfast", Number: 1, TargetRecipeCount: 10, Recipes: recipes},
		},
	}
	meta := Metadata{Title: "t", Language: "en", GenerationDate: time.Now()}
	var doc bytes.Buffer
	if err := RenderHTML(&doc, Kind2, meta, b2); err != nil {
		t.Fatalf("render: %v", err)
	}
	pdf, err := PrintPDF(context.Background(), doc.Bytes(), meta)
	if err != nil {
		if errors.Is(err, ErrChromiumUnavailable) {
			t.Skipf("chromium present but not runnable here: %v", err)
		}
		t.Fatalf("PrintPDF: %v", err)
	}

	pages := pageBoxes(t, pdf)
	// The opener page carries both the chapter subtitle ("...recipes selected...") and the
	// tenth (last) recipe's distinctive title word. If the list overflowed, "Buckwheat" would
	// appear on a later page instead of alongside "selected".
	openerIdx := -1
	for i, p := range pages {
		hasSelected, hasBuckwheat := false, false
		for _, w := range p.Words {
			switch strings.TrimSpace(w.Text) {
			case "selected":
				hasSelected = true
			case "Buckwheat":
				hasBuckwheat = true
			}
		}
		if hasSelected && hasBuckwheat {
			openerIdx = i
			break
		}
	}
	if openerIdx == -1 {
		t.Fatal("no single page carries both the chapter subtitle and the tenth recipe's " +
			"title -- the ten-item list did not fit on the chapter opener's own page")
	}
	if openerIdx+1 >= len(pages) {
		t.Fatal("no page follows the chapter opener")
	}
	// The page immediately after the opener must be a genuine recipe page (it carries the
	// INGREDIENTS heading every recipe page prints), not a continuation of the chapter list.
	hasIngredients := false
	for _, w := range pages[openerIdx+1].Words {
		if strings.TrimSpace(w.Text) == "INGREDIENTS" {
			hasIngredients = true
			break
		}
	}
	if !hasIngredients {
		t.Fatal("the page after the chapter opener is not a recipe page -- the ten-item " +
			"list spilled onto it instead of fitting on the opener's own page")
	}
}
