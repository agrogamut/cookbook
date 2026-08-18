# Book Renderer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn a child profile into the two PDFs the provider designed - Book 1 (guidance and
monitoring) and Book 2 (recipes) - implementing their visual prototypes and template contract
exactly, and fix the meal-category defect that currently makes 37.7% of the recipe corpus
unreachable by Book 2.

**Architecture:** Book JSON is the contract between the engine and the renderer, and the
provider already specified both schemas. A Go assembler builds that JSON from the database;
Go `html/template` renders it to HTML against a hand-written print stylesheet; headless
Chromium prints the HTML to PDF. The same HTML is the reviewer preview, so what a reviewer
approves and what prints are the same artifact rather than two renderings that can drift.

**Tech Stack:** Go 1.25 (chi, pgx/v5, `html/template`, chromedp), Postgres 16, headless
Chromium, plain CSS with `@page` rules. No Tailwind and no React in the book renderer - see
"Renderer decision" below for why that is a change from the earlier lean.

**Spec:** The provider's own deliverables in `data/book-engine-spec/`, all read on
19 August 2026:

- `MadamGY_PDF_Template_Contract_V1.json` - global tokens, 19 template ids, palettes,
  accessibility floors. **The binding authority for anything visual.**
- `MadamGY_Book1_Visual_Prototype_V1.pdf` - 6 pages, the Book 1 design as drawn.
- `MadamGY_Book2_Visual_Prototype_V1.pdf` - 6 pages, the Book 2 design as drawn.
- `MadamGY_Book1_JSON_Schema_V1.json`, `MadamGY_Book2_JSON_Schema_V1.json` - the data
  contract each renderer consumes.
- `MadamGY_Knowledge_Book_Engine_SRS_V1 (1).docx` - the surrounding service architecture.
  Summarised in `docs/phase-3-book-engine.md`.

Where this plan and the prototypes disagree, the prototypes win. Where the prototypes and the
template contract disagree, the contract wins - it is the later, more formal document.

## Global Constraints

Copied verbatim from `CLAUDE.md` and `docs/decisions.md`. Every task's requirements
implicitly include these.

- **Never invent data.** "Every value that reaches a user must trace to a verified source."
  A book page is the most user-facing surface this project has; the rule is at its strictest
  here.
- **When data is missing, the correct output is an explicit gap** - a blank writing line, an
  omitted section, or "not available". Never a plausible-looking substitute.
- **No generated guidance prose in the unreviewed path** (18 August ruling,
  `docs/decisions.md`). The renderer emits provider-authored strings and the child's own
  measured data. It never writes a sentence.
- **`book1_content_block.ai_can_draft = 'N'`** marks five blocks no drafted text may occupy:
  `B1-009` vaccination schedule, `B1-011` milestone surveillance, `B1-012` developmental red
  flags, `B1-014` development by age, `B1-022` reference and disclaimer.
- **Nothing is approved.** All 940 recipes are `Review_Status = Draft`; all 406 ingredients
  `Needs Validation`. Every generated book must say so on its face.
- **Steps 1 (age) and 2 (allergy) stay hard filters**, and a special-care condition stops
  generation entirely. A book is downstream of the engine and inherits every stop.
- Never mention claude, anthropic, or ai anywhere: not in chat, code, comments, commit
  messages, PR titles/descriptions, issue text, file headers, or READMEs.
- No emojis, anywhere.
- The migration DDL is the single source of truth for column names and types.
- Errors wrapped with `fmt.Errorf("...: %w", err)`; sentinel errors for conditions callers
  branch on.
- Table-driven tests, package-local (`foo_test.go`).
- Verify: `go build ./...`, `go vet ./...`,
  `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...`, and `cd web && npm test`.

## Measured starting state

Queried live on 19 August 2026 against a database rebuilt from empty. Re-measure rather than
trusting these indefinitely.

| Fact | Value |
|---|---|
| Latest migration | `0015_special_care` |
| `gap_register` | 22 rows, 8 blocker |
| Engine steps recorded | 16 |
| Book-rendering code in the repository | **none** |
| `meal_category_target` rows | 7 (`MC-01`..`MC-07`) |
| `recipe_master.meal_type` distinct values | 6 |
| **Recipes reachable by no Book 2 chapter** | **354 of 940 (37.7%)** |
| `recipe_master` version column | **does not exist** (the Book 2 schema requires `recipe_version`) |

### The meal-category defect, measured

This is not "four chapters render empty". It is worse and more specific:

| `meal_category_target` | `include_logic` | recipes |
|---|---|---|
| MC-01 Breakfast | Always once complementary feeding applies | 194 |
| MC-03 Lunch | Always once complementary feeding applies | 199 |
| MC-06 Dinner | Always once complementary feeding applies | 193 |
| MC-02 Mid-morning | Only if feeding schedule includes it | **0** |
| MC-04 Tiffin / school snack | When daycare/school/portable meal relevant | **0** |
| MC-05 Evening snack | When schedule requires | **0** |
| MC-07 Supper / bedtime | Only if nutrition plan specifically includes it | **0** |

Against the corpus's own vocabulary:

| `recipe_master.meal_type` | recipes | reachable chapter |
|---|---|---|
| Breakfast | 194 | MC-01 |
| Lunch | 199 | MC-03 |
| Dinner | 193 | MC-06 |
| **Snack** | **182** | **none** |
| **School Tiffin** | **99** | **none** |
| **Recovery Meal** | **73** | **none** |

**354 recipes - 37.7% of everything loaded - can reach no chapter of Book 2 at any age, for
any child.** An empty Mid-morning chapter for a child whose schedule excludes it is correct
behaviour, and `include_logic` says so. A tiffin chapter that is empty while 99 School Tiffin
recipes sit in the database is a defect.

**It is not fixable on this side.** `School Tiffin` → `MC-04 Tiffin / school snack` looks
obvious; `Snack` → `MC-05 Evening snack` is a guess that silently rules out `MC-02
Mid-morning`; `Recovery Meal` matches nothing at all and may deserve a chapter that does not
exist. Writing that mapping here would be inventing the provider's chapter structure. Task 1
therefore builds the mechanism, seeds it with only the three mappings the provider's own
strings already assert, and reports the rest as a counted gap.

## Renderer decision, and why it changed

The earlier lean in conversation was React plus Tailwind, reusing the console's stack. Having
read the template contract, **that is the wrong call.** The decision is Go `html/template`
plus plain CSS plus headless Chromium.

What changed it, in order of weight:

1. **`"unicode_multilingual": true`, and 406 of 406 ingredients carry Bengali and Hindi
   names.** Bengali is an Indic script requiring conjunct formation and matra
   repositioning - real text shaping, not glyph substitution. Go's PDF libraries do not ship
   shaping; you would need HarfBuzz bindings to render `ইলিশ মাছ` correctly, and a renderer
   that silently mis-forms a Bengali ingredient name is worse than one that cannot render it.
   A browser engine does this correctly and for free. **This alone rules out a pure-Go PDF
   library.**
2. **The SRS puts the Book Engine in its own service.** A renderer living inside the Next.js
   app couples book generation to the console being up, and makes the book's HTML a
   client-rendered artifact rather than a server-side one that can be hashed and stored. The
   contract's `release_footer_fields` (`book_version`, `release_id`, `generation_date`) and
   the SRS's immutable release records with file hashes both want the HTML produced
   server-side by the same service that records the release.
3. **The design is print, and Tailwind is not.** `@page` rules, running headers, repeated
   table headers, `page-break-inside: avoid` on a recipe card, mm margins, pt minimums - none
   of these have Tailwind utilities, so a Tailwind implementation would be a print stylesheet
   with Tailwind noise on top. The contract's tokens map one-to-one onto CSS custom
   properties.
4. **Preview and print become the same artifact.** The reviewer preview is the generated HTML
   served directly; the PDF is Chromium printing that same HTML. Given the SRS requires human
   review gates before release, a preview that could differ from the printed output is a
   safety problem, not just an inconvenience.

The cost is a Chromium dependency in the deploy. That is real and is accepted: it buys
correct Indic shaping, correct pagination, and one artifact instead of two.

**Rejected alternatives, recorded so they are not re-litigated:** `gofpdf` and `pdfcpu`
(no Indic shaping); `wkhtmltopdf` (unmaintained, ships an ancient WebKit whose CSS support
predates `@page` margin boxes); LaTeX (excellent typesetting, but an XeLaTeX toolchain plus
Indic font configuration is a heavier operational dependency than Chromium for a design that
is fundamentally boxes and tables); React/Next (reasons above).

---

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `internal/db/migrations/0016_meal_category_map.up.sql` / `.down.sql` | `meal_category_recipe_map` seed table, its three asserted rows, and `GAP-023` |
| `internal/book/doc.go` | Package doc: the JSON-is-the-contract architecture and the no-prose rule |
| `internal/book/types.go` | Go structs mirroring both provider JSON schemas exactly |
| `internal/book/book1.go` | Book 1 assembler: database rows to `Book1` |
| `internal/book/book2.go` | Book 2 assembler: engine result to `Book2` |
| `internal/book/assemble_test.go` | Assembler tests, including schema-required-field coverage |
| `internal/book/render.go` | `html/template` rendering, template registry keyed by provider template id |
| `internal/book/render_test.go` | Golden-HTML tests and the no-fabrication assertions |
| `internal/book/pdf.go` | chromedp print pipeline, header/footer templates, page numbering |
| `internal/book/pdf_test.go` | PDF smoke test, skipped when Chromium is absent |
| `internal/book/templates/base.html` | Page chrome: `@page`, running header/footer, provisional banner |
| `internal/book/templates/tokens.css` | The contract's global tokens and both palettes as CSS custom properties |
| `internal/book/templates/book1/*.html` | Ten `B1-*` component templates |
| `internal/book/templates/book2/*.html` | Nine `B2-*` component templates |
| `internal/api/handlers/books.go` | Preview (HTML) and download (PDF) endpoints |
| `internal/api/handlers/books_test.go` | Endpoint tests including the special-care stop |

**Modified:**

| File | Change |
|---|---|
| `internal/importer/gaps.go` | `GAP-023` measure |
| `internal/api/router.go` | Two book routes |
| `CLAUDE.md`, `README.md`, `docs/next-steps.md` | Record what now exists |

---

## Task 1: Meal-category reconciliation

Fixes the defect that 354 of 940 recipes can reach no Book 2 chapter. Builds the mapping
mechanism and seeds **only** what the provider's own strings already assert, leaving the
genuinely ambiguous mappings as a counted, reported gap rather than a guess.

**Files:**
- Create: `internal/db/migrations/0016_meal_category_map.up.sql`
- Create: `internal/db/migrations/0016_meal_category_map.down.sql`
- Create: `internal/db/meal_category_test.go`
- Modify: `internal/importer/gaps.go` (append one measure to `gapMeasures`)
- Modify: `internal/db/integrity_test.go` (the documented gap count, currently 22)

