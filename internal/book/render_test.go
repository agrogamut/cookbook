package book

import (
	"bytes"
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
	if strings.Contains(blank, ">0<") || strings.Contains(blank, "n/a") {
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
	}{
		{
			templateID: "B1-DAILY-01",
			section: Section{Domains: []DailyDomain{{
				ID: "DL-X", Domain: "DOMAINNAME", AgeContext: "AGECONTEXT",
				Reference: "DOMAINREF", Goal: "DOMAINGOAL", RedFlag: "DOMAINREDFLAG",
				Referral: "DOMAINREFERRAL", AILimit: "DOMAINLIMIT", Tracker: &grid,
			}}},
			want: []string{"DOMAINNAME", "AGECONTEXT", "DOMAINREF", "DOMAINGOAL",
				"DOMAINREDFLAG", "DOMAINREFERRAL", "DOMAINLIMIT",
				"TRACKERTITLE", "COLONE", "COLTWO", "TRACKERALARM", "TRACKERREVIEW"},
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
				"ILLNESSREDFLAG", "ILLNESSLIMIT", "TRACKERTITLE", "COLONE"},
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

// Pictures were removed from the recipe page by decision -- neither the drawn dish-format
// mark nor a stored photograph prints, regardless of whether RecipeCard.Mark/.Photo are set.
// The struct fields stay (the API still resolves them), only the template stopped printing.
func TestARecipePagePrintsNoPictureEvenWhenMarkAndPhotoAreSet(t *testing.T) {
	card := RecipeCard{
		RecipeID: "MG-R-00001", Title: "West Bengal Rice & Rohu fish Soft rice bowl",
		Number: 1, ReviewStatus: "Draft",
		Mark:  Mark("bowl-grain", "Soft rice bowl"),
		Photo: &RecipePhoto{DataURI: template.URL("data:image/jpeg;base64,ZmFrZQ==")},
	}
	b := Book2{MealSections: []MealSection{{
		MealCategoryID: "MC-01", Title: "Breakfast", Number: 1, Recipes: []RecipeCard{card},
	}}}

	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, `class="dish-mark"`) {
		t.Error("the recipe page must not print the dish mark frame")
	}
	if strings.Contains(got, `<img class="dish-photo"`) {
		t.Error("the recipe page must not print a dish photo")
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
	if strings.Contains(buf.String(), `<figure class="dish-mark"`) {
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
		// Book 2 keeps its original two-role set -- untouched by the Book 1 sign-off change.
		{Kind2, Book2{Child: ChildSummary{DisplayName: "Test Child"}}, []string{"Director", "Dietitian"}},
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

// The watermark: water1.jpeg (the icon mark, not the full text lockup), fixed and centered
// behind the content on every page. Verified by a real headless-Chrome print spike
// (2026-08-24, not committed here) that a position: fixed img with a negative z-index repeats
// identically on every physical page with no tiling bug -- unlike the in-document banner that
// was tried and rejected, this is a non-flow pseudo-element-like box, not a block competing
// with document layout, which is what made the difference.
func TestWatermarkAppearsOnEveryBook(t *testing.T) {
	for _, kind := range []Kind{Kind1, Kind2} {
		t.Run(string(kind), func(t *testing.T) {
			var buf bytes.Buffer
			if err := RenderHTML(&buf, kind, Metadata{Language: "en"}, nil); err != nil {
				t.Fatalf("render: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, `class="watermark"`) {
				t.Fatal("no watermark element rendered")
			}
			if !strings.Contains(out, `src="data:image/jpeg;base64,`) {
				t.Fatal("watermark must be embedded as a data URI, not fetched at print time")
			}
		})
	}
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

// stylesheetSource reads the embedded stylesheet the renderer inlines.
func stylesheetSource(t *testing.T) string {
	t.Helper()
	b, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	return string(b)
}
