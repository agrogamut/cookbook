# Book 1 Content Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render the 25 Book 1 content blocks that currently print nothing, using provider
content already imported and currently unread, and give both books the structural furniture
(contents page, part dividers, correct page breaks) that a printed book needs.

**Architecture:** Four provider tables imported by migration `0013` are read by nothing:
`book1_daily_life_module` (13), `book1_monitoring_template` (18),
`book1_illness_feeding_block` (5) and `book1_evidence_source` (13). A fifth,
`age_feeding_stage_master` (10 stages x 43 columns), is read by the engine but never by the
book. Together with each block's own declared layout columns (`table_or_format`,
`writable_fields`, `monitoring_fields`, `parent_facing_output`) they supply the content for
31 of the 32 blocks. This plan adds a hand-written block-to-source seed, five templates, and
the loaders behind them. No new prose is written anywhere.

**Tech Stack:** Go 1.26, pgx/v5, golang-migrate, `html/template`, chromedp, Postgres 16.

**Spec:** `CLAUDE.md` (the hard rule and the Book 1 input table) plus
`docs/decisions.md` (the 18 August prose ruling). There is no separate design doc: this
plan implements content that the provider already authored and the importer already loaded,
so the argument is the data, recorded in Task 0's audit.

## Global Constraints

Copied verbatim from `CLAUDE.md`. Every task's requirements implicitly include these.

- **Every value that reaches a user must trace to a verified source.** The only sources this
  plan uses are the provider's own workbook tables. No external dataset, no computation, no
  drafted text.
- **When data is missing, the correct output is an explicit gap** - `null`, "not available",
  a disabled option, or a shorter list. Never a plausible-looking value.
- **No model-generated guidance prose in the unreviewed path.** A blank tracker whose column
  headings are the provider's own `writable_fields` invents nothing. A paragraph of sleep
  advice does. This plan renders structure and provider text; it writes no advice.
- **`ai_can_draft = 'N'`** on B1-009, B1-011, B1-012, B1-014 and B1-022 marks five blocks no
  drafted text may occupy. All five already render from provider tables and stay that way.