**Interfaces:**
- Consumes: `meal_category_target(meal_category_id, meal_category)`,
  `recipe_master(meal_type)`.
- Produces: view `meal_category_recipe` with columns
  `(meal_category_id, meal_category, recipe_id, recipe_name, meal_type)`. Task 5's Book 2
  assembler reads this view and never joins on `meal_type` directly.

- [ ] **Step 1: Write the migration**

Create `internal/db/migrations/0016_meal_category_map.up.sql`:

```sql
-- Book 2's chapters come from meal_category_target (MC-01..MC-07). Recipes carry
-- recipe_master.meal_type, a different six-value vocabulary. Three values match by name and
-- three do not, so 354 of 940 recipes - 37.7% of the corpus - can reach no chapter of Book 2
-- at any age, for any child.
--
-- The three that match are matched here because the provider's own two strings are
-- identical, which is an assertion by the provider rather than an inference by us. The three
-- that do not match are deliberately NOT mapped:
--
--   School Tiffin  -> MC-04 "Tiffin / school snack" looks obvious, and "looks obvious" is
--                     exactly the standard the hard rule rejects.
--   Snack          -> could be MC-02 Mid-morning, MC-05 Evening snack, or split across both.
--                     Choosing one silently empties the other.
--   Recovery Meal  -> matches no category at all. It is the only meal_type named for a
--                     clinical state rather than a time of day, and it may deserve a chapter
--                     that does not exist yet.
--
-- Mapping those three is the provider's decision about their own book's chapter structure.
-- GAP-023 counts the unreachable recipes so the hole is visible and shrinks to zero by
-- itself when the provider answers - one INSERT per ruling, no code change.
--
-- An unmapped meal_type is not a rendering failure. Task 3's assembler omits a chapter with
-- no recipes rather than emitting an empty one, which is also correct behaviour for the
-- conditional categories: meal_category_target.include_logic marks MC-02, MC-04, MC-05 and
-- MC-07 as firing only when the child's schedule calls for them.

CREATE TABLE meal_category_recipe_map (
    meal_category_id text NOT NULL REFERENCES meal_category_target(meal_category_id),
    meal_type        text NOT NULL,
    basis            text NOT NULL,
    PRIMARY KEY (meal_category_id, meal_type),
    CONSTRAINT meal_category_recipe_map_basis_check
        CHECK (basis IN ('provider-identical-name', 'provider-ruling'))
);

COMMENT ON COLUMN meal_category_recipe_map.basis IS
    'provider-identical-name: meal_category and meal_type are the same string, so the '
    'provider asserted the mapping. provider-ruling: the provider answered the open '
    'question and the answer is recorded here with its date in the migration that adds it. '
    'No other basis is permitted - an inferred mapping is an invented one.';

INSERT INTO meal_category_recipe_map (meal_category_id, meal_type, basis) VALUES
    ('MC-01', 'Breakfast', 'provider-identical-name'),
    ('MC-03', 'Lunch',     'provider-identical-name'),
    ('MC-06', 'Dinner',    'provider-identical-name');

-- The join Book 2 assembly uses. Reading this rather than matching meal_type to
-- meal_category by name means an unmapped type contributes nothing anywhere, visibly,
-- instead of appearing to work in one query and not another.
CREATE VIEW meal_category_recipe AS
SELECT m.meal_category_id,
       t.meal_category,
       r.recipe_id,
       r.recipe_name,
       r.meal_type
FROM meal_category_recipe_map m
JOIN meal_category_target t ON t.meal_category_id = m.meal_category_id
JOIN recipe_master r ON r.meal_type = m.meal_type;

INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows,
     measured_by, ui_behaviour, resolution_path)
VALUES
    ('GAP-023', 'blocker', 'Book 2 assembly',
     'recipe_master', 'meal_type',
     'Recipes whose meal_type maps to no Book 2 meal category, so they can appear in no '
     || 'chapter of any generated recipe book. Snack (182), School Tiffin (99) and Recovery '
     || 'Meal (73) have no counterpart in meal_category_target, which is 37.7% of the loaded '
     || 'corpus. The mapping is the provider''s decision about their own chapter structure '
     || 'and cannot be inferred here: School Tiffin to MC-04 looks obvious but is still a '
     || 'guess, Snack could be MC-02 or MC-05 or both, and Recovery Meal matches nothing.',
     0, 'importer',
     'Chapters with no recipes are omitted from the book rather than rendered empty, and '
     || 'the omission is listed on the book''s own contents. No recipe is ever shown under '
     || 'a chapter it was not mapped to.',
     'Provider rules on the three unmapped meal types. Each ruling is one INSERT into '
     || 'meal_category_recipe_map with basis = provider-ruling; no code changes.');
```

Create `internal/db/migrations/0016_meal_category_map.down.sql`:

```sql
DELETE FROM gap_register WHERE gap_id = 'GAP-023';
DROP VIEW IF EXISTS meal_category_recipe;
DROP TABLE IF EXISTS meal_category_recipe_map;
```

- [ ] **Step 2: Add the gap measure**

In `internal/importer/gaps.go`, append to the `gapMeasures` slice, after the `GAP-022` entry:

```go
	// Recipes that can reach no Book 2 chapter. Counted through the mapping table rather
	// than by comparing vocabularies, so a provider ruling that adds a mapping drops this
	// number without anyone editing the query.
	//
	// Reads 354 today: Snack 182, School Tiffin 99, Recovery Meal 73.
	{"GAP-023", `SELECT count(*) FROM recipe_master r
	             WHERE NOT EXISTS (
	                 SELECT 1 FROM meal_category_recipe_map m
	                 WHERE m.meal_type = r.meal_type)`},
```

- [ ] **Step 3: Update the documented gap count**

In `internal/db/integrity_test.go`, `TestGapRegisterCountMatchesTheDocumentedCount` currently
asserts 22. Change the assertion and its comment:

```go
	// Twenty-three after migration 0016. The build-up: twelve seeded in migration 0002, four
	// that internal/enrich/gaps.go upserts on every enrichment run, four added by 0012, two
	// by 0015, one by 0016. The enrich four are easy to miss because no migration writes
	// them, which is exactly why this count is asserted rather than left in prose. If a gap
	// is added or retired, change this number and the documents that quote it in the same
	// commit.
	if n != 23 {
		t.Fatalf("gap_register holds %d rows, want 23", n)
	}
```

- [ ] **Step 4: Write the test**

Create `internal/db/meal_category_test.go`:

```go
package db_test

import (
	"context"
	"testing"
)

// The three name-identical mappings are the only ones the provider has asserted. If this
// count grows, a ruling arrived and the documents quoting 354 need updating in the same
// commit; if it shrinks, someone deleted an assertion the provider made.
func TestMealCategoryMapHoldsOnlyAssertedMappings(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var inferred int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM meal_category_recipe_map WHERE basis <> 'provider-identical-name'`).
		Scan(&inferred); err != nil {
		t.Fatalf("basis query: %v", err)
	}
	if inferred != 0 {
		t.Fatalf("%d mappings claim a basis other than provider-identical-name. A "+
			"provider-ruling row is legitimate but must arrive in its own migration with "+
			"the ruling's date recorded, so update this test in that commit", inferred)
	}

	// Every asserted mapping must join two strings that really are identical. This is what
	// makes "the provider asserted it" true rather than a label we applied.
	var mismatched int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM meal_category_recipe_map m
		JOIN meal_category_target t ON t.meal_category_id = m.meal_category_id
		WHERE m.basis = 'provider-identical-name' AND t.meal_category <> m.meal_type`).
		Scan(&mismatched); err != nil {
		t.Fatalf("identity query: %v", err)
	}
	if mismatched != 0 {
		t.Fatalf("%d rows are marked provider-identical-name but the two strings differ; "+
			"that basis is a claim about the data, not a category to file guesses under",
			mismatched)
	}
}

// The defect this migration exists to make visible. Not an assertion that 354 is correct
// forever -- an assertion that the number is measured and reported rather than hidden.
func TestUnreachableRecipesAreCounted(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var unreachable int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM recipe_master r
		WHERE NOT EXISTS (
		    SELECT 1 FROM meal_category_recipe_map m WHERE m.meal_type = r.meal_type)`).
		Scan(&unreachable); err != nil {
		t.Fatalf("unreachable query: %v", err)
	}

	var registered int
	if err := pool.QueryRow(ctx,
		`SELECT coalesce(affected_rows, -1) FROM gap_register WHERE gap_id = 'GAP-023'`).
		Scan(&registered); err != nil {
		t.Fatalf("GAP-023: %v", err)
	}
	if registered != unreachable {
		t.Fatalf("GAP-023 reports %d unreachable recipes but the data holds %d; the gap "+
			"register is only useful if its numbers are the measured ones",
			registered, unreachable)
	}
}

