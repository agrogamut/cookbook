# Book 1 Content Layer and Canonical Child Profile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Import all nine sheets of `MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx` into
Postgres, and add the persisted child profile - including dated growth measurements and
three-state allergy - that every Book 1 page depends on.

**Architecture:** Two migrations and one new Go package. The nine Book 1 tables use the
existing importer verbatim: header-matching against `information_schema`, upsert plus
sweep, content hashing. No parser is written for any provider column. The child profile is
a **new persistence layer beside** `models.ChildProfile`, not a change to it - the stored
row holds `date_of_birth` and derives `age_months` at query time, so a book generated today
and read in six months does not carry a stale age on every page. Two engine changes follow
from the profile: suspected allergens rank down rather than filter, and expired acute
conditions stop driving a nutrition target.

**Tech Stack:** Go 1.25, pgx v5 (`pgxpool`), golang-migrate, excelize v2 (via
`internal/xlsx`), chi v5.

**Spec:** `docs/superpowers/specs/2026-08-18-phase-3-foundation-design.md`, sections 6 and
7. Every header, row count, primary key and value range below was read live from the
workbook, not inferred.

**Depends on:** `docs/superpowers/plans/2026-08-18-engine-honesty-and-inputs.md`. That plan
adds migration `0012`, so this one starts at `0013`. Nothing else here needs it.

## Global Constraints

- Go 1.25. `go build ./...`, `go vet ./...` and `go test ./...` must stay green.
- **The migration DDL is the single source of truth for column names and types.** A header
  with no matching column, or a `NOT NULL` column with no matching header, is an error, not
  a silent drop. Never add a column the workbook does not have.
- **Never parse a provider guidance column.** `nutrition_target_link`, `clinical_rule_link`
  and `safety_link` are text written for a human. Store verbatim, render verbatim.
- **Hard rule: never invent data.** No block variant, no milestone, no vaccine date, no
  growth measurement is written by this codebase. Every row comes from the workbook or from
  a clinician through the profile tables.
- Errors wrapped `fmt.Errorf("...: %w", err)` at each boundary.
- Table-driven tests, package-local. The DB-backed suites need `TEST_DATABASE_URL`.
- No emoji anywhere. No attribution trailers on any commit.
- Re-running the importer twice must produce identical `content_hash` values per table.

---

## Ground truth read from the workbook (cite, do not re-derive)

```
File: data/provider/MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx
Nine sheets. Header rows are MIXED 3 and 4 -- never assume.

sheet                        hdr  data rows  primary key       distinct/blank/dup
Book 1 Content Master         4     32       Block_ID          32 / 0 / none
IAP Vaccination 2025          4     44       Schedule_ID       44 / 0 / none
Development Milestones        4     33       Milestone_ID      33 / 0 / none
Parent Monitoring Templates   3     18       Template_ID       18 / 0 / none
Illness Feeding Content       3      5       Illness_Block_ID   5 / 0 / none
Book Assembly Logic           3     16       Order             16 / 0 / none
Evidence Register             3     13       Source_ID         13 / 0 / none
Review Release Checklist      3     15       Check_ID          15 / 0 / none
Daily Life Development        4     13       DailyLife_ID      13 / 0 / none

xlsx.Snake() lowercases and collapses non-alphanumerics. It does NOT split camel case:
  DailyLife_ID -> dailylife_id      (not daily_life_id)
  AI_Can_Draft -> ai_can_draft
  Pass_YN      -> pass_yn
  Book1_Display-> book1_display
  URL          -> url

Reserved / type-name identifiers among the columns: "order", "time", "input", "output",
"parameter", "section", "domain", "reference", "comments", "status", "owner", "area".
The importer wraps every identifier in pgx.Identifier{}.Sanitize(), so these work -- but
the DDL must quote "order" and "time" itself.

Numeric columns, verified against every cell:
  Age_From_Mo, Age_To_Mo   integer  values {0,6,12,18,36,120} / {36,72,120,144,216,228}
  Book_Order               integer  1..32, and NOT in Block_ID order
  Age_Months (milestones)  integer  13 distinct
  Order (assembly)         integer  1-11, 15-19; nothing numbered 12, 13 or 14 exists
  Age_Min_Months           TEXT     contains 'Varies', 'Any', and 1.5 / 2.5 / 3.5
  Age_Max_Months           TEXT     same -- the 6/10/14-week doses are fractional months

Flag columns with closed vocabularies:
  AI_Can_Draft            {Y, N}  -- N on B1-009, B1-011, B1-012, B1-014, B1-022
  Blocks_Release_if_Fail  {Y}     -- all 15 rows
  Pass_YN                 blank on all 15 rows (the checklist is unrun)
  Status (content blocks) {Draft} -- all 32 rows

Book_Order vs Block_ID: B1-023..B1-032 (daily-life) occupy positions 15-24, pushing
B1-015..B1-022 to 25-32. Sorting by id gives the wrong book.
```

---

### Task 1: Migration `0013` - the nine Book 1 tables

**Files:**
- Create: `internal/db/migrations/0013_book1_content.up.sql`
- Create: `internal/db/migrations/0013_book1_content.down.sql`

**Interfaces:**
- Produces: tables `book1_content_block`, `book1_vaccine_schedule`,
  `book1_development_milestone`, `book1_monitoring_template`,
  `book1_illness_feeding_block`, `book1_assembly_step`, `book1_evidence_source`,
  `book1_release_check`, `book1_daily_life_module`. Task 2 binds sheets to them; Task 3
  asserts them; Task 4 serves `book1_content_block`.

- [ ] **Step 1: Write `internal/db/migrations/0013_book1_content.up.sql`**

