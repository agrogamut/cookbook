# Book layout and imagery - implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make both printed books read as books - full sheets, correct centring, breaks that
land where a reader expects - fix the console preview so an operator judges the real document,
and give Book 2 pictures that are drawn from provider data rather than taken from someone else.

**Architecture:** Layout is fixed in `internal/book/templates/tokens.css` and the templates it
serves, with two Go-side changes: page breaks move from block to provider-declared part, and
table column widths are computed from content instead of being uniform. Imagery is a new layer -
three migrations (format marks, composition shares, an empty photograph table), eleven embedded
SVG archetypes, and a recipe-page illustration unit. The console stops rendering HTML and embeds
the printed PDF, with the HTML preview kept as the no-Chromium fallback and given screen styling
for the first time. A printed-page fill guard runs over both books and its budget drops task by
task.

**Tech Stack:** Go 1.26, `html/template`, chromedp + headless Chromium, Postgres 16 (pgx/v5,
golang-migrate), Next.js App Router + React + Tailwind + shadcn/ui, vitest + jsdom, poppler
(`pdftotext`, test-only).

**Spec:** `docs/superpowers/specs/2026-08-20-book-layout-and-imagery-design.md`

## Global Constraints

Every task's requirements implicitly include this section. Values are copied verbatim from
`CLAUDE.md` and the spec.

- **Never invent data.** Every value reaching a reader traces to a provider workbook, a named
  external dataset with row id and URL, or a documented computation over one of those, labelled
  `derived` and carrying its formula, its source rows and a confidence where a join was involved.
  A missing value renders as an explicit gap, never as `0` and never as a plausible number.
- **No model-generated guidance prose in the unreviewed path.** No task in this plan writes
  recipe text, preparation steps, safety notes or clinical advice.
- **No external photographs.** See spec 3.2. Do not fetch, embed or link `image-url` from
  `indian_food_dataset.csv`. Do not relax the print browser's
  `--host-resolver-rules=MAP * ~NOTFOUND`.
- **Never mark anything approved.** `Review_Status` and `Data_Quality` print verbatim.
- Steps 1 (age) and 2 (allergy) stay hard filters. No page offers an override.
- Contract floors, never crossed downward: `safe_margin_mm` 12, `minimum_body_pt` 9.5,
  `minimum_table_pt` 8.5, `body_max_line_length_chars` 85, `table_header_repeat`,
  `clinical_warning_visibility` high, `avoid_color_only_meaning`.
- Page geometry stays A4, margins top 22mm / side 20mm / bottom 20mm, text block 170 x 255mm.
  `tokens.css` and the header/footer templates in `pdf.go` must agree.
- `table-layout: fixed` stays. Removing it reintroduces the 79%-scale defect, in which one
  over-wide table silently shrinks the whole document and puts 10.5pt body text on paper at
  8.3pt.
- No attribution trailers of any kind in commits, comments, docs or code. No emojis.
- Verify before calling backend work done: `go build ./...`, `go vet ./...`, and
  `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...`. Frontend: `cd web && npm test`.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `internal/book/pagefit_test.go` | new. Printed-page fill and page-open guards over both books | 1 |
| `internal/book/templates/tokens.css` | modify. Cover, breaks, screen media, illustration unit | 2,3,4,5,6,7,9,10,12 |
| `internal/book/templates/book1/cover.html` | modify. Photograph variant class | 2 |
| `internal/book/templates/book2/cover.html` | modify. Same variant contract, no photograph | 2 |
| `internal/book/types.go` | modify. `Section.StartsPart`, `RecipeCard.Illustration`, `ColumnWidths` | 3,6,9,10,11 |
| `internal/book/book1.go` | modify. Set `StartsPart` during assembly | 3 |
| `internal/book/templates/book1/body.html` | modify. Break on part, not block | 3 |
| `internal/book/pagepolicy.go` | new. Blocks that must start a sheet regardless of part | 3 |
| `internal/book/tracker.go` | modify. Row groups of four, title into `thead` | 4,5 |
| `internal/book/templates/book1/tracker.html` | modify. `tbody` groups, spanning title row | 4,5 |
| `internal/book/colwidth.go` | new. Column width from longest unbreakable token | 6 |
| `internal/book/colwidth_test.go` | new. Table-driven width tests | 6 |
| `internal/db/migrations/0019_recipe_format_mark.{up,down}.sql` | new. 28 format rows to 11 marks | 8 |
| `internal/db/migrations/0020_recipe_composition_share.{up,down}.sql` | new. Mass share view + macro map | 10 |
| `internal/db/migrations/0021_recipe_photo.{up,down}.sql` | new. Empty photograph table, GAP-025 re-measured | 11 |
| `internal/book/marks/*.svg` | new. Eleven line-art archetypes | 9 |
| `internal/book/marks.go` | new. `embed.FS` loader, validation, lookup | 9 |
| `internal/book/marks_test.go` | new. Seed and artwork agree; no external references | 9 |
| `internal/book/book2.go` | modify. Load mark, composition and photograph per card | 9,10,11 |
| `internal/book/templates/book2/recipe.html` | modify. Illustration unit | 9,10,11 |
| `internal/db/book_imagery_test.go` | new. Every recipe resolves one mark; shares sum to 1 | 8,10 |
| `web/src/components/book-generator.tsx` | modify. Embed the printed PDF, reuse the blob | 13 |
| `web/src/lib/api.ts` | modify. `generateBookSetWithPdfs` | 13 |
| `web/src/components/__tests__/book-generator.test.tsx` | modify. PDF path and 503 fallback | 13 |
| `CLAUDE.md`, `docs/decisions.md` | modify. Record what changed and the imagery ruling | 14 |

---

## Task 1: The printed-page fill guard

Nothing measures whether a printed page is full, which is why 38% of Book 1 could ship ending
above 62% of the sheet with a green suite. Build the measurement first; every later task is
scored by it.

**Files:**
- Create: `internal/book/pagefit_test.go`

**Interfaces:**
- Consumes: `book.Set`, `book.PrintPDF` (existing), the `pdf_test.go` fixtures
- Produces: `pageFills(pdf []byte) ([]float64, error)`, `maxUnderfilledPages` budget constant

- [ ] **Step 1: Write the failing test**

`pdftotext -bbox` emits one `<word xMin= yMin= xMax= yMax=>` per word, in PostScript points on
an A4 page 841.89pt tall. The text block runs from 22mm (62.36pt) to 277mm (785.20pt). The
running head sits above it and the folio below, so both are excluded by point range rather than
by string matching - the head's text changes with the provider's review status.

```go
package book

import (
	"encoding/xml"
	"fmt"
	"os/exec"
	"testing"
)

// Page geometry in PostScript points, matching tokens.css and pdf.go. A4 is 841.89pt tall;
// the 22mm top and 20mm bottom margins put the text block between these two lines.
const (
	textBlockTopPt    = 62.36
	textBlockBottomPt = 785.20
)

// underfillThreshold is the fraction of the text block a page must reach. 0.62 is not a taste
// call: below it a reader turning the page sees more paper than text, which is the state 18 of
// Book 1's 47 pages shipped in.
const underfillThreshold = 0.62

// maxUnderfilledPages is a budget, not a target, and it is lowered by each task in this plan
// that improves it. A budget rather than a per-page allowlist because page numbers move
// whenever a break moves, and an allowlist keyed on them would need rewriting every task.
//
// Book 1: 18 -> Book 2: 4. Both books are measured against one budget because the display
// pages that legitimately end early (cover, chapter opener, imprint) exist in both.
const maxUnderfilledPages = 22

type bboxDoc struct {
	Pages []struct {
		Words []struct {
			YMax float64 `xml:"yMax,attr"`
		} `xml:"word"`
	} `xml:"body>doc>page"`
}

// pageFills returns, for each page, the last text baseline as a fraction of the text block.
// A page with no text in the block yields 0.
func pageFills(t *testing.T, pdf []byte) []float64 {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext (poppler) is not installed; the page-fill guard cannot run")
	}
	cmd := exec.Command("pdftotext", "-bbox", "-", "-")
	cmd.Stdin = bytesReader(pdf)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	var doc bboxDoc
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse pdftotext bbox output: %v", err)
	}
	fills := make([]float64, 0, len(doc.Pages))
	for _, p := range doc.Pages {
		last := 0.0
		for _, w := range p.Words {
			// Outside the text block is the running head or the folio, neither of which is
			// content and neither of which fills a page.
			if w.YMax <= textBlockTopPt || w.YMax >= textBlockBottomPt {
				continue
			}
			if w.YMax > last {
				last = w.YMax
			}
		}
		if last == 0 {
			fills = append(fills, 0)
			continue
		}
		fills = append(fills, (last-textBlockTopPt)/(textBlockBottomPt-textBlockTopPt))
	}
	return fills
}

func TestNoMorePagesEndEarlyThanTheBudget(t *testing.T) {
	set := printableSet(t) // shared fixture, see Step 3
	total, under := 0, 0
	var detail []string
	for _, b := range []struct {
		name string
		pdf  []byte
	}{{"book1", set.Book1PDF}, {"book2", set.Book2PDF}} {
		for i, f := range pageFills(t, b.pdf) {
			total++
			if f < underfillThreshold {
				under++
				detail = append(detail, fmt.Sprintf("%s p%d ends at %.0f%%", b.name, i+1, f*100))
			}
		}
	}
	if under > maxUnderfilledPages {
		t.Errorf("%d of %d printed pages end above %.0f%% of the text block, budget is %d:\n  %v",
			under, total, (1-underfillThreshold)*100, maxUnderfilledPages, detail)
	}
	t.Logf("page fill: %d of %d pages under budget %d", under, total, maxUnderfilledPages)
}
```