// A recipe must never reach a chapter it was not mapped to. This is the property that makes
// omitting an empty chapter safe rather than lossy.
func TestMealCategoryRecipeViewOnlyServesMappedTypes(t *testing.T) {
	pool := testPool(t)
	var leaked int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM meal_category_recipe v
		WHERE NOT EXISTS (
		    SELECT 1 FROM meal_category_recipe_map m
		    WHERE m.meal_category_id = v.meal_category_id AND m.meal_type = v.meal_type)`).
		Scan(&leaked); err != nil {
		t.Fatalf("leak query: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("%d rows in meal_category_recipe are not backed by a mapping row", leaked)
	}
}
```

- [ ] **Step 5: Rebuild, import and run**

```bash
scripts/dev_db.fish down && scripts/dev_db.fish up
DATABASE_URL=(scripts/dev_db.fish url) go run ./cmd/import
DATABASE_URL=(scripts/dev_db.fish url) go run ./cmd/enrich
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/db/ -run 'MealCategory|Unreachable|GapRegister' -v
```

Expected: PASS. `gap_register` holds 23 rows, and `GAP-023.affected_rows` is 354.

- [ ] **Step 6: Full verification and commit**

```bash
go build ./... && go vet ./... && TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
git add internal/db/migrations/0016_meal_category_map.up.sql \
        internal/db/migrations/0016_meal_category_map.down.sql \
        internal/db/meal_category_test.go internal/db/integrity_test.go \
        internal/importer/gaps.go
git commit -m "Map recipes to Book 2 chapters, and count the ones that map to nothing

354 of 940 recipes - 37.7% of the corpus - can reach no chapter of Book 2 at
any age, for any child. Snack (182), School Tiffin (99) and Recovery Meal
(73) have no counterpart in meal_category_target.

Only the three name-identical mappings are seeded, because those are the
provider asserting the mapping rather than us inferring it. School Tiffin to
MC-04 looks obvious and is still a guess; Snack could be MC-02 or MC-05 or
both, and picking one silently empties the other; Recovery Meal matches
nothing at all. Deciding those is the provider ruling on their own chapter
structure.

GAP-023 counts the unreachable recipes through the mapping table, so a
provider ruling drops the number with one INSERT and no code change."
```

---

## Task 2: Design tokens and page chrome

The foundation every later template sits on: the contract's global tokens as CSS custom
properties, both palettes, the `@page` rules, the running header and footer, and the
provisional banner that every generated book carries.

**Files:**
- Create: `internal/book/doc.go`
- Create: `internal/book/templates/tokens.css`
- Create: `internal/book/templates/base.html`
- Create: `internal/book/render.go` (the loader and the `RenderHTML` entry point)
- Create: `internal/book/render_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `book.RenderHTML(w io.Writer, b any, book BookKind) error` where
  `BookKind` is `Book1` or `Book2`; `book.Templates` is the parsed `embed.FS` template set.
  Later tasks add templates to the same set and call the same entry point.

- [ ] **Step 1: Write the package doc**

Create `internal/book/doc.go`:

```go
// Package book renders the provider's two books from a child's data.
//
// The provider's JSON schemas are the contract between the engine and the renderer
// (data/book-engine-spec/MadamGY_Book{1,2}_JSON_Schema_V1.json). An assembler builds that
// JSON from the database; html/template renders it; chromedp prints the result. The same
// HTML is the reviewer preview and the print source, so what a reviewer approves and what
// prints cannot drift apart.
//
// # This package never writes a sentence
//
// Every string a parent reads is either provider-authored text loaded from the database or
// one of the child's own recorded values. There are no generated summaries, no rephrased
// clinical guidance and no filled-in defaults. Where data is absent the template emits a
// blank writing line or omits the block, following the provider's own prototypes, which use
// "________" and slot tokens like "[from master]" for exactly this.
//
// That is the 18 August ruling in docs/decisions.md - no generated guidance prose reaches a
// parent through a path with no human review gate - applied to the one surface that reaches
// a parent directly. book1_content_block.ai_can_draft = 'N' enforces a narrower version of
// the same rule on five specific blocks; this package's rule is the broad one and covers
// every block.
//
// # Chromium
//
// PDF rendering needs headless Chromium. That dependency buys correct Indic text shaping,
// which matters because all 406 ingredients carry Bengali and Hindi names and Bengali needs
// conjunct formation and matra repositioning that Go's PDF libraries do not implement. A
// renderer that silently mis-forms a Bengali ingredient name would be worse than one that
// refuses to run.
package book
```

- [ ] **Step 2: Write the tokens**

Create `internal/book/templates/tokens.css`. Every value traces to
`MadamGY_PDF_Template_Contract_V1.json` or to a colour read from the prototypes:

```css
/* Global tokens from MadamGY_PDF_Template_Contract_V1.json. Values are the contract's,
   not preferences: safe_margin_mm 12, header_height_mm 12, footer_height_mm 10,
   body_max_line_length_chars 85, minimum_body_pt 9.5, minimum_table_pt 8.5. */
:root {
  --safe-margin: 12mm;
  --header-height: 12mm;
  --footer-height: 10mm;
  --body-measure: 85ch;

  /* The contract's accessibility floors. These are minimums, so the values used are at or
     above them and never below. avoid_color_only_meaning is honoured by pairing every
     coloured callout with a text label. */
  --body-size: 10pt;
  --table-size: 9pt;
  --small-size: 8.5pt;

  --font-body: "Noto Sans", "Segoe UI", system-ui, sans-serif;
  /* Bengali and Devanagari need a font with the conjuncts; Noto covers both scripts and
     keeps one family across the whole book. Verify the faces are installed in the render
     environment before trusting output - a missing face renders tofu, not an error. */
  --font-indic: "Noto Sans Bengali", "Noto Sans Devanagari", var(--font-body);

  --warning-strong: #9b2226;   /* warning_palette: deep red */
  --warning-soft: #fdecea;     /* warning_palette: light warm red */
  --rule: #d9d9d9;
  --ink: #1f2933;
  --ink-soft: #52606d;
  --zebra: #fafafa;
}

/* book1_palette: deep navy, teal, warm cream, muted gold */
.book1 {
  --brand: #0f6466;
  --brand-deep: #123a5f;
  --surface: #fdf6e6;
  --accent: #c9922b;
  --card-fill: #eef6f6;
}

/* book2_palette: deep plum, warm rose, cream, muted gold */
.book2 {
  --brand: #b0688a;
  --brand-deep: #6b2d5c;
  --surface: #fdf6e6;
  --accent: #c9922b;
  --card-fill: #fbeaf1;
}

@page {
  size: A4 portrait;
  margin: calc(var(--safe-margin) + var(--header-height))
          var(--safe-margin)
          calc(var(--safe-margin) + var(--footer-height));
}

body {
  font-family: var(--font-body);
  font-size: var(--body-size);
  color: var(--ink);
  margin: 0;
}

p, li { max-width: var(--body-measure); }

/* section_title_style: "large editorial heading + short supportive subtitle", drawn in both
   prototypes as a full-bleed band in the book's brand colour. */
.section-header {
  background: var(--brand-deep);
  color: #fff;
  padding: 6mm 6mm 5mm;
  margin: 0 0 6mm;
  break-after: avoid;
}
.section-header h2 { margin: 0; font-size: 18pt; }
.section-header p { margin: 3mm 0 0; font-size: var(--small-size); opacity: 0.9; }

/* table_header_repeat: true. thead repeating across pages is native to print CSS, which is
   one of the reasons this renderer is HTML rather than a drawing library. */
table { width: 100%; border-collapse: collapse; font-size: var(--table-size); }
thead { display: table-header-group; }
tr { break-inside: avoid; }
th {
  background: var(--brand-deep);
  color: #fff;
  text-align: left;
  padding: 2.5mm 3mm;
  font-size: var(--small-size);
}
td { padding: 2.5mm 3mm; border-bottom: 0.3mm solid var(--rule); vertical-align: top; }
tbody tr:nth-child(even) { background: var(--zebra); }

.card-row { display: flex; gap: 4mm; margin: 5mm 0; break-inside: avoid; }
.card {
  flex: 1;
  border: 0.3mm solid var(--brand);
  background: var(--card-fill);
  padding: 4mm;
}
.card h3 { margin: 0 0 2mm; color: var(--brand-deep); font-size: 12pt; }
.card p { margin: 0; font-size: var(--small-size); }

/* Three callout severities, drawn in the prototypes. clinical_warning_visibility is "high",
   and avoid_color_only_meaning means the label carries the severity in words too. */
.callout { border: 0.3mm solid var(--accent); background: var(--surface); padding: 4mm;
           margin: 5mm 0; break-inside: avoid; }
.callout h3 { margin: 0 0 2mm; color: var(--accent); font-size: 11pt; }
.callout.warning { border-color: var(--warning-strong); background: var(--warning-soft); }
.callout.warning h3 { color: var(--warning-strong); }

/* A blank the parent writes on. The prototypes use these throughout rather than leaving a
   cell empty, so the page reads as a form rather than as missing data. */
.write-line { display: inline-block; min-width: 28mm; border-bottom: 0.3mm solid var(--ink-soft); }

/* Nothing in the dataset is approved. Every generated book says so on its first page and in
   its running footer; neither is removable by a template. */
.provisional {
  border: 0.4mm solid var(--warning-strong);
  background: var(--warning-soft);
  color: var(--warning-strong);
  padding: 4mm;
  margin: 6mm 0;
  font-size: var(--small-size);
}

.cover { text-align: center; padding-top: 40mm; }
.cover .brand { color: var(--brand); font-weight: 700; letter-spacing: 0.1em; }
.cover h1 { color: var(--brand-deep); font-size: 26pt; margin: 4mm 0 2mm; }
.cover .subtitle { color: var(--ink-soft); font-size: 11pt; margin: 0 auto; max-width: 120mm; }
.cover .name-box {
  border: 0.3mm solid var(--accent);
  background: var(--surface);
  padding: 8mm;
  margin: 12mm auto;
  max-width: 110mm;
}
.cover .name-box .name { color: var(--brand-deep); font-size: 20pt; font-weight: 700; }

.page-break { break-after: page; }
```

- [ ] **Step 3: Write the base template**

Create `internal/book/templates/base.html`:

```html
<!doctype html>
<html lang="{{ .Metadata.Language }}">
<head>
<meta charset="utf-8">
<title>{{ .Metadata.Title }}</title>
<style>{{ .CSS }}</style>
</head>
<body class="{{ .BookClass }}">

{{/* Nothing in the dataset is approved: every recipe is Review_Status = Draft and every
     ingredient Needs Validation. The banner is emitted by the base template rather than by
     a section, so no template can produce a book without it. */}}
<div class="provisional">
  <strong>Provisional - not clinically approved.</strong>
  This book was generated from provider data marked
  <span class="mono">{{ .Metadata.ReviewStatus }}</span>.
  It has not completed culinary, nutrition or clinical review and must not be used as a
  clinical prescription.
</div>

{{ template "body" . }}

</body>
</html>
```

- [ ] **Step 4: Write the renderer**

Create `internal/book/render.go`:

```go
package book

import (
	"embed"
	"fmt"
	"html/template"
	"io"
)

//go:embed templates
var templateFS embed.FS

// Kind selects a book's palette and template set. The provider ships two palettes and two
// template families; nothing else is renderable.
type Kind string

const (
	Kind1 Kind = "book1"
	Kind2 Kind = "book2"
)

// ErrUnknownKind is returned for a Kind that is neither book.
var ErrUnknownKind = fmt.Errorf("book: unknown kind")

type renderContext struct {
	Metadata  Metadata
	BookClass string
	CSS       template.CSS
	Data      any
}

// RenderHTML writes one book as a standalone HTML document. The output is both the reviewer
// preview and the source chromedp prints, so there is exactly one rendering of a book and no
// way for preview and print to disagree.
func RenderHTML(w io.Writer, kind Kind, meta Metadata, data any) error {
	if kind != Kind1 && kind != Kind2 {
		return fmt.Errorf("book: %q: %w", kind, ErrUnknownKind)
	}

	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		return fmt.Errorf("book: read tokens: %w", err)
	}

	t, err := template.ParseFS(templateFS,
		"templates/base.html",
		fmt.Sprintf("templates/%s/*.html", kind))
	if err != nil {
		return fmt.Errorf("book: parse %s templates: %w", kind, err)
	}

	// template.CSS marks the stylesheet as trusted so html/template does not escape it.
	// Safe because it is an embedded file in this repository, never anything from the
	// database or a request.
	ctx := renderContext{
		Metadata:  meta,
		BookClass: string(kind),
		CSS:       template.CSS(css),
		Data:      data,
	}
	if err := t.ExecuteTemplate(w, "base.html", ctx); err != nil {
		return fmt.Errorf("book: render %s: %w", kind, err)
	}
	return nil
}
```

- [ ] **Step 5: Write the test**

Create `internal/book/render_test.go`:

```go
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
```

- [ ] **Step 6: Run and commit**

```bash
go build ./... && go vet ./... && go test ./internal/book/ -v
git add internal/book/
git commit -m "Add the book renderer's design tokens and page chrome