```sql
-- Book 1 Content Master, all nine sheets.
--
-- CLAUDE.md described this workbook as "32 blocks, 44 vaccine rows, 33 milestone rows" and
-- internal/importer/spec.go excluded it as "PDF assembly, not consumed by the web engine".
-- Both are understatements: the workbook also carries a Book Assembly Logic sheet (the
-- Book 1 analogue of Book 2's Recipe Selection Logic), a release checklist, an evidence
-- register, parent-writable monitoring templates and the daily-life modules. It is the
-- entire general content layer of output Book 1.
--
-- Columns are verbatim from the workbook headers as normalised by xlsx.Snake(). Types are
-- text unless every cell in the column parses as a whole number, matching the provider
-- layer's existing convention -- migration 0001 declares no date and no boolean anywhere,
-- because a provider column is stored as shipped and interpreted later.
--
-- Three columns break that convention deliberately, and each says why at the column.

-- ---------------------------------------------------------------------------
-- The block registry. 32 rows. This is the conditional-firing table: which sections
-- of a child's Book 1 exist at all.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_content_block (
    block_id                text PRIMARY KEY,
    part                    text,
    section                 text,
    subsection              text,
    age_from_mo             integer,
    age_to_mo               integer,
    trigger_or_eligibility  text,
    content_purpose         text,
    parent_facing_output    text,
    personalization_inputs  text,
    table_or_format         text,
    writable_fields         text,
    monitoring_fields       text,
    ideal_vs_actual         text,
    alarm_or_red_flag_block text,
    doctor_approach_block   text,
    nutrition_target_link   text,
    clinical_rule_link      text,
    safety_link             text,
    source_id               text,
    -- Deliberate deviation from the provider layer's all-text convention. This is a
    -- generation gate, not a description: the provider marks five blocks (B1-009
    -- vaccination schedule, B1-011 milestone surveillance, B1-012 developmental red
    -- flags, B1-014 development by age, B1-022 reference and disclaimer) as blocks no
    -- drafted text may occupy. A plain text column lets a future value that is neither
    -- Y nor N read as "not N" and silently open the gate. The CHECK makes an unexpected
    -- value fail the import loudly, which is the correct failure mode for a safety gate.
    ai_can_draft            text NOT NULL CHECK (ai_can_draft IN ('Y', 'N')),
    human_approval          text,
    -- Render order. NOT block-id order: B1-023..B1-032 occupy positions 15-24 and push
    -- B1-015..B1-022 to 25-32. Anything that assembles a book sorts by this.
    book_order              integer NOT NULL,
    status                  text
);

COMMENT ON TABLE book1_content_block IS
    'The 32 Book 1 content blocks with their firing conditions, personalization inputs '
    'and per-block generation gate. Imported verbatim; nutrition_target_link, '
    'clinical_rule_link and safety_link are guidance text for a human, never foreign keys.';

COMMENT ON COLUMN book1_content_block.trigger_or_eligibility IS
    'Free text stating when this block fires, e.g. "Always", "Illness selected", '
    '"At least 2 measurements". 22 distinct values across 32 blocks. Not parsed.';

COMMENT ON COLUMN book1_content_block.personalization_inputs IS
    'Semicolon-separated list of the profile facts this block needs. This column is the '
    'empirical specification for the canonical child profile: it says which page goes '
    'blank without each field.';

COMMENT ON COLUMN book1_content_block.nutrition_target_link IS
    'Guidance text, not a foreign key. Holds "NT00" on one row and "NT02/03/04/05", '
    '"All active targets", "Target-specific", "Age-specific" and "N/A" on others -- the '
    'same shape as nutrition_target_master.hard_exclusions. Surfaced verbatim.';

COMMENT ON COLUMN book1_content_block.ai_can_draft IS
    'Y or N. N means no drafted text may occupy this block, ever. Enforced by CHECK here '
    'and asserted by TestAICanDraftGateIsPinned.';

-- ---------------------------------------------------------------------------
-- IAP-ACVIP 2025 immunization schedule. 44 rows. A tracking template, not a catch-up
-- scheduler: the provider's own row 2 note says product-specific, catch-up, high-risk
-- and immunocompromised schedules require pediatrician review.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_vaccine_schedule (
    schedule_id      text PRIMARY KEY,
    age              text,
    -- TEXT, not integer, and this is not laziness. Every cell was checked: the columns
    -- hold 'Varies' and 'Any' as well as 1.5, 2.5 and 3.5, because the 6-, 10- and
    -- 14-week doses are fractional months. Declaring integer makes the import fail;
    -- declaring numeric loses 'Varies'. Verbatim text keeps both, and any numeric range
    -- an assembler needs is a documented derivation over this column, not a retype of it.
    age_min_months   text,
    age_max_months   text,
    vaccine          text,
    dose_or_event    text,
    routine_status   text,
    important_note   text,
    -- The parent-writable columns. Blank in the workbook and blank after import: a dose
    -- nobody recorded renders as an empty writable row, never as an inferred one.
    parent_given_date text,
    "time"           text,
    brand            text,
    batch_no         text,
    clinic_doctor    text,
    aefi_or_reaction text,
    parent_notes     text,
    next_due         text,
    source_id        text,
    review_status    text
);

COMMENT ON TABLE book1_vaccine_schedule IS
    'IAP-ACVIP 2025 schedule, 44 rows. The parent_given_date, time, brand, batch_no, '
    'clinic_doctor, aefi_or_reaction, parent_notes and next_due columns are writable '
    'template fields, empty by design. Never fabricate a date or a reaction.';

-- ---------------------------------------------------------------------------
-- Developmental milestones. 33 rows. Surveillance references, not diagnostic cut-offs.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_development_milestone (
    milestone_id           text PRIMARY KEY,
    age_reference          text,
    age_months             integer,
    domain                 text,
    reference_milestone    text,
    parent_actual_status   text,
    date_first_observed    text,
    parent_example_or_note text,
    ideal_vs_actual_result text,
    concern_or_red_flag    text,
    action_if_concern      text,
    source_basis           text,
    clinical_review_status text
);

COMMENT ON TABLE book1_development_milestone IS
    'Age-referenced milestones for parent surveillance. The provider states these are '
    'not pass/fail diagnostic cut-offs and require developmental-pediatric review before '
    'commercial release. The engine renders reference beside observed and never interprets.';

-- ---------------------------------------------------------------------------
-- Parent-writable monitoring templates. 18 rows. The reference-vs-actual pattern that
-- carries Book 1's personalization.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_monitoring_template (
    template_id               text PRIMARY KEY,
    section                   text,
    parameter                 text,
    reference_or_ideal_column text,
    actual_column             text,
    date_time                 text,
    parent_notes              text,
    alarm_column              text,
    doctor_review_column      text,
    frequency                 text
);

-- ---------------------------------------------------------------------------
-- Illness feeding content. 5 rows.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_illness_feeding_block (
    illness_block_id             text PRIMARY KEY,
    situation                    text,
    supportive_feeding_message   text,
    what_to_monitor              text,
    red_flags_or_approach_doctor text,
    book_engine_limit            text,
    source_id                    text,
    status                       text
);

COMMENT ON COLUMN book1_illness_feeding_block.book_engine_limit IS
    'What the engine must not do for this illness, in the provider''s words, e.g. '
    '"No diagnosis or drug dosing", "Food advice cannot replace ORS/medical assessment".';

-- ---------------------------------------------------------------------------
-- Book 1 assembly pipeline. 16 rows. The Book 1 analogue of Book 2's Recipe Selection
-- Logic sheet, and equally authoritative.
--
-- The sheet numbers its steps 1-11, then 17-19, then 15-16. Nothing numbered 12, 13 or
-- 14 exists in the workbook. Recorded as GAP-014 rather than renumbered: renumbering
-- would hide a hole in the specification.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_assembly_step (
    "order"             integer PRIMARY KEY,
    engine_step         text,
    input               text,
    output              text,
    hard_stop_condition text,
    reviewer            text
);

COMMENT ON TABLE book1_assembly_step IS
    'The provider-authored Book 1 assembly pipeline. Treat as authoritative the way '
    'Book2_Content_Master''s Recipe Selection Logic sheet is treated for the recipe '
    'engine. Steps 12, 13 and 14 are absent from the workbook -- see GAP-014.';

-- ---------------------------------------------------------------------------
-- Evidence register. 13 rows. Sources with URLs and their stated limitations.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_evidence_source (
    source_id            text PRIMARY KEY,
    authority            text,
    topic                text,
    reference            text,
    url                  text,
    how_used             text,
    important_limitation text,
    last_checked         text,
    status               text
);

COMMENT ON COLUMN book1_evidence_source.last_checked IS
    'ISO-shaped date as text, matching the provider layer''s convention of storing dates '
    'verbatim. Migration 0001 declares no date column anywhere for the same reason.';

-- ---------------------------------------------------------------------------
-- Release checklist. 15 rows, all unrun: pass_yn is blank on every row and reviewer_name
-- is empty. That is the honest state, and it is why nothing may ship.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_release_check (
    check_id               text PRIMARY KEY,
    area                   text,
    mandatory_check        text,
    owner                  text,
    pass_yn                text CHECK (pass_yn IS NULL OR pass_yn IN ('Y', 'N')),
    reviewer_name          text,
    review_date            text,
    comments               text,
    -- Same reasoning as ai_can_draft: this decides whether a failed check stops a
    -- release, so an unexpected value must fail the import rather than be interpreted.
    blocks_release_if_fail text CHECK (blocks_release_if_fail IS NULL OR blocks_release_if_fail IN ('Y', 'N'))
);

COMMENT ON TABLE book1_release_check IS
    'The Book 1 release gate, 15 checks. pass_yn is NULL on every row as shipped: no '
    'check has been run. Never populate these locally -- a locally-passed release check '
    'is the clearest possible form of marking unapproved data approved.';

-- ---------------------------------------------------------------------------
-- Daily-life development modules. 13 rows. Toilet, sleep, dental, self-care, screen,
-- activity, school and adolescent self-management.
-- ---------------------------------------------------------------------------
CREATE TABLE book1_daily_life_module (
    dailylife_id                  text PRIMARY KEY,
    domain                        text,
    suggested_age_context         text,
    readiness_or_reference        text,
    parent_actual_status          text,
    date_or_frequency             text,
    parent_example_or_note        text,
    progress_goal                 text,
    concern_or_red_flag           text,
    approach_doctor_or_specialist text,
    book1_display                 text,
    ai_limit                      text,
    review_status                 text
);

COMMENT ON COLUMN book1_daily_life_module.dailylife_id IS
    'Column is dailylife_id, not daily_life_id: xlsx.Snake() collapses non-alphanumerics '
    'but does not split camel case, so the header DailyLife_ID normalises this way. The '
    'DDL matches the workbook one-for-one by design.';

COMMENT ON COLUMN book1_daily_life_module.ai_limit IS
    'Per-module prohibition in the provider''s words, e.g. "Do not diagnose enuresis '
    'automatically", "No psychiatric diagnosis". 13 distinct values, one per module.';
```

- [ ] **Step 2: Write `internal/db/migrations/0013_book1_content.down.sql`**

```sql
DROP TABLE IF EXISTS book1_daily_life_module;
DROP TABLE IF EXISTS book1_release_check;
DROP TABLE IF EXISTS book1_evidence_source;
DROP TABLE IF EXISTS book1_assembly_step;
DROP TABLE IF EXISTS book1_illness_feeding_block;
DROP TABLE IF EXISTS book1_monitoring_template;
DROP TABLE IF EXISTS book1_development_milestone;
DROP TABLE IF EXISTS book1_vaccine_schedule;
DROP TABLE IF EXISTS book1_content_block;
```

- [ ] **Step 3: Verify the migration applies to a clean database**

```bash
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import
psql $DATABASE_URL -c "\dt book1_*"
```

Expected: nine tables listed, all empty (Task 2 fills them). The importer runs migrations
on startup, so no separate migrate step exists.

- [ ] **Step 4: Commit**

```bash
git add internal/db/migrations/0013_book1_content.up.sql internal/db/migrations/0013_book1_content.down.sql
git commit -m "Add the nine Book 1 content tables"
```

---

### Task 2: Bind the nine sheets to the importer

**Files:**
- Modify: `internal/importer/spec.go`

**Interfaces:**
- Consumes: the nine tables from Task 1.
- Produces: nine populated tables after `go run ./cmd/import`. Task 3 asserts the counts.

- [ ] **Step 1: Correct the stale comment above `Specs`**

The existing comment claims Book 1 content blocks are deliberately not imported. Replace
that sentence:

```go
// Sheets deliberately not imported are recorded in gap_register rather than omitted
// silently: the Review/Version Control workbook (an empty scaffold) and the Page Registry
// (a PDF pagination concern with no web consumer).
//
// Book1_Content_Master was in that list until migration 0013. It is not a PDF-assembly
// concern: it is the entire general content layer of output Book 1, including the
// provider's own Book 1 assembly pipeline, and nothing downstream can be built without it.
```

- [ ] **Step 2: Append the nine specs**

Add to the end of the `Specs` slice. Order does not matter here because none of these
tables reference another, but they are listed sheet-order for readability:

```go
	// ---- Book 1 content layer, migration 0013 -------------------------------
	// Header rows are mixed 3 and 4 within this one workbook. Each is declared, never
	// guessed: xlsx.Load errors if the declared row does not hold the expected FirstCol.
	{
		Table: "book1_content_block", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "Book 1 Content Master", HeaderRow: 4,
		PrimaryKey: []string{"block_id"}, FirstCol: "block_id",
	},
	{
		Table: "book1_vaccine_schedule", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "IAP Vaccination 2025", HeaderRow: 4,
		PrimaryKey: []string{"schedule_id"}, FirstCol: "schedule_id",
	},
	{
		Table: "book1_development_milestone", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "Development Milestones", HeaderRow: 4,
		PrimaryKey: []string{"milestone_id"}, FirstCol: "milestone_id",
	},
	{
		Table: "book1_monitoring_template", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "Parent Monitoring Templates", HeaderRow: 3,
		PrimaryKey: []string{"template_id"}, FirstCol: "template_id",
	},
	{
		Table: "book1_illness_feeding_block", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "Illness Feeding Content", HeaderRow: 3,
		PrimaryKey: []string{"illness_block_id"}, FirstCol: "illness_block_id",
	},
	{
		Table: "book1_assembly_step", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "Book Assembly Logic", HeaderRow: 3,
		PrimaryKey: []string{"order"}, FirstCol: "order",
	},
	{
		Table: "book1_evidence_source", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "Evidence Register", HeaderRow: 3,
		PrimaryKey: []string{"source_id"}, FirstCol: "source_id",
	},
	{
		Table: "book1_release_check", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "Review Release Checklist", HeaderRow: 3,
		PrimaryKey: []string{"check_id"}, FirstCol: "check_id",
	},
	{
		Table: "book1_daily_life_module", File: "MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx",
		Sheet: "Daily Life Development", HeaderRow: 4,
		PrimaryKey: []string{"dailylife_id"}, FirstCol: "dailylife_id",
	},
```

No `ScopeColumn` on any of them: Book 1 content is not regional, so the India+Bangladesh
scope filter does not apply.

- [ ] **Step 3: Run the import and read the row counts**

```bash
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import
psql $DATABASE_URL -c "SELECT table_name, rows_read, rows_written, rows_skipped FROM import_table_stat WHERE run_id = (SELECT max(run_id) FROM import_table_stat) AND table_name LIKE 'book1%' ORDER BY table_name"
```

Expected: 32, 44, 33, 18, 5, 16, 13, 15, 13 written, zero skipped.

If the import fails with `column "x" is NOT NULL but sheet has no matching header`, or
`value "Varies" is not an integer`, the DDL is wrong, not the workbook. Fix the type in
migration `0013` and re-run against a fresh database — that loud failure is the importer
working as designed.

- [ ] **Step 4: Confirm idempotency**

```bash
go run ./cmd/import
psql $DATABASE_URL -c "SELECT a.table_name FROM import_table_stat a JOIN import_table_stat b USING (table_name) WHERE a.run_id = (SELECT max(run_id) FROM import_table_stat) AND b.run_id = (SELECT max(run_id) - 1 FROM import_table_stat) AND a.content_hash <> b.content_hash"
```

Expected: zero rows.

- [ ] **Step 5: Commit**