- [ ] **Step 2: Run it to confirm it measures**

```
scripts/dev_db.fish up
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -run TestNoMorePagesEndEarly -v
```

Expected: PASS, with the log line reporting `22 of 65 pages under budget 22`. If it reports a
different count, set `maxUnderfilledPages` to that count and note the number in the commit
message - the budget records today's state, and the later tasks are what lower it.

- [ ] **Step 3: Add the shared printable fixture**

`pdf_test.go` already builds a printable set; extract it so both files use one fixture rather
than each assembling its own child. Add to `pagefit_test.go`:

```go
// printableSet assembles and prints both books once for the whole file. Printing is the
// expensive part of this suite -- two Chromium renders -- and every guard here reads the same
// two documents, so they are printed once and shared.
type printedSet struct{ Book1PDF, Book2PDF []byte }

func printableSet(t *testing.T) printedSet { /* reuse pdf_test.go's fixture child */ }
```

Reuse the existing skip contract: no `TEST_DATABASE_URL` skips, no Chromium skips with
`ErrChromiumUnavailable`.

- [ ] **Step 4: Add the page-open guard**

A page must not open on an orphaned warning or a bare table continuation. Both are detectable
from the same bbox data: the first text on the sheet, matched against the callout and column-head
vocabularies.

```go
// TestNoPageOpensOnAnOrphanedWarning: a page whose first line is a callout heading has been
// split away from the domain that raised it. Measured on pages 28 and 30 of Book 1.
func TestNoPageOpensOnAnOrphanedWarning(t *testing.T) { /* first-word-of-page check */ }
```

Budget this the same way: a constant, lowered by task 7.

- [ ] **Step 5: Commit**

```bash
git add internal/book/pagefit_test.go
git commit -m "Measure how full a printed page actually is"
```

---

## Task 2: The cover centres, and fits with a photograph

Two defects in twelve lines of CSS. Both are visible on every book an operator has printed.

**Files:**
- Modify: `internal/book/templates/tokens.css:419-445`
- Modify: `internal/book/templates/book1/cover.html:9`
- Modify: `internal/book/templates/book2/cover.html:9`
- Test: `internal/book/pagefit_test.go`, `internal/book/render_test.go`

**Interfaces:**
- Consumes: `Book1.Child.Photo` (existing, `*ChildPhoto`)
- Produces: the `.cover.with-photo` / `.cover.no-photo` class contract on the cover root

- [ ] **Step 1: Write the failing test**

```go
// The cover is one page, whether or not a photograph was supplied.
//
// It was not. With a photograph the stack measured 262mm into a 255mm block: the facts row was
// pushed onto page 2 and printed alone above 250mm of white, and the contents page moved to
// page 3. Without one the same stack measured 201mm and left the foot floating 54mm above the
// bottom margin. A declared drop that does not account for the tallest optional band above it
// is a drop that is wrong for one of the two covers.
func TestTheCoverIsOnePageWithAndWithoutAPhotograph(t *testing.T) {
	for _, tc := range []struct {
		name  string
		photo bool
	}{{"with a photograph", true}, {"without one", false}} {
		t.Run(tc.name, func(t *testing.T) {
			pdf := printBook1(t, withPhoto(tc.photo))
			fills := pageFills(t, pdf)
			// Page 2 is the contents page in both cases. If the cover spilled, page 2 holds the
			// three facts cells and ends in the first fifth of the sheet.
			if fills[1] < 0.30 {
				t.Errorf("page 2 ends at %.0f%%: the cover spilled onto it", fills[1]*100)
			}
			// And the cover itself reaches the foot of its own sheet rather than stopping in
			// the middle of nowhere.
			if fills[0] < 0.80 {
				t.Errorf("the cover ends at %.0f%% of the block; the facts row should sit near the foot", fills[0]*100)
			}
		})
	}
}

// The cover's centred blocks are actually centred.
//
// tokens.css set the auto side margins on one line and threw them away two rules later with a
// `margin` shorthand, so the title printed about 15mm left of the page centre and the standfirst
// about 27mm left of it, each with its text centred inside a box hard against the left edge.
func TestTheCoverTitleIsCentredOnThePage(t *testing.T) {
	pdf := printBook1(t, withPhoto(false))
	box := wordBox(t, pdf, 1, "Ananya")  // any word of the cover title
	pageCentre := 595.28 / 2             // A4 width in points
	titleCentre := (box.XMin + box.XMax) / 2
	if math.Abs(titleCentre-pageCentre) > 6 { // 6pt is about 2mm
		t.Errorf("cover title centre is %.1fpt, page centre is %.1fpt", titleCentre, pageCentre)
	}
}
```

- [ ] **Step 2: Run to verify both fail**

```
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -run 'TestTheCover' -v
```

Expected: `TestTheCoverIsOnePageWithAndWithoutAPhotograph/with_a_photograph` FAILs with page 2
ending near 5%; `TestTheCoverTitleIsCentredOnThePage` FAILs with the title centre about 42pt left.

- [ ] **Step 3: Fix the centring**

In `tokens.css`, fold the auto margins into the rules that set the other margins rather than
declaring them separately where a later shorthand can drop them. Delete line 419's standalone
rule and write:

```css
/* Centred blocks need their own measure and their own centring: text-align does nothing for a
   block whose width is already the full column, so each caps its width and takes auto side
   margins.

   The auto margins live in the same rule as the vertical ones and are never set by a separate
   earlier rule. They were, and a `margin: 0 0 5mm` shorthand two rules later silently reset
   them: the cover printed with its title 15mm left of centre and its standfirst 27mm left, each
   centred inside a box hard against the left edge of the column. A shorthand that resets a
   longhand set elsewhere is invisible in review and obvious on paper. */
.cover h1 {
  font-size: var(--display);
  line-height: 1.15;
  margin: 0 auto 5mm;
  color: var(--brand-deep);
  max-width: 140mm;
}
.cover .cover-sub {
  font-size: var(--lead);
  font-style: italic;
  color: var(--ink-soft);
  margin: 0 auto;
  max-width: 116mm;
}
```

- [ ] **Step 4: Make the drop account for the portrait**

The template knows whether a photograph is present, so the stylesheet can. Replace the single
`.cover .cover-main { margin-top: 105mm; }` with the two variants, and record the arithmetic:

```css
/* The drop that carries the title into the lower half of the sheet. Two declared distances
   rather than one, because the band above the title is 75mm taller when a portrait is present
   and a single constant cannot be right for both covers.

   Measured against the 255mm text block, kicker 7mm and facts block 38mm:

     no portrait:   7 + 78 + 19 (title) + 18 (standfirst) + 38 = 160mm, foot at 63% -- and the
                    facts block's own 24mm gap plus the sheet's remaining space carry it down
     with portrait: 7 + 26 + 75 (portrait) + 19 + 18 + 38 = 183mm

   Both fit. Both are checked by TestTheCoverIsOnePageWithAndWithoutAPhotograph, which is the
   only thing that keeps them right: this arithmetic is a prediction and the printed sheet is
   the measurement. */
.cover.no-photo .cover-main   { margin-top: 78mm; }
.cover.with-photo .cover-main { margin-top: 26mm; }
```

- [ ] **Step 5: Set the class in both cover templates**

`book1/cover.html`:

```html
<div class="cover {{ if .Child.Photo }}with-photo{{ else }}no-photo{{ end }}">
```

`book2/cover.html` carries no photograph and never will - it is a working recipe document - so
it declares the variant literally rather than branching on a field it does not have:

```html
{{/* no-photo, always: the portrait belongs on Book 1's cover, which is the identification
     document. Book 2 is the one a cook keeps open on a counter. */}}
<div class="cover no-photo">
```

- [ ] **Step 6: Push the facts block to the foot**

The facts row must sit near the bottom margin on both variants without a flex distribution - see
the reasoning above `.cover` in `tokens.css`, which still holds. Give it a declared minimum gap
that the two drops above are calibrated against:

```css
.cover .cover-facts {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 4mm 6mm;
  border-top: 0.5mm solid var(--rule-strong);
  padding-top: 4mm;
  margin-top: 24mm;
}
```