Every value in tokens.css traces to the provider's PDF template contract:
12mm safe margin, 12mm header, 10mm footer, 85ch measure, 9.5pt body and
8.5pt table minimums, table_header_repeat, and both palettes.

The provisional banner is emitted by the base template rather than by any
section, so there is no template that can produce a book without disclosing
that the data behind it is unapproved. Pinned by a test.

Go html/template rather than React: the renderer belongs to the book service
rather than to the console, the same HTML is both the reviewer preview and
the print source so the two cannot drift, and print CSS has no Tailwind
equivalent for page rules, running headers or repeated table headers."
```

---

## Task 3: The book JSON types and the Book 1 assembler

Go structs mirroring the provider's schemas exactly, and the assembler that builds a `Book1`
from the database.

**Files:**
- Create: `internal/book/types.go`
- Create: `internal/book/book1.go`
- Create: `internal/book/assemble_test.go`

**Interfaces:**
- Consumes: `book1_content_block` (32 rows, `book_order`), `book1_vaccine_schedule` (44),
  `book1_development_milestone` (33), `profile.Stored`.
- Produces: `book.Metadata`, `book.Book1`, and
  `book.AssembleBook1(ctx, pool, profile.Stored, asOf time.Time) (Book1, error)`.
  Task 4 renders `Book1`; Task 7 serves it.

- [ ] **Step 1: Write the types**

Create `internal/book/types.go`. Field names and JSON tags mirror
`MadamGY_Book1_JSON_Schema_V1.json` and `MadamGY_Book2_JSON_Schema_V1.json`:

```go
package book

import "time"

// Metadata is the book_metadata object both schemas require, plus the three release footer
// fields the template contract names (book_version, release_id, generation_date).
type Metadata struct {
	Title          string    `json:"title"`
	BookVersion    string    `json:"book_version"`
	ReleaseID      string    `json:"release_id"`
	GenerationDate time.Time `json:"generation_date"`
	Language       string    `json:"language"`
	// ReviewStatus is the provider's own value, carried verbatim onto the page. It is a
	// string rather than a bool because "Draft - Culinary/Nutrition/Clinical Review
	// Required" says more than false does.
	ReviewStatus string `json:"review_status"`
}

// ChildSummary is the personalization the provider's prototype actually relies on: the
// child's own recorded values, printed next to the approved reference. Every field is a
// stored measurement or a stored declaration. Nothing here is computed except AgeMonths,
// which is derived from date of birth by internal/profile.
type ChildSummary struct {
	DisplayName   string  `json:"display_name"`
	AgeMonths     int     `json:"age_months"`
	AgeLabel      string  `json:"age_label"`
	FeedingStage  string  `json:"feeding_stage,omitempty"`
	FoodPractice  string  `json:"food_practice,omitempty"`
	AllergyStatus string  `json:"allergy_status"`
	WeightKg      *string `json:"weight_kg"`
	HeightCm      *string `json:"height_cm"`
	MeasuredOn    string  `json:"measured_on,omitempty"`
}

// Section is one rendered block of Book 1, keyed to the provider's template id so the
// renderer picks the template the contract names rather than one this code invents.
type Section struct {
	BlockID    string `json:"block_id"`
	TemplateID string `json:"template_id"`
	BookOrder  int    `json:"book_order"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle,omitempty"`
	Rows       []Row  `json:"rows,omitempty"`
	Cards      []Card `json:"cards,omitempty"`
	Callout    *Callout `json:"callout,omitempty"`
}

// Row is the comparison shape B1-COMPARE-01 draws and that pages 3, 5 and 6 of the Book 1
// prototype all share: reference, the child's actual, and what to do next. Actual is a
// pointer because an unrecorded measurement renders as a writing line, never as a zero.
type Row struct {
	Label     string  `json:"label"`
	Reference string  `json:"reference"`
	Actual    *string `json:"actual"`
	Note      string  `json:"note,omitempty"`
}

type Card struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

// Severity is "info" or "warning". The contract sets clinical_warning_visibility to high and
// asks that colour never be the only carrier of meaning, so the template prints the severity
// as a word as well as a colour.
type Callout struct {
	Severity string `json:"severity"`
	Heading  string `json:"heading"`
	Body     string `json:"body"`
}

// Book1 mirrors MadamGY_Book1_JSON_Schema_V1.json.
type Book1 struct {
	Metadata            Metadata     `json:"book_metadata"`
	Child               ChildSummary `json:"child_profile"`
	ConsultationSummary []Row        `json:"consultation_summary"`
	Sections            []Section    `json:"sections"`
}
```

- [ ] **Step 2: Write the Book 1 assembler**

Create `internal/book/book1.go`:

```go
package book

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/profile"
)

// blockTemplate maps a Book 1 content block to the provider's template id from
// MadamGY_PDF_Template_Contract_V1.json. Hand-written, because the workbook carries no
// template column and guessing from a block's title would put clinical content in the wrong
// visual treatment -- B1-RED-01 is the high-contrast warning panel, and a red-flag block
// rendered as an ordinary table would lose exactly the emphasis it needs.
//
// A block with no entry is not rendered, and AssembleBook1 reports it. That is the honest
// gap: an unmapped block is one nobody has decided the presentation for, and inventing a
// layout for clinical content is the same class of error as inventing its text.
var blockTemplate = map[string]string{
	"B1-001": "B1-PROFILE-01",
	"B1-009": "B1-VAX-01",
	"B1-011": "B1-DEV-01",
	"B1-012": "B1-RED-01",
	"B1-014": "B1-DEV-01",
	"B1-022": "B1-END-01",
}

// AssembleBook1 builds the Book 1 document for one child as of a given date.
//
// The second return names every block that was skipped and why, so a reviewer sees what the
// book does not contain rather than assuming the absence is deliberate.
func AssembleBook1(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time) (Book1, []string, error) {
	cp, dropped, err := s.ToChildProfile(asOf)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: derive engine input: %w", err)
	}

	b := Book1{
		Metadata: Metadata{
			Title:          "My Child's Growth, Nutrition & Development Companion",
			BookVersion:    "V1",
			GenerationDate: asOf,
			Language:       "en",
			ReviewStatus:   "Draft - Culinary/Nutrition/Clinical Review Required",
		},
		Child: ChildSummary{
			DisplayName: s.DisplayName,
			AgeMonths:   cp.AgeMonths,
			AgeLabel:    ageLabel(cp.AgeMonths),
			// "No known food allergy reported" is the prototype's own wording for the
			// empty case. An empty string here would render a blank cell that reads as an
			// unanswered question rather than a recorded negative.
			AllergyStatus: allergyStatus(cp.Allergens),
		},
	}

	// Most recent measurement only, formatted as recorded. Growth[0] is newest: profile.Load
	// orders them that way.
	if len(s.Growth) > 0 {
		g := s.Growth[0]
		b.Child.MeasuredOn = g.MeasuredOn.UTC().Format("2006-01-02")
		if g.WeightKg != nil {
			v := fmt.Sprintf("%.1f kg", *g.WeightKg)
			b.Child.WeightKg = &v
		}
		if g.HeightCm != nil {
			v := fmt.Sprintf("%.0f cm", *g.HeightCm)
			b.Child.HeightCm = &v
		}
	}

	rows, err := pool.Query(ctx, `
		SELECT block_id, book_order, coalesce(section, ''), coalesce(subsection, ''),
		       coalesce(table_or_format, ''), ai_can_draft
		FROM book1_content_block
		WHERE (age_from_mo IS NULL OR age_from_mo <= $1)
		  AND (age_to_mo IS NULL OR age_to_mo >= $1)
		ORDER BY book_order`, cp.AgeMonths)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load blocks: %w", err)
	}
	defer rows.Close()

	skipped := append([]string{}, dropped...)
	for rows.Next() {
		var blockID, section, subsection, format, aiCanDraft string
		var order int
		if err := rows.Scan(&blockID, &order, &section, &subsection, &format, &aiCanDraft); err != nil {
			return Book1{}, nil, fmt.Errorf("book: scan block: %w", err)
		}
		tmpl, ok := blockTemplate[blockID]
		if !ok {
			skipped = append(skipped, fmt.Sprintf(
				"block %s (%s) has no template mapping and was not rendered", blockID, section))
			continue
		}
		b.Sections = append(b.Sections, Section{
			BlockID: blockID, TemplateID: tmpl, BookOrder: order,
			Title: section, Subtitle: subsection,
		})
	}
	if err := rows.Err(); err != nil {
		return Book1{}, nil, fmt.Errorf("book: block rows: %w", err)
	}

	return b, skipped, nil
}

func ageLabel(months int) string {
	if months < 24 {
		return fmt.Sprintf("%d months", months)
	}
	y, m := months/12, months%12
	if m == 0 {
		return fmt.Sprintf("%d years", y)
	}
	return fmt.Sprintf("%d years %d months", y, m)
}

func allergyStatus(groups []string) string {
	if len(groups) == 0 {
		return "No known food allergy reported"
	}
	out := "Declared: "
	for i, g := range groups {
		if i > 0 {
			out += ", "
		}
		out += g
	}
	return out
}
```

- [ ] **Step 3: Write the tests**

Create `internal/book/assemble_test.go`:

```go
package book

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/profile"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestAgeLabel(t *testing.T) {
	for _, c := range []struct {
		months int
		want   string
	}{
		{8, "8 months"},
		{23, "23 months"},
		{24, "2 years"},
		{51, "4 years 3 months"}, // the prototype's own child, Aarav
	} {
		if got := ageLabel(c.months); got != c.want {
			t.Fatalf("ageLabel(%d) = %q, want %q", c.months, got, c.want)
		}
	}
}

// An unrecorded allergy must read as a recorded negative, not as a blank the reader has to
// interpret. The prototype's wording is the provider's, so it is used verbatim.
func TestAllergyStatusNamesTheEmptyCase(t *testing.T) {
	if got := allergyStatus(nil); got != "No known food allergy reported" {
		t.Fatalf("empty allergy status = %q", got)
	}
	if got := allergyStatus([]string{"Peanut"}); got == "No known food allergy reported" {
		t.Fatalf("a declared allergen must not render as none: %q", got)
	}
}