```bash
git add internal/importer/spec.go
git commit -m "Import all nine Book 1 content sheets"
```

---

### Task 3: Book 1 integrity invariants

**Files:**
- Create: `internal/db/book1_test.go`

**Interfaces:**
- Consumes: the nine tables.
- Produces: the assertions any Book 1 assembler will be built against.

- [ ] **Step 1: Write the failing tests**

Create `internal/db/book1_test.go`:

```go
package db

import (
	"context"
	"strings"
	"testing"
)

func TestBook1RowCountsMatchTheWorkbook(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	cases := []struct {
		table string
		want  int
	}{
		{"book1_content_block", 32},
		{"book1_vaccine_schedule", 44},
		{"book1_development_milestone", 33},
		{"book1_monitoring_template", 18},
		{"book1_illness_feeding_block", 5},
		{"book1_assembly_step", 16},
		{"book1_evidence_source", 13},
		{"book1_release_check", 15},
		{"book1_daily_life_module", 13},
	}
	for _, c := range cases {
		t.Run(c.table, func(t *testing.T) {
			var n int
			// Table names come from this test's own literal list, never from input.
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+c.table).Scan(&n); err != nil {
				t.Fatalf("count %s: %v", c.table, err)
			}
			if n != c.want {
				t.Fatalf("%s holds %d rows, workbook has %d", c.table, n, c.want)
			}
		})
	}
}

// TestAICanDraftGateIsPinned fixes the five blocks the provider marks as blocks no
// drafted text may occupy. It is written before any assembler exists, deliberately: a
// constraint that predates the code it constrains is cheap, and one retrofitted after an
// assembler is written is an argument.
func TestAICanDraftGateIsPinned(t *testing.T) {
	pool := testPool(t)
	rows, err := pool.Query(context.Background(),
		`SELECT block_id FROM book1_content_block WHERE ai_can_draft = 'N' ORDER BY block_id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// B1-009 vaccination schedule, B1-011 milestone surveillance, B1-012 developmental
	// red flags, B1-014 development by age, B1-022 reference and disclaimer.
	want := []string{"B1-009", "B1-011", "B1-012", "B1-014", "B1-022"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ai_can_draft = 'N' blocks are %v, want %v. If the provider changed this "+
			"set, update the list here in the same commit that re-imports -- never widen "+
			"it silently, because widening it permits drafted text into a block the "+
			"provider closed.", got, want)
	}
}

func TestBookOrderIsCompleteAndNotBlockIDOrder(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Every position 1..32 used exactly once, so an assembler sorting by book_order
	// produces a book with no hole and no collision.
	var distinct, minOrder, maxOrder int
	err := pool.QueryRow(ctx,
		`SELECT count(DISTINCT book_order), min(book_order), max(book_order) FROM book1_content_block`).
		Scan(&distinct, &minOrder, &maxOrder)
	if err != nil {
		t.Fatalf("order stats: %v", err)
	}
	if distinct != 32 || minOrder != 1 || maxOrder != 32 {
		t.Fatalf("book_order is not a complete 1..32 permutation: distinct=%d min=%d max=%d",
			distinct, minOrder, maxOrder)
	}

	// And it genuinely differs from id order, so a future refactor that "simplifies" by
	// sorting on block_id is caught here rather than by a reader of a wrong book.
	var mismatches int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT block_id,
			       row_number() OVER (ORDER BY block_id)  AS by_id,
			       row_number() OVER (ORDER BY book_order) AS by_order
			FROM book1_content_block
		) t WHERE by_id <> by_order`).Scan(&mismatches)
	if err != nil {
		t.Fatalf("order comparison: %v", err)
	}
	if mismatches == 0 {
		t.Fatal("book_order now matches block_id order; the daily-life blocks B1-023..B1-032 " +
			"are supposed to sort into positions 15-24. Verify the import before relaxing this.")
	}
}

// TestGuidanceColumnsAreNotForeignKeys documents why nutrition_target_link is text. If a
// future change makes every value resolve, a mapping table becomes a reasonable choice --
// but it must be a deliberate decision, not an assumption a parser quietly encoded.
func TestGuidanceColumnsAreNotForeignKeys(t *testing.T) {
	pool := testPool(t)
	var unresolvable int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM book1_content_block b
		WHERE b.nutrition_target_link IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM nutrition_target_master n
		      WHERE n.target_code = b.nutrition_target_link)`).Scan(&unresolvable)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if unresolvable == 0 {
		t.Fatal("every nutrition_target_link now resolves to a target_code; the column " +
			"could become a real foreign key. Make that call explicitly rather than " +
			"leaving this test asserting the opposite.")
	}
	t.Logf("%d of 32 blocks carry a nutrition_target_link that is guidance text rather "+
		"than a target code (e.g. \"NT02/03/04/05\", \"All active targets\", \"N/A\")", unresolvable)
}

// TestReleaseChecklistIsUnrun is a safety assertion, not a data assertion. Every check
// ships blank. If one is ever populated, it happened on this side, and a locally-passed
// release check is the clearest possible form of marking unapproved data approved.
func TestReleaseChecklistIsUnrun(t *testing.T) {
	pool := testPool(t)
	var passed int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM book1_release_check WHERE pass_yn IS NOT NULL`).Scan(&passed)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if passed != 0 {
		t.Fatalf("%d release check(s) carry a pass_yn value. The workbook ships all 15 "+
			"blank. Nothing on this side may mark a provider release check as passed.", passed)
	}
}

func TestAssemblyStepsRecordTheMissingRange(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM book1_assembly_step WHERE "order" BETWEEN 12 AND 14`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Fatalf("steps 12-14 now exist (%d rows). The provider filled the hole -- update "+
			"GAP-014 and this test together.", n)
	}
	var gap int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM gap_register WHERE gap_id = 'GAP-014'`).Scan(&gap); err != nil {
		t.Fatalf("gap lookup: %v", err)
	}
	if gap != 1 {
		t.Fatal("the missing assembly steps must be recorded in gap_register as GAP-014")
	}
}
```

- [ ] **Step 2: Run tests, confirm they pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/db/ -run 'TestBook1|TestAICanDraft|TestBookOrder|TestGuidance|TestReleaseChecklist|TestAssemblySteps' -v`

Expected: all PASS. `TestGuidanceColumnsAreNotForeignKeys` logs the unresolvable count.

If `TestBook1RowCountsMatchTheWorkbook` fails, Task 2's import did not complete — do not
change the expected numbers; they were read from the workbook.

- [ ] **Step 3: Commit**

```bash
git add internal/db/book1_test.go
git commit -m "Pin the Book 1 invariants, including the per-block generation gate"
```

---

### Task 4: `GET /api/reference/book1-blocks` and a console tab

**Files:**
- Modify: `internal/api/handlers/reference.go`
- Modify: `internal/api/router.go`
- Modify: `web/src/lib/types.ts`, `web/src/lib/api.ts`, `web/src/app/reference/page.tsx`
- Test: `internal/api/handlers/reference_test.go`

**Interfaces:**
- Produces: `(*Handlers).ReferenceBook1Blocks`. This is the cheapest proof the import
  landed, and the first screen an assembler author will read.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/handlers/reference_test.go`:

```go
func TestReferenceBook1BlocksAreInBookOrder(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/book1-blocks", nil)
	rec := httptest.NewRecorder()

	h.ReferenceBook1Blocks(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		BlockID    string `json:"block_id"`
		BookOrder  int    `json:"book_order"`
		AICanDraft string `json:"ai_can_draft"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("expected 32 blocks, got %d", len(got))
	}
	for i := range got {
		if got[i].BookOrder != i+1 {
			t.Fatalf("row %d has book_order %d; the endpoint must return blocks in render "+
				"order so a client never has to re-sort and never sorts by id", i, got[i].BookOrder)
		}
	}
	var closed int
	for _, b := range got {
		if b.AICanDraft == "N" {
			closed++
		}
	}
	if closed != 5 {
		t.Fatalf("expected 5 blocks closed to drafted text, got %d", closed)
	}
}
```

- [ ] **Step 2: Run test, confirm it fails**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/api/handlers/... -run TestReferenceBook1Blocks -v`

Expected: FAIL, `undefined: h.ReferenceBook1Blocks`.

- [ ] **Step 3: Write the handler**

Append to `internal/api/handlers/reference.go`:

