package book

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Skips rather than fails without a browser, so `go test ./...` stays green on a machine
// with no Chromium -- the same contract the integrity suite has with TEST_DATABASE_URL. A
// skipped test is not a passing test; run it with a browser before calling this done.
func TestPrintPDFProducesAPDF(t *testing.T) {
	// Every name a Chromium build ships under, because a probe that misses the one name
	// installed here would skip silently and a skipped test is not a passing test. Arch
	// packages the browser as google-chrome-stable, Debian as google-chrome, and the
	// open-source builds as chromium or chromium-browser.
	if !browserOnPath() {
		t.Skip("no chromium on PATH")
	}

	var doc bytes.Buffer
	meta := Metadata{
		Title: "t", Language: "en", ReviewStatus: "Draft",
		BookVersion: "V1", ReleaseID: "TEST", GenerationDate: time.Now(),
	}
	if err := RenderHTML(&doc, Kind1, meta, Book1{Metadata: meta}); err != nil {
		t.Fatalf("render: %v", err)
	}

	out, err := PrintPDF(context.Background(), doc.Bytes(), meta)
	if err != nil {
		if errors.Is(err, ErrChromiumUnavailable) {
			t.Skipf("chromium present but not runnable here: %v", err)
		}
		t.Fatalf("PrintPDF: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF, first bytes: %q", out[:min(8, len(out))])
	}
}

// TestNoPageContentOverflowsTheTextBlock is the guard for a defect that printed an entire
// book at four fifths size without failing a single assertion.
//
// Book 1's reference table -- five columns of citation prose plus an unbreakable source id --
// resolved to 191mm inside a 170mm text block. Chromium does not clip an over-wide print
// document and does not report it: it scales the whole document down by whatever factor makes
// it fit. Every page of that book printed at 79%, which put 10.5pt body text at 8.3pt, under
// the contract's minimum_body_pt of 9.5. It was found by measuring glyph heights in two PDFs
// and noticing that the same stylesheet produced a 36.8pt capital in one book and a 46.3pt
// capital in the other.
//
// So the check is on the layout, not on the output: load each book at exactly the width the
// printed page gives its content and require that nothing sticks out. A document that fits
// is a document Chromium prints at the size it was designed at.
//
// Skips without a browser, like the print test above. A skipped test is not a passing test.
//
// NOT YET PROVEN TO CATCH THE DEFECT. Removing `table-layout: fixed` from tokens.css -- the
// change that reintroduces the exact bug this test is named for -- still leaves it green, so
// the emulated viewport is not constraining layout the way the printed page does. Until that
// is closed the test is exercise, not a guard: it renders both books through the real
// pipeline and would catch a gross overflow, and it would not have caught the one that
// shipped. Finishing it means measuring against the print fragmentainer rather than an
// emulated device width -- most likely by printing to PDF and comparing a known glyph's
// height against its declared point size, which is how the defect was actually found.
func TestNoPageContentOverflowsTheTextBlock(t *testing.T) {
	if !browserOnPath() {
		t.Skip("no chromium on PATH")
	}

	// A4 210mm less the 20mm side margins declared in tokens.css and set on the print in
	// pdf.go, converted at the CSS reference of 96dpi.
	const textBlockPx = 170.0 / 25.4 * 96.0

	for _, tc := range []struct {
		name string
		kind Kind
		data any
	}{
		{"book1", Kind1, widestBook1()},
		{"book2", Kind2, widestBook2()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := Metadata{
				Title: tc.name, Language: "en", ReviewStatus: "Draft",
				BookVersion: "V1", GenerationDate: time.Now(),
			}
			var doc bytes.Buffer
			if err := RenderHTML(&doc, tc.kind, meta, tc.data); err != nil {
				t.Fatalf("render: %v", err)
			}

			offenders, err := overflowingElements(t, doc.Bytes(), textBlockPx)
			if err != nil {
				t.Fatalf("measure: %v", err)
			}
			if len(offenders) != 0 {
				t.Fatalf("%d element(s) wider than the %0.f px text block:\n%s\n\n"+
					"Chromium does not report this: it silently scales the whole document "+
					"down to fit, so every page prints below its declared type size. Give "+
					"the offending table explicit column widths, or let its long values wrap.",
					len(offenders), textBlockPx, strings.Join(offenders, "\n"))
			}
		})
	}
}