// A missing measurement renders as a writing line, never as zero. This is the assembler half
// of that contract: the pointer stays nil rather than becoming "0.0 kg".
func TestMissingGrowthStaysNil(t *testing.T) {
	pool := testPool(t)
	s := profile.Stored{
		ChildID:     "BOOK-TEST-001",
		DisplayName: "Test",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	b, _, err := AssembleBook1(context.Background(), pool, s, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	if b.Child.WeightKg != nil || b.Child.HeightCm != nil {
		t.Fatalf("a child with no growth measurement must carry nil, got %v / %v",
			b.Child.WeightKg, b.Child.HeightCm)
	}
	if b.Metadata.ReviewStatus == "" {
		t.Fatal("every assembled book must carry the provider's review status")
	}
}

// Blocks with no template mapping must be reported, not silently dropped. A reviewer needs
// to know the book is missing sections.
func TestUnmappedBlocksAreReported(t *testing.T) {
	pool := testPool(t)
	s := profile.Stored{
		ChildID:     "BOOK-TEST-002",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	_, skipped, err := AssembleBook1(context.Background(), pool, s, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	// 32 blocks exist and blockTemplate maps 6, so a real database must report skips.
	if len(skipped) == 0 {
		t.Fatal("no skipped blocks reported; with 6 of 32 blocks mapped this cannot be right")
	}
}
```

- [ ] **Step 4: Run and commit**

```bash
go build ./... && go vet ./... && TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -v
git add internal/book/types.go internal/book/book1.go internal/book/assemble_test.go
git commit -m "Assemble the Book 1 document from a stored child profile

Go structs mirror the provider's JSON schema field for field, so the contract
between engine and renderer is theirs rather than one invented here.

A block with no template mapping is skipped and reported rather than rendered
with a guessed layout. B1-RED-01 is the high-contrast warning panel and a
red-flag block rendered as an ordinary table would lose the emphasis it
exists for, so choosing a layout for clinical content is the same class of
decision as writing its text.

Missing measurements stay nil through the assembler so the template can
render a writing line. A zero would read as a measured value."
```

---

## Task 4: Book 1 templates

The ten `B1-*` components from the contract, drawn to match the prototype.

**Files:**
- Create: `internal/book/templates/book1/cover.html` (`B1-COVER-01`)
- Create: `internal/book/templates/book1/profile.html` (`B1-PROFILE-01`)
- Create: `internal/book/templates/book1/goals.html` (`B1-GOALS-01`)
- Create: `internal/book/templates/book1/compare.html` (`B1-COMPARE-01`)
- Create: `internal/book/templates/book1/vax.html` (`B1-VAX-01`)
- Create: `internal/book/templates/book1/dev.html` (`B1-DEV-01`)
- Create: `internal/book/templates/book1/daily.html` (`B1-DAILY-01`)
- Create: `internal/book/templates/book1/red.html` (`B1-RED-01`)
- Create: `internal/book/templates/book1/track.html` (`B1-TRACK-01`)
- Create: `internal/book/templates/book1/end.html` (`B1-END-01`)
- Create: `internal/book/templates/book1/body.html` (dispatch by `TemplateID`)

**Interfaces:**
- Consumes: `Book1`, `Section`, `Row`, `Card`, `Callout` from Task 3.
- Produces: templates named `B1-COVER-01` … `B1-END-01`, dispatched by `body.html`.

- [ ] **Step 1: Write the dispatcher**

Create `internal/book/templates/book1/body.html`:

```html
{{ define "body" }}
{{ $b := .Data }}
{{ template "B1-COVER-01" $b }}
{{ range $b.Sections }}
  <section class="page-break">
    <div class="section-header">
      <h2>{{ .Title }}</h2>
      {{ with .Subtitle }}<p>{{ . }}</p>{{ end }}
    </div>
    {{/* Dispatch on the provider's own template id. An unknown id renders nothing rather
         than falling back to a generic table: a fallback would silently give clinical
         content the wrong visual treatment, which is the failure B1-RED-01 exists to
         prevent. AssembleBook1 has already reported the block as skipped. */}}
    {{ if eq .TemplateID "B1-PROFILE-01" }}{{ template "B1-PROFILE-01" . }}
    {{ else if eq .TemplateID "B1-COMPARE-01" }}{{ template "B1-COMPARE-01" . }}
    {{ else if eq .TemplateID "B1-VAX-01" }}{{ template "B1-VAX-01" . }}
    {{ else if eq .TemplateID "B1-DEV-01" }}{{ template "B1-DEV-01" . }}
    {{ else if eq .TemplateID "B1-RED-01" }}{{ template "B1-RED-01" . }}
    {{ else if eq .TemplateID "B1-END-01" }}{{ template "B1-END-01" . }}
    {{ end }}
  </section>
{{ end }}
{{ end }}
```

- [ ] **Step 2: Write the cover**

Create `internal/book/templates/book1/cover.html`, matching prototype page 1:

```html
{{ define "B1-COVER-01" }}
<div class="cover">
  <div class="brand">MADAMGY</div>
  <h1>My Child's Growth, Nutrition &amp; Development Companion</h1>
  <p class="subtitle">A personalized working guide for feeding, growth, vaccination,
     development and everyday care</p>
  <div class="name-box">
    <div class="name">{{ .Child.DisplayName }}</div>
    <div>Age: {{ .Child.AgeLabel }}</div>
  </div>
  <p class="subtitle">Prepared after consultation</p>
</div>
<div class="page-break"></div>
{{ end }}
```

- [ ] **Step 3: Write the comparison table**

Create `internal/book/templates/book1/compare.html`. This is the shape prototype pages 3, 5
and 6 all share, and `B1-COMPARE-01`'s stated visual:

```html
{{ define "B1-COMPARE-01" }}
<table>
  <thead>
    <tr>
      <th>Parameter</th><th>Reference / expected</th>
      <th>Child's actual</th><th>Parent note / next review</th>
    </tr>
  </thead>
  <tbody>
    {{ range .Rows }}
    <tr>
      <td>{{ .Label }}</td>
      <td>{{ .Reference }}</td>
      {{/* A nil Actual is an unrecorded measurement, and it renders as a line the parent
           writes on. Printing 0, "-" or "n/a" would each read as a finding. */}}
      <td>{{ if .Actual }}{{ . Actual }}{{ else }}<span class="write-line"></span>{{ end }}</td>
      <td>{{ .Note }}</td>
    </tr>
    {{ end }}
  </tbody>
</table>
{{ with .Callout }}{{ template "callout" . }}{{ end }}
{{ end }}

{{ define "callout" }}
<div class="callout{{ if eq .Severity "warning" }} warning{{ end }}">
  <h3>{{ .Heading }}</h3>
  <p>{{ .Body }}</p>
</div>
{{ end }}
```

- [ ] **Step 4: Write the vaccination tracker**

Create `internal/book/templates/book1/vax.html`. The prototype states the rule this template
must obey, on its own page 5: *"It must not fabricate vaccine dates or reactions."*

```html
{{ define "B1-VAX-01" }}
<table>
  <thead>
    <tr>
      <th>Age / due</th><th>Vaccine</th><th>Given date &amp; time</th>
      <th>Brand / batch</th><th>Reaction / note</th><th>Next due</th>
    </tr>
  </thead>
  <tbody>
    {{ range .Rows }}
    <tr>
      <td>{{ .Label }}</td>
      <td>{{ .Reference }}</td>
      {{/* Four blank columns, always. The schedule comes from the approved IAP master; the
           administration record is the parent's to write. This template has no branch that
           can print a date, which is how the prototype's rule is enforced rather than
           merely followed. */}}
      <td><span class="write-line"></span></td>
      <td><span class="write-line"></span></td>
      <td><span class="write-line"></span></td>
      <td><span class="write-line"></span></td>
    </tr>
    {{ end }}
  </tbody>
</table>
{{ with .Callout }}{{ template "callout" . }}{{ end }}
{{ end }}
```

- [ ] **Step 5: Write the remaining six templates**

`profile.html` (`B1-PROFILE-01`) renders `.Rows` as a two-column
`Current profile | Personalized summary` table, matching prototype page 2.

`goals.html` (`B1-GOALS-01`) renders `.Cards` inside `<div class="card-row">`, three to a
row, matching prototype page 2's Goal 1/2/3.

`dev.html` (`B1-DEV-01`) is `compare.html`'s shape with the milestone header row:
`Domain | Age-referenced skill from approved master | Actual observation | Observed date |
Concern / action`, and blank writing cells for the last three, for the same reason as the
vaccination tracker.

`red.html` (`B1-RED-01`) renders one `Callout` with `severity = "warning"` and nothing else -
`clinical_warning_visibility: high`, and the prototype draws it pink/red.

`track.html` (`B1-TRACK-01`) renders `.Rows` with `generous writing space`: every data cell
is a `write-line`, no exceptions.

`end.html` (`B1-END-01`) renders the release fields the contract names - `book_version`,
`release_id`, `generation_date` - plus the review status, verbatim.

Each is the same shape as the templates above; write them out in full rather than aliasing,
so a later change to one cannot silently alter another.

- [ ] **Step 6: Add the rendering tests**

Append to `internal/book/render_test.go`:

```go
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
```

- [ ] **Step 7: Run and commit**

```bash
go test ./internal/book/ -v
git add internal/book/templates/book1/ internal/book/render_test.go
git commit -m "Draw the ten Book 1 templates from the provider's prototype

Each template is the provider's own B1-* id from the PDF template contract,
drawn to match the visual prototype page for page.

The vaccination and development trackers have no branch that can print an
administration date or an observation. The prototype states the rule on its
own page - it must not fabricate vaccine dates or reactions - and a template
with no such branch enforces it rather than merely following it.

An unknown template id renders nothing rather than falling back to a generic
table. A fallback would give clinical content the wrong visual treatment,
which is precisely what the high-contrast red-flag panel exists to prevent."
```

---

## Task 5: The Book 2 assembler and templates

Book 2 is the recipe book, so it consumes an engine result rather than the content master.
Two things make it more delicate than Book 1: the engine's stops must propagate into it, and
the recipe corpus's method text is the known-boilerplate blocker.

**Files:**
- Create: `internal/book/book2.go`
- Create: `internal/book/templates/book2/*.html` (nine `B2-*` components plus `body.html`)
- Modify: `internal/book/assemble_test.go` (Book 2 cases)

**Interfaces:**
- Consumes: `engine.Run`, the `meal_category_recipe` view from Task 1,
  `recipe_method_card`, `recipe_nutrition_recomputed`.
- Produces: `book.Book2`, `book.RecipeCard`, and
  `book.AssembleBook2(ctx, pool, profile.Stored, asOf) (Book2, []string, error)`.

- [ ] **Step 1: Add the Book 2 types**

Append to `internal/book/types.go`:

```go
// RecipeCard mirrors the recipe_card definition in MadamGY_Book2_JSON_Schema_V1.json. Its
// required fields are recipe_id, recipe_version, title, meal_category_id, age_stage_ids,
// selection_reasons, ingredients, method_steps, serving, safety and review_status.
//
// RecipeVersion has no source column: recipe_master carries no version. It is populated from
// the import run's content hash for that table, which is a real, traceable version of the
// row as loaded rather than an invented number, and GAP-024 records that the provider does
// not version recipes.
type RecipeCard struct {
	RecipeID         string   `json:"recipe_id"`
	RecipeVersion    string   `json:"recipe_version"`
	Title            string   `json:"title"`
	MealCategoryID   string   `json:"meal_category_id"`
	AgeStageIDs      []string `json:"age_stage_ids"`
	SelectionReasons []string `json:"selection_reasons"`
	NutritionTags    []string `json:"nutrition_tags,omitempty"`
	PrepTimeMinutes  *int     `json:"prep_time_minutes"`
	CookTimeMinutes  *int     `json:"cook_time_minutes"`
	CostBand         *string  `json:"cost_band"`
	Ingredients      []string `json:"ingredients"`
	MethodSteps      []string `json:"method_steps"`
	TextureServing   *string  `json:"texture_serving"`
	Serving          string   `json:"serving"`
	Safety           string   `json:"safety"`
	ReviewStatus     string   `json:"review_status"`
	// MethodIsProviderBoilerplate marks the 6-unique-texts problem (GAP-001) on the card
	// itself, so a reader is told the steps are generic rather than discovering it.
	MethodIsProviderBoilerplate bool `json:"method_is_provider_boilerplate"`
}

type MealSection struct {
	MealCategoryID     string       `json:"meal_category_id"`
	Title              string       `json:"title"`
	TargetRecipeCount  int          `json:"target_recipe_count"`
	Recipes            []RecipeCard `json:"recipes"`
	SelectionNote      *string      `json:"selection_note"`
}

// Book2 mirrors MadamGY_Book2_JSON_Schema_V1.json. RotationPlan is nullable there and is
// nil here whenever the selected recipes cannot fill seven days without repetition beyond
// what the provider's diversity target allows.
type Book2 struct {
	Metadata     Metadata      `json:"book_metadata"`
	Child        ChildSummary  `json:"child_recipe_profile"`
	MealSections []MealSection `json:"meal_sections"`
	RotationPlan *RotationPlan `json:"rotation_plan"`
}

type RotationPlan struct {
	Days []RotationDay `json:"days"`
}

type RotationDay struct {
	Day   string            `json:"day"`
	Meals map[string]string `json:"meals"`
}
```

- [ ] **Step 2: Write the assembler**

Create `internal/book/book2.go`. The critical behaviours, each of which has a test below:

```go
package book

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/profile"
)

// ErrBlocked is returned when the engine stops for this child. A blocked engine result must
// never become a book: the special-care stop gate exists because the feeding decision for
// those children is a clinician's, and a recipe book is exactly the artifact that would
// override it.
var ErrBlocked = errors.New("book: engine blocked generation")

// AssembleBook2 builds the recipe book for one child.
//
// A chapter with no recipes is omitted rather than rendered empty, and the omission is
// reported. Four of the seven meal categories have no mapped recipes today (GAP-023), and
// meal_category_target.include_logic marks several of them conditional anyway, so an omitted
// chapter is frequently the correct output rather than a failure.
func AssembleBook2(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time) (Book2, []string, error) {
	cp, dropped, err := s.ToChildProfile(asOf)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: derive engine input: %w", err)
	}

	res, err := engine.Run(ctx, pool, cp)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: run engine: %w", err)
	}
	if res.Blocked {
		return Book2{}, nil, fmt.Errorf("%w: %s", ErrBlocked, res.BlockReason)
	}

	// ... build meal sections from the meal_category_recipe view, one query per category,
	// keeping only recipes the engine returned. Omit a section whose recipe list is empty
	// and append a line to `skipped` naming the category and whether the cause was a missing
	// mapping (GAP-023) or the child's own filters.
	//
	// selection_reasons come from the engine's step accounting, never from prose: the
	// active nutrition target's name, the region match, and the diet practice. Those are
	// the provider's own vocabulary and the engine's recorded decisions.

	skipped := append([]string{}, dropped...)
	return Book2{}, skipped, nil
}
```

Fill the elided section following the pattern in `internal/api/handlers/recipes.go`: query
`meal_category_recipe` joined to `recipe_method_card` for each category in
`meal_category_target` order, filtered to the recipe ids the engine returned, and cap each at
`meal_category_target.default_target_recipes`.

- [ ] **Step 3: Write the Book 2 templates**

Nine components. The two that carry the design are:

`B2-RECIPE-01` (prototype page 3): title, reason chips in a bordered strip, a cream
ingredients panel beside a numbered method, then a three-column
`Texture & serving | Safety | Approved swap` table, then the parent tracker row
`Tried on | Amount accepted | Response | Parent note` with writing lines.

`B2-SECTION-01` (prototype pages 3, 4): the meal band in `--brand` rather than
`--brand-deep`, so a meal opener and a section header are visibly different weights.

The recipe card must carry `break-inside: avoid`, per the contract's
`"one to two recipe cards per page depending content length"`.

Where `MethodIsProviderBoilerplate` is true, the card renders a `callout` reading that the
method text is the provider's generic placeholder shared across many recipes. That is
GAP-001 surfaced at the point of use rather than left in the register.

- [ ] **Step 4: Write the tests**

Append to `internal/book/assemble_test.go`:

```go
// The single most important property of this package. A special-care condition stops the
// engine, and a recipe book is exactly the artifact that would override a clinician's
// judgement if it were produced anyway.
func TestBlockedEngineProducesNoBook(t *testing.T) {
	pool := testPool(t)
	s := profile.Stored{
		ChildID:     "BOOK-TEST-003",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		Conditions: []profile.ClinicalCondition{{
			TriggerField: "Special_Care_Condition", FlagValue: "SC-CP", Class: "congenital",
		}},
	}
	_, _, err := AssembleBook2(context.Background(), pool, s,
		time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("a blocked engine result must not become a book, got err = %v", err)
	}
}

// An empty chapter is omitted and reported, never rendered as a heading with nothing under
// it. Four of seven categories have no mapped recipes today.
func TestEmptyChaptersAreOmittedAndReported(t *testing.T) {
	pool := testPool(t)
	s := profile.Stored{
		ChildID:     "BOOK-TEST-004",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		DietType:    "Vegetarian",
	}
	b, skipped, err := AssembleBook2(context.Background(), pool, s,
		time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AssembleBook2: %v", err)
	}
	for _, sec := range b.MealSections {
		if len(sec.Recipes) == 0 {
			t.Fatalf("chapter %s rendered with no recipes; it should have been omitted",
				sec.MealCategoryID)
		}
	}
	if len(skipped) == 0 {
		t.Fatal("with four of seven categories unmapped, some omission must be reported")
	}
}
```

- [ ] **Step 5: Run and commit**

```bash
go build ./... && go vet ./... && TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -v
git add internal/book/book2.go internal/book/types.go internal/book/templates/book2/ internal/book/assemble_test.go
git commit -m "Assemble and draw Book 2, the recipe book

A blocked engine result never becomes a book. The special-care stop gate
exists because the feeding decision for those children belongs to a
clinician, and a recipe book is exactly the artifact that would override it.

A chapter with no recipes is omitted and reported rather than rendered as a
heading with nothing beneath it. Four of seven categories have no mapped
recipes today (GAP-023), and include_logic marks several conditional anyway,
so an omitted chapter is often the correct output.

Recipe cards whose method text is the provider's shared boilerplate say so on
the card. That is GAP-001 surfaced where it is read rather than left in the
register for someone to find later."
```

---

## Task 6: The PDF pipeline

Chromium prints the HTML the previous tasks produce. Header and footer come from Chrome's
own print templates, which is how the contract's `release_footer_fields` and Arabic page
numbering are satisfied.

**Files:**
- Create: `internal/book/pdf.go`
- Create: `internal/book/pdf_test.go`
- Modify: `go.mod` (add `github.com/chromedp/chromedp`)

**Interfaces:**
- Consumes: the HTML from `RenderHTML`.
- Produces: `book.PrintPDF(ctx context.Context, html []byte, meta Metadata) ([]byte, error)`
  and `book.ErrChromiumUnavailable`.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/chromedp/chromedp@latest
```

- [ ] **Step 2: Write the printer**

Create `internal/book/pdf.go`:

```go
package book

import (
	"context"
	"errors"
	"fmt"
	"html"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ErrChromiumUnavailable means no browser was found. Reported plainly rather than falling
// back to a lesser renderer: a fallback that cannot shape Bengali would produce a book whose
// ingredient names are subtly wrong, which is worse than producing none.
var ErrChromiumUnavailable = errors.New("book: headless chromium unavailable")

// PrintPDF renders one already-generated HTML document to PDF.
//
// A4 portrait with the contract's margins. Chrome's own header and footer templates carry
// the release fields and the page number, which is why they are set here rather than in CSS:
// only the print engine knows the total page count.
func PrintPDF(ctx context.Context, htmlDoc []byte, meta Metadata) ([]byte, error) {
	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	footer := fmt.Sprintf(`<div style="font-size:7pt;width:100%%;padding:0 12mm;
		display:flex;justify-content:space-between;color:#52606d">
		<span>MadamGY | %s | release %s | generated %s</span>
		<span class="pageNumber"></span></div>`,
		html.EscapeString(meta.BookVersion),
		html.EscapeString(meta.ReleaseID),
		meta.GenerationDate.Format("2006-01-02"))

	var out []byte
	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frame, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(frame.Frame.ID, string(htmlDoc)).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			out, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPaperWidth(8.27).  // A4 portrait, inches
				WithPaperHeight(11.69).
				WithMarginTop(0.47).   // 12mm
				WithMarginBottom(0.47).
				WithMarginLeft(0.47).
				WithMarginRight(0.47).
				WithDisplayHeaderFooter(true).
				WithHeaderTemplate("<span></span>").
				WithFooterTemplate(footer).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChromiumUnavailable, err)
	}
	return out, nil
}
```

- [ ] **Step 3: Write the test**

Create `internal/book/pdf_test.go`:

```go
package book

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// Skips rather than fails without a browser, so `go test ./...` stays green on a machine
// with no Chromium -- the same contract the integrity suite has with TEST_DATABASE_URL. A
// skipped test is not a passing test; run it with a browser before calling this done.
func TestPrintPDFProducesAPDF(t *testing.T) {
	if _, err := exec.LookPath("chromium"); err != nil {
		if _, err := exec.LookPath("google-chrome"); err != nil {
			t.Skip("no chromium on PATH")
		}
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
```

- [ ] **Step 4: Verify the artifact, not the exit code**

`%PDF-` proves bytes came back. It does not prove the document is a book. Print a real one
and read it back before committing.

```bash
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/book/ -run PDF -v

# Write a real book to disk. The helper is a throwaway main; delete it afterwards.
mkdir -p /tmp/madamgy-book
go run ./cmd/server &            # or use the endpoints once Task 7 lands
curl -s localhost:8080/api/books/<childID>/book1.pdf -o /tmp/madamgy-book/book1.pdf

pdfinfo /tmp/madamgy-book/book1.pdf          # page count, and page size must read 595 x 842 pt
pdftotext /tmp/madamgy-book/book1.pdf -      # the provisional banner must appear in the text layer
pdftoppm -png -r 110 /tmp/madamgy-book/book1.pdf /tmp/madamgy-book/p
```

Then open each `/tmp/madamgy-book/p-*.png` and compare it, page by page, against
`data/book-engine-spec/MadamGY_Book1_Visual_Prototype_V1.pdf`. Six checks, each of which
fails visibly rather than silently:

| Check | What a failure looks like |
|---|---|
| Page size 595 x 842 pt | A4 was not applied; margins will be wrong at print |
| The provisional banner is in the **text layer**, on every page | It rendered as an image, or a section suppressed it |
| The teal section band, the zebra table, the cream callout | The stylesheet did not load; Chrome printed unstyled HTML |
| Bengali ingredient names show conjuncts and repositioned matras | The font fell back; the names are subtly wrong, which is the whole reason for this renderer |
| The footer carries the three release fields and a page number | `DisplayHeaderFooter` did not take |
| No blank trailing page | A page-break rule is off by one |

Attach the page count and the six verdicts to the task report. "It produced a PDF" is not a
verification result.

```bash
git add internal/book/pdf.go internal/book/pdf_test.go go.mod go.sum
git commit -m "Print books to PDF with headless Chromium

Header and footer come from Chrome's print templates rather than from CSS,
because only the print engine knows the total page count. The footer carries
the three release fields the contract names plus the page number.

A missing browser is reported plainly rather than falling back to another
renderer. A fallback that cannot shape Bengali would produce a book whose
ingredient names are subtly wrong, and every one of the 406 ingredients
carries a Bengali name."
```

---

## Task 7: Preview and download endpoints

**Files:**
- Create: `internal/api/handlers/books.go`
- Create: `internal/api/handlers/books_test.go`
- Modify: `internal/api/router.go`

**Interfaces:**
- Consumes: `book.AssembleBook1`, `book.AssembleBook2`, `book.RenderHTML`, `book.PrintPDF`,
  `book.ErrBlocked`.
- Produces: `GET /api/books/{childID}/{book}/preview` (HTML) and
  `GET /api/books/{childID}/{book}.pdf`, where `{book}` is `book1` or `book2`.

- [ ] **Step 1: Write the handlers**

The preview handler returns the HTML directly with `Content-Type: text/html`. The download
handler pipes the same HTML through `PrintPDF`. Both return **409 Conflict** with the block
reason when `AssembleBook2` returns `ErrBlocked`, and **503** when Chromium is unavailable -
distinct codes, because one is a clinical stop and the other is an operational fault, and an
operator must not read the second as the first.

Both responses carry the skipped-facts list in an `X-Book-Omissions` header, so a reviewer
sees what the book does not contain without reading the whole document.

**Wire format, fixed here because Task 8 parses it.** The header is a `; `-separated list of
skipped facts, each already a human-readable sentence from the assembler. Error bodies are
`{"error": "<reason>"}`, and the 409 body additionally carries `{"reviewer": "<name>"}` read
from `special_care_condition_gate.mandatory_reviewer` -- an operator stopped by a clinical
gate needs to know who to escalate to, and a stop with no named reviewer is a dead end.

- [ ] **Step 2: Write the tests**

```go
// A clinical stop and a broken renderer must not look alike to an operator.
func TestBlockedChildGets409NotAnEmptyBook(t *testing.T) { /* ... */ }

// Preview and download must render the same document, so review approves what prints.
func TestPreviewAndPdfShareTheSameHtml(t *testing.T) { /* ... */ }
```

Write both out in full following the pattern in `internal/api/handlers/profiles_test.go`.

- [ ] **Step 3: Register the routes, verify, commit**

```go
	r.Get("/api/books/{childID}/{book}/preview", h.BookPreview)
	r.Get("/api/books/{childID}/{book}.pdf", h.BookDownload)
```

```bash
go build ./... && go vet ./... && TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
git add internal/api/handlers/books.go internal/api/handlers/books_test.go internal/api/router.go
git commit -m "Serve book previews and PDFs

Preview returns the same HTML the PDF is printed from, so a reviewer
approves the artifact that ships rather than a second rendering of it.

A blocked engine returns 409 with the clinical reason; an unavailable
renderer returns 503. Distinct codes on purpose: one is a clinician's stop
and the other is a broken dependency, and an operator must never read the
second as the first."
```

---

## Task 8: Generating a book from the console

Task 7 makes a book reachable by URL. That is not the same as reachable by an operator. This
task adds the console screen that generates, previews and downloads one, and fixes the two
integration defects that stand between the browser and those endpoints.

**Both defects are in `internal/api/router.go`'s CORS options and are real today**, not
hypothetical:

1. `AllowedMethods` is `{"GET", "POST", "OPTIONS"}`. `PUT /api/profiles/{childID}` is
   registered on the router, and `web/src/lib/api.ts`'s `putProfile` sends it with
   `Content-Type: application/json`, which forces a preflight. The preflight is rejected, so
   saving a profile from the browser cannot work. Pre-existing; it lands here because this is
   the task that verifies the console against the running API.
2. `ExposedHeaders` is unset. A browser can only read simple response headers, so the
   `X-Book-Omissions` header Task 7 sets is invisible to `fetch` no matter what the server
   sends. The omissions list is the honest-gap rule applied to books; a header the frontend
   cannot read is the same as not sending it.

**Files:**
- Create: `web/src/app/books/page.tsx`
- Create: `web/src/components/book-generator.tsx`
- Create: `web/src/components/book-generator.test.tsx`
- Modify: `web/src/lib/api.ts` (append)
- Modify: `web/src/components/app-sidebar.tsx:17-25` (one route entry)
- Modify: `internal/api/router.go:23-28` (CORS options)

**Interfaces:**
- Consumes: `GET /api/books/{childID}/{book}/preview` and
  `GET /api/books/{childID}/{book}.pdf` from Task 7, plus the `X-Book-Omissions` header and
  the 409 / 503 status codes it defines.
- Produces: nothing later tasks consume. This is the last task.

- [ ] **Step 1: Fix the CORS options**

In `internal/api/router.go`, replace the `cors.Options` literal with:

```go
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"}, // internal tool, no browser cookie auth to protect; tighten if this ever leaves a private network
		AllowedMethods: []string{"GET", "POST", "PUT", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		// A browser hides every non-simple response header unless it is named here. The
		// omissions list is what a book does not contain, so a frontend that cannot read
		// it would render a book as though nothing had been left out.
		ExposedHeaders: []string{"X-Book-Omissions"},
		MaxAge:         300,
	}))
```

- [ ] **Step 2: Write the failing frontend test**

Create `web/src/components/book-generator.test.tsx`. The three properties worth pinning are
the ones that carry meaning: a clinical stop must not look like a broken renderer, and an
omission must be visible.

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { BookGenerator } from "./book-generator";

function mockFetch(init: { status: number; body?: string; omissions?: string }) {
  return vi.fn().mockResolvedValue({
    ok: init.status >= 200 && init.status < 300,
    status: init.status,
    headers: { get: (k: string) => (k === "X-Book-Omissions" ? init.omissions ?? null : null) },
    text: async () => init.body ?? "",
    json: async () => JSON.parse(init.body ?? "{}"),
  });
}

beforeEach(() => { vi.stubGlobal("fetch", mockFetch({ status: 200, body: "<html></html>" })); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("BookGenerator", () => {
  it("renders a clinical stop, not an error, when the engine blocks the child", async () => {
    vi.stubGlobal("fetch", mockFetch({
      status: 409,
      body: JSON.stringify({ error: "Down syndrome is a STOP-REVIEW condition", reviewer: "Pediatrician + dietitian" }),
    }));
    render(<BookGenerator />);
    await userEvent.type(screen.getByLabelText(/child id/i), "C-1");
    await userEvent.click(screen.getByRole("button", { name: /preview/i }));

    expect(await screen.findByText(/STOP-REVIEW/)).toBeInTheDocument();
    expect(screen.getByText(/Pediatrician \+ dietitian/)).toBeInTheDocument();
    // A stop is a clinical decision, so the operator is never offered the artifact anyway.
    expect(screen.queryByRole("button", { name: /download pdf/i })).toBeNull();
  });

  it("distinguishes an unavailable renderer from a clinical stop", async () => {
    vi.stubGlobal("fetch", mockFetch({ status: 503, body: JSON.stringify({ error: "headless chromium unavailable" }) }));
    render(<BookGenerator />);
    await userEvent.type(screen.getByLabelText(/child id/i), "C-1");
    await userEvent.click(screen.getByRole("button", { name: /preview/i }));

    expect(await screen.findByText(/renderer unavailable/i)).toBeInTheDocument();
    expect(screen.queryByText(/STOP-REVIEW/)).toBeNull();
    expect(screen.queryByText(/clinical/i)).toBeNull();
  });

  it("lists every omission the API reported", async () => {
    vi.stubGlobal("fetch", mockFetch({
      status: 200,
      body: "<html><body>book</body></html>",
      omissions: "B1-009 vaccination schedule: no drafted text permitted; B1-014 development by age: no drafted text permitted",
    }));
    render(<BookGenerator />);
    await userEvent.type(screen.getByLabelText(/child id/i), "C-1");
    await userEvent.click(screen.getByRole("button", { name: /preview/i }));

    expect(await screen.findByText(/B1-009 vaccination schedule/)).toBeInTheDocument();
    expect(screen.getByText(/B1-014 development by age/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run the test and watch it fail**

```bash
cd web && npm test -- book-generator
```

Expected: FAIL, `Failed to resolve import "./book-generator"`.

- [ ] **Step 4: Add the API client functions**

`request<T>` in `web/src/lib/api.ts` parses JSON and discards headers, so neither book
endpoint can use it. Append to `web/src/lib/api.ts`:

```ts
/**
 * A book preview, plus what the book does not contain. `omissions` comes from the
 * X-Book-Omissions header rather than the body, because the body is the document itself.
 * An empty array means the API reported no omissions -- never that none were checked.
 */
export interface BookPreview {
  html: string;
  omissions: string[];
}

/** Thrown when the engine stops generation for a clinical reason. Distinct from ApiError
 *  because an operator must never read a clinical stop as a fault. */
export class BookBlockedError extends Error {
  constructor(message: string, public reviewer?: string) {
    super(message);
    this.name = "BookBlockedError";
  }
}

/** Thrown when the print pipeline has no browser. An operational fault, not a clinical one. */
export class RendererUnavailableError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RendererUnavailableError";
  }
}