```go
// ReferenceBook1Blocks returns the 32 Book 1 content blocks in render order.
//
// Ordered by book_order, never by block_id: the daily-life blocks B1-023..B1-032 occupy
// positions 15-24, so id order produces a different book. Returning them pre-sorted means
// no client can get that wrong.
//
// The link columns are returned verbatim. They are guidance text for a human, not
// references, and a client renders them as text.
func (h *Handlers) ReferenceBook1Blocks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT block_id, book_order, part, section, subsection,
		       age_from_mo, age_to_mo, trigger_or_eligibility,
		       personalization_inputs, table_or_format,
		       nutrition_target_link, clinical_rule_link, safety_link,
		       ai_can_draft, human_approval, status
		FROM book1_content_block
		ORDER BY book_order`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "book1 block list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type block struct {
		BlockID              string  `json:"block_id"`
		BookOrder            int     `json:"book_order"`
		Part                 *string `json:"part"`
		Section              *string `json:"section"`
		Subsection           *string `json:"subsection"`
		AgeFromMo            *int    `json:"age_from_mo"`
		AgeToMo              *int    `json:"age_to_mo"`
		TriggerOrEligibility *string `json:"trigger_or_eligibility"`
		PersonalizationInput *string `json:"personalization_inputs"`
		TableOrFormat        *string `json:"table_or_format"`
		NutritionTargetLink  *string `json:"nutrition_target_link"`
		ClinicalRuleLink     *string `json:"clinical_rule_link"`
		SafetyLink           *string `json:"safety_link"`
		AICanDraft           string  `json:"ai_can_draft"`
		HumanApproval        *string `json:"human_approval"`
		Status               *string `json:"status"`
	}
	out := []block{}
	for rows.Next() {
		var b block
		if err := rows.Scan(&b.BlockID, &b.BookOrder, &b.Part, &b.Section, &b.Subsection,
			&b.AgeFromMo, &b.AgeToMo, &b.TriggerOrEligibility,
			&b.PersonalizationInput, &b.TableOrFormat,
			&b.NutritionTargetLink, &b.ClinicalRuleLink, &b.SafetyLink,
			&b.AICanDraft, &b.HumanApproval, &b.Status); err != nil {
			writeError(w, http.StatusInternalServerError, "book1 block scan failed: "+err.Error())
			return
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "book1 block rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 4: Register the route**

In `internal/api/router.go`:

```go
	r.Get("/api/reference/book1-blocks", h.ReferenceBook1Blocks)
```

- [ ] **Step 5: Add the frontend type and client function**

In `web/src/lib/types.ts`:

```ts
export interface Book1Block {
  block_id: string;
  book_order: number;
  part: string | null;
  section: string | null;
  subsection: string | null;
  age_from_mo: number | null;
  age_to_mo: number | null;
  trigger_or_eligibility: string | null;
  personalization_inputs: string | null;
  table_or_format: string | null;
  nutrition_target_link: string | null;
  clinical_rule_link: string | null;
  safety_link: string | null;
  ai_can_draft: "Y" | "N";
  human_approval: string | null;
  status: string | null;
}
```

In `web/src/lib/api.ts`:

```ts
export function getBook1Blocks(): Promise<Book1Block[]> {
  return request<Book1Block[]>("/api/reference/book1-blocks");
}
```

- [ ] **Step 6: Add the tab to `web/src/app/reference/page.tsx`**

Add `getBook1Blocks` to the `Promise.all`, a fourth `TabsTrigger`, and this `TabsContent`:

```tsx
      <TabsContent value="book1">
        <p className="mb-2 text-xs text-muted-foreground">
          The 32 Book 1 content blocks in render order (book_order, not block id). Blocks
          marked <span className="font-mono">draft: no</span> are closed to generated text
          by the provider and may only ever carry provider-authored content.
        </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="text-right">#</TableHead>
              <TableHead>Block</TableHead>
              <TableHead>Section</TableHead>
              <TableHead>Fires when</TableHead>
              <TableHead>Needs</TableHead>
              <TableHead>Draft</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {book1Blocks.map((b) => (
              <TableRow key={b.block_id}>
                <TableCell className="text-right font-mono text-xs">{b.book_order}</TableCell>
                <TableCell className="font-mono text-xs">{b.block_id}</TableCell>
                <TableCell className="text-xs">
                  {b.section}
                  {b.subsection && <span className="text-muted-foreground"> / {b.subsection}</span>}
                </TableCell>
                <TableCell className="text-xs">{b.trigger_or_eligibility ?? "not available"}</TableCell>
                <TableCell className="max-w-xs truncate text-xs text-muted-foreground"
                           title={b.personalization_inputs ?? undefined}>
                  {b.personalization_inputs ?? "not available"}
                </TableCell>
                <TableCell>
                  <Badge variant="outline"
                         className={b.ai_can_draft === "N" ? "border-destructive text-destructive" : ""}>
                    {b.ai_can_draft === "N" ? "no" : "yes"}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TabsContent>
```

Import `Badge` from `@/components/ui/badge` in that file if it is not already imported.

- [ ] **Step 7: Verify and commit**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/api/... -v
cd web && pnpm exec tsc --noEmit
```

Then with both servers running, open `/reference`, select the Book 1 tab, and confirm 32
rows in book order with five red "no" badges.

```bash
cd /home/ghoul/graveyard/recipie
git add internal/api/handlers/reference.go internal/api/router.go internal/api/handlers/reference_test.go web/src/lib/types.ts web/src/lib/api.ts web/src/app/reference/page.tsx
git commit -m "Serve the Book 1 block registry in render order"
```

---

### Task 5: Migration `0014` - the canonical child profile

**Files:**
- Create: `internal/db/migrations/0014_child_profile.up.sql`
- Create: `internal/db/migrations/0014_child_profile.down.sql`

**Interfaces:**
- Produces: `child_profile`, `child_growth_measurement`, `child_allergen`,
  `child_preference`, `child_clinical_condition`. Task 6 reads and writes them.

- [ ] **Step 1: Write `internal/db/migrations/0014_child_profile.up.sql`**

```sql
-- The canonical child profile.
--
-- This is a persistence layer BESIDE models.ChildProfile, not a replacement for it.
-- ChildProfile stays the engine's query input; these tables are what a consultation
-- produces and what a book is generated from. The SRS draws the same line, between an
-- immutable child_profile_snapshot and a recipe engine that takes a query.
--
-- The field list is derived from book1_content_block.personalization_inputs -- the
-- provider's own per-block statement of which facts each page needs -- rather than from
-- SRS prose, because that column says which page goes blank without each field.
--
-- Fields deliberately absent:
--   equipment            recipe_master has no equipment column to match against. Storing
--                        an input the engine cannot use is how a form starts lying.
--   z-scores, BMI class  clinician-entered when they arrive, never computed here:
--                        computing one means choosing a growth reference, which is a
--                        clinical decision this project has no basis to make.
--   age_months           derived from date_of_birth at query time. Never stored.

CREATE TABLE child_profile (
    child_id      text PRIMARY KEY,
    case_id       text,

    -- Book 1 schema constrains display_name to 100 characters.
    display_name  text CHECK (display_name IS NULL OR length(display_name) <= 100),

    -- The source of truth for age. A book generated today and read in six months has a
    -- stale age on every page unless age is derived at generation time from this.
    date_of_birth date NOT NULL,

    -- Growth reference selection only. All 13 nutrition targets are
    -- sex_applicability = 'All', so sex never changes recipe ranking and must not be
    -- collected as though it does.
    sex           text CHECK (sex IS NULL OR sex IN ('male', 'female', 'other')),

    language_id   text,
    region_culture text,
    cuisine_code  text,

    -- Family-declared, recorded verbatim, NEVER inferred from region, name or language.
    -- The masters carry religious_cultural_inference_rule precisely to forbid that.
    diet_type            text,
    vegan                boolean NOT NULL DEFAULT false,
    religious_restriction text,

    budget_band       text,
    max_prep_time_min integer,
    max_cook_time_min integer,

    -- Every clinically meaningful row records who set it and when. Release
    -- reproducibility depends on it, and so does the SRS.
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_by text,
    updated_at timestamptz
);

COMMENT ON TABLE child_profile IS
    'One row per child. Holds date_of_birth rather than age: age_months is derived at '
    'query time and stamped with a generation date, so a reader can tell how old the '
    'personalization is.';

COMMENT ON COLUMN child_profile.diet_type IS
    'Family-declared practice. Diet is a nested permission chain (vegan subset vegetarian '
    'subset eggetarian subset non-vegetarian) -- see docs/decisions.md. Never inferred.';

-- ---------------------------------------------------------------------------
-- Growth. Many rows per child, and the trend is the clinical point: a single weight
-- column would destroy the thing Book 1 exists to show. The provider's own prototype
-- puts reference against actual side by side and reserves space for serial measurement.
-- ---------------------------------------------------------------------------
CREATE TABLE child_growth_measurement (
    measurement_id        bigserial PRIMARY KEY,
    child_id              text NOT NULL REFERENCES child_profile(child_id) ON DELETE CASCADE,
    measured_on           date NOT NULL,
    weight_kg             numeric,
    height_cm             numeric,
    head_circumference_cm numeric,

    -- Clinician-entered interpretation. NT03, NT04 and NT05 activate on a z-score the
    -- clinician supplies; nothing here computes one.
    bmi_for_age_z         numeric,
    weight_for_age_z      numeric,
    height_for_age_z      numeric,
    interpretation        text,

    measured_by text NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (child_id, measured_on)
);

CREATE INDEX child_growth_measurement_child_date_idx
    ON child_growth_measurement (child_id, measured_on DESC);

COMMENT ON COLUMN child_growth_measurement.bmi_for_age_z IS
    'Clinician-entered, never computed. Computing a z-score means choosing a growth '
    'reference, which is a clinical decision, not an engineering one.';

-- ---------------------------------------------------------------------------
-- Allergy, in three states rather than a flat list.
--
-- AS-002 marks suspected allergy hard_block = N. Unnecessary elimination is itself a
-- recognised cause of faltering growth, so treating every suspicion as confirmation is a
-- different risk, not the cautious one.
--
-- 'resolved' does not exist in the provider data at all, which means an allergy recorded
-- at age three currently excludes food permanently. Outgrowing milk and egg allergy is
-- routine in pediatrics; this column is the fix.
-- ---------------------------------------------------------------------------
CREATE TABLE child_allergen (
    child_id       text NOT NULL REFERENCES child_profile(child_id) ON DELETE CASCADE,
    allergen_group text NOT NULL,
    status         text NOT NULL CHECK (status IN ('confirmed', 'suspected', 'resolved')),
    severity       text CHECK (severity IS NULL OR severity IN ('mild', 'systemic')),
    source         text NOT NULL CHECK (source IN ('parent_reported', 'clinician_documented')),
    last_reaction_on date,
    entered_by     text NOT NULL,
    entered_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (child_id, allergen_group)
);

COMMENT ON TABLE child_allergen IS
    'confirmed and systemic are hard filters. suspected ranks down and raises a review '
    'flag, never filters. resolved keeps history and excludes nothing. The four allergen '
    'groups with no corpus tag (GAP-013) are unaffected by any of this and must not be '
    'obscured by it: an unscreened group is unscreened whatever its status.';

-- ---------------------------------------------------------------------------
-- Preferences. Family-sourced, ranker only. A picky child with eight dislikes would
-- empty a hard-filtered list -- filter collapse in a new costume.
-- ---------------------------------------------------------------------------
CREATE TABLE child_preference (
    child_id      text NOT NULL REFERENCES child_profile(child_id) ON DELETE CASCADE,
    ingredient_id text NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('like', 'dislike', 'accepted')),
    entered_by    text NOT NULL,
    entered_at    timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (child_id, ingredient_id, kind)
);

COMMENT ON COLUMN child_preference.kind IS
    'accepted means eaten without incident, and feeds Book 2''s tracker rather than the '
    'ranker. Severe aversion is not a dislike: that is CR-FEED-003 and needs feeding-team '
    'input, so it belongs in child_clinical_condition.';

-- ---------------------------------------------------------------------------
-- Clinical conditions, with the time dimension nothing else in the model has.
--
-- A diarrhoea flag entered three weeks ago must stop pushing NT12. Without onset_date and
-- expiry, stale acute flags silently distort every later generation.
-- ---------------------------------------------------------------------------
CREATE TABLE child_clinical_condition (
    child_id      text NOT NULL REFERENCES child_profile(child_id) ON DELETE CASCADE,

    -- Matches clinical_rule_master.trigger_field exactly. No FK is declared, for the same
    -- reason culture_region_map declares none: migrations run before cmd/import populates
    -- the provider tables. Both directions are asserted by the integrity suite instead.
    trigger_field text NOT NULL,
    flag_value    text NOT NULL,

    -- An intake grouping layered over the engine's real question, which is not duration
    -- but action (hold / retarget / constrain / rank -- already encoded in
    -- clinical_rule_master.engine_action). Class stays a UI concern.
    class text NOT NULL CHECK (class IN ('acute', 'chronic', 'congenital')),

    onset_date        date,
    -- Acute conditions only. NULL on an acute condition means the expiry window is
    -- unknown: the condition is reported stale rather than silently applied forever.
    -- The window per class is a clinical value and is outstanding to the provider.
    expires_after_days integer,

    specialist_target_id text,
    entered_by  text NOT NULL,
    entered_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (child_id, trigger_field),
    CHECK (class <> 'acute' OR onset_date IS NOT NULL)
);

COMMENT ON COLUMN child_clinical_condition.expires_after_days IS
    'Acute conditions only, and NULL means unknown rather than never. An acute condition '
    'past its window, or one with no window at all, is surfaced as stale and does not '
    'drive a nutrition target. See outstanding provider question 12.';
```

- [ ] **Step 2: Write `internal/db/migrations/0014_child_profile.down.sql`**

```sql
DROP TABLE IF EXISTS child_clinical_condition;
DROP TABLE IF EXISTS child_preference;
DROP TABLE IF EXISTS child_allergen;
DROP TABLE IF EXISTS child_growth_measurement;
DROP TABLE IF EXISTS child_profile;
```

- [ ] **Step 3: Verify against a clean database**

```bash
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import
psql $DATABASE_URL -c "\dt child_*"
```

Expected: five tables. The importer sweeps only tables it owns, so these stay empty across
re-imports rather than being deleted — confirm by re-running the import and re-listing.

- [ ] **Step 4: Commit**

```bash
git add internal/db/migrations/0014_child_profile.up.sql internal/db/migrations/0014_child_profile.down.sql
git commit -m "Add the canonical child profile and dated growth measurements"
```

---

### Task 6: `internal/profile` - store, load, derive

**Files:**
- Create: `internal/profile/profile.go`
- Test: `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: the five tables from Task 5, `models.ChildProfile`.
- Produces: `profile.Stored`, `profile.Save(ctx, pool, s) error`,
  `profile.Load(ctx, pool, childID) (Stored, error)`,
  `(Stored).ToChildProfile(asOf time.Time) (models.ChildProfile, []string, error)` where
  the second return names facts that were dropped and why. Tasks 7 and 8 extend it.

- [ ] **Step 1: Write the failing tests**

Create `internal/profile/profile_test.go`:

```go
package profile

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func date(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestAgeMonthsIsDerivedFromDateOfBirth(t *testing.T) {
	cases := []struct {
		name string
		dob  string
		asOf string
		want int
	}{
		{"exact month boundary", "2026-02-18", "2026-08-18", 6},
		{"one day short of the boundary", "2026-02-19", "2026-08-18", 5},
		{"exactly one year", "2025-08-18", "2026-08-18", 12},
		{"newborn", "2026-08-18", "2026-08-18", 0},
		{"leap-day birth in a non-leap year", "2024-02-29", "2026-03-01", 24},
		{"adolescent", "2008-08-18", "2026-08-18", 216},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ageMonths(date(c.dob), date(c.asOf))
			if got != c.want {
				t.Fatalf("ageMonths(%s, %s) = %d, want %d", c.dob, c.asOf, got, c.want)
			}
		})
	}
}

func TestAgeMonthsRejectsAFutureBirthDate(t *testing.T) {
	if got := ageMonths(date("2026-09-01"), date("2026-08-18")); got != -1 {
		t.Fatalf("a DOB after the reference date must return -1 so the caller can reject "+
			"it, not a plausible-looking 0; got %d", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	s := Stored{
		ChildID:     "TEST-CHILD-0001",
		DisplayName: "Round Trip",
		DateOfBirth: date("2023-08-18"),
		Sex:         "female",
		DietType:    "Vegetarian",
		CreatedBy:   "profile_test",
		Growth: []GrowthMeasurement{
			{MeasuredOn: date("2026-02-01"), WeightKg: ptr(12.1), HeightCm: ptr(88.0), MeasuredBy: "clinician-a"},
			{MeasuredOn: date("2026-08-01"), WeightKg: ptr(13.4), HeightCm: ptr(92.5), MeasuredBy: "clinician-a"},
		},
		Allergens: []DeclaredAllergen{
			{Group: "Peanut", Status: "confirmed", Severity: "systemic", Source: "clinician_documented", EnteredBy: "clinician-a"},
			{Group: "Milk", Status: "resolved", Source: "parent_reported", EnteredBy: "clinician-a"},
		},
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM child_profile WHERE child_id = $1`, s.ChildID)
	})

	if err := Save(ctx, pool, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(ctx, pool, s.ChildID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Growth) != 2 {
		t.Fatalf("growth rows = %d, want 2; one child has many measurements and the trend "+
			"is the clinical point", len(got.Growth))
	}
	// Newest first, so a caller reading Growth[0] gets the current measurement.
	if !got.Growth[0].MeasuredOn.After(got.Growth[1].MeasuredOn) {
		t.Fatalf("growth rows must load newest first, got %v then %v",
			got.Growth[0].MeasuredOn, got.Growth[1].MeasuredOn)
	}
	if len(got.Allergens) != 2 {
		t.Fatalf("allergen rows = %d, want 2", len(got.Allergens))
	}

	// Saving again must not duplicate: this is the same upsert-and-sweep contract the
	// workbook importer holds itself to.
	if err := Save(ctx, pool, s); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	again, err := Load(ctx, pool, s.ChildID)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if len(again.Growth) != 2 || len(again.Allergens) != 2 {
		t.Fatalf("re-saving duplicated rows: growth=%d allergens=%d", len(again.Growth), len(again.Allergens))
	}
}