// overflowingElements loads a document at a fixed width and returns every element wider than
// it, described well enough to find in a template.
func overflowingElements(t *testing.T, htmlDoc []byte, widthPx float64) ([]string, error) {
	t.Helper()

	ctx, cancel := allocatorFor(context.Background())
	defer cancel()
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// The measurement is on the print layout, not the screen one: @page margins, the
	// break rules and the print-only display changes all move widths around.
	js := fmt.Sprintf(`(() => {
		const limit = %f + 1;
		const out = [];
		for (const el of document.querySelectorAll('body *')) {
			const w = el.getBoundingClientRect().width;
			if (w > limit) {
				out.push(Math.round(w) + 'px  ' + el.tagName.toLowerCase() +
					(el.className ? '.' + String(el.className).split(' ').join('.') : '') +
					'  |' + (el.textContent || '').trim().slice(0, 60).replace(/\s+/g, ' ') + '|');
			}
		}
		return out;
	})()`, widthPx)

	var out []string
	err := chromedp.Run(ctx,
		emulation.SetEmulatedMedia().WithMedia("print"),
		emulation.SetDeviceMetricsOverride(int64(widthPx), 2000, 1, false),
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frame, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(frame.Frame.ID, string(htmlDoc)).Do(ctx)
		}),
		chromedp.Evaluate(js, &out),
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// widestBook1 is a Book 1 built from the widest content the corpus actually contains: the
// five-column reference table with its longest citations and unbreakable source ids, an
// eight-column tracker, the seven-column growth table and a full safety card. These are the
// shapes that overflow; a fixture of short strings would pass whatever the stylesheet did.
func widestBook1() Book1 {
	longest := "IAP-HRINFANT-2025"
	return Book1{
		Metadata: Metadata{Language: "en", ReviewStatus: "Draft"},
		Child: ChildSummary{
			DisplayName: "Ananya Roy", DateOfBirth: "2022-03-14",
			AgeMonths: 53, AgeLabel: "4 years 5 months",
		},
		Sections: []Section{
			{
				BlockID: "B1-022", TemplateID: "B1-REFS-01",
				Title: "Reference & Disclaimer", Subtitle: "Evidence/version page",
				// Several rows, not one. An auto-layout table sizes its columns from the
				// widest cell in each, so a single row understates the width the real
				// thirteen-row table reaches -- the fixture has to carry the longest value
				// of every column at once, which is what the corpus does.
				Refs: []EvidenceSource{
					{
						SourceID:  longest,
						Authority: "IAP Neurodevelopmental Pediatrics Chapter",
						Topic:     "High-risk infant follow-up",
						Reference: "Consensus Guidelines on Developmentally Supportive Follow-Up for High-Risk Infants",
						HowUsed:   "High-risk infant screening/escalation",
						Limitation: "High-risk infants require validated screening and specialist " +
							"follow-up rather than a general schedule",
					},
					{
						SourceID:   "IAP-STG-CONSTIPATION",
						Authority:  "Indian Academy of Pediatrics",
						Topic:      "Constipation",
						Reference:  "Standard Treatment Guidelines listing",
						HowUsed:    "Constipation module",
						Limitation: "Does not reproduce treatment/medication advice",
					},
					{
						SourceID:   "WHO-GROWTH",
						Authority:  "WHO",
						Topic:      "Growth standards/reference",
						Reference:  "WHO Child Growth Standards 0-5 and Growth Reference 5-19",
						HowUsed:    "Growth monitoring reference",
						Limitation: "Use validated z-score calculation module",
					},
				},
			},
			{
				BlockID: "B1-003", TemplateID: "B1-GROWTH-01",
				Title: "Growth Monitoring", Subtitle: "Anthropometry",
				Growth: []GrowthRow{{
					MeasuredOn: "2026-08-01", WeightKg: "15.1 kg", HeightCm: "101.0 cm",
					Interpretation: "Within expected range for age",
					MeasuredBy:     "Clinic",
				}},
			},
			{
				BlockID: "B1-025", TemplateID: "B1-TRACKER-01",
				Title: "Sleep & Routine", Subtitle: "Sleep habits and sleep-quality monitoring",
				Trackers: []TrackerSpec{{
					Title:     "Sleep diary",
					Reference: "Family/age-appropriate target",
					Frequency: "Selected week",
					Alarm:     "Loud habitual snoring, breathing pauses or marked daytime sleepiness",
					Review:    "Pediatric/sleep/ENT review as indicated",
					Columns: []string{
						"Daily/weekly", "Family/age-appropriate target", "Actual sleep pattern",
						"Bedtime", "wake time", "naps", "night waking", "snoring",
					},
					Rows: 4,
				}},
			},
			{
				BlockID: "B1-018", TemplateID: "B1-SAFETY-01",
				Title: "Allergy & Safety", Subtitle: "Personal safety page",
				Safety: &SafetyCard{
					Confirmed: []string{"Peanut"},
					Choking: []ChokingRule{{
						Food: "Sausage/hotdog-type food", Risk: "Round coin pieces",
						Rule: "Adapt or block", AgeFor: "<5 years",
					}},
				},
			},
		},
	}
}