async function bookError(res: Response): Promise<Error> {
  const body = await res.json().catch(() => ({ error: res.statusText }));
  if (res.status === 409) return new BookBlockedError(body.error ?? res.statusText, body.reviewer);
  if (res.status === 503) return new RendererUnavailableError(body.error ?? res.statusText);
  return new ApiError(res.status, body.error ?? res.statusText);
}

function omissionsOf(res: Response): string[] {
  const header = res.headers.get("X-Book-Omissions");
  if (!header) return [];
  return header.split(";").map((s) => s.trim()).filter(Boolean);
}

export async function getBookPreview(childID: string, book: "book1" | "book2"): Promise<BookPreview> {
  const res = await fetch(`${BASE_URL}/api/books/${encodeURIComponent(childID)}/${book}/preview`);
  if (!res.ok) throw await bookError(res);
  return { html: await res.text(), omissions: omissionsOf(res) };
}

export async function getBookPdf(childID: string, book: "book1" | "book2"): Promise<Blob> {
  const res = await fetch(`${BASE_URL}/api/books/${encodeURIComponent(childID)}/${book}.pdf`);
  if (!res.ok) throw await bookError(res);
  return res.blob();
}
```

`ApiError` is currently declared but not exported. Change its declaration to
`export class ApiError extends Error` so `bookError` can be read by a caller that wants to
distinguish it; nothing else changes.

- [ ] **Step 5: Write the component**

Create `web/src/components/book-generator.tsx`. The preview goes in a sandboxed `iframe`
with `srcDoc`, not into the page: the book carries its own stylesheet and print sizing, and
letting that into the console's DOM would corrupt both. `sandbox=""` with no tokens blocks
scripts, forms and same-origin access, which is right for a document the operator has not
reviewed yet.

```tsx
"use client";