func TestToChildProfileCarriesConfirmedAllergensAndDropsResolved(t *testing.T) {
	s := Stored{
		ChildID:     "T",
		DateOfBirth: date("2023-08-18"),
		DietType:    "Vegetarian",
		Allergens: []DeclaredAllergen{
			{Group: "Peanut", Status: "confirmed"},
			{Group: "Milk", Status: "resolved"},
			{Group: "Soy", Status: "suspected"},
		},
	}
	cp, notes, err := s.ToChildProfile(date("2026-08-18"))
	if err != nil {
		t.Fatalf("ToChildProfile: %v", err)
	}
	if cp.AgeMonths != 36 {
		t.Fatalf("AgeMonths = %d, want 36", cp.AgeMonths)
	}
	if len(cp.Allergens) != 1 || cp.Allergens[0] != "Peanut" {
		t.Fatalf("Allergens = %v; only confirmed allergens are hard filters", cp.Allergens)
	}
	if len(cp.SuspectedAllergens) != 1 || cp.SuspectedAllergens[0] != "Soy" {
		t.Fatalf("SuspectedAllergens = %v; suspected ranks down, it never filters", cp.SuspectedAllergens)
	}
	// A resolved allergy that silently vanished would be indistinguishable from one that
	// was never recorded, so the caller is told.
	var mentionsMilk bool
	for _, n := range notes {
		if contains(n, "Milk") {
			mentionsMilk = true
		}
	}
	if !mentionsMilk {
		t.Fatalf("notes must record that a resolved allergen was excluded from filtering; got %v", notes)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(haystack == needle || len(needle) == 0 ||
			// simple substring search, avoiding a strings import in the test
			indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func ptr[T any](v T) *T { return &v }
```

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/profile/... -v`

Expected: FAIL — the package does not exist.

- [ ] **Step 3: Add `SuspectedAllergens` to `models.ChildProfile`**

In `internal/models/profile.go`, after the `Allergens` field:

```go
	// SuspectedAllergens are allergen groups the family or clinician has raised but not
	// confirmed. AS-002 marks these hard_block = N, so they are a ranker input, never a
	// filter: unnecessary elimination is itself a recognised cause of faltering growth,
	// and treating every suspicion as confirmation is a different risk rather than the
	// cautious choice. See internal/engine/rank.go, applySuspectedAllergenRank.
	SuspectedAllergens []string `json:"suspected_allergens,omitempty"`
```

- [ ] **Step 4: Write `internal/profile/profile.go`**

```go
// Package profile holds the canonical child profile: what a consultation produces and
// what a book is generated from.
//
// This is deliberately separate from models.ChildProfile, which is the engine's query
// input. The stored profile keeps date_of_birth and derives age at query time, so a book
// generated today and read in six months does not carry a stale age on every page. The
// SRS draws the same line, between an immutable profile snapshot and an engine that takes
// a query.
package profile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// ErrNotFound is returned by Load when no profile exists for the given child id.
var ErrNotFound = errors.New("profile not found")

// ErrInvalidProfile marks stored data the caller must correct before the engine can run
// against it -- today, only a date of birth in the future.
var ErrInvalidProfile = errors.New("invalid stored profile")

// GrowthMeasurement is one dated set of anthropometry. One child has many, and the trend
// is the clinical point.
//
// Every z-score is clinician-entered. Nothing here computes one: that would mean choosing
// a growth reference, which is a clinical decision this project has no basis to make.
type GrowthMeasurement struct {
	MeasuredOn          time.Time
	WeightKg            *float64
	HeightCm            *float64
	HeadCircumferenceCm *float64
	BMIForAgeZ          *float64
	WeightForAgeZ       *float64
	HeightForAgeZ       *float64
	Interpretation      string
	MeasuredBy          string
}

// DeclaredAllergen carries the three states the provider's own masters distinguish and
// the flat []string on models.ChildProfile cannot express.
type DeclaredAllergen struct {
	Group          string // allergen_mapping.allergen_group
	Status         string // confirmed | suspected | resolved
	Severity       string // mild | systemic, empty when unknown
	Source         string // parent_reported | clinician_documented
	LastReactionOn *time.Time
	EnteredBy      string
}

// Preference is a family-sourced ranking input. Never a filter: a picky child with eight
// dislikes would empty a hard-filtered list.
type Preference struct {
	IngredientID string
	Kind         string // like | dislike | accepted
	EnteredBy    string
}

// ClinicalCondition carries the time dimension the rest of the model lacks. An acute
// condition entered three weeks ago must stop driving a nutrition target.
type ClinicalCondition struct {
	TriggerField       string // clinical_rule_master.trigger_field
	FlagValue          string
	Class              string // acute | chronic | congenital
	OnsetDate          *time.Time
	ExpiresAfterDays   *int
	SpecialistTargetID string
	EnteredBy          string
}

// Stored is one child's full persisted profile.
type Stored struct {
	ChildID              string
	CaseID               string
	DisplayName          string
	DateOfBirth          time.Time
	Sex                  string
	LanguageID           string
	RegionCulture        string
	CuisineCode          string
	DietType             string
	Vegan                bool
	ReligiousRestriction string
	BudgetBand           string
	MaxPrepTimeMin       int
	MaxCookTimeMin       int
	CreatedBy            string

	Growth     []GrowthMeasurement
	Allergens  []DeclaredAllergen
	Preferences []Preference
	Conditions []ClinicalCondition
}

// ageMonths returns completed months between dob and asOf, or -1 if dob is after asOf.
//
// -1 rather than 0 on purpose: a newborn is genuinely 0 months old, so returning 0 for a
// future date of birth would make a data-entry error indistinguishable from a real
// newborn, and the engine would happily rank infant purees for a child who does not exist
// yet.
func ageMonths(dob, asOf time.Time) int {
	if dob.After(asOf) {
		return -1
	}
	months := int(asOf.Year()-dob.Year())*12 + int(asOf.Month()) - int(dob.Month())
	if asOf.Day() < dob.Day() {
		months--
	}
	if months < 0 {
		return 0
	}
	return months
}

// ToChildProfile derives the engine's query input from the stored profile as of a given
// date. The second return names every stored fact that did not reach the query and why,
// so a caller can show the operator what was dropped rather than leaving it invisible.
func (s Stored) ToChildProfile(asOf time.Time) (models.ChildProfile, []string, error) {
	age := ageMonths(s.DateOfBirth, asOf)
	if age < 0 {
		return models.ChildProfile{}, nil, fmt.Errorf(
			"profile %s: date of birth %s is after the reference date %s: %w",
			s.ChildID, s.DateOfBirth.Format("2006-01-02"), asOf.Format("2006-01-02"), ErrInvalidProfile)
	}

	cp := models.ChildProfile{
		AgeMonths:      age,
		DietType:       s.DietType,
		Vegan:          s.Vegan,
		RegionCulture:  s.RegionCulture,
		CuisineCode:    s.CuisineCode,
		BudgetBand:     s.BudgetBand,
		MaxPrepTimeMin: s.MaxPrepTimeMin,
		MaxCookTimeMin: s.MaxCookTimeMin,
	}

	var notes []string
	for _, a := range s.Allergens {
		switch a.Status {
		case "confirmed":
			cp.Allergens = append(cp.Allergens, a.Group)
		case "suspected":
			// AS-002: hard_block = N. Ranks down, raises a review flag, never filters.
			cp.SuspectedAllergens = append(cp.SuspectedAllergens, a.Group)
			notes = append(notes, fmt.Sprintf(
				"%s is suspected, not confirmed: it ranks recipes down and raises a review flag, and does not exclude anything (AS-002)", a.Group))
		case "resolved":
			notes = append(notes, fmt.Sprintf(
				"%s is recorded as resolved and excludes nothing; it is kept in history", a.Group))
		}
	}

	return cp, notes, nil
}

// Save upserts the profile and replaces its child rows in one transaction.
//
// Child rows are deleted and reinserted rather than individually reconciled: a profile is
// small, the write is transactional, and the alternative is a per-row diff that can leave
// a stale measurement behind. This is the same upsert-and-sweep contract the workbook
// importer holds itself to, at a much smaller scale.
func Save(ctx context.Context, pool *pgxpool.Pool, s Stored) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO child_profile (child_id, case_id, display_name, date_of_birth, sex,
			language_id, region_culture, cuisine_code, diet_type, vegan,
			religious_restriction, budget_band, max_prep_time_min, max_cook_time_min, created_by)
		VALUES ($1,$2,nullif($3,''),$4,nullif($5,''),nullif($6,''),nullif($7,''),nullif($8,''),
			nullif($9,''),$10,nullif($11,''),nullif($12,''),nullif($13,0),nullif($14,0),$15)
		ON CONFLICT (child_id) DO UPDATE SET
			case_id = excluded.case_id,
			display_name = excluded.display_name,
			date_of_birth = excluded.date_of_birth,
			sex = excluded.sex,
			language_id = excluded.language_id,
			region_culture = excluded.region_culture,
			cuisine_code = excluded.cuisine_code,
			diet_type = excluded.diet_type,
			vegan = excluded.vegan,
			religious_restriction = excluded.religious_restriction,
			budget_band = excluded.budget_band,
			max_prep_time_min = excluded.max_prep_time_min,
			max_cook_time_min = excluded.max_cook_time_min,
			updated_by = excluded.created_by,
			updated_at = now()`,
		s.ChildID, nullString(s.CaseID), s.DisplayName, s.DateOfBirth, s.Sex,
		s.LanguageID, s.RegionCulture, s.CuisineCode, s.DietType, s.Vegan,
		s.ReligiousRestriction, s.BudgetBand, s.MaxPrepTimeMin, s.MaxCookTimeMin, s.CreatedBy)
	if err != nil {
		return fmt.Errorf("profile: upsert %s: %w", s.ChildID, err)
	}

	for _, table := range []string{
		"child_growth_measurement", "child_allergen", "child_preference", "child_clinical_condition",
	} {
		// Table names come from this literal list, never from input.
		if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE child_id = $1", s.ChildID); err != nil {
			return fmt.Errorf("profile: clear %s for %s: %w", table, s.ChildID, err)
		}
	}

	for _, g := range s.Growth {
		_, err = tx.Exec(ctx, `
			INSERT INTO child_growth_measurement (child_id, measured_on, weight_kg, height_cm,
				head_circumference_cm, bmi_for_age_z, weight_for_age_z, height_for_age_z,
				interpretation, measured_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,nullif($9,''),$10)`,
			s.ChildID, g.MeasuredOn, g.WeightKg, g.HeightCm, g.HeadCircumferenceCm,
			g.BMIForAgeZ, g.WeightForAgeZ, g.HeightForAgeZ, g.Interpretation, g.MeasuredBy)
		if err != nil {
			return fmt.Errorf("profile: insert growth %s for %s: %w",
				g.MeasuredOn.Format("2006-01-02"), s.ChildID, err)
		}
	}

	for _, a := range s.Allergens {
		_, err = tx.Exec(ctx, `
			INSERT INTO child_allergen (child_id, allergen_group, status, severity, source,
				last_reaction_on, entered_by)
			VALUES ($1,$2,$3,nullif($4,''),$5,$6,$7)`,
			s.ChildID, a.Group, a.Status, a.Severity, defaultSource(a.Source),
			a.LastReactionOn, defaultActor(a.EnteredBy, s.CreatedBy))
		if err != nil {
			return fmt.Errorf("profile: insert allergen %s for %s: %w", a.Group, s.ChildID, err)
		}
	}

	for _, p := range s.Preferences {
		_, err = tx.Exec(ctx, `
			INSERT INTO child_preference (child_id, ingredient_id, kind, entered_by)
			VALUES ($1,$2,$3,$4)`,
			s.ChildID, p.IngredientID, p.Kind, defaultActor(p.EnteredBy, s.CreatedBy))
		if err != nil {
			return fmt.Errorf("profile: insert preference %s for %s: %w", p.IngredientID, s.ChildID, err)
		}
	}

	for _, c := range s.Conditions {
		_, err = tx.Exec(ctx, `
			INSERT INTO child_clinical_condition (child_id, trigger_field, flag_value, class,
				onset_date, expires_after_days, specialist_target_id, entered_by)
			VALUES ($1,$2,$3,$4,$5,$6,nullif($7,''),$8)`,
			s.ChildID, c.TriggerField, c.FlagValue, c.Class, c.OnsetDate,
			c.ExpiresAfterDays, c.SpecialistTargetID, defaultActor(c.EnteredBy, s.CreatedBy))
		if err != nil {
			return fmt.Errorf("profile: insert condition %s for %s: %w", c.TriggerField, s.ChildID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("profile: commit %s: %w", s.ChildID, err)
	}
	return nil
}

// Load reads one profile and all its child rows. Growth measurements come back newest
// first, so a caller reading Growth[0] gets the current measurement.
func Load(ctx context.Context, pool *pgxpool.Pool, childID string) (Stored, error) {
	var s Stored
	err := pool.QueryRow(ctx, `
		SELECT child_id, coalesce(case_id,''), coalesce(display_name,''), date_of_birth,
		       coalesce(sex,''), coalesce(language_id,''), coalesce(region_culture,''),
		       coalesce(cuisine_code,''), coalesce(diet_type,''), vegan,
		       coalesce(religious_restriction,''), coalesce(budget_band,''),
		       coalesce(max_prep_time_min,0), coalesce(max_cook_time_min,0), created_by
		FROM child_profile WHERE child_id = $1`, childID).
		Scan(&s.ChildID, &s.CaseID, &s.DisplayName, &s.DateOfBirth, &s.Sex, &s.LanguageID,
			&s.RegionCulture, &s.CuisineCode, &s.DietType, &s.Vegan, &s.ReligiousRestriction,
			&s.BudgetBand, &s.MaxPrepTimeMin, &s.MaxCookTimeMin, &s.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stored{}, fmt.Errorf("profile %s: %w", childID, ErrNotFound)
	}
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load %s: %w", childID, err)
	}

	growthRows, err := pool.Query(ctx, `
		SELECT measured_on, weight_kg, height_cm, head_circumference_cm,
		       bmi_for_age_z, weight_for_age_z, height_for_age_z,
		       coalesce(interpretation,''), measured_by
		FROM child_growth_measurement WHERE child_id = $1 ORDER BY measured_on DESC`, childID)
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load growth %s: %w", childID, err)
	}
	defer growthRows.Close()
	for growthRows.Next() {
		var g GrowthMeasurement
		if err := growthRows.Scan(&g.MeasuredOn, &g.WeightKg, &g.HeightCm, &g.HeadCircumferenceCm,
			&g.BMIForAgeZ, &g.WeightForAgeZ, &g.HeightForAgeZ, &g.Interpretation, &g.MeasuredBy); err != nil {
			return Stored{}, fmt.Errorf("profile: scan growth %s: %w", childID, err)
		}
		s.Growth = append(s.Growth, g)
	}
	if err := growthRows.Err(); err != nil {
		return Stored{}, fmt.Errorf("profile: growth rows %s: %w", childID, err)
	}

	allergenRows, err := pool.Query(ctx, `
		SELECT allergen_group, status, coalesce(severity,''), source, last_reaction_on, entered_by
		FROM child_allergen WHERE child_id = $1 ORDER BY allergen_group`, childID)
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load allergens %s: %w", childID, err)
	}
	defer allergenRows.Close()
	for allergenRows.Next() {
		var a DeclaredAllergen
		if err := allergenRows.Scan(&a.Group, &a.Status, &a.Severity, &a.Source,
			&a.LastReactionOn, &a.EnteredBy); err != nil {
			return Stored{}, fmt.Errorf("profile: scan allergen %s: %w", childID, err)
		}
		s.Allergens = append(s.Allergens, a)
	}
	if err := allergenRows.Err(); err != nil {
		return Stored{}, fmt.Errorf("profile: allergen rows %s: %w", childID, err)
	}

	prefRows, err := pool.Query(ctx, `
		SELECT ingredient_id, kind, entered_by
		FROM child_preference WHERE child_id = $1 ORDER BY kind, ingredient_id`, childID)
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load preferences %s: %w", childID, err)
	}
	defer prefRows.Close()
	for prefRows.Next() {
		var p Preference
		if err := prefRows.Scan(&p.IngredientID, &p.Kind, &p.EnteredBy); err != nil {
			return Stored{}, fmt.Errorf("profile: scan preference %s: %w", childID, err)
		}
		s.Preferences = append(s.Preferences, p)
	}
	if err := prefRows.Err(); err != nil {
		return Stored{}, fmt.Errorf("profile: preference rows %s: %w", childID, err)
	}

	condRows, err := pool.Query(ctx, `
		SELECT trigger_field, flag_value, class, onset_date, expires_after_days,
		       coalesce(specialist_target_id,''), entered_by
		FROM child_clinical_condition WHERE child_id = $1 ORDER BY trigger_field`, childID)
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load conditions %s: %w", childID, err)
	}
	defer condRows.Close()
	for condRows.Next() {
		var c ClinicalCondition
		if err := condRows.Scan(&c.TriggerField, &c.FlagValue, &c.Class, &c.OnsetDate,
			&c.ExpiresAfterDays, &c.SpecialistTargetID, &c.EnteredBy); err != nil {
			return Stored{}, fmt.Errorf("profile: scan condition %s: %w", childID, err)
		}
		s.Conditions = append(s.Conditions, c)
	}
	if err := condRows.Err(); err != nil {
		return Stored{}, fmt.Errorf("profile: condition rows %s: %w", childID, err)
	}

	return s, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// defaultSource keeps the CHECK constraint satisfiable without silently upgrading the