Leave this rule as it is. If Step 4's arithmetic leaves the foot short of 80% fill, adjust the
two `margin-top` values in Step 4 - never this one, which is the gap between the standfirst and
the rule and is a typographic measure, not a positioning device.

- [ ] **Step 7: Run the tests**

```
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -run 'TestTheCover' -v
```

Expected: PASS for all three subtests.

- [ ] **Step 8: Look at the printed sheets**

Counting is not looking, and every layout defect in this project's history passed its own count.

```
scripts/dev_db.fish up
go build -o /tmp/mgy-server ./cmd/server
DATABASE_URL=(scripts/dev_db.fish url) PORT=8099 /tmp/mgy-server &
curl -s -o /tmp/b1.pdf -X POST localhost:8099/api/books/generate/book1.pdf \
  -H 'content-type: application/json' --data @testdata/preview-child-with-photo.json
pdftoppm -r 100 -png -f 1 -l 2 /tmp/b1.pdf /tmp/cover
```

Open `/tmp/cover-01.png` and `/tmp/cover-02.png`. The cover is one sheet, the title is on the
page centre, the facts row sits above the bottom margin, and page 2 is the contents page.

- [ ] **Step 9: Commit**

```bash
git add internal/book/templates/tokens.css internal/book/templates/book1/cover.html \
        internal/book/templates/book2/cover.html internal/book/pagefit_test.go
git commit -m "Centre the cover and fit it on one sheet with a photograph"
```

---

## Task 3: Book 1 breaks on the provider's parts, not on every block

The single largest cause of white paper. Thirty-one blocks each take a fresh sheet, so a block
holding three writing lines gets one.

**Files:**
- Modify: `internal/book/types.go` (add `Section.StartsPart`)
- Modify: `internal/book/book1.go` (set it during assembly)
- Modify: `internal/book/templates/book1/body.html:15`
- Create: `internal/book/pagepolicy.go`
- Test: `internal/book/assemble_test.go`, `internal/book/pagefit_test.go`

**Interfaces:**
- Consumes: `book1_content_block.part` (migration `0013`, already imported)
- Produces: `Section.StartsPart bool`, `book.MustStartASheet(blockID string) bool`

- [ ] **Step 1: Write the failing test**

```go
// Book 1 breaks where the provider grouped its blocks, not once per block.
//
// The workbook puts the thirty-two blocks into fifteen parts, and the pairs mean something: A is
// the profile and the consultation summary, B is growth and its trend page, E is the vaccination
// pair. Breaking per block gave a three-line page its own sheet and left eighteen of forty-seven
// pages ending above 62% of the block.
func TestBook1BreaksOnPartNotOnBlock(t *testing.T) {
	b := assembleBook1Fixture(t)
	starts := 0
	for _, s := range b.Sections {
		if s.StartsPart {
			starts++
		}
	}
	if starts >= len(b.Sections) {
		t.Fatalf("%d of %d sections start a sheet: that is one per block", starts, len(b.Sections))
	}
	// Every part that rendered starts exactly one sheet, and no part starts two.
	seen := map[string]int{}
	for _, s := range b.Sections {
		if s.StartsPart {
			seen[s.Part]++
		}
	}
	for part, n := range seen {
		if n != 1 {
			t.Errorf("part %s starts %d sheets, want 1", part, n)
		}
	}
	// The first rendered section always starts one, whatever part it is in.
	if !b.Sections[0].StartsPart {
		t.Error("the first section must start a sheet")
	}
}

// A block declared a full-page form still gets its own sheet.
func TestFullPageFormsStillStartASheet(t *testing.T) {
	b := assembleBook1Fixture(t)
	for _, s := range b.Sections {
		if MustStartASheet(s.BlockID) && !s.StartsPart {
			t.Errorf("%s is a declared full-page form and must start a sheet", s.BlockID)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -run 'TestBook1BreaksOnPart|TestFullPageForms' -v
```

Expected: FAIL - `StartsPart` does not exist, so the package does not compile. That is the
failure; write the field next.

- [ ] **Step 3: Add the field**

In `types.go`, on `Section`, beside `Part`:

```go
	// StartsPart marks the first rendered block of each provider part, and the blocks named by
	// pagepolicy.go. It is the only thing that forces a page break in Book 1.
	//
	// Not derived in the template from a "did Part change" comparison: a part whose every block
	// was omitted must not leave its successor un-marked, and the template cannot see which
	// blocks were dropped. Assembly knows, so assembly decides.
	StartsPart bool `json:"starts_part,omitempty"`
```

- [ ] **Step 4: Write the page policy**

```go
package book

// pagepolicy.go -- which Book 1 blocks must begin a sheet regardless of the part they sit in.
//
// Layout policy, deliberately code and not a migration. The provider's parts are data and live
// in book1_content_block; "this form needs a whole sheet to be usable" is a decision about
// printing that this project made, and putting it in a table would dress it as the provider's.
//
// Each entry is a full-page form: a grid a parent fills in over weeks, where a break mid-form
// costs the reader the column headings on one of the two halves. Everything else runs on.
var mustStartASheet = map[string]string{
	"B1-013": "age-specific monitoring: one grid per monitoring parameter, and the set fills a sheet",
	"B1-019": "food acceptance tracker: two grids the parent fills daily",
	"B1-020": "weekly review dashboard: eighteen monitoring areas in one table",
	"B1-022": "reference and disclaimer: the evidence table runs the full sheet",
}

// MustStartASheet reports whether a block begins a page whatever part it belongs to.
func MustStartASheet(blockID string) bool { _, ok := mustStartASheet[blockID]; return ok }
```

- [ ] **Step 5: Set it during assembly**