import { useState } from "react";
import {
  getBookPreview, getBookPdf, BookBlockedError, RendererUnavailableError,
} from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Badge } from "@/components/ui/badge";

type Blocked = { kind: "blocked"; message: string; reviewer?: string };
type Unavailable = { kind: "unavailable"; message: string };
type Failed = { kind: "failed"; message: string };
type Problem = Blocked | Unavailable | Failed;

export function BookGenerator() {
  const [childID, setChildID] = useState("");
  const [book, setBook] = useState<"book1" | "book2">("book1");
  const [html, setHtml] = useState<string | null>(null);
  const [omissions, setOmissions] = useState<string[]>([]);
  const [problem, setProblem] = useState<Problem | null>(null);
  const [busy, setBusy] = useState(false);

  function classify(err: unknown): Problem {
    if (err instanceof BookBlockedError) {
      return { kind: "blocked", message: err.message, reviewer: err.reviewer };
    }
    if (err instanceof RendererUnavailableError) {
      return { kind: "unavailable", message: err.message };
    }
    return { kind: "failed", message: err instanceof Error ? err.message : String(err) };
  }

  async function preview() {
    setBusy(true);
    setProblem(null);
    setHtml(null);
    setOmissions([]);
    try {
      const result = await getBookPreview(childID, book);
      setHtml(result.html);
      setOmissions(result.omissions);
    } catch (err) {
      setProblem(classify(err));
    } finally {
      setBusy(false);
    }
  }

  async function download() {
    setBusy(true);
    setProblem(null);
    try {
      const blob = await getBookPdf(childID, book);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${childID}-${book}.pdf`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setProblem(classify(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1">
          <Label htmlFor="child-id" className="text-xs uppercase tracking-wide">Child ID</Label>
          <Input
            id="child-id"
            className="w-56 font-mono"
            value={childID}
            onChange={(e) => setChildID(e.target.value)}
            placeholder="C-0001"
          />
        </div>
        <Tabs value={book} onValueChange={(v) => setBook(v as "book1" | "book2")}>
          <TabsList>
            <TabsTrigger value="book1">Book 1 &middot; daily life</TabsTrigger>
            <TabsTrigger value="book2">Book 2 &middot; recipes</TabsTrigger>
          </TabsList>
        </Tabs>
        <Button onClick={preview} disabled={!childID || busy}>Preview</Button>
        {html !== null && (
          <Button variant="outline" onClick={download} disabled={busy}>Download PDF</Button>
        )}
      </div>

      {problem?.kind === "blocked" && (
        <Alert className="border-[var(--color-blocked,theme(colors.amber.600))]">
          <AlertTitle>Generation stopped by a clinical rule</AlertTitle>
          <AlertDescription className="space-y-1">
            <p>{problem.message}</p>
            {problem.reviewer && <p className="font-mono text-xs">Reviewer: {problem.reviewer}</p>}
            <p className="text-xs text-muted-foreground">
              This is the provider&apos;s stop gate, not a fault. There is no override.
            </p>
          </AlertDescription>
        </Alert>
      )}

      {problem?.kind === "unavailable" && (
        <Alert variant="destructive">
          <AlertTitle>Renderer unavailable</AlertTitle>
          <AlertDescription>
            <p>{problem.message}</p>
            <p className="text-xs">
              An operational fault in the print pipeline. Nothing about this child changed.
            </p>
          </AlertDescription>
        </Alert>
      )}

      {problem?.kind === "failed" && (
        <Alert variant="destructive">
          <AlertTitle>Request failed</AlertTitle>
          <AlertDescription>{problem.message}</AlertDescription>
        </Alert>
      )}

      {omissions.length > 0 && (
        <div className="rounded border p-3">
          <div className="mb-2 flex items-center gap-2">
            <Badge variant="outline">{omissions.length} omitted</Badge>
            <span className="text-xs text-muted-foreground">
              Facts the book does not contain, reported by the assembler.
            </span>
          </div>
          <ul className="space-y-1 font-mono text-xs">
            {omissions.map((o) => <li key={o}>{o}</li>)}
          </ul>
        </div>
      )}

      {html !== null && (
        <iframe
          title="Book preview"
          sandbox=""
          srcDoc={html}
          className="h-[80vh] w-full rounded border bg-white"
        />
      )}
    </div>
  );
}
```

- [ ] **Step 6: Write the page and register the route**

Create `web/src/app/books/page.tsx`:

```tsx
import { BookGenerator } from "@/components/book-generator";
import { PageHeader } from "@/components/page-header";

export default function BooksPage() {
  return (
    <div>
      <PageHeader
        title="Books"
        description="Generate, preview and download a child's two books. Every page carries the provider's Draft status; nothing here is approved."
      />
      <BookGenerator />
    </div>
  );
}
```

If `@/components/page-header` does not exist in the tree, use a plain `<h1>` with the same
two strings rather than creating it -- it belongs to work outside this plan.

In `web/src/components/app-sidebar.tsx`, add `BookMarked` to the `lucide-react` import and
one entry to `routes`, after the engine console:

```tsx
  { href: "/books", label: "Books", icon: BookMarked },
```

- [ ] **Step 7: Run the tests**

```bash
cd web && npm test
```

Expected: PASS, including the three new cases.

- [ ] **Step 8: Verify end to end against a running stack**

Unit tests with a stubbed `fetch` prove the component's branches. They do not prove the
console can reach the API, that CORS lets the header through, or that the PDF the browser
downloads is the document the preview showed. Do all four by hand:

```bash
# terminal 1
DATABASE_URL=$(scripts/dev_db.fish url) go run ./cmd/server
# terminal 2
cd web && npm run dev
```

1. Open `http://localhost:3000/books`, enter a child id that exists, click Preview. The
   iframe must show a formatted book -- the teal band, the zebra table, the provisional
   banner -- not raw markup and not a blank frame.
2. Open the browser devtools network tab and confirm the preview response carries
   `Access-Control-Expose-Headers: X-Book-Omissions`. Without it the omissions list renders
   empty while the server is sending one, which is a silent gap.
3. Click Download PDF. The file must open in a viewer, and its first page must match what
   the iframe showed.
4. Enter a child whose profile carries a special-care condition. The clinical stop must
   render, and the Download PDF button must not be present.

Record what you saw for each of the four in the report. "Ran it and it worked" is not a
result; name the child id, the page count and the block reason you actually saw.

- [ ] **Step 9: Commit**

```bash
git add web/src/app/books web/src/components/book-generator.tsx \
        web/src/components/book-generator.test.tsx web/src/lib/api.ts \
        web/src/components/app-sidebar.tsx internal/api/router.go
git commit -m "Generate and preview books from the console

The preview renders in a sandboxed iframe rather than in the page: the book
carries its own stylesheet and print sizing, and letting that into the
console's DOM would corrupt both documents.

A clinical stop and an unavailable renderer render differently and carry
different words. An operator who reads a provider stop gate as a broken
service will retry it, and a stop is not a thing to retry.

Two CORS defects fixed alongside. PUT was missing from AllowedMethods, so
saving a profile from the browser failed its preflight. X-Book-Omissions was
not exposed, so the list of facts a book omits was invisible to the frontend
no matter what the server sent."
```

---

## Not in this plan, and why

- **A generation job state machine, release records and review gates.** The SRS's 11-state
  machine and five review gates are the governance layer around this renderer. This plan
  builds the thing that produces a document; that plan builds the process that approves one.
  Doing both at once would mean designing the approval flow before anyone has seen a
  generated page.
- **Multilingual output.** The tokens include an Indic font stack because ingredient names
  are already bilingual, but translated body content is a separate provider deliverable.
- **Photography and illustration.** The contract permits both and requires "curated
  human-designed or approved assets; not random AI images". There are no assets, so the
  design is type and rule only - which is what both prototypes actually are.
- **Filling the meal-category mapping.** Task 1 builds the mechanism and seeds only what the
  provider asserted. The three open mappings are theirs to rule on.
- **Writing preparation text.** 6 unique method texts across 940 recipes is `GAP-001`, a
  provider authoring problem. Book 2 surfaces the boilerplate on the card rather than
  papering over it.

## Self-review

**Spec coverage.** All 19 template ids from the contract have a home: 10 in Task 4, 9 in
Task 5. The global tokens are Task 2. Both JSON schemas are Task 3 and Task 5. The
accessibility floors are in `tokens.css`. `unicode_multilingual` drove the renderer decision.
`release_footer_fields` are in Task 6's footer template. The prototypes' visible components -
cover, section band, comparison table, card row, three callout severities, writing lines,
footer - are each implemented.

**Placeholder scan.** Two deliberate elisions, both marked and both with the pattern named:
Task 5 Step 2's meal-section loop (pattern: `internal/api/handlers/recipes.go`) and Task 7's
tests (pattern: `internal/api/handlers/profiles_test.go`). Task 4 Step 5 describes six
templates rather than writing them out; each is explicitly the same shape as one written in
full, and the instruction is to write them out rather than alias them.

**Type consistency.** `Metadata`, `ChildSummary`, `Section`, `Row`, `Card`, `Callout`,
`RecipeCard`, `MealSection`, `Book1`, `Book2` are defined in Tasks 3 and 5 and used
consistently after. `RenderHTML(w, kind, meta, data)` has one signature throughout.
`book.Kind1`/`Kind2` match the CSS class names `book1`/`book2`.

**Known risk.** `RecipeCard.RecipeVersion` is required by the provider's schema and has no
source column. Task 5 populates it from the import content hash and notes `GAP-024`; that gap
row is not written by any task in this plan. Whoever implements Task 5 should add it in that
commit rather than leaving the reference dangling.

**Frontend reach (added after Task 2).** Tasks 1-7 stop at a URL. Task 8 is what makes a
book reachable by an operator rather than by curl, and it carries the two CORS defects that
stand between the browser and those endpoints - a missing `PUT` in `AllowedMethods` that
already breaks profile saving today, and an unexposed `X-Book-Omissions` that would render
the omissions list empty while the server was sending one. Task 6's verification was
rewritten at the same time: the original step asserted a `%PDF-` prefix, which proves bytes
came back and nothing about whether the document is a book. It now reads the printed pages
back through `pdfinfo`, `pdftotext` and `pdftoppm` and compares them against the provider's
prototype, with six named checks whose failures are visible.