// trustworthiness of a claim: an unstated source is parent_reported, the weaker of the
// two, never clinician_documented.
func defaultSource(s string) string {
	if s == "" {
		return "parent_reported"
	}
	return s
}

func defaultActor(actor, fallback string) string {
	if actor == "" {
		return fallback
	}
	return actor
}
```

- [ ] **Step 5: Run tests, confirm pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/profile/... -v`

Expected: PASS, including all six age-derivation cases.

- [ ] **Step 6: `go build ./... && go vet ./...`, then commit**

```bash
git add internal/profile internal/models/profile.go
git commit -m "Add the profile package deriving engine input from date of birth"
```

---

### Task 7: Suspected allergens rank down

**Files:**
- Modify: `internal/engine/rank.go`
- Modify: `internal/engine/pipeline.go`
- Test: `internal/engine/rank_test.go`

**Interfaces:**
- Consumes: `models.ChildProfile.SuspectedAllergens` from Task 6.
- Produces: `applySuspectedAllergenRank(ctx, pool, p, recipes) ([]models.RankedRecipe, models.StepResult, error)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/rank_test.go`:

```go
func TestApplySuspectedAllergenRankDemotesButNeverRemoves(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	ranked, _, err := rankByTarget(ctx, pool, "NT00", ids)
	if err != nil {
		t.Fatalf("rankByTarget: %v", err)
	}

	p := models.ChildProfile{AgeMonths: 36, SuspectedAllergens: []string{"Peanut"}}
	out, step, err := applySuspectedAllergenRank(ctx, pool, p, ranked)
	if err != nil {
		t.Fatalf("applySuspectedAllergenRank: %v", err)
	}

	// AS-002 marks suspected allergy hard_block = N. Unnecessary elimination is itself a
	// recognised cause of faltering growth, so this must never behave like step 2.
	if len(out) != len(ranked) {
		t.Fatalf("a suspected allergen must not remove recipes: in=%d out=%d", len(ranked), len(out))
	}
	if step.CandidatesIn != step.CandidatesOut {
		t.Fatalf("suspected-allergen step must report equal in/out: %+v", step)
	}
	if step.Note == "" {
		t.Fatal("the step must say how many recipes it demoted and that it excluded none")
	}

	// Peanut-tagged recipes must be denser in the bottom half than the top half.
	half := len(out) / 2
	if half < 4 {
		t.Skip("candidate pool too small to measure")
	}
	tagged := taggedRecipeIDs(t, pool, "Peanut")
	var top, bottom int
	for i, r := range out {
		if !tagged[r.RecipeID] {
			continue
		}
		if i < half {
			top++
		} else {
			bottom++
		}
	}
	if top+bottom == 0 {
		t.Fatal("no peanut-tagged recipes in the pool; the fixture assumption is wrong")
	}
	if bottom <= top {
		t.Fatalf("suspected allergen not demoted: %d tagged in the top half, %d in the bottom", top, bottom)
	}
}

func taggedRecipeIDs(t *testing.T, pool *pgxpool.Pool, group string) map[string]bool {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT r.recipe_id FROM recipe_master r
		JOIN allergen_tag_vocabulary v ON v.allergen_group = $1 AND v.corpus_tag IS NOT NULL
		WHERE r.allergen_tags ILIKE '%' || v.corpus_tag || '%'`, group)
	if err != nil {
		t.Fatalf("tagged lookup: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("tagged scan: %v", err)
		}
		out[id] = true
	}
	return out
}