- **`book1_daily_life_module.ai_limit`** carries a per-row prohibition ("Do not diagnose
  sleep disorder"). It is printed on the page it constrains, not silently honoured.
- **Every unit of a book is either rendered or reported.** Block-level omissions carry the
  `[block] ` marker and are counted against the corpus total by the conservation tests.
- **The provider's `Review_Status` and `Data_Quality` stay verbatim.** Nothing is marked
  approved locally, and no template may produce a book without the provisional banner.
- **Steps 1 (age) and 2 (allergy) stay hard filters** with no override anywhere in the UI or
  the book.
- **Never mention claude, anthropic or ai** in code, comments, commit messages or output.
  No emojis.
- Verify with `go build ./...`, `go vet ./...`, and
  `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...` before calling a task done.

---

## Why this plan exists

Measured on the current `main` (`0892413`), a book generated for a 4-year-old:

| | Book 1 | Book 2 |
|---|---|---|
| Pages | 11 | 21 |
| Sections rendered | 7 of 32 blocks | - |
| Blocks reported as omitted | 25 | - |

Of those 25 omissions, 24 read `has no template mapping and was not rendered`. That message
is accurate about the code and wrong about the data: the provider declared each block's
format, its writable columns and its monitoring axis, and shipped the body content in four
separate tables. The mapping is absent, not the content.

One block stays omitted after this plan and must: **B1-004**, growth trend interpretation.
Its declared input is a z-score/percentile engine. Interpreting a trend means computing
against the WHO reference tables, which this project does not carry, and a trend stated
without them is a clinical finding with no source. Recorded z-scores print because a
clinician entered them; none is ever computed.

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `internal/db/migrations/0018_book1_block_sources.up.sql` | `book1_block_source` seed: which provider rows feed which block. Plus `GAP-025`, `GAP-026`. |
| `internal/db/migrations/0018_book1_block_sources.down.sql` | Drop the table and the two gap rows. |
| `internal/book/sources.go` | Loaders for the four unread tables plus the block-source map. One query each, no per-block round trips. |
| `internal/book/sources_test.go` | Every seeded source id resolves; every loader returns the expected row count. |
| `internal/book/templates/book1/stage.html` | `B1-STAGE-01` - age-stage feeding guidance from `age_feeding_stage_master`. |
| `internal/book/templates/book1/daily.html` | `B1-DAILY-01` - a daily-life domain: reference, goal, red flag, referral, tracker. |
| `internal/book/templates/book1/illness.html` | `B1-ILLNESS-01` - illness feeding blocks. |
| `internal/book/templates/book1/tracker.html` | `B1-TRACKER-01` - generic reference-vs-actual tracker from declared columns. |
| `internal/book/templates/book1/safety.html` | `B1-SAFETY-01` - the child's own allergy and choking card. |
| `internal/book/templates/book1/refs.html` | `B1-REFS-01` - evidence sources with their stated limitations. |
| `internal/book/templates/book1/contents.html` | `B1-TOC-01` - contents page, built from what actually rendered. |
| `internal/book/templates/book1/divider.html` | `B1-PART-01` - part divider. |

**Modified:**

| File | Change |
|---|---|
| `internal/book/book1.go` | `blockTemplate` grows to 31 entries; per-template content population; parts; contents. |
| `internal/book/types.go` | `Section` gains `Tracker`, `Domain`, `Illness`, `Refs`, `Stage`, `Part`; new row types. |
| `internal/book/templates/book1/body.html` | Dispatch the six new template ids; emit contents and dividers. |
| `internal/book/templates/tokens.css` | `break-before` instead of `break-after`; divider, contents and tracker styles. |
| `internal/book/templates/book2/body.html` | Contents page for Book 2. |
| `internal/book/assemble_test.go` | Conservation updated: 31 rendered, 1 reported. |
| `internal/book/render_test.go` | Template dispatch for the six new ids; writing-line assertions per template. |
| `internal/db/book1_test.go` | Block-source seed integrity, both join directions. |
| `CLAUDE.md` | The Book 1 input table and the gap-register arithmetic. |

---

## Task 0: Audit the block-to-source mapping and write the seed data

This task produces the mapping every later task consumes. It is a data task, not a code
task, and it is done first because a wrong row here is a wrong page in a pediatric book.

**Files:**
- Create: `docs/book1-block-sources.md` (the audit, with the reasoning per row)

- [ ] **Step 1: Dump every unmapped block with its declared shape**

```bash
export PGPASSWORD=recipie
psql -h localhost -p 55432 -U recipie -d recipie -x -c "
  SELECT block_id, part, section, subsection, age_from_mo, age_to_mo,
         content_purpose, parent_facing_output, table_or_format,
         writable_fields, monitoring_fields, ideal_vs_actual,
         alarm_or_red_flag_block, doctor_approach_block,
         nutrition_target_link, clinical_rule_link, source_id, ai_can_draft
  FROM book1_content_block ORDER BY book_order" > /tmp/blocks.txt
```

- [ ] **Step 2: Dump the four candidate source tables in full**

```bash
for t in book1_daily_life_module book1_monitoring_template \
         book1_illness_feeding_block book1_evidence_source; do
  echo "=== $t ==="
  psql -h localhost -p 55432 -U recipie -d recipie -x -c "SELECT * FROM $t"
done > /tmp/sources.txt
```

- [ ] **Step 3: Write the audit, one row per block**

For each of the 32 blocks record: the block id, the source table and row ids that feed it,
the template it should use, and one sentence of justification. Where a block has no source,
say so and say what its absence costs. This is the file a reviewer checks the seed against.

The mapping below is the starting point, derived from `domain`, `section` and `parameter`
matching. **Verify each row against the dumps before seeding it** - a domain name that reads
like a block title is a hypothesis, not a join.

| Block | Section | Template | Source rows |
|---|---|---|---|
| B1-001 | Child Profile | `B1-PROFILE-01` | (built) child profile |
| B1-002 | Consultation Summary | `B1-TRACKER-01` | writable only: `writable_fields` |
| B1-003 | Growth Monitoring | `B1-GROWTH-01` | (built) `child_growth_measurement` |
| B1-004 | Growth Monitoring (trend) | **none** | **stays omitted - no z-score engine** |
| B1-005 | Personal Nutrition Target | `B1-STAGE-01` | `age_feeding_stage_master` for age |
| B1-006 | Meal schedule | `B1-STAGE-01` | `age_feeding_stage_master` meal frequency |
| B1-007 | Feeding Approach | `B1-STAGE-01` | `age_feeding_stage_master` responsive/variety rules |
| B1-008 | Feeding Approach (comparison) | `B1-STAGE-01` | `age_feeding_stage_master` adjacent stages |
| B1-009 | Vaccination | `B1-VAX-01` | (built) `book1_vaccine_schedule` |
| B1-010 | Vaccine reaction log | `B1-TRACKER-01` | `writable_fields`; `PM-VAX-01` |
| B1-011 | Development Monitoring | `B1-DEV-01` | (built) `book1_development_milestone` |
| B1-012 | Development red flags | `B1-RED-01` | (built) milestone `concern_or_red_flag` |
| B1-013 | Age-specific Monitoring | `B1-TRACKER-01` | `PM-DEV-01`, `PM-GROW-*` |
| B1-014 | Development by Age | `B1-DEV-01` | (built) `book1_development_milestone` |
| B1-015 | Common Illness Feeding | `B1-ILLNESS-01` | `IF-001`..`IF-005` |
| B1-016 | Illness supportive table | `B1-ILLNESS-01` | `IF-001`..`IF-005`, `PM-ILL-01` |
| B1-017 | Food + bowel tracker | `B1-TRACKER-01` | `PM-BOWEL-01`, `PM-ILL-01` |
| B1-018 | Allergy & Safety | `B1-SAFETY-01` | `child_allergen`, `allergy_safety_master`, `choking_texture_safety` |
| B1-019 | Food Acceptance Tracker | `B1-TRACKER-01` | `PM-FOOD-01`, `PM-DIV-01` |
| B1-020 | Weekly Monitoring | `B1-TRACKER-01` | `PM-GROW-01/02`, `PM-FOOD-01`, `PM-DIV-01`, `PM-BOWEL-01`, `PM-ACT-01` |
| B1-021 | Follow-up | `B1-TRACKER-01` | `PM-FU-01` |
| B1-022 | Reference & Disclaimer | `B1-REFS-01` | all 13 `book1_evidence_source` |
| B1-023 | Toilet Training readiness | `B1-DAILY-01` | `DL-TOIL-01`, `DL-TOIL-02`; `PM-TOIL-01` |
| B1-024 | Toilet Training red flag | `B1-DAILY-01` | `DL-TOIL-03`; `PM-TOIL-01` |
| B1-025 | Sleep & Routine | `B1-DAILY-01` | `DL-SLEEP-01`; `PM-SLEEP-01` |
| B1-026 | Oral & Dental Health | `B1-DAILY-01` | `DL-DENT-01`; `PM-DENT-01` |
| B1-027 | Self-Care & Adaptive | `B1-DAILY-01` | `DL-SELF-01`, `DL-SELF-02`; `PM-SELF-01` |
| B1-028 | Screen & Digital Habits | `B1-DAILY-01` | `DL-SCREEN-01`, `DL-SCREEN-02`; `PM-SCR-01` |
| B1-029 | Physical Activity | `B1-DAILY-01` | `DL-ACT-01`; `PM-ACT-01` |
| B1-030 | School, Learning & Behaviour | `B1-DAILY-01` | `DL-SCHOOL-01`, `DL-BEH-01`; `PM-SCHOOL-01`, `PM-BEH-01` |
| B1-031 | Daily-Life Dashboard | `B1-TRACKER-01` | every `PM-*` in age context |
| B1-032 | Adolescent Self-Management | `B1-DAILY-01` | `DL-ADO-01` |

- [ ] **Step 4: Commit the audit**

```bash
git add docs/book1-block-sources.md
git commit -m "Record which provider rows feed each Book 1 block"
```

---

## Task 1: Seed the block-to-source map

**Files:**
- Create: `internal/db/migrations/0018_book1_block_sources.up.sql`
- Create: `internal/db/migrations/0018_book1_block_sources.down.sql`
- Test: `internal/db/book1_test.go` (modify)

**Interfaces:**
- Produces: table `book1_block_source(block_id, source_table, source_row_id, ordinal)`,
  primary key `(block_id, source_table, source_row_id)`, FK on `block_id`.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/book1_test.go`:

```go
// TestBlockSourceSeedResolvesBothWays holds the hand-written block-to-source map to the same
// standard as culture_region_map: every row must name a source that exists, and every source
// table that has content must be reachable from some block. A seed row pointing at a deleted
// provider row is a page that renders empty; a source table nothing points at is content the
// provider shipped and the book silently drops.
func TestBlockSourceSeedResolvesBothWays(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, tc := range []struct{ table, idCol string }{
		{"book1_daily_life_module", "dailylife_id"},
		{"book1_monitoring_template", "template_id"},
		{"book1_illness_feeding_block", "illness_block_id"},
		{"book1_evidence_source", "source_id"},
	} {
		t.Run(tc.table+"/no dangling seed rows", func(t *testing.T) {
			var n int
			err := pool.QueryRow(ctx, `
				SELECT count(*) FROM book1_block_source s
				WHERE s.source_table = $1
				  AND NOT EXISTS (
				    SELECT 1 FROM `+tc.table+` t WHERE t.`+tc.idCol+` = s.source_row_id)`,
				tc.table).Scan(&n)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if n != 0 {
				t.Errorf("%d seed rows name a %s row that does not exist", n, tc.table)
			}
		})

		t.Run(tc.table+"/no unreachable source rows", func(t *testing.T) {
			var n int
			err := pool.QueryRow(ctx, `
				SELECT count(*) FROM `+tc.table+` t
				WHERE NOT EXISTS (
				  SELECT 1 FROM book1_block_source s
				  WHERE s.source_table = $1 AND s.source_row_id = t.`+tc.idCol+`)`,
				tc.table).Scan(&n)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if n != 0 {
				t.Errorf("%d %s rows are reachable from no block and would never print", n, tc.table)
			}
		})
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/db/ -run TestBlockSourceSeed -v
```

Expected: FAIL, `relation "book1_block_source" does not exist`.

- [ ] **Step 3: Write the migration**

`0018_book1_block_sources.up.sql`. Header comment first, explaining why the map is
hand-written:

```sql
-- Which provider rows supply the body of each Book 1 block.
--
-- Hand-written, and it has to be: the workbook carries no foreign key between
-- book1_content_block and the four tables that hold its content. The join is by meaning --
-- DL-SLEEP-01's "Sleep routine" domain against B1-025's "Sleep & Routine" section -- and a
-- fuzzy name match would put a toilet-training red flag on a dental page. Twenty-nine rows
-- read once by a person beats a matcher nobody can audit, which is the same argument that
-- produced culture_region_map in 0002.
--
-- ordinal fixes the order rows appear within a block, because the workbook's own row order
-- is alphabetical by id and DL-TOIL-01 (readiness) must precede DL-TOIL-02 (progress).

CREATE TABLE book1_block_source (
    block_id      text    NOT NULL REFERENCES book1_content_block(block_id),
    source_table  text    NOT NULL,
    source_row_id text    NOT NULL,
    ordinal       integer NOT NULL DEFAULT 1,
    note          text,
    PRIMARY KEY (block_id, source_table, source_row_id),

    -- An allowlist, not free text. A typo'd table name would silently produce a block that
    -- loads nothing, and the loader has one query per table anyway.
    CONSTRAINT book1_block_source_table_known CHECK (source_table IN (
        'book1_daily_life_module',
        'book1_monitoring_template',
        'book1_illness_feeding_block',
        'book1_evidence_source'))
);

INSERT INTO book1_block_source (block_id, source_table, source_row_id, ordinal, note) VALUES
    ('B1-023', 'book1_daily_life_module',    'DL-TOIL-01',   1, 'readiness precedes progress'),
    ('B1-023', 'book1_daily_life_module',    'DL-TOIL-02',   2, NULL),
    ('B1-023', 'book1_monitoring_template',  'PM-TOIL-01',   1, NULL),
    ('B1-024', 'book1_daily_life_module',    'DL-TOIL-03',   1, NULL),
    ('B1-024', 'book1_monitoring_template',  'PM-TOIL-01',   1, NULL),
    ('B1-025', 'book1_daily_life_module',    'DL-SLEEP-01',  1, NULL),
    ('B1-025', 'book1_monitoring_template',  'PM-SLEEP-01',  1, NULL),
    ('B1-026', 'book1_daily_life_module',    'DL-DENT-01',   1, NULL),
    ('B1-026', 'book1_monitoring_template',  'PM-DENT-01',   1, NULL),
    ('B1-027', 'book1_daily_life_module',    'DL-SELF-01',   1, 'self-feeding before dressing'),
    ('B1-027', 'book1_daily_life_module',    'DL-SELF-02',   2, NULL),
    ('B1-027', 'book1_monitoring_template',  'PM-SELF-01',   1, NULL),
    ('B1-028', 'book1_daily_life_module',    'DL-SCREEN-01', 1, 'meal screen before bedtime screen'),
    ('B1-028', 'book1_daily_life_module',    'DL-SCREEN-02', 2, NULL),
    ('B1-028', 'book1_monitoring_template',  'PM-SCR-01',    1, NULL),
    ('B1-029', 'book1_daily_life_module',    'DL-ACT-01',    1, NULL),
    ('B1-029', 'book1_monitoring_template',  'PM-ACT-01',    1, NULL),
    ('B1-030', 'book1_daily_life_module',    'DL-SCHOOL-01', 1, NULL),
    ('B1-030', 'book1_daily_life_module',    'DL-BEH-01',    2, NULL),
    ('B1-030', 'book1_monitoring_template',  'PM-SCHOOL-01', 1, NULL),
    ('B1-030', 'book1_monitoring_template',  'PM-BEH-01',    2, NULL),
    ('B1-032', 'book1_daily_life_module',    'DL-ADO-01',    1, NULL),
    ('B1-015', 'book1_illness_feeding_block','IF-001',       1, NULL),
    ('B1-015', 'book1_illness_feeding_block','IF-002',       2, NULL),
    ('B1-015', 'book1_illness_feeding_block','IF-003',       3, NULL),
    ('B1-015', 'book1_illness_feeding_block','IF-004',       4, NULL),
    ('B1-015', 'book1_illness_feeding_block','IF-005',       5, NULL),
    ('B1-016', 'book1_monitoring_template',  'PM-ILL-01',    1, NULL),
    ('B1-017', 'book1_monitoring_template',  'PM-BOWEL-01',  1, NULL),
    ('B1-013', 'book1_monitoring_template',  'PM-DEV-01',    1, NULL),
    ('B1-013', 'book1_monitoring_template',  'PM-GROW-01',   2, NULL),
    ('B1-013', 'book1_monitoring_template',  'PM-GROW-02',   3, NULL),
    ('B1-013', 'book1_monitoring_template',  'PM-GROW-03',   4, NULL),
    ('B1-010', 'book1_monitoring_template',  'PM-VAX-01',    1, NULL),
    ('B1-019', 'book1_monitoring_template',  'PM-FOOD-01',   1, NULL),
    ('B1-019', 'book1_monitoring_template',  'PM-DIV-01',    2, NULL),
    ('B1-021', 'book1_monitoring_template',  'PM-FU-01',     1, NULL);

-- B1-020 (weekly dashboard) and B1-031 (daily-life dashboard) are summaries over every
-- monitoring template in the child's age context rather than a fixed row list, so they are
-- resolved in Go and carry no seed rows. The reachability half of
-- TestBlockSourceSeedResolvesBothWays is satisfied by the per-domain blocks above.

-- B1-022 takes every evidence source; listing 13 ids here would be a second copy of the
-- table that drifts when the provider adds a source.
INSERT INTO book1_block_source (block_id, source_table, source_row_id, ordinal)
SELECT 'B1-022', 'book1_evidence_source', source_id, row_number() OVER (ORDER BY source_id)
FROM book1_evidence_source;

-- Two gaps this plan records rather than fills.
--
-- Column names and the severity vocabulary are the register's own, checked against the live
-- schema rather than guessed: severity is CHECKed against blocker/major/minor/parked,
-- measured_by against seed/importer, and ui_behaviour and resolution_path are NOT NULL --
-- the register refuses a gap that does not say what the UI does about it and how it closes.
INSERT INTO gap_register
  (gap_id, severity, area, source_table, source_column, description,
   affected_rows, measured_by, ui_behaviour, resolution_path, measured_at)
VALUES
  ('GAP-025', 'minor', 'book2', 'recipe_master', NULL,
   'No recipe imagery exists. No provider workbook carries an image column and neither loaded external dataset has one. The Tier 2 image sets (Khana, FoodBD) label dish types, not the 940 combinatorially-named provider recipes, so a join would reproduce the wrong-match failure already measured on method text; both also lack a use basis for operational output.',
   940, 'seed',
   'Recipe cards print without a photograph and reserve no empty frame for one.',
   'Commission photographs against the recipe list, or obtain an image set keyed to Recipe_ID. No join fills this.',
   now()),
  ('GAP-026', 'major', 'book1', 'book1_content_block', 'doctor_approach_block',
   'Twenty-seven of the 32 Book 1 blocks declare a doctor_approach_block or alarm_or_red_flag_block, but the workbook carries no text for either on the block row itself. Where a mapped source row supplies concern_or_red_flag or approach_doctor_or_specialist the page prints it verbatim; where none does, there is no provider text for that box.',
   27, 'seed',
   'The page prints the heading and a writing line. No advice is drafted to fill it.',
   'Provider supplies the red-flag and doctor-approach text per block, or a clinical editor writes it through the reviewed path.',
   now());
```

`0018_book1_block_sources.down.sql`:

```sql
DELETE FROM gap_register WHERE gap_id IN ('GAP-025', 'GAP-026');
DROP TABLE IF EXISTS book1_block_source;
```

- [ ] **Step 4: Rebuild and run the test**

The database must be rebuilt, not migrated in place, for the same reason `CLAUDE.md` records
for the 0012/0013 merge:

```bash
scripts/dev_db.fish down && scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import && go run ./cmd/enrich
TEST_DATABASE_URL=$DATABASE_URL go test ./internal/db/ -run TestBlockSource -v
```

Expected: PASS. `SELECT count(*) FROM gap_register` now returns 26.

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/0018_* internal/db/book1_test.go
git commit -m "Map each Book 1 block to the provider rows that fill it"
```

---

## Task 2: Load the four unread source tables

**Files:**
- Create: `internal/book/sources.go`
- Test: `internal/book/sources_test.go`

**Interfaces:**
- Consumes: `book1_block_source` from Task 1.
- Produces:

```go
// BlockSources is every provider row that feeds a block, keyed by block id.
type BlockSources struct {
    Daily      map[string][]DailyLifeModule
    Monitoring map[string][]MonitoringTemplate
    Illness    map[string][]IllnessFeedingBlock
    Evidence   map[string][]EvidenceSource
}

func LoadBlockSources(ctx context.Context, pool *pgxpool.Pool) (BlockSources, error)
func LoadAgeStage(ctx context.Context, pool *pgxpool.Pool, ageMonths int) (*AgeStage, error)
```

- [ ] **Step 1: Write the failing test**

```go
func TestLoadBlockSourcesGroupsByBlock(t *testing.T) {
	pool := testPool(t)
	src, err := book.LoadBlockSources(context.Background(), pool)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// B1-027 seeds two daily-life rows in a deliberate order: self-feeding before dressing.
	// Ordinal is the provider-facing reason the seed carries the column at all, so the
	// loader honouring it is worth pinning rather than assuming.
	got := src.Daily["B1-027"]
	if len(got) != 2 {
		t.Fatalf("B1-027: want 2 daily-life rows, got %d", len(got))
	}
	if got[0].DailyLifeID != "DL-SELF-01" || got[1].DailyLifeID != "DL-SELF-02" {
		t.Errorf("ordinal not honoured: got %s then %s", got[0].DailyLifeID, got[1].DailyLifeID)
	}
	if len(src.Evidence["B1-022"]) != 13 {
		t.Errorf("B1-022: want all 13 evidence sources, got %d", len(src.Evidence["B1-022"]))
	}
}

func TestLoadAgeStageCoversEveryAge(t *testing.T) {
	pool := testPool(t)
	// Every month from birth to 18 years must resolve to exactly one stage, or a child of
	// that age gets a feeding page with nothing on it. The masters declare 10 stages with
	// no gaps; this asserts the declaration rather than trusting it.
	for m := 0; m <= 228; m++ {
		st, err := book.LoadAgeStage(context.Background(), pool, m)
		if err != nil {
			t.Fatalf("age %d months: %v", m, err)
		}
		if st == nil {
			t.Errorf("age %d months resolves to no feeding stage", m)
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -run 'TestLoadBlockSources|TestLoadAgeStage' -v
```

Expected: FAIL, `undefined: book.LoadBlockSources`.

- [ ] **Step 3: Implement `sources.go`**

Four queries, each joining its table to `book1_block_source` and ordering by
`(block_id, ordinal)`. One query per table, not one per block: 32 blocks x 4 tables is 128
round trips for 49 rows of data.

`LoadAgeStage` selects the row where `age_from_months <= $1 AND age_to_months >= $1`,
ordered by `age_from_months DESC` and limited to 1, so an overlap resolves to the more
specific stage rather than erroring. Return `nil, nil` when no stage matches - the caller
reports it as an omission, and a missing stage is an absence rather than a failure.

Every struct field is the provider's text verbatim. No field is defaulted, trimmed to a
sentence, or reworded.

- [ ] **Step 4: Run the tests**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/ -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/book/sources.go internal/book/sources_test.go
git commit -m "Read the four Book 1 provider tables nothing was reading"
```

---

## Task 3: The daily-life template

Ten blocks, the largest single group, and the one that gives a parent the age-relevant
everyday content: sleep, screens, teeth, toilet training, activity, school, self-care.

**Files:**
- Create: `internal/book/templates/book1/daily.html`
- Modify: `internal/book/types.go`, `internal/book/book1.go`,
  `internal/book/templates/book1/body.html`
- Test: `internal/book/render_test.go`

**Interfaces:**
- Consumes: `BlockSources.Daily`, `BlockSources.Monitoring` from Task 2.
- Produces: `Section.Domains []DailyDomain` where

```go
// DailyDomain is one daily-life module as the provider wrote it. Every field is their text;
// none is summarised, and an empty one prints as a writing line rather than as absent.
type DailyDomain struct {
    ID         string
    Domain     string // "Sleep routine"
    AgeContext string // "All ages"
    Reference  string // readiness_or_reference
    Goal       string // progress_goal
    RedFlag    string // concern_or_red_flag
    Referral   string // approach_doctor_or_specialist
    // AILimit is the provider's own prohibition on this row ("Do not diagnose sleep
    // disorder"). Printed on the page it constrains: a limit honoured silently is a limit
    // the next person to touch this template does not know about.
    AILimit    string
    Display    string // book1_display, the tracker's name
    Tracker    *TrackerSpec
}
```

- [ ] **Step 1: Write the failing test**

```go
// TestDailyTemplatePrintsProviderTextAndNothingElse pins the two halves of the prose rule on
// the busiest new template: the provider's own reference, goal, red flag and referral text
// must all reach the page, and the page must carry no sentence that is not one of them.
func TestDailyTemplatePrintsProviderTextAndNothingElse(t *testing.T) {
	sec := book.Section{
		BlockID: "B1-025", TemplateID: "B1-DAILY-01", Title: "Sleep & Routine",
		Domains: []book.DailyDomain{{
			ID: "DL-SLEEP-01", Domain: "Sleep routine", AgeContext: "All ages",
			Reference: "Consistent age-appropriate routine and adequate restorative sleep",
			Goal:      "Regular bedtime/wake pattern",
			RedFlag:   "Loud habitual snoring, breathing pauses, marked daytime sleepiness",
			Referral:  "Pediatric/sleep/ENT review as indicated",
			AILimit:   "Do not diagnose sleep disorder",
			Display:   "Sleep diary",
			Tracker: &book.TrackerSpec{
				Columns: []string{"Bedtime", "Sleep onset", "Waking", "Naps", "Parent note"},
				Rows:    7,
			},
		}},
	}
	html := renderSection(t, sec)

	for _, want := range []string{
		"Consistent age-appropriate routine and adequate restorative sleep",
		"Regular bedtime/wake pattern",
		"Loud habitual snoring",
		"Pediatric/sleep/ENT review as indicated",
		"Do not diagnose sleep disorder",
		"Sleep diary",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("provider text missing from the page: %q", want)
		}
	}

	// Seven blank rows, one per day, each with a writing line per column. Asserted on the
	// element and not on the class name: tokens.css defines .write-line and is inlined into
	// every render, so a substring test for "write-line" passes on a template that emits
	// none. That exact defect already shipped once on this branch.
	if got := strings.Count(html, `<span class="write-line">`); got != 7*5 {
		t.Errorf("tracker: want %d writing lines (7 rows x 5 columns), got %d", 7*5, got)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/book/ -run TestDailyTemplate -v
```

Expected: FAIL, `unknown field Domains in struct literal`.

- [ ] **Step 3: Add the types, the template and the dispatch**

`daily.html` renders, per domain: a definition table of reference / goal, then the tracker
grid with `writable_fields` as headers and `Rows` blank rows, then the red-flag callout at
`severity="warning"` and the referral note. The `ai_limit` prints as a small footnote under
the domain heading.

An empty provider field renders as `<span class="write-line"></span>`, never as an empty
cell and never as invented filler.

- [ ] **Step 4: Run the tests, then verify by looking**

```bash
go test ./internal/book/ -v
```

Then print a real book and read the page - the count above proves writing lines exist, not
that the page is legible:

```bash
curl -s -X POST localhost:8080/api/books/generate/book1.pdf \
  -H 'content-type: application/json' -d @/tmp/gen.json -o /tmp/b1.pdf
pdftoppm -png -r 80 -f 12 -l 16 /tmp/b1.pdf /tmp/daily
```

Open the images. Check: no heading stranded at a page foot, no tracker split across a page
with its header lost, no red-flag callout separated from the domain it belongs to.

- [ ] **Step 5: Commit**

```bash
git add internal/book/templates/book1/daily.html internal/book/types.go \
        internal/book/book1.go internal/book/templates/book1/body.html \
        internal/book/render_test.go
git commit -m "Render the ten daily-life blocks from the provider's modules"
```

---

## Task 4: The tracker template

Nine blocks: consultation goals, vaccine reaction log, age-specific monitoring, bowel and
food trackers, food acceptance, weekly dashboard, follow-up, daily-life dashboard.

**Files:**
- Create: `internal/book/templates/book1/tracker.html`
- Modify: `internal/book/types.go`, `internal/book/book1.go`, `body.html`
- Test: `internal/book/render_test.go`

**Interfaces:**
- Produces:

```go
// TrackerSpec is a blank form: the provider's declared columns and a row count.
//
// The columns come from the block's own writable_fields, split on ';', or from a monitoring
// template's reference/actual/date/notes/alarm/review columns. Reading those columns as the
// form's headers is following the provider's layout declaration, not inventing a layout --
// which is the distinction that lets a tracker print under the prose rule while drafted
// advice may not.
type TrackerSpec struct {
    Title      string
    Reference  string // reference_or_ideal_column, the "ideal" side of ideal-vs-actual
    Frequency  string // "Selected week" - decides Rows
    Columns    []string
    Rows       int
    AlarmNote  string // alarm_column, printed as a warning callout under the grid
    ReviewNote string // doctor_review_column
}
```

- [ ] **Step 1: Write the failing test**

```go
// TestTrackerRowCountFollowsFrequency pins the one derived value on this template. A weekly
// tracker with one row is useless and a daily one with 30 is three wasted pages, so the row
// count comes from the provider's own frequency string rather than a constant -- and an
// unrecognised frequency falls to a labelled default rather than guessing high.
func TestTrackerRowCountFollowsFrequency(t *testing.T) {
	for _, tc := range []struct {
		frequency string
		wantRows  int
	}{
		{"Daily / visit", 14},
		{"Selected week", 7},
		{"Weekly", 8},
		{"Monthly/visit", 6},
		{"At follow-up", 3},
		{"As needed", 6},
		{"", 6}, // no declared frequency: the labelled default
	} {
		t.Run(tc.frequency, func(t *testing.T) {
			if got := book.TrackerRows(tc.frequency); got != tc.wantRows {
				t.Errorf("frequency %q: want %d rows, got %d", tc.frequency, tc.wantRows, got)
			}
		})
	}
}

// TestTrackerNeverPrintsAnEmptyGrid: a block whose declared writable_fields is empty has no
// form to print, and a heading over a zero-column table is the empty-section defect this
// package has already shipped once. It must be reported as an omission instead.
func TestTrackerNeverPrintsAnEmptyGrid(t *testing.T) {
	sec := book.Section{TemplateID: "B1-TRACKER-01", Tracker: &book.TrackerSpec{Columns: nil, Rows: 7}}
	if book.SectionHasContent(sec) {
		t.Error("a tracker with no columns reports as having content; it would print an empty grid")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Expected: FAIL, `undefined: book.TrackerRows`.

- [ ] **Step 3: Implement**

`TrackerRows` matches the frequency string case-insensitively against the provider's own
vocabulary (the 18 distinct `frequency` values in `book1_monitoring_template`), and returns
6 for anything unrecognised. The default is documented in the code as a default, and the
template prints the frequency string on the page so a reader can see what the grid is for.

`SectionHasContent` is the guard the conservation test needs: a section is rendered only if
it has rows, cards, domains, a tracker with columns, or a callout. Wire it into
`AssembleBook1` so a block that resolves to nothing is reported, not printed empty.

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/book/ -v
```

- [ ] **Step 5: Commit**

```bash
git commit -am "Print the nine tracker blocks from their declared columns"
```

---

## Task 5: Illness feeding, age-stage feeding, safety card and references

Four templates, grouped into one task because each is a single table read into a single
table-shaped page, and a reviewer would accept or reject them together.

**Files:**
- Create: `internal/book/templates/book1/illness.html`, `stage.html`, `safety.html`,
  `refs.html`
- Modify: `internal/book/types.go`, `internal/book/book1.go`, `body.html`
- Test: `internal/book/render_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// TestIllnessBlockPrintsTheEngineLimit: every illness row carries a book_engine_limit ("does
// not replace clinical assessment"), and an illness feeding page that omits it reads as
// treatment advice. It is the single most load-bearing string on the page.
func TestIllnessBlockPrintsTheEngineLimit(t *testing.T) {
	sec := book.Section{
		TemplateID: "B1-ILLNESS-01",
		Illness: []book.IllnessBlock{{
			ID: "IF-002", Situation: "Acute diarrhoea",
			SupportiveMessage: "Continue age-appropriate feeding as tolerated",
			WhatToMonitor:     "Stool frequency; hydration",
			RedFlags:          "Blood in stool; reduced urine output; lethargy",
			EngineLimit:       "Does not replace clinical assessment",
		}},
	}
	html := renderSection(t, sec)
	if !strings.Contains(html, "Does not replace clinical assessment") {
		t.Error("illness page omits the provider's engine limit")
	}
	if !strings.Contains(html, "Blood in stool") {
		t.Error("illness page omits the red flags")
	}
}

// TestReferencePagePrintsEveryLimitation: the reference page exists to say what each source
// does and does not cover. Printing the citation without important_limitation turns a
// hedged source into an endorsement.
func TestReferencePagePrintsEveryLimitation(t *testing.T) {
	sec := book.Section{
		TemplateID: "B1-REFS-01",
		Refs: []book.EvidenceSource{
			{ID: "WHO-GROWTH", Authority: "WHO", Topic: "Growth standards/reference",
				Limitation: "Use validated z-score calculation module"},
			{ID: "IAP-ACVIP-2025", Authority: "IAP / ACVIP", Topic: "Vaccination 0-18 y",
				Limitation: "Catch-up/product/special-risk details require clinical judgement"},
		},
	}
	html := renderSection(t, sec)
	for _, want := range []string{"Use validated z-score calculation module",
		"require clinical judgement", "WHO-GROWTH", "IAP-ACVIP-2025"} {
		if !strings.Contains(html, want) {
			t.Errorf("reference page missing %q", want)
		}
	}
}

// TestSafetyCardNeverOffersAnOverride is the safety boundary on paper. Steps 1 and 2 are hard
// filters with no operator override, and the printed page must not suggest one either.
func TestSafetyCardNeverOffersAnOverride(t *testing.T) {
	sec := book.Section{
		TemplateID: "B1-SAFETY-01",
		Safety: &book.SafetyCard{
			Confirmed: []string{"Peanut", "Cow milk"},
			Suspected: []string{"Egg"},
			Choking:   []book.ChokingRule{{Food: "Whole nuts", Rule: "Not before 5 years"}},
		},
	}
	html := renderSection(t, sec)
	if !strings.Contains(html, "Peanut") || !strings.Contains(html, "Cow milk") {
		t.Error("safety card omits a confirmed allergen")
	}
	// Suspected and confirmed must be separately labelled: a parent reading one list cannot
	// tell which exclusions are diagnosed and which are being ruled out.
	if !strings.Contains(html, "Suspected") || !strings.Contains(html, "Egg") {
		t.Error("safety card does not distinguish suspected from confirmed")
	}
	for _, forbidden := range []string{"override", "show anyway", "include excluded"} {
		if strings.Contains(strings.ToLower(html), forbidden) {
			t.Errorf("safety card offers an override path: %q", forbidden)
		}
	}
}
```

- [ ] **Step 2: Run and watch them fail**

- [ ] **Step 3: Implement the four templates**

`stage.html` prints the matched `age_feeding_stage_master` row as a labelled definition
list: meal frequency (breastfed and non-breastfed as separate rows, never merged), texture
minimum, responsive feeding rule, variety rule, animal-source rule, fruit and vegetable
rule, choking control, honey rule, salt and sugar approach, hard exclusions. Every value the
provider's own sentence. `B1-008`'s comparison table shows the adjacent stages either side
so a parent sees what changes next; the stages come from the same table.

`safety.html` reads the child's own `child_allergen` rows and joins to
`allergy_safety_master` and `choking_texture_safety` for the age band.

- [ ] **Step 4: Run the tests and print a book**

- [ ] **Step 5: Commit**

```bash
git commit -am "Render illness, feeding-stage, safety and reference blocks"
```

---

## Task 6: Structure - contents page, part dividers, page breaks

**Files:**
- Create: `internal/book/templates/book1/contents.html`, `divider.html`
- Modify: `internal/book/templates/tokens.css`, `body.html` (both books),
  `internal/book/book1.go`, `internal/book/book2.go`
- Test: `internal/book/render_test.go`, `internal/book/pdf_test.go`

- [ ] **Step 1: Write the failing test**

```go
// TestContentsListsOnlyRenderedSections: a contents page that names a block the book does not
// contain sends a reader hunting for a page that is not there. It is built from the rendered
// sections, never from the block list.
func TestContentsListsOnlyRenderedSections(t *testing.T) {
	b := book.Book1{Sections: []book.Section{
		{BlockID: "B1-001", Title: "Child Profile", Part: "A"},
		{BlockID: "B1-025", Title: "Sleep & Routine", Part: "J"},
	}}
	html := renderBook1(t, b)
	for _, want := range []string{"Child Profile", "Sleep & Routine"} {
		if !strings.Contains(html, want) {
			t.Errorf("contents omits %q", want)
		}
	}
	// B1-004 is always omitted and must never appear in a contents list.
	if strings.Contains(html, "Trend") {
		t.Error("contents names a section the book does not contain")
	}
}

// TestNoTrailingBlankPage: break-after: page on every section emits a break after the last
// one too, so the book ends on an empty sheet. Counted from the printed PDF, because this is
// a fragmentation behaviour no HTML assertion can see.
func TestNoTrailingBlankPage(t *testing.T) {
	if !book.BrowserAvailable() {
		t.Skip("no browser on PATH")
	}
	pdf := printTestBook1(t)
	last := pageText(t, pdf, numPages(t, pdf))
	// The running footer is drawn by Chromium on every page including a blank one, so a
	// blank page is not zero-length -- it is a page with nothing but the footer.
	if strings.TrimSpace(stripFooter(last)) == "" {
		t.Error("the book ends on a blank page")
	}
}
```

- [ ] **Step 2: Run and watch them fail**

- [ ] **Step 3: Implement**

In `tokens.css`, replace `.page-break { break-after: page; }` with
`.page-break { break-before: page; }` and apply it to every section but the first. Add a
comment recording why: `break-after` on the last section emits a break with nothing behind
it, and Chromium honours it as a physical page.

`divider.html` prints a part divider using `book1_content_block.part` (A-O, 15 parts) with
the part's first section title as its label - the workbook carries no part names, so the
divider is labelled by its contents rather than by an invented name, and a part with one
section gets no divider at all.

`contents.html` lists the rendered sections grouped by part. Page numbers are deliberately
absent: Chromium assigns them at print and the template cannot know them, and a contents
page with wrong numbers is worse than one with none.

- [ ] **Step 4: Print and read the result**

```bash
curl -s -X POST localhost:8080/api/books/generate.zip \
  -H 'content-type: application/json' -d @/tmp/gen.json -o /tmp/books.zip
unzip -o /tmp/books.zip -d /tmp/out
pdftoppm -png -r 60 /tmp/out/*-book1.pdf /tmp/out/pg
```

Open every page image. This is the check that counting cannot do: a section heading over an
empty area, a table whose header repeated onto a page with no rows under it, a callout split
from its block, a divider immediately followed by another divider.

- [ ] **Step 5: Commit**

```bash
git commit -am "Give both books a contents page, part dividers and correct page breaks"
```

---

## Task 7: Update the conservation tests and the documentation

**Files:**
- Modify: `internal/book/assemble_test.go`, `internal/db/book1_test.go`, `CLAUDE.md`

- [ ] **Step 1: Update the conservation assertion**

`TestEveryBlockIsEitherRenderedOrReported` currently passes with 7 rendered and 25 reported.
Tighten it: the count must still conserve, and B1-004 must be the only block-level omission
for a child whose age puts every other block in range.

```go
// TestOnlyTheZScoreBlockIsUnmapped: conservation alone cannot see this. 7-rendered-plus-25-
// reported conserves exactly as well as 31-plus-1, which is how the book shipped nearly empty
// while its own test stayed green. This pins which block is missing, not just how many.
func TestOnlyTheZScoreBlockIsUnmapped(t *testing.T) {
	pool := testPool(t)
	// A 4-year-old: inside the age range of every block except the adolescent one.
	b1, omissions, err := book.AssembleBook1(context.Background(), pool, fourYearOld(t), asOf)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	var unmapped []string
	for _, o := range omissions {
		if strings.Contains(o, "no template mapping") {
			unmapped = append(unmapped, o)
		}
	}
	if len(unmapped) != 1 || !strings.Contains(unmapped[0], "B1-004") {
		t.Errorf("want only B1-004 unmapped, got %d: %v", len(unmapped), unmapped)
	}
	if len(b1.Sections) < 25 {
		t.Errorf("want at least 25 rendered sections for a 4-year-old, got %d", len(b1.Sections))
	}
}
```

- [ ] **Step 2: Run the whole suite**

```bash
go build ./... && go vet ./...
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
cd web && npm test
```

- [ ] **Step 3: Update `CLAUDE.md`**

Three edits:

1. The "What the books know about a child" table: the four `no` rows stay `no`, but the "On
   the page" column changes for the blocks that now render a tracker rather than nothing.
2. The gap-register arithmetic: 24 becomes 26, and the check line
   `SELECT count(*) FROM gap_register` returning 24 becomes 26.
3. A new subsection under "The book renderer" recording what this plan established: the
   provider's content was present and unread, the block-to-source map is hand-written and
   why, and `writable_fields`-as-column-headers is following a layout declaration rather
   than inventing one.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "Hold the book to rendering every block the provider supplied content for"
```

---

## What this plan does not do

Stated so the next person does not read the omissions as oversights.

- **Recipe photography.** `GAP-025`. No workbook and no loaded dataset carries an image, and
  the Tier 2 image sets label dish types rather than these 940 recipes. Filling this needs
  photographs commissioned against the recipe list, not another join.
- **Guidance prose on any block.** `GAP-026`. Where the provider supplied a red flag or a
  referral line, it prints. Where they did not, the page prints the heading and a writing
  line. Drafting the missing advice is the 18 August ruling's exact prohibition.
- **B1-004 growth trend interpretation.** Needs the WHO reference tables.
- **The five blockers.** Filter collapse, the 6-11 month iron gap, boilerplate prep text,
  culture linkage and approval status are unchanged by this plan. Book 1 gets fuller; Book 2
  still carries six unique preparation texts across 940 recipes, and that is the next thing
  worth arguing about.