In `book1.go`, after the sections slice is final (after omissions have been removed, so a
dropped first-block-of-part does not take its part's break with it):

```go
	// Mark the sheet starts. Done after omission, not during: a part whose first block was left
	// out must still start a sheet at whichever of its blocks did render, and only the final
	// slice knows which that is.
	seenPart := map[string]bool{}
	for i := range sections {
		s := &sections[i]
		if !seenPart[s.Part] {
			seenPart[s.Part] = true
			s.StartsPart = true
			continue
		}
		s.StartsPart = MustStartASheet(s.BlockID)
	}
```

- [ ] **Step 6: Break on the flag in the template**

`book1/body.html`, replacing the unconditional `<section class="page-break">`:

```html
  {{/* One sheet per provider part, not one per block. See pagepolicy.go and the design note in
       docs/superpowers/specs/2026-08-20-book-layout-and-imagery-design.md section 2.1. */}}
  <section{{ if .StartsPart }} class="page-break"{{ end }}>
```

- [ ] **Step 7: Keep headings with what follows them**

A block that now runs on can put its heading at the foot of a page. `.page-head` already carries
`break-after: avoid`; add the same to the section's own opening unit in `tokens.css`:

```css
/* A section that runs on inside a part must not leave its heading stranded at the foot of a
   sheet. The head plus the purpose line plus the covers list is short enough to keep whole; the
   tables and forms below it are not, and are free to split. */
.section-open { break-inside: avoid; break-after: avoid; }
```

Wrap the head, purpose and covers of `book1/body.html` in `<div class="section-open">`.

- [ ] **Step 8: Run the tests and the fill guard**

```
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -v
```

Expected: the two new tests PASS. `TestNoMorePagesEndEarlyThanTheBudget` logs a materially lower
count - Book 1 should fall from 47 pages to roughly 36 and from 18 underfilled to under 8.

- [ ] **Step 9: Lower the budget to what was measured**

Set `maxUnderfilledPages` in `pagefit_test.go` to the count the previous step logged, and re-run
to confirm it still passes. The budget records the state the code is actually in; it is never set
above what was measured.

- [ ] **Step 10: Look at the printed sheets**

Print Book 1 and rasterise all of it. Read every page. Check specifically:
- No heading sits alone at the foot of a sheet.
- Parts A-F pair onto shared sheets and the pairing reads as intentional.
- Part J's ten daily-life domains still each fill their sheet.
- The contents page still lists every section that rendered.

- [ ] **Step 11: Commit**

```bash
git add internal/book/types.go internal/book/book1.go internal/book/pagepolicy.go \
        internal/book/templates/book1/body.html internal/book/templates/tokens.css \
        internal/book/assemble_test.go internal/book/pagefit_test.go
git commit -m "Break Book 1 where the provider grouped its blocks"
```

---

## Task 4: Writing rows stop stranding

Page 42 of Book 1 is three writing lines, a repeated column header, and 92% white. The row group
is the fix: Chromium honours `break-inside: avoid` on `tbody`, so rows emitted in fours move in
fours and a tail of one to three cannot occur.

**Files:**
- Modify: `internal/book/tracker.go`
- Modify: `internal/book/templates/book1/tracker.html`
- Modify: `internal/book/templates/tokens.css`
- Test: `internal/book/tracker_test.go`, `internal/book/pagefit_test.go`

**Interfaces:**
- Consumes: `TrackerSpec.Rows` (existing count)
- Produces: `TrackerSpec.RowGroups() [][]int` - the rows chunked for the template

- [ ] **Step 1: Write the failing test**

```go
func TestWritingRowsAreEmittedInGroupsThatCannotStrand(t *testing.T) {
	for _, tc := range []struct {
		rows  int
		want  []int   // the size of each group
	}{
		{rows: 4, want: []int{4}},
		{rows: 8, want: []int{4, 4}},
		{rows: 10, want: []int{4, 3, 3}},  // never a tail smaller than three
		{rows: 5, want: []int{3, 2}},      // a five-row form splits rather than stranding one
		{rows: 3, want: []int{3}},
		{rows: 1, want: []int{1}},
	} {
		got := rowGroupSizes(tc.rows)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%d rows: groups %v, want %v", tc.rows, got, tc.want)
		}
	}
}

// The regression this exists for: Book 1 page 42 was three writing lines and a column header on
// an otherwise blank sheet, the tail of the food-diversity grid.
func TestNoPrintedPageIsAlmostEntirelyBlank(t *testing.T) {
	set := printableSet(t)
	for _, b := range []struct{ name string; pdf []byte }{
		{"book1", set.Book1PDF}, {"book2", set.Book2PDF},
	} {
		for i, f := range pageFills(t, b.pdf) {
			if f > 0 && f < 0.15 {
				t.Errorf("%s p%d ends at %.0f%%: a page holding almost nothing", b.name, i+1, f*100)
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify both fail**

Expected: `rowGroupSizes` undefined (compile failure), and once it exists,
`TestNoPrintedPageIsAlmostEntirelyBlank` FAILs on `book1 p42 ends at 2%`.

- [ ] **Step 3: Implement the grouping**

```go
// rowGroupSizes splits n writing rows into groups that each move whole across a page break.
//
// Chromium honours break-inside: avoid on a tbody, and ignores CSS orphans/widows on table rows
// entirely -- which is why the food-diversity grid put three rows and a repeated column header
// alone on Book 1 page 42, with 92% of the sheet blank.
//
// Four is the group size: a 6.5mm writing row times four is 26mm, small enough that a group
// almost always fits in whatever is left of a sheet, and large enough that a stranded group is
// still a usable form. A remainder of one or two is folded back by rebalancing the last two
// groups, so no group is ever smaller than three unless the whole form is.
func rowGroupSizes(n int) []int {
	const group = 4
	if n <= group {
		return []int{n}
	}
	var out []int
	for left := n; left > 0; left -= group {
		out = append(out, min(group, left))
	}
	// Rebalance a stranded tail of one or two into the group before it.
	if last := len(out) - 1; last > 0 && out[last] < 3 {
		total := out[last-1] + out[last]
		out[last-1] = (total + 1) / 2
		out[last] = total / 2
	}
	return out
}
```

- [ ] **Step 4: Emit the groups**

`book1/tracker.html`, replacing the single `<tbody>`:

```html
    {{ range .RowGroups }}
    <tbody class="rowgroup">
      {{ range . }}
      <tr>{{ range $.Columns }}<td><span class="write-line"></span></td>{{ end }}</tr>
      {{ end }}
    </tbody>
    {{ end }}
```

and in `tokens.css`:

```css
/* A group of writing rows moves whole. See rowGroupSizes in tracker.go for why four, and for
   the page-42 regression that made this necessary. */
tbody.rowgroup { break-inside: avoid; }
```

- [ ] **Step 5: Run the tests**

Both PASS. `TestNoMorePagesEndEarlyThanTheBudget` improves again; lower the budget to the
measured count.

- [ ] **Step 6: Commit**

```bash
git add internal/book/tracker.go internal/book/templates/book1/tracker.html \
        internal/book/templates/tokens.css internal/book/tracker_test.go internal/book/pagefit_test.go
git commit -m "Move writing rows in groups so a form cannot strand three lines"
```

---

## Task 5: A continued table says what it is

Pages 17, 42, 44 and 47 of Book 1 open with a bare column header. The reader has the columns and
no idea which table they belong to.

**Files:**
- Modify: `internal/book/templates/book1/tracker.html`
- Modify: `internal/book/templates/tokens.css`
- Test: `internal/book/render_test.go`, `internal/book/pagefit_test.go`

- [ ] **Step 1: Write the failing test**

```go
// A table that continues onto a new sheet carries its own name with it.
//
// thead already repeats -- that is table-header-repeat from the contract, and why the column
// header appears at all. The title sat outside the table in a <p class="tracker-title">, so it
// did not. Four pages of Book 1 opened with columns belonging to a table named on the sheet
// before.
func TestAContinuedTrackerNamesItself(t *testing.T) {
	html := renderBook1Fixture(t)
	// The title is inside the thead, as a spanning row above the column names.
	if !strings.Contains(html, `<tr class="tracker-caption"><th colspan=`) {
		t.Error("the tracker title must sit in thead so it repeats with the column header")
	}
	if strings.Contains(html, `<p class="tracker-title">`) {
		t.Error("the tracker title must no longer sit outside the table")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

- [ ] **Step 3: Move the title into `thead`**

```html
  <table class="tracker-table">
    <thead>
      {{/* The title lives in thead so Chromium repeats it with the column header on every sheet
           the table continues onto. Outside the table it printed once, and four of Book 1's
           pages opened with columns belonging to a table named on the previous sheet. */}}
      <tr class="tracker-caption"><th colspan="{{ len .Columns }}">{{ .Title }}</th></tr>
      <tr>{{ range .Columns }}<th>{{ . }}</th>{{ end }}</tr>
    </thead>
```

- [ ] **Step 4: Style it as a title, not as a column header**

```css
/* The caption row is a title that happens to live in thead, so it must not inherit the column
   header's treatment: sentence case, brand colour, no letterspacing, and a rule under it. */
.tracker-caption th {
  font-family: var(--font-sans);
  font-size: var(--caption-size);
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--brand-deep);
  text-align: left;
  padding: 3mm 0 1.5mm;
  border-bottom: none;
}
```

- [ ] **Step 5: Run the tests, print, and read pages 17, 42, 44 and 47's successors**

- [ ] **Step 6: Commit**

```bash
git commit -am "Repeat a tracker's name with its column header"
```

---

## Task 6: Column widths come from the content

`overflow-wrap: break-word` is doing what it was asked to and breaking words mid-syllable,
because nothing sizes a column to what it must hold.

**Files:**
- Create: `internal/book/colwidth.go`, `internal/book/colwidth_test.go`
- Modify: `internal/book/types.go` (add `Widths []int` to the table-bearing types)
- Modify: `internal/book/tracker.go`, `internal/book/book1.go`
- Modify: `internal/book/templates/book1/tracker.html` and the other table templates
- Test: `internal/book/pdf_test.go`

**Interfaces:**
- Produces: `func ColumnWidths(headers []string, cells [][]string, total int) []int`

- [ ] **Step 1: Write the failing test**

```go
func TestColumnWidthsFitTheLongestUnbreakableToken(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		cells   [][]string
		want    []int  // percentages, summing to 100
	}{
		{
			name:    "a long header gets the room it needs",
			headers: []string{"Date", "Head circumference", "Note"},
			cells:   [][]string{{"2026-08-01", "", ""}},
			want:    []int{22, 48, 30},
		},
		{
			name:    "an unbreakable identifier is never narrower than itself",
			headers: []string{"Source", "Topic"},
			cells:   [][]string{{"IAP-STG-CONSTIPATION", "Constipation"}},
			want:    []int{55, 45},
		},
		{
			name:    "equal content divides equally",
			headers: []string{"A", "B", "C", "D"},
			cells:   [][]string{{"x", "x", "x", "x"}},
			want:    []int{25, 25, 25, 25},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ColumnWidths(tc.headers, tc.cells, 100)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			sum := 0
			for _, w := range got {
				sum += w
			}
			if sum != 100 {
				t.Errorf("widths sum to %d, want 100", sum)
			}
		})
	}
}

// Nothing in either printed book breaks inside a word.
//
// The tokens below were all read off printed sheets: "HEAD CIRCUMFER / ENCE" on Book 1 page 6,
// "Language/cognitiv / e" on page 14, "Social/communicati / on" on page 18,
// "IAP-STG-CONSTIPATIO / N" on page 46. A broken identifier is a wrong identifier.
func TestNoWordBreaksInsideItself(t *testing.T) {
	set := printableSet(t)
	for _, frag := range []string{
		"CIRCUMFER", "cognitiv ", "communicati ", "CONSTIPATIO ", "interpretatio ",
	} {
		for _, b := range []struct{ name string; pdf []byte }{
			{"book1", set.Book1PDF}, {"book2", set.Book2PDF},
		} {
			if strings.Contains(pdfText(t, b.pdf), frag) {
				t.Errorf("%s breaks a word: %q appears at a line end", b.name, frag)
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify both fail**

- [ ] **Step 3: Implement**

```go
package book

// colwidth.go -- table column widths computed from the content, so fixed table layout does not
// have to break words to fit.
//
// table-layout: fixed is not negotiable: without it one over-wide table silently scales the
// whole printed document, which is how Book 1 shipped with 10.5pt body text on paper at 8.3pt.
// What fixed layout costs is that every column is the same width unless something says
// otherwise, and a uniform column narrower than the longest word in it breaks that word.
//
// So: measure. Each column's demand is the longest unbreakable token it must hold -- a word,
// or an identifier like IAP-STG-CONSTIPATION which no hyphenation dictionary may split -- plus
// a share of the column's average content length, which keeps a column of long sentences from
// being sized by one short header. Widths are then normalised to the total.
//
// Character counts, not typographic measurement. Both faces are set at the table size and no
// column is sized so tightly that the difference between an "i" and an "m" decides whether a
// word fits; the guard is TestNoWordBreaksInsideItself, which reads printed sheets.

const (
	// contentWeight balances the longest token against the average cell length. At 0 a column
	// is sized purely by its widest word, which gives a narrow column of long prose. At 1 it is
	// sized purely by its average, which breaks the widest word. 0.5 was set against the two
	// tables that were breaking and checked on the printed sheets.
	contentWeight = 0.5
	// minColumnPct floors every column so a column of empty writing lines is still writable.
	minColumnPct = 8
)

// ColumnWidths returns one percentage per column, summing to total.
func ColumnWidths(headers []string, cells [][]string, total int) []int {
	// 1. demand per column: longest unbreakable token, blended with mean cell length
	// 2. normalise to total
	// 3. apply minColumnPct, taking the shortfall from the widest columns
	// 4. fix rounding drift by adding the remainder to the widest column
	panic("implement")
}

// longestToken returns the length of the longest run with no space in it. Slashes and hyphens
// are not break opportunities here: "Language/cognitive" and "IAP-STG-CONSTIPATION" are single
// tokens because breaking either produces a string that reads as a different value.
func longestToken(s string) int { panic("implement") }
```

- [ ] **Step 4: Emit a colgroup**

Every table template that carries computed widths:

```html
    <colgroup>{{ range .Widths }}<col style="width: {{ . }}%">{{ end }}</colgroup>
```

- [ ] **Step 5: Keep `break-word` as the last resort**

Do not remove `overflow-wrap: break-word`. With computed widths it should never fire, and if a
future column does overflow, a broken word is still better than a document silently scaled to
79%. Add the reasoning as a comment beside the rule.

- [ ] **Step 6: Run the tests, print, read the pages that were breaking**

- [ ] **Step 7: Commit**

```bash
git commit -am "Size table columns from what they hold, so words stop breaking"
```

---

## Task 7: A callout keeps with what raised it

Pages 28 and 30 of Book 1 open with a `Warning: when to seek advice` box belonging to the
previous domain.

**Files:**
- Modify: `internal/book/templates/tokens.css`
- Test: `internal/book/pagefit_test.go`

- [ ] **Step 1: Write the failing test** - the page-open guard from Task 1 Step 4, with its
  budget set to the current count.

- [ ] **Step 2: Bind the callout to its preceding content**

CSS `break-before: avoid` is unreliable in Chromium for this. What works is making the callout
and the block above it one unbreakable unit, which is only safe because both are short:

```css
/* A domain's warning and the row that raised it move together.
   
   `break-before: avoid` on the callout is the obvious construction and Chromium does not honour
   it against a fragmentainer boundary. Wrapping the pair in an unbreakable unit does work, and
   is only sound because the pair is short: a warning is at most three lines and the referral
   line one. The domain around them stays breakable -- a domain runs taller than a page, and
   "keep together" is a promise the fragmenter cannot keep for it. */
.domain-warning-pair { break-inside: avoid; }
```

- [ ] **Step 3: Wrap the pair in `book1/daily.html` and `book1/illness.html`**

- [ ] **Step 4: Run, print, read pages 27-31**

- [ ] **Step 5: Commit**

```bash
git commit -am "Keep a warning on the page with what raised it"
```

---

## Task 8: The dish-format mark seed

Twenty-eight dish formats cover all 940 in-scope recipes. Eleven drawn archetypes cover the
twenty-eight. The map is hand-written, like `culture_region_map` and `book1_block_source`,
because the join is by meaning.

**Files:**
- Create: `internal/db/migrations/0019_recipe_format_mark.{up,down}.sql`
- Create: `internal/db/book_imagery_test.go`
- Modify: `CLAUDE.md` (migration list)

**Interfaces:**
- Produces: table `recipe_format_mark(format_pattern text pk, mark_id text, note text)`,
  view `recipe_mark` resolving `recipe_id -> mark_id, format_label`

- [ ] **Step 1: Write the failing test**

```go
// Every in-scope recipe resolves to exactly one mark, and every seeded format matches at least
// one recipe. Both directions, because a seed that matches nothing is dead weight and a recipe
// that matches nothing prints a page with a hole where the illustration goes.
func TestEveryRecipeResolvesOneFormatMark(t *testing.T) {
	db := testDB(t)
	var unmatched int
	must(db.QueryRow(ctx, `
		SELECT count(*) FROM recipe_master r
		WHERE NOT EXISTS (SELECT 1 FROM recipe_format_mark m
		                  WHERE r.recipe_name LIKE '%' || m.format_pattern || '%')`).Scan(&unmatched))
	if unmatched != 0 {
		t.Errorf("%d recipes match no dish format", unmatched)
	}
	var ambiguous int
	must(db.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT r.recipe_id FROM recipe_master r
			JOIN recipe_format_mark m ON r.recipe_name LIKE '%' || m.format_pattern || '%'
			GROUP BY 1 HAVING count(*) > 1) x`).Scan(&ambiguous))
	if ambiguous != 0 {
		t.Errorf("%d recipes match more than one dish format", ambiguous)
	}
	var dead int
	must(db.QueryRow(ctx, `
		SELECT count(*) FROM recipe_format_mark m
		WHERE NOT EXISTS (SELECT 1 FROM recipe_master r
		                  WHERE r.recipe_name LIKE '%' || m.format_pattern || '%')`).Scan(&dead))
	if dead != 0 {
		t.Errorf("%d seeded formats match no recipe", dead)
	}
}
```

- [ ] **Step 2: Run to verify it fails** - the table does not exist.

- [ ] **Step 3: Write the migration**

```sql
-- 0019_recipe_format_mark.up.sql
--
-- Which drawn mark illustrates each dish format.
--
-- The provider encodes the dish format in the recipe name -- "{Region} {Ing1} & {Ing2} {Format}"
-- -- and the vocabulary is closed: these twenty-eight formats match all 940 in-scope recipes,
-- verified in both directions by TestEveryRecipeResolvesOneFormatMark.
--
-- The mark is line art of the format, not of the dish, and the page captions it with the format
-- name so the claim is explicit. It asserts nothing the provider did not record. There is no
-- photograph here and none is coming from a dataset (GAP-025); the reasoning, including why the
-- external corpus's image-url column is not usable, is in
-- docs/superpowers/specs/2026-08-20-book-layout-and-imagery-design.md section 3.2.
--
-- Hand-written, like culture_region_map and book1_block_source, because the join is by meaning:
-- a fuzzy match would put a flatbread on a porridge.

CREATE TABLE recipe_format_mark (
  format_pattern text PRIMARY KEY,
  mark_id        text NOT NULL,
  note           text NOT NULL
);

INSERT INTO recipe_format_mark (format_pattern, mark_id, note) VALUES
  ('Soft rice bowl',              'bowl-grain',   'served in a bowl, grain-led'),
  ('Regional rice bowl',          'bowl-grain',   'served in a bowl, grain-led'),
  ('High-protein regional bowl',  'bowl-grain',   'served in a bowl, grain-led'),
  ('Adolescent power bowl',       'bowl-grain',   'served in a bowl, grain-led'),
  ('Protein snack bowl',          'bowl-grain',   'served in a bowl, grain-led'),
  ('Balanced pulao bowl',         'bowl-grain',   'one-pot seasoned rice, served in a bowl'),
  ('Soft protein rice',           'bowl-grain',   'grain-led, served in a bowl'),
  ('Millet meal',                 'bowl-grain',   'grain-led, served in a bowl'),
  ('High-fibre meal',             'bowl-grain',   'grain-led, served in a bowl'),
  ('Quick school/college meal',   'bowl-grain',   'grain-led, served in a bowl'),
  ('Thick porridge bowl',         'bowl-porridge','spoonable, served in a bowl with a spoon'),
  ('Single-grain porridge',       'bowl-porridge','spoonable, served in a bowl with a spoon'),
  ('Savory porridge',             'bowl-porridge','spoonable, served in a bowl with a spoon'),
  ('Vegetable mash',              'dish-mash',    'mashed, served in a shallow dish'),
  ('Fruit-cereal mash',           'dish-mash',    'mashed, served in a shallow dish'),
  ('Dal-rice mash',               'dish-mash',    'mashed, served in a shallow dish'),
  ('Family-style khichdi',        'pot-khichdi',  'one-pot dal and rice, served from the pot'),
  ('Soft khichdi',                'pot-khichdi',  'one-pot dal and rice, served from the pot'),
  ('Mini cutlet/patty',           'patty',        'shaped and pan-cooked'),
  ('Mini pancake/cheela',         'pancake',      'batter cooked flat on a griddle'),
  ('Savory pancake',              'pancake',      'batter cooked flat on a griddle'),
  ('Stuffed flatbread',           'flatbread',    'filled griddle bread'),
  ('Lunchbox wrap',               'wrap',         'filled and rolled'),
  ('School tiffin roll',          'wrap',         'filled and rolled'),
  ('Soft finger bites',           'finger-bites', 'small pieces eaten by hand'),
  ('Breakfast upma/poha style',   'plate-upma',   'savoury semolina or flattened rice, on a plate'),
  ('Sports snack',                'snack',        'portable, eaten away from a table'),
  ('Sports recovery meal',        'snack',        'portable, eaten away from a table');

-- One row per recipe, resolving name to mark. A view rather than a column on recipe_master:
-- the provider's table is never modified, and this is our reading of their name.
CREATE VIEW recipe_mark AS
SELECT r.recipe_id,
       m.mark_id,
       m.format_pattern AS format_label,
       m.note           AS format_note,
       'derived'        AS value_kind
FROM recipe_master r
JOIN recipe_format_mark m ON r.recipe_name LIKE '%' || m.format_pattern || '%';
```

`0019_recipe_format_mark.down.sql` drops the view then the table.

- [ ] **Step 4: Re-import and run the test**

```
scripts/dev_db.fish down && scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import && go run ./cmd/enrich
TEST_DATABASE_URL=$DATABASE_URL go test ./internal/db/ -run TestEveryRecipeResolvesOneFormatMark -v
```

Expected: PASS with 0 unmatched, 0 ambiguous, 0 dead.

If any format matches more than one recipe pattern, the cause is one pattern being a substring of
another - fix the seed, not the query.

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/0019_recipe_format_mark.up.sql \
        internal/db/migrations/0019_recipe_format_mark.down.sql internal/db/book_imagery_test.go
git commit -m "Map every dish format to the mark that illustrates it"
```

---

## Task 9: The marks, drawn, and on the page

**Files:**
- Create: `internal/book/marks/{bowl-grain,bowl-porridge,dish-mash,pot-khichdi,patty,pancake,flatbread,wrap,finger-bites,plate-upma,snack}.svg`
- Create: `internal/book/marks.go`, `internal/book/marks_test.go`
- Modify: `internal/book/types.go`, `internal/book/book2.go`,
  `internal/book/templates/book2/recipe.html`, `internal/book/templates/tokens.css`

**Interfaces:**
- Consumes: view `recipe_mark` from Task 8
- Produces: `RecipeCard.Mark *DishMark`, `DishMark{ID, FormatLabel string, SVG template.HTML}`

- [ ] **Step 1: Write the failing test**

```go
// Every seeded mark id has artwork, and every piece of artwork is seeded. A mark id with no file
// prints a hole; a file no seed names is dead weight nobody will notice going stale.
func TestEveryMarkIDHasArtworkAndViceVersa(t *testing.T) { /* seed ids vs embed.FS names */ }

// A mark is inert. It goes through the same browser that prints the book, so it must not be able
// to fetch, script or reference anything.
//
// The print tab has script execution disabled and all network blocked, so this is defence in
// depth rather than the only control -- but the marks are the first assets in this project
// authored as markup rather than as data, and an asset that would be dangerous if either control
// were relaxed should not be checked in.
func TestMarksCannotFetchOrScript(t *testing.T) {
	for name, svg := range allMarks(t) {
		for _, forbidden := range []string{
			"<script", "<image", "<foreignObject", "xlink:href", "href=", "url(", "javascript:",
			"<use", "@import", "onload", "onerror",
		} {
			if strings.Contains(strings.ToLower(svg), forbidden) {
				t.Errorf("%s contains %q", name, forbidden)
			}
		}
	}
}

// A mark is a drawing and must not read as a photograph: stroked line art, no raster payload.
func TestMarksAreLineArt(t *testing.T) {
	for name, svg := range allMarks(t) {
		if strings.Contains(svg, "base64") {
			t.Errorf("%s embeds raster data", name)
		}
		if !strings.Contains(svg, "stroke=") {
			t.Errorf("%s has no stroke: a mark is line art", name)
		}
	}
}

// The illustration reaches the page, captioned with the format it depicts.
func TestARecipePagePrintsItsMarkAndNamesTheFormat(t *testing.T) {
	html := renderBook2Fixture(t)
	if !strings.Contains(html, `class="dish-mark"`) {
		t.Error("a recipe page must carry its dish mark")
	}
	if !strings.Contains(html, `class="dish-mark-caption"`) {
		t.Error("the mark must be captioned with the format it depicts, so its claim is checkable")
	}
}
```

- [ ] **Step 2: Run to verify all four fail**

- [ ] **Step 3: Draw the eleven marks**

One SVG per archetype, `viewBox="0 0 64 64"`, `fill="none"`, `stroke="currentColor"`,
`stroke-width="1.6"`, `stroke-linecap="round"`, `stroke-linejoin="round"`. No text, no raster, no
external reference. Each is a single-colour outline that reads at 22mm and at 12mm.

Subjects, one line each so the drawing is unambiguous:
- `bowl-grain` - a deep bowl in three-quarter view, grains suggested by short strokes at the rim
- `bowl-porridge` - the same bowl with a spoon resting across it and a soft surface line
- `dish-mash` - a shallow wide dish, one fork mark across the surface
- `pot-khichdi` - a lidded pot with two handles, steam as two short curves
- `patty` - three rounds on a plate, one in front overlapping
- `pancake` - a stack of two flat discs on a plate, seen from a low angle
- `flatbread` - a round bread folded once, a filling line at the fold
- `wrap` - a rolled cylinder cut on the diagonal, the spiral visible at the cut
- `finger-bites` - four small cubes arranged loosely, no plate
- `plate-upma` - a flat plate with a mounded centre and a lemon wedge at the rim
- `snack` - a small lidded tiffin box with a handle

- [ ] **Step 4: Write the loader**

```go
package book

import "embed"

//go:embed marks/*.svg
var markFS embed.FS

// DishMark is the drawn illustration for a recipe's dish format.
//
// It depicts the format -- a provider-recorded value read out of the recipe name -- and never
// the dish. The page captions it with the format name so a reader can check the claim against
// the title above it. No photograph of any recipe in this corpus exists (GAP-025), and the
// external corpus's image-url column is not a source; see the design note.
type DishMark struct {
	ID          string        `json:"id"`
	FormatLabel string        `json:"format_label"`
	SVG         template.HTML `json:"-"`
}
```

`template.HTML` is load-bearing and needs the same treatment `ChildPhoto.DataURI` got: the escape
hatch is only sound because the marks are checked-in files validated by
`TestMarksCannotFetchOrScript`, never anything from a request or the database. Say so in the doc
comment.

- [ ] **Step 5: Load the mark per card in `book2.go`, and render it**

The illustration unit sits top-right, opposite the title:

```html
  {{ with .Mark }}
  <figure class="dish-mark">
    {{ .SVG }}
    <figcaption class="dish-mark-caption">{{ .FormatLabel }}</figcaption>
  </figure>
  {{ end }}
```

```css
/* The illustration unit: the mark that shows the shape of the dish, opposite the title.
   
   A drawing rather than a photograph, and captioned with the format it depicts, because no
   photograph of any recipe in this corpus exists and the nearest available images are of other
   people's dishes under an unstated licence. See the design note, section 3.2.
   
   Floated rather than gridded: the recipe head is a variable number of lines and a grid row
   sized to the tallest cell left a gap under a one-line title. */
.dish-mark { float: right; width: 26mm; margin: 0 0 4mm 6mm; color: var(--brand); }
.dish-mark svg { width: 26mm; height: 26mm; display: block; }
.dish-mark-caption {
  font-family: var(--font-sans);
  font-size: var(--small-size);
  color: var(--ink-soft);
  text-align: center;
  margin-top: 1.5mm;
}
```

- [ ] **Step 6: Run the tests, print Book 2, and look at every recipe page**

Check the mark reads at 26mm, the caption matches the format in the title, and the float does not
collide with the metastrip below.

- [ ] **Step 7: Commit**

```bash
git add internal/book/marks internal/book/marks.go internal/book/marks_test.go \
        internal/book/types.go internal/book/book2.go \
        internal/book/templates/book2/recipe.html internal/book/templates/tokens.css
git commit -m "Draw the dish format on every recipe page"
```

---

## Task 10: The composition band

What the dish is made of, by mass, from the provider's own quantities.

**Files:**
- Create: `internal/db/migrations/0020_recipe_composition_share.{up,down}.sql`
- Modify: `internal/book/types.go`, `internal/book/book2.go`,
  `internal/book/templates/book2/recipe.html`, `internal/book/templates/tokens.css`
- Test: `internal/db/book_imagery_test.go`, `internal/book/render_test.go`

**Interfaces:**
- Consumes: `recipe_ingredient_mapping.quantity_g`, `ingredient_master.food_group`
- Produces: view `recipe_composition_share(recipe_id, macro_group, share, basis_g, value_kind)`

- [ ] **Step 1: Write the failing test**

```go
// Shares sum to 1 for every recipe, and an ingredient whose food group is not mapped is carried
// as its own share rather than renormalised away. A band that silently drops 12% of the dish is
// a band that says the dish is something it is not.
func TestCompositionSharesSumToOneAndCarryTheUnmapped(t *testing.T) { /* ... */ }

// The band is labelled derived and states its formula, like every derived value in this project.
func TestTheCompositionBandStatesItsBasis(t *testing.T) { /* ... */ }
```

- [ ] **Step 2: Run to verify it fails**

- [ ] **Step 3: Write the migration**

```sql
-- 0020_recipe_composition_share.up.sql
--
-- What a recipe is made of, by ingredient mass, grouped.
--
-- Formula, derived and recorded here as the hard rule requires:
--
--   share(group) = sum(quantity_g where macro_group = group) / sum(quantity_g)
--
-- over recipe_ingredient_mapping, joined to ingredient_master for the food group. The source
-- rows are the mapping rows themselves; basis_g is their total, so a reader can reproduce it.
--
-- This is a mass share, not a nutrition claim. It says nothing about energy, protein or
-- adequacy, and it is deliberately not drawn against a reference intake -- this project carries
-- no reference intakes and would have to invent them.

-- 21 food groups appear on recipes; 7 macro groups is what a band can distinguish at 26mm.
-- Hand-written, and 'Unmapped' is a real bucket rather than a silent drop.
CREATE TABLE food_group_macro (
  food_group  text PRIMARY KEY,
  macro_group text NOT NULL,
  note        text NOT NULL
);

INSERT INTO food_group_macro (food_group, macro_group, note) VALUES
  ('Cereal','Grain',''), ('Cereal product','Grain',''), ('Millet','Grain',''),
  ('Millet product','Grain',''), ('Pseudocereal','Grain',''), ('Cereal-like seed','Grain',''),
  ('Flour','Grain',''), ('Starch','Grain',''),
  ('Pulse','Pulse & legume',''), ('Pulse product','Pulse & legume',''),
  ('Legume','Pulse & legume',''), ('Legume/Vegetable','Pulse & legume',''),
  ('Nut/Legume','Pulse & legume',''), ('Soy product','Pulse & legume',''),
  ('Vegetable','Vegetable',''), ('Leafy vegetable','Vegetable',''),
  ('Root vegetable','Vegetable',''), ('Tuber','Vegetable',
   'grouped with vegetables for mass share only -- the nutrition ranker still excludes tuber from
    its fruit/veg measure, and the two measures answer different questions'),
  ('Fruit','Fruit',''), ('Dried fruit','Fruit',''), ('Fruit/Fat source','Fruit',''),
  ('Dairy','Dairy',''),
  ('Fish','Animal protein',''), ('Animal protein','Animal protein',''),
  ('Fat','Fat, seed & spice',''), ('Seed','Fat, seed & spice',''), ('Spice','Fat, seed & spice','');

CREATE VIEW recipe_composition_share AS
WITH m AS (
  SELECT rim.recipe_id,
         COALESCE(fgm.macro_group, 'Unmapped') AS macro_group,
         sum(rim.quantity_g) AS grams
  FROM recipe_ingredient_mapping rim
  JOIN ingredient_master im USING (ingredient_id)
  LEFT JOIN food_group_macro fgm ON fgm.food_group = im.food_group
  GROUP BY 1, 2
), t AS (SELECT recipe_id, sum(grams) AS basis_g FROM m GROUP BY 1)
SELECT m.recipe_id, m.macro_group,
       round(m.grams / t.basis_g, 4) AS share,
       m.grams, t.basis_g,
       'derived' AS value_kind,
       'share = sum(quantity_g) per macro group / sum(quantity_g); source rows are recipe_ingredient_mapping'
         AS formula
FROM m JOIN t USING (recipe_id);
```

- [ ] **Step 4: Render the band under the mark**

A single horizontal bar, segments in the palette, each labelled in words as well as by colour
(`avoid_color_only_meaning` from the contract), with the basis stated:

```html
  {{ with .Composition }}
  <div class="composition">
    <div class="composition-bar">
      {{ range .Shares }}<span class="seg seg-{{ .Slug }}" style="width: {{ .Pct }}%"></span>{{ end }}
    </div>
    <ul class="composition-key">
      {{ range .Shares }}<li><span class="swatch seg-{{ .Slug }}"></span>{{ .Group }}
        <span class="mono">{{ .Pct }}%</span></li>{{ end }}
    </ul>
    <p class="composition-basis">By ingredient mass, {{ .BasisG }} total. Derived from the
      recipe's own quantities; not a nutrition claim.</p>
  </div>
  {{ end }}
```

- [ ] **Step 5: Run, print, read**

Check the band's segments and its key agree, that a recipe with an unmapped ingredient shows an
`Unmapped` segment, and that the unit does not push the tracker onto a second sheet - the recipe
pages currently end at 86% and have about 35mm of room.

- [ ] **Step 6: Commit**

```bash
git commit -am "Show what a recipe is made of, by mass, from its own quantities"
```

---

## Task 11: A real photograph gets a home, empty

The illustration is the standing state, not a placeholder. This is the slot a commissioned
photograph lands in, and the mechanism that closes `GAP-025` without a code change.

**Files:**
- Create: `internal/db/migrations/0021_recipe_photo.{up,down}.sql`
- Modify: `internal/book/book2.go`, `internal/book/templates/book2/recipe.html`
- Test: `internal/db/book_imagery_test.go`

- [ ] **Step 1: Write the failing test**

```go
// The photograph table exists and is empty, and GAP-025 is measured from it rather than asserted
// in prose -- so the gap closes itself the day photographs are commissioned.
func TestRecipePhotographsAreCountedNotAsserted(t *testing.T) {
	var photos, gapCount int
	must(db.QueryRow(ctx, `SELECT count(*) FROM recipe_photo`).Scan(&photos))
	must(db.QueryRow(ctx, `SELECT missing_count FROM gap_register WHERE gap_id = 'GAP-025'`).Scan(&gapCount))
	var recipes int
	must(db.QueryRow(ctx, `SELECT count(*) FROM recipe_master`).Scan(&recipes))
	if gapCount != recipes-photos {
		t.Errorf("GAP-025 says %d recipes lack a photograph; %d of %d do", gapCount, recipes-photos, recipes)
	}
}

// When a photograph exists it prints and the mark steps aside; when it does not, the mark prints
// and nothing hints at a missing picture.
func TestAPhotographReplacesTheMarkAndItsAbsenceShowsNothing(t *testing.T) { /* ... */ }
```

- [ ] **Step 2: Write the migration**

```sql
-- 0021_recipe_photo.up.sql
--
-- Commissioned photographs of provider recipes. Zero rows today and that is the point: the slot
-- exists so a photograph can arrive without a schema change, and GAP-025 is re-measured from
-- count(*) rather than restated in prose.
--
-- Every column that makes a photograph usable is required, because a picture with no licence and
-- no credit is a picture that cannot be printed in a document handed to a family. Nothing in
-- this project may write a row here from a scrape or a join -- see the design note, section 3.2.

CREATE TABLE recipe_photo (
  recipe_id   text PRIMARY KEY REFERENCES recipe_master(recipe_id) ON DELETE CASCADE,
  media_type  text NOT NULL CHECK (media_type IN ('image/png','image/jpeg','image/webp')),
  bytes       bytea NOT NULL,
  credit      text NOT NULL CHECK (credit <> ''),
  licence     text NOT NULL CHECK (licence <> ''),
  source_url  text,
  consent_ref text,
  added_by    text NOT NULL CHECK (added_by <> ''),
  added_at    timestamptz NOT NULL DEFAULT now()
);

-- GAP-025 stops being a sentence and becomes a count.
UPDATE gap_register
SET measurement_sql = 'SELECT count(*) FROM recipe_master r
                       WHERE NOT EXISTS (SELECT 1 FROM recipe_photo p WHERE p.recipe_id = r.recipe_id)'
WHERE gap_id = 'GAP-025';
```

Check the actual `gap_register` column names before writing this - the importer's gap refresh in
`internal/importer/gaps.go` owns the re-measurement and the new gap must join it.

- [ ] **Step 3: Prefer the photograph in `book2.go`, and render it**

- [ ] **Step 4: Run, re-import, verify GAP-025 still counts 940**

- [ ] **Step 5: Commit**

```bash
git commit -am "Give a commissioned recipe photograph somewhere to land"
```

---

## Task 12: The HTML preview looks like sheets

`tokens.css` has one screen rule. The console renders the document at 1700px against a 170mm
design, which is most of what the operator has been calling misalignment.

**Files:**
- Modify: `internal/book/templates/tokens.css`
- Test: `internal/book/render_test.go`

- [ ] **Step 1: Write the failing test**

```go
// The stylesheet has a screen half. Without one the console's iframe lays the document out at
// the iframe's own width -- about 1700 CSS pixels against a 170mm text block -- so every grid
// spreads, prose capped at 108mm sits in a 450mm column, and no page boundary is visible.
func TestTheStylesheetHasAScreenPresentation(t *testing.T) {
	css := stylesheet(t)
	if !strings.Contains(css, "@media screen") {
		t.Error("the preview has no screen styling and renders at iframe width")
	}
}

// The provisional banner shows on screen and is suppressed in print, where the running head
// carries it on every sheet instead. Neither state may be lost.
func TestTheProvisionalBannerIsOnScreenAndNotInPrint(t *testing.T) { /* ... */ }
```

- [ ] **Step 2: Add the screen half**

```css
/* ------------------------------------------------------------------------------------------
   Screen.
   
   Everything above this point is print geometry, and @page does nothing on screen. Without a
   screen half the console's iframe lays the document out at its own width -- about 1700 CSS
   pixels against the 170mm text block this was designed for -- and every alignment reads wrong:
   grids spread, tables stretch, and prose capped at the 108mm measure sits in a 450mm column
   with 340mm of nothing beside it.
   
   This is an approximation and is labelled as one in the console. Without JavaScript the
   browser cannot paginate, so the sheet boundaries drawn here are the forced breaks the
   document declares, not the breaks Chromium will resolve at print time. The console embeds the
   real PDF for that reason; this styling serves the fallback shown when no browser is available
   to print with.
   ------------------------------------------------------------------------------------------ */
@media screen {
  body {
    background: #8a8a8a;
    margin: 0;
    padding: 8mm 0;
  }
  /* One column at page width, with the page margins drawn, so every measure in the document
     resolves against the width it was designed for. */
  .cover, .page-break, section, .recipe, .chapter {
    box-sizing: content-box;
    width: 170mm;
    margin-left: auto;
    margin-right: auto;
    background: #fff;
    padding: var(--page-margin-top) var(--page-margin-side) var(--page-margin-bottom);
  }
  /* A visible boundary wherever the document declares a page start. */
  .page-break, .recipe, .chapter, .cover {
    margin-top: 8mm;
    box-shadow: 0 1mm 3mm rgba(0, 0, 0, 0.35);
  }
  .provisional {
    width: 170mm;
    margin: 0 auto 8mm;
    box-sizing: content-box;
    padding: 4mm var(--page-margin-side);
    background: var(--warning-soft);
  }
}
```

- [ ] **Step 3: Run the tests, then look at the preview in a browser at three widths**

- [ ] **Step 4: Commit**

```bash
git commit -am "Give the HTML preview a page to lay out on"
```

---

## Task 13: The console shows the printed PDF

**Files:**
- Modify: `web/src/lib/api.ts`, `web/src/components/book-generator.tsx`
- Test: `web/src/components/__tests__/book-generator.test.tsx`

**Interfaces:**
- Consumes: `POST /api/books/generate`, `POST /api/books/generate/{book}.pdf` (both exist)
- Produces: `BookSet.book1PdfUrl`, `book2PdfUrl` - object URLs, or `null` when printing was
  unavailable

- [ ] **Step 1: Write the failing tests**

```ts
it("embeds the printed PDF, not the HTML", async () => { /* ... */ });

it("falls back to the HTML preview when no browser is available, and says so", async () => {
  // 503 RendererUnavailableError on the PDF route while /generate succeeded: the operator gets
  // the HTML preview plus a line saying it is an approximation and why, rather than an error
  // that hides a preview that does work.
});

it("opens the tab from the blob it already has, without printing again", async () => {
  // Today openInTab re-prints. The console holds the PDF, so the tab opens what was reviewed --
  // which also removes the only way the tab and the preview could differ.
});

it("revokes the previous object URLs when a new set is generated", async () => {
  // Two PDFs per run, held for the life of the component. Without revocation an operator running
  // twenty lookups an hour leaks forty documents.
});
```

- [ ] **Step 2: Run to verify they fail**

```
cd web && npm test
```

- [ ] **Step 3: Fetch both PDFs alongside the set**

In `generate()`, after `generateBooks` resolves, fetch the two PDFs in parallel. Classify a
`RendererUnavailableError` as fallback rather than failure; let every other error surface.

- [ ] **Step 4: Embed, and reuse the blob for the tab**

Replace the `srcDoc` iframe with an `<iframe src={pdfUrl}>` when a URL is present, keeping the
`srcDoc` fallback and adding the approximation note beside it. `openInTab` becomes
`window.open(pdfUrl, "_blank")` with the existing synchronous-open guard for popup blockers.

- [ ] **Step 5: Revoke on regenerate and on unmount**

- [ ] **Step 6: Run the suite and click through the console**

```
cd web && npm test
cd web && npm run dev   # then generate a book and check both tabs and both downloads
```

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/api.ts web/src/components/book-generator.tsx \
        web/src/components/__tests__/book-generator.test.tsx
git commit -m "Show the printed PDF in the console instead of an approximation of it"
```

---

## Task 14: Record what changed

**Files:**
- Modify: `CLAUDE.md`, `docs/decisions.md`

- [ ] **Step 1: Update the book-renderer section of `CLAUDE.md`**

State: breaks are per provider part with a code-owned opt-in list; the cover has two declared
drops and why; page fill is a measured budget and what it is; the three new migrations; the
illustration layer and that it is drawn, not photographed; the console embeds the PDF.

- [ ] **Step 2: Add the imagery ruling to `docs/decisions.md`**

| Decision | Where |
|---|---|
| No external photographs on recipe pages: wrong dish, unstated licence, remote fetch, blocked network | this spec 3.2 |
| Recipe illustration is drawn from provider columns and captioned with the format it depicts | this spec 3.2 |
| Book 1 breaks on the provider's parts, not per block | this spec 2.1 |
| Printed page fill is a budgeted guard, not a taste call | this spec 3.4 |

- [ ] **Step 3: Full verification**

```
go build ./...
go vet ./...
scripts/dev_db.fish down && scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import && go run ./cmd/enrich
TEST_DATABASE_URL=$DATABASE_URL go test ./...
cd web && npm test
```

`SELECT count(*) FROM gap_register` still returns 27.

- [ ] **Step 4: Print both books one last time and read every page**

Not a skim. Every sheet. The four defects that shipped on the last book change were all invisible
to a green suite and all obvious on paper.

- [ ] **Step 5: Commit**

```bash
git commit -am "Bring the docs up to what the book renderer now does"
```

---

## Self-review

**Spec coverage.** 2.1 -> Task 3. 2.2 -> Task 2. 2.3 -> Task 4. 2.4 -> Task 5. 2.5 -> Task 6.
2.6 -> Task 7. 2.7 -> Tasks 12 and 13. 3.1 -> Tasks 12, 13. 3.2 -> Tasks 8, 9, 10, 11. 3.3 ->
Tasks 2, 3. 3.4 -> Task 1, lowered by 3, 4, 7. Section 4's out-of-scope items have no task, as
intended.

**Type consistency.** `Section.StartsPart` (Task 3) is read only by `book1/body.html` (Task 3).
`RecipeCard.Mark *DishMark` (Task 9) and `RecipeCard.Composition` (Task 10) are set in `book2.go`
and read in `book2/recipe.html`. `ColumnWidths` (Task 6) returns `[]int` and every table template
ranges it as percentages. `pageFills` and `printableSet` (Task 1) are used by Tasks 2, 4, 6, 7.

**Known soft spots.**
- Task 2's cover arithmetic is a prediction. The test measures the printed sheet, and the drops
  are adjusted until it passes - the numbers in the plan are a starting point, not a result.
- Task 6's `contentWeight` of 0.5 is a starting value. Only
  `TestNoWordBreaksInsideItself` decides whether it is right.
- Task 11's `gap_register` update must match the column names the importer's gap refresh
  actually uses. Read `internal/importer/gaps.go` before writing that migration.
- Task 9 asks for eleven drawings. If they cannot be made to read at 26mm, the fallback is a
  larger mark at 32mm on a narrower recipe head, not a photograph.