func TestApplySuspectedAllergenRankIsANoOpWhenNoneDeclared(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, _ := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	ranked, _, _ := rankByTarget(ctx, pool, "NT00", ids)

	out, step, err := applySuspectedAllergenRank(ctx, pool, models.ChildProfile{AgeMonths: 36}, ranked)
	if err != nil {
		t.Fatalf("applySuspectedAllergenRank: %v", err)
	}
	if len(out) != len(ranked) || step.CandidatesIn != step.CandidatesOut {
		t.Fatalf("no suspected allergens must be a pure no-op: %+v", step)
	}
}
```

Add `"github.com/jackc/pgx/v5/pgxpool"` to that file's imports if not already present.

Update the persona step-count assertion in `internal/engine/pipeline_test.go` from 14 to
15, and its message to mention that step 2 is now recorded twice: once as the confirmed
hard filter and once as the suspected-allergen ranker.

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/... -run TestApplySuspectedAllergen -v`

Expected: FAIL, `undefined: applySuspectedAllergenRank`.

- [ ] **Step 3: Write the ranker in `internal/engine/rank.go`**

```go
// applySuspectedAllergenRank is the ranking half of engine step 2.
//
// Step 2's hard filter removes confirmed allergens and is never relaxed. This handles the
// other state the provider's masters distinguish and the flat allergen list cannot:
// AS-002 marks a suspected allergy hard_block = N, so it must rank down and raise a review
// flag rather than exclude.
//
// That is not the timid choice, it is the correct one. Unnecessary elimination is itself a
// recognised cause of faltering growth in children, so treating every suspicion as a
// confirmation trades one risk for a different one rather than removing risk.
//
// A group with no corpus tag demotes nothing, for the same reason it screens nothing --
// there is no tag to match. That is reported so it does not read as a working demotion.
func applySuspectedAllergenRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if len(p.SuspectedAllergens) == 0 || stepIn == 0 {
		return recipes, models.StepResult{
			Step: 2, Name: "Allergy - suspected, ranker", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no suspected allergens declared, step is a no-op",
		}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT r.recipe_id
		FROM recipe_master r
		JOIN allergen_tag_vocabulary v
		  ON v.allergen_group = ANY($2) AND v.corpus_tag IS NOT NULL
		WHERE r.recipe_id = ANY($1)
		  AND (r.allergen_tags ILIKE '%' || v.corpus_tag || '%'
		       OR EXISTS (
		           SELECT 1 FROM recipe_ingredient_mapping m
		           WHERE m.recipe_id = r.recipe_id
		             AND m.ingredient_allergen_tag ILIKE '%' || v.corpus_tag || '%'))`,
		ids, p.SuspectedAllergens)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: suspected allergen rank: %w", err)
	}
	defer rows.Close()

	demote := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: suspected allergen scan: %w", err)
		}
		demote[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: suspected allergen rows: %w", err)
	}

	// Larger than every other adjustment in this file (culture 0.05, availability 0.05,
	// budget 0.03, diet 0.04, duplicate -0.02) because a suspected allergen is a safety
	// signal rather than a preference, and it should push a recipe clearly down the list.
	// Still bounded, because it must not empty a page or override the nutrition ordering
	// outright -- that is what the confirmed state is for.
	const penalty = 0.15

	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		if demote[out[i].RecipeID] {
			out[i].RankedScore -= penalty
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	unscreened, err := unscreenedGroups(ctx, pool, p.SuspectedAllergens)
	if err != nil {
		return nil, models.StepResult{}, err
	}

	note := fmt.Sprintf("%d of %d candidates carry a suspected allergen tag and were ranked down by %.2f; none were excluded, because AS-002 marks a suspected allergy hard_block = N",
		len(demote), stepIn, penalty)
	if len(unscreened) > 0 {
		note += fmt.Sprintf(". Suspected group(s) %v have no corpus tag, so they demoted nothing", unscreened)
	}

	return out, models.StepResult{
		Step: 2, Name: "Allergy - suspected, ranker", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn, Note: note,
	}, nil
}

// unscreenedGroups returns the subset of groups with no corpus tag. Shared by the
// suspected-allergen ranker and step 2's hard filter so the two can never disagree about
// which groups the corpus cannot screen.
func unscreenedGroups(ctx context.Context, pool *pgxpool.Pool, groups []string) ([]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT allergen_group FROM allergen_tag_vocabulary
		WHERE allergen_group = ANY($1) AND corpus_tag IS NULL
		ORDER BY allergen_group`, groups)
	if err != nil {
		return nil, fmt.Errorf("engine: unscreened group lookup: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, fmt.Errorf("engine: unscreened group scan: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engine: unscreened group rows: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: Use the shared helper in `steps_hard.go`**

`allergyFilter` computes the same list inline. Replace its `absentRows` query block with a
call to `unscreenedGroups(ctx, pool, p.Allergens)`, keeping the existing `note`
construction unchanged. Two code paths deciding which groups are unscreened is exactly the
kind of duplication that drifts.

- [ ] **Step 5: Wire it into `internal/engine/pipeline.go`**

Immediately after the diet-preference ranker added by the other plan's Task 6:

```go
	ranked, step2rank, err := applySuspectedAllergenRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step2rank)
```

- [ ] **Step 6: Run the full engine suite, confirm pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/... -v`

Expected: PASS at 15 recorded steps.

- [ ] **Step 7: Commit**

```bash
git add internal/engine/rank.go internal/engine/steps_hard.go internal/engine/pipeline.go internal/engine/rank_test.go internal/engine/pipeline_test.go
git commit -m "Rank suspected allergens down instead of filtering them out"
```

---

### Task 8: Acute conditions expire