// widestBook2 is a recipe page carrying the longest title, ingredient list and method the
// corpus produces, plus the nutrition strip and the four-column family tracker.
func widestBook2() Book2 {
	prep, cook := 15, 20
	band, texture := "Moderate", "Family texture"
	return Book2{
		Metadata: Metadata{Language: "en", ReviewStatus: "Draft"},
		Child: ChildSummary{
			DisplayName: "Ananya Roy", AgeLabel: "4 years 5 months",
			FoodPractice: "Vegetarian", AllergyStatus: "Peanut (confirmed)",
		},
		MealSections: []MealSection{{
			MealCategoryID: "MC-02", Title: "Lunch", Number: 1, TargetRecipeCount: 25,
			Recipes: []RecipeCard{{
				RecipeID:      "MG-R-00839",
				RecipeVersion: "d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513",
				Title:         "Himalayan India Ragi & Sattu (roasted gram flour) Stuffed flatbread",
				Number:        1,
				RegionCulture: "Himalayan India",
				ReviewStatus:  "Draft - Culinary/Nutrition/Clinical Review Required",
				SelectionReasons: []string{
					"Nutrition target NT00: no applicable clinical marker, routine age-appropriate default",
					"Diet practice: Vegetarian",
				},
				NutritionTags:   []string{"Clinical tag: Recovery/low-appetite option"},
				PrepTimeMinutes: &prep, CookTimeMinutes: &cook,
				CostBand: &band, TextureServing: &texture,
				Serving: "262 g",
				Safety: "Adult supervision; age-safe texture; remove bones/pits/seeds; " +
					"apply allergen exclusions; no honey below 12 months.",
				Ingredients: []IngredientLine{
					{Name: "Sattu (roasted gram flour)", Bengali: "ছাতু", Quantity: "50 g"},
				},
				MethodSteps: []string{
					"Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.",
				},
				MethodIsProviderBoilerplate: true,
				Meta: []RecipeMeta{
					{Label: "Prep", Value: "15 min", Mono: true},
					{Label: "Cook", Value: "20 min", Mono: true},
					{Label: "Serving", Value: "262 g", Mono: true},
					{Label: "Texture", Value: "Family texture"},
					{Label: "Cost band", Value: "Moderate"},
				},
				Nutrition: &RecipeNutrition{
					EnergyKcal: "296", ProteinG: "13.7", IronMg: "6.0", CalciumMg: "253",
					Coverage: 0.54, CoveragePct: 54, BasisG: "175 g",
					Formula: "sum(quantity_g / 100 * ingredient value per 100 g)",
				},
			}},
		}},
	}
}