**Files:**
- Modify: `internal/profile/profile.go`
- Test: `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: `Stored.Conditions`.
- Produces: `ToChildProfile` populating `models.ChildProfile.ClinicalFlags` from live
  conditions only, and naming stale and unbounded ones in its notes.

- [ ] **Step 1: Write the failing tests**

Append to `internal/profile/profile_test.go`:

```go
func TestToChildProfileDropsExpiredAcuteConditions(t *testing.T) {
	onset := date("2026-07-01")
	fourteen := 14

	s := Stored{
		ChildID:     "T",
		DateOfBirth: date("2023-08-18"),
		Conditions: []ClinicalCondition{
			// Entered six weeks before the reference date with a 14-day window.
			{TriggerField: "Acute_Diarrhoea", FlagValue: "Yes", Class: "acute",
				OnsetDate: &onset, ExpiresAfterDays: &fourteen},
			// Chronic conditions never expire.
			{TriggerField: "Coeliac_Status", FlagValue: "Confirmed", Class: "chronic"},
		},
	}
	cp, notes, err := s.ToChildProfile(date("2026-08-18"))
	if err != nil {
		t.Fatalf("ToChildProfile: %v", err)
	}
	if _, present := cp.ClinicalFlags["Acute_Diarrhoea"]; present {
		t.Fatal("an acute condition 48 days past a 14-day window must not still drive a " +
			"nutrition target; a stale diarrhoea flag pushing NT12 distorts every later generation")
	}
	if cp.ClinicalFlags["Coeliac_Status"] != "Confirmed" {
		t.Fatalf("a chronic condition must persist; ClinicalFlags = %v", cp.ClinicalFlags)
	}
	var explained bool
	for _, n := range notes {
		if indexOf(n, "Acute_Diarrhoea") >= 0 {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("dropping a condition must be explained, not silent; notes = %v", notes)
	}
}

func TestToChildProfileKeepsALiveAcuteCondition(t *testing.T) {
	onset := date("2026-08-15")
	fourteen := 14
	s := Stored{
		ChildID: "T", DateOfBirth: date("2023-08-18"),
		Conditions: []ClinicalCondition{
			{TriggerField: "Acute_Diarrhoea", FlagValue: "Yes", Class: "acute",
				OnsetDate: &onset, ExpiresAfterDays: &fourteen},
		},
	}
	cp, _, err := s.ToChildProfile(date("2026-08-18"))
	if err != nil {
		t.Fatalf("ToChildProfile: %v", err)
	}
	if cp.ClinicalFlags["Acute_Diarrhoea"] != "Yes" {
		t.Fatalf("an acute condition 3 days into a 14-day window is live; ClinicalFlags = %v", cp.ClinicalFlags)
	}
}

func TestToChildProfileFlagsAnAcuteConditionWithNoWindow(t *testing.T) {
	onset := date("2026-01-01")
	s := Stored{
		ChildID: "T", DateOfBirth: date("2023-08-18"),
		Conditions: []ClinicalCondition{
			// No expires_after_days: the clinical window for this class is outstanding to
			// the provider (question 12), so the code cannot invent one.
			{TriggerField: "Persistent_Vomiting", FlagValue: "Yes", Class: "acute", OnsetDate: &onset},
		},
	}
	cp, notes, err := s.ToChildProfile(date("2026-08-18"))
	if err != nil {
		t.Fatalf("ToChildProfile: %v", err)
	}
	// Kept, because dropping it would mean inventing an expiry window nobody has stated,
	// and because Persistent_Vomiting escalates -- failing to apply it is the dangerous
	// direction. But the staleness must be visible.
	if cp.ClinicalFlags["Persistent_Vomiting"] != "Yes" {
		t.Fatal("an acute condition with no stated window must still apply: inventing an " +
			"expiry is exactly what the hard rule forbids")
	}
	var warned bool
	for _, n := range notes {
		if indexOf(n, "230 days") >= 0 || indexOf(n, "no expiry window") >= 0 {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("an acute condition with no window must be reported as possibly stale; notes = %v", notes)
	}
}
```

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/profile/... -run TestToChildProfile -v`

Expected: FAIL — `ClinicalFlags` is never populated.

- [ ] **Step 3: Extend `ToChildProfile`**

Insert before the `return` in `ToChildProfile`:

```go
	for _, c := range s.Conditions {
		if c.Class == "acute" {
			live, note := acuteStatus(c, asOf)
			if note != "" {
				notes = append(notes, note)
			}
			if !live {
				continue
			}
		}
		if cp.ClinicalFlags == nil {
			cp.ClinicalFlags = map[string]string{}
		}
		cp.ClinicalFlags[c.TriggerField] = c.FlagValue
	}
```

And add the helper:

```go
// acuteStatus decides whether an acute condition still applies, and returns the note the
// caller should show either way.
//
// Two cases, and the difference matters:
//
//   - A condition past a stated window is dropped. A diarrhoea flag entered three weeks
//     ago must stop pushing NT12, or every later generation is distorted by a fact that
//     is no longer true.
//   - A condition with no stated window is KEPT and reported as possibly stale. Dropping
//     it would mean inventing an expiry the provider has not given (outstanding question
//     12), and several acute triggers escalate -- failing to apply one is the dangerous
//     direction, while applying a stale one is merely wrong in the cautious direction.
func acuteStatus(c ClinicalCondition, asOf time.Time) (live bool, note string) {
	if c.OnsetDate == nil {
		// The CHECK constraint on child_clinical_condition forbids this, so reaching it
		// means the row was written outside this package. Keep the flag and say so.
		return true, fmt.Sprintf(
			"%s is acute with no onset date, so its age cannot be checked; it is being applied as entered", c.TriggerField)
	}

	days := int(asOf.Sub(*c.OnsetDate).Hours() / 24)

	if c.ExpiresAfterDays == nil {
		return true, fmt.Sprintf(
			"%s is acute, entered %d days ago, and no expiry window is set for its class; it is still being applied and may be stale (provider question 12)",
			c.TriggerField, days)
	}

	if days > *c.ExpiresAfterDays {
		return false, fmt.Sprintf(
			"%s is acute, entered %d days ago, past its %d-day window; it no longer drives a nutrition target",
			c.TriggerField, days, *c.ExpiresAfterDays)
	}
	return true, ""
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/profile/... -v`

Expected: PASS.

- [ ] **Step 5: `go build ./... && go vet ./...`, then commit**

```bash
git add internal/profile/profile.go internal/profile/profile_test.go
git commit -m "Expire acute conditions, and report the ones with no stated window"
```

---

### Task 9: Full verification and documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/not-built.md`
- Modify: `README.md`

**Interfaces:** none. Verification only.

- [ ] **Step 1: Run everything from a clean database**

```bash
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import
go run ./cmd/enrich
go build ./...
go vet ./...
TEST_DATABASE_URL=$DATABASE_URL go test ./... -v
```

Expected: every package PASS. Row counts 32/44/33/18/5/16/13/15/13 in the Book 1 tables.

- [ ] **Step 2: Confirm idempotency including the new tables**

```bash
go run ./cmd/import
psql $DATABASE_URL -c "SELECT a.table_name FROM import_table_stat a JOIN import_table_stat b USING (table_name) WHERE a.run_id = (SELECT max(run_id) FROM import_table_stat) AND b.run_id = (SELECT max(run_id) - 1 FROM import_table_stat) AND a.content_hash <> b.content_hash"
```

Expected: zero rows.

- [ ] **Step 3: Confirm the profile tables survive a re-import**

```bash
psql $DATABASE_URL -c "INSERT INTO child_profile (child_id, date_of_birth, created_by) VALUES ('SMOKE-0001', '2023-08-18', 'smoke-test')"
go run ./cmd/import
psql $DATABASE_URL -c "SELECT count(*) FROM child_profile WHERE child_id = 'SMOKE-0001'"
psql $DATABASE_URL -c "DELETE FROM child_profile WHERE child_id = 'SMOKE-0001'"
```

Expected: `1`. The importer's sweep deletes only from tables it owns; a clinician-entered
profile must never be removed by re-importing a workbook.

- [ ] **Step 4: Update `CLAUDE.md`**

- In "Datasets", change the `Book1_Content_Master` row's Sheets count to `9` and its Core
  rows to `32 blocks, 44 vaccine, 33 milestones, 16 assembly steps, 15 release checks`.
- In "What exists now", add `0013_book1_content` and `0014_child_profile` to the migration
  list, and `internal/profile/` to the package list.
- In the Phase 2 status section, add a line recording that `Book1_Content_Master` is now
  imported and that `internal/importer/spec.go`'s exclusion comment was corrected.
- Add a short subsection under the Phase 3 heading noting that
  `book1_content_block.ai_can_draft = 'N'` is an enforced gate on five blocks, pinned by
  `TestAICanDraftGateIsPinned`, and that it is narrower than the 18 August prose ruling
  rather than a replacement for it.

- [ ] **Step 5: Update `docs/not-built.md`**

- §1.3: the congenital hold mechanism is wired; the condition list remains provider work.
  Point at `GAP-015`.
- §2.1: mark `display_name`, `date_of_birth`, `sex`, `language_id`, dated growth,
  `likes`/`dislikes`/`accepted` and the clinical-condition fields as built in
  `child_profile` and its four child tables. Leave `vaccine_history[]`,
  `development_observations[]`, `priority_goals[]`, `consultation_date`, `reviewed_by` and
  `clinician_approval_id` outstanding, and note that the first two are now unblocked
  because their reference tables exist.
- Keep `equipment` listed as deliberately not collected, with the reason.
- §3: unchanged. None of it is fixed by this work.

- [ ] **Step 6: Update `README.md`**

Add `/api/reference/book1-blocks` to the endpoint table, and add a line to the run
instructions noting that `cmd/import` now loads 30 tables from 11 workbooks.

- [ ] **Step 7: Commit**

```bash
git add CLAUDE.md docs/not-built.md README.md
git commit -m "Document the Book 1 content layer and the canonical child profile"
```

---

## Self-review notes

Checked against the spec, sections 6 and 7:

- Spec §6 table of nine sheets, mixed header rows: Task 1 DDL, Task 2 bindings.
- Spec §6a (Book Assembly Logic authoritative, steps 12-14 missing): Task 1's
  `book1_assembly_step` plus `TestAssemblyStepsRecordTheMissingRange` in Task 3. `GAP-014`
  is seeded by the other plan's Task 3; this plan asserts it exists.
- Spec §6b (link columns are guidance, no parser): Task 1's column comments,
  `TestGuidanceColumnsAreNotForeignKeys` in Task 3.
- Spec §6c (`AI_Can_Draft` enforced before any assembler): Task 1's CHECK,
  `TestAICanDraftGateIsPinned` in Task 3, the console badge in Task 4.
- Spec §6 `Book_Order` not id order: Task 1 comment,
  `TestBookOrderIsCompleteAndNotBlockIDOrder` in Task 3, `ORDER BY book_order` in Task 4.
- Spec §7 tables, dated growth, three-state allergy, acute expiry, no equipment,
  clinician-entered z-scores: Task 5.
- Spec §7 DOB stored and age derived, profile as a layer beside `ChildProfile`: Task 6.
- Spec §7 "suspected ranks down and raises a review flag": Task 7.
- Spec §7 acute expiry: Task 8.
- Spec §7 immutability shaping (who set it, when): `entered_by`/`entered_at` on every
  child table and `created_by`/`created_at` on the parent, Task 5.

Type consistency: `Stored`, `GrowthMeasurement`, `DeclaredAllergen`, `Preference` and
`ClinicalCondition` are defined in Task 6 step 4 and used by the Task 6 tests written in
step 1, which is why step 2 expects a compile failure. `models.ChildProfile.SuspectedAllergens`
is added in Task 6 step 3, produced by `ToChildProfile` in step 4, and consumed by
`applySuspectedAllergenRank` in Task 7. `unscreenedGroups` is introduced in Task 7 step 3
and adopted by `steps_hard.go` in step 4, so only one definition ever exists. The step
count moves 13 -> 14 in the other plan's Task 6 and 14 -> 15 in this plan's Task 7; run
the plans in that order or reconcile the assertion once.

Cross-plan dependency: this plan's `GAP-014` assertion in Task 3 requires the other plan's
Task 3 to have run. If these are executed independently, either run that task first or
temporarily drop the second half of `TestAssemblyStepsRecordTheMissingRange`.
