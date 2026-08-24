# Recipe Photo Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Book 2 real dish-format photography (one representative photo per drawn-mark archetype, real or AI-invented recipes both covered) sourced from two verified, cleanly-licensed datasets, without touching the print browser's network block or GAP-025's actual (still-open) meaning.

**Architecture:** A new `internal/photomatch` package fetches a bounded set of candidate images from two external APIs (HuggingFace's `datasets-server` REST API and Mendeley's public dataset API) into `data/external/photos/`, matches them to the 11 `recipe_format_mark` archetypes via a hand-written label map, and writes them into a new `dish_format_photo` table keyed to `mark_id`. At render time, `internal/book/photo.go` looks up a representative photo by `mark_id` and embeds it as a `data:` URI, the same mechanism the cover photo already uses — no live network access from the print path.

**Tech Stack:** Go (stdlib `net/http`, `encoding/json`, `encoding/csv`), Postgres via pgx/v5, golang-migrate.

**Spec:** `docs/superpowers/specs/2026-08-24-recipe-photo-pipeline-design.md` (read the "Correction" note at the top — the committed spec was revised once during planning: source dataset swapped after checking a bad licence, and the schema changed from extending `recipe_photo` to a new `dish_format_photo` table after finding `recipe_photo`'s FK cannot reference an AI-invented recipe).

## Global Constraints

- No `claude`/`anthropic`/`ai` mentions anywhere: code, comments, commit messages, no attribution trailers. No emojis.
- `internal/importer`'s upsert-and-sweep must never touch `dish_format_photo` or `photo_label_archetype_map` — both are hand-written/pipeline-owned tables, never provider workbook data.
- Nothing in the print/render path (`internal/book/photo.go`, `book2.go`, `invented.go`, templates) may perform a network fetch. All fetching happens in `internal/photomatch`'s `-fetch` mode only, run offline before a book is ever generated.
- `credit`/`licence` columns are `NOT NULL` and filled with the literal placeholder string `"internal use — recipe photo match pipeline"` (verbatim, from the spec's Decision 2) — do not research or substitute the datasets' real licence text into the database (they're already verified in the spec's Decision 1; this is a deliberate placeholder, not an unknown).
- Migration number is `0026` — `0024` (`ingredient_allergen_override`) and `0025` (`ai_recipe`) are taken by concurrent work already merged on this branch. Verify with `ls internal/db/migrations/ | sort | tail -5` before writing the migration file, in case another migration has landed since this plan was written.
- `go build ./...`, `go vet ./...`, and `go test ./...` (with `TEST_DATABASE_URL` set) must stay green after every task.

---

## Task 1: Migration 0026 — `dish_format_photo`, `photo_label_archetype_map`, external sources, GAP-029

**Files:**
- Create: `internal/db/migrations/0026_dish_format_photo.up.sql`
- Create: `internal/db/migrations/0026_dish_format_photo.down.sql`

**Interfaces:**
- Produces: table `dish_format_photo` (columns: `photo_id bigserial PK`, `mark_id text NOT NULL`, `media_type text NOT NULL`, `bytes bytea NOT NULL`, `credit text NOT NULL`, `licence text NOT NULL`, `source_dataset text NOT NULL`, `source_row_id text NOT NULL`, `source_label text NOT NULL`, `added_by text NOT NULL`, `added_at timestamptz NOT NULL DEFAULT now()`), index `dish_format_photo_mark_idx (mark_id)`.
- Produces: table `photo_label_archetype_map` (columns: `source_dataset text NOT NULL`, `source_label text NOT NULL`, `mark_id text` nullable, `note text NOT NULL`, `PRIMARY KEY (source_dataset, source_label)`), pre-seeded with every mapped/explicitly-excluded label from both datasets.
- Produces: two new rows in `external_source` (`source_key = 'BHARAT-INDIAN-FOODS'`, `source_key = 'FOODBD'`).
- Produces: one new row in `gap_register` (`gap_id = 'GAP-029'`).
- Consumes: existing `external_source` schema from migration `0005` (this task only inserts rows, doesn't alter the table).
- Consumes: existing `gap_register` schema from migration `0002`, with the severity `CHECK` constraint as altered by migration `0023` (`'warning', 'major', 'minor', 'parked'`) — use `'minor'`, matching GAP-025's own severity.

- [ ] **Step 1: Confirm the migration number is still free**

Run: `ls internal/db/migrations/ | sort | tail -5`
Expected: highest migration is `0025_ai_recipe.*` (or `0025_something_else` if renamed) and no `0026_*` exists yet. If a `0026` already exists from concurrent work, use `0027` instead and update every reference to `0026` in this plan.

- [ ] **Step 2: Write the up migration**

```sql
-- internal/db/migrations/0026_dish_format_photo.up.sql

-- Real dish-format photography, sourced from two verified, cleanly-licensed datasets
-- (see docs/superpowers/specs/2026-08-24-recipe-photo-pipeline-design.md).
--
-- Keyed to the archetype (mark_id), not to a recipe. Two reasons, both load-bearing:
--
--   1. Matching is format-bucket only, deliberately -- the same order of claim
--      recipe_format_mark's drawn marks already make ("this is what a khichdi looks
--      like", not "this is what THIS khichdi looks like"). A per-recipe row would claim
--      more precision than the match actually has.
--   2. recipe_photo's recipe_id FK targets recipe_master only. An AI-invented recipe
--      (ai_recipe, AR-#####) has no recipe_master row, so a per-recipe table could never
--      hold its photo. Keying to mark_id covers both real and invented recipes uniformly,
--      the same way marks.go's embedded SVGs already do.
--
-- recipe_photo (migration 0021) is untouched and keeps its original, narrower meaning: a
-- real, commissioned photograph of one specific recipe. This table never writes there and
-- GAP-025 (which recipe_photo measures) does not move because of this pipeline -- see
-- GAP-029 below for what this pipeline actually delivers.
CREATE TABLE dish_format_photo (
  photo_id        bigserial PRIMARY KEY,
  mark_id         text NOT NULL CHECK (mark_id <> ''),
  media_type      text NOT NULL CHECK (media_type IN ('image/png','image/jpeg','image/webp')),
  bytes           bytea NOT NULL,
  credit          text NOT NULL CHECK (credit <> ''),
  licence         text NOT NULL CHECK (licence <> ''),
  source_dataset  text NOT NULL,
  source_row_id   text NOT NULL,
  source_label    text NOT NULL,
  added_by        text NOT NULL CHECK (added_by <> ''),
  added_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dish_format_photo_mark_idx ON dish_format_photo (mark_id);

COMMENT ON TABLE dish_format_photo IS
    'Real photography per dish-format archetype (the same 11 marks.go archetypes), not '
    'per recipe. Written only by cmd/photomatch, never by cmd/import. See GAP-029: this '
    'is a lesser claim than a per-recipe commissioned photo, which stays GAP-025 and '
    'stays open.';

-- Hand-written, like recipe_format_mark and external_cuisine_region_map: both source
-- datasets have small, closed label vocabularies, so this is a real editorial judgement
-- per label, not a fuzzy string match. mark_id NULL means deliberately excluded -- the
-- label names an ingredient or a format with no honest archetype fit, not an oversight.
-- Not every one of FoodBD's 67 classes is listed: unlisted labels (raw ingredients like
-- "banana", "rice", "egg") are implicitly unmapped by absence from this table. A handful
-- of labels a reader might specifically expect to see mapped are listed explicitly with
-- NULL and a reason, so the omission reads as reviewed rather than missed.
CREATE TABLE photo_label_archetype_map (
  source_dataset  text NOT NULL,
  source_label    text NOT NULL,
  mark_id         text,
  note            text NOT NULL,
  PRIMARY KEY (source_dataset, source_label)
);

COMMENT ON COLUMN photo_label_archetype_map.mark_id IS
    'NULL means deliberately excluded -- see note. Never a fuzzy match target.';

INSERT INTO photo_label_archetype_map (source_dataset, source_label, mark_id, note) VALUES
  -- bharat-raghunathan/indian-foods-dataset, all 15 classes, CC0
  ('BHARAT-INDIAN-FOODS', 'biryani',      'bowl-grain',    'rice dish, grain-led'),
  ('BHARAT-INDIAN-FOODS', 'cholebhature', 'flatbread',     'bhature is a fried flatbread; chole is the side'),
  ('BHARAT-INDIAN-FOODS', 'dabeli',       'snack',         'stuffed bun, portable snack'),
  ('BHARAT-INDIAN-FOODS', 'dal',          NULL,            'lentil side, not a served format on its own'),
  ('BHARAT-INDIAN-FOODS', 'dhokla',       'snack',         'steamed cake, eaten as a snack'),
  ('BHARAT-INDIAN-FOODS', 'dosa',         'pancake',       'batter cooked flat on a griddle'),
  ('BHARAT-INDIAN-FOODS', 'jalebi',       'snack',         'sweet snack'),
  ('BHARAT-INDIAN-FOODS', 'kathiroll',    'wrap',          'filled and rolled'),
  ('BHARAT-INDIAN-FOODS', 'kofta',        'patty',         'shaped and fried ball, closest to patty'),
  ('BHARAT-INDIAN-FOODS', 'naan',         'flatbread',     'griddle bread'),
  ('BHARAT-INDIAN-FOODS', 'pakora',       'snack',         'fried snack'),
  ('BHARAT-INDIAN-FOODS', 'paneer',       NULL,            'ingredient, not a served format'),
  ('BHARAT-INDIAN-FOODS', 'panipuri',     'snack',         'street snack'),
  ('BHARAT-INDIAN-FOODS', 'pavbhaji',     'dish-mash',     'mashed vegetable mix, served in a shallow dish'),
  ('BHARAT-INDIAN-FOODS', 'vadapav',      'snack',         'fried-fritter sandwich, portable snack'),

  -- FoodBD, selected dish-format classes out of 67 total (raw ingredients unlisted, see comment above), CC BY 4.0
  ('FOODBD', 'biriyani',       'bowl-grain',    'rice dish, grain-led'),
  ('FOODBD', 'khichuri',       'pot-khichdi',   'one-pot dal and rice'),
  ('FOODBD', 'payesh',         'bowl-porridge', 'spoonable rice pudding'),
  ('FOODBD', 'ruti',           'flatbread',     'griddle bread'),
  ('FOODBD', 'puri',           'flatbread',     'fried flatbread'),
  ('FOODBD', 'vorta',          'dish-mash',     'mashed vegetable or fish preparation'),
  ('FOODBD', 'begun-vaji',     'dish-mash',     'stir-fried vegetable side, shallow dish'),
  ('FOODBD', 'korola-vaji',    'dish-mash',     'stir-fried vegetable side, shallow dish'),
  ('FOODBD', 'kumra-vaji',     'dish-mash',     'stir-fried vegetable side, shallow dish'),
  ('FOODBD', 'chichinga-vaji', 'dish-mash',     'stir-fried vegetable side, shallow dish'),
  ('FOODBD', 'vaji',           'dish-mash',     'stir-fried vegetable side, shallow dish, generic label'),
  ('FOODBD', 'lal-shak',       'dish-mash',     'sauteed leafy green side'),
  ('FOODBD', 'shak',           'dish-mash',     'sauteed leafy green side, generic label'),
  ('FOODBD', 'kabab',          'patty',         'shaped and pan-cooked'),
  ('FOODBD', 'chop-alu',       'patty',         'shaped potato patty'),
  ('FOODBD', 'beguni',         'patty',         'shaped and fried'),
  ('FOODBD', 'peyaju',         'patty',         'shaped and fried lentil fritter'),
  ('FOODBD', 'piyaju',         'patty',         'shaped and fried lentil fritter, alternate spelling'),
  ('FOODBD', 'roll',           'wrap',          'filled and rolled'),
  ('FOODBD', 'puffed-rice',    'snack',         'portable snack'),
  ('FOODBD', 'jilapi',         'snack',         'sweet snack'),
  ('FOODBD', 'chomchom',       'snack',         'sweet snack'),
  ('FOODBD', 'sweets',         'snack',         'sweet snack, generic label'),
  ('FOODBD', 'daal',           NULL,            'lentil side, not a served format on its own'),
  ('FOODBD', 'noodles',        NULL,            'no archetype fits; not a home format this project's recipes use'),
  ('FOODBD', 'salad',          NULL,            'raw side, not a served format');

-- Both datasets were checked directly, not assumed. bharat-raghunathan's dataset card
-- states CC0 (public domain, no restriction). FoodBD's Mendeley listing states CC BY 4.0.
-- local_file/sha256 start empty and are filled by cmd/photomatch -fetch on its first run,
-- the same post-hoc UPDATE pattern enrichment_run already uses for rows_loaded on the
-- existing two external_source rows -- the manifest these values describe doesn't exist
-- until the fetch actually runs.
INSERT INTO external_source
    (source_key, name, publisher, url, licence, region_scope, retrieved_on, local_file, sha256, used_for) VALUES
    ('BHARAT-INDIAN-FOODS',
     'Indian Foods Dataset',
     'bharat-raghunathan (Hugging Face), derived from Kaggle: The Massive Indian Foods Dataset',
     'https://huggingface.co/datasets/bharat-raghunathan/indian-foods-dataset',
     'CC0: Public Domain',
     'India',
     CURRENT_DATE,
     'data/external/photos/manifest.csv',
     '',
     'Representative photography per dish-format archetype (dish_format_photo). Format-level only, never claimed as a photo of any specific recipe.'),

    ('FOODBD',
     'FoodBD: A Polygon-Annotated Meal Image Dataset of Bangladeshi Cuisine with Visual and Nutritional Labels',
     'Ahmed, Haque, Huq, Ali, Masud, Naznin (Mendeley Data)',
     'https://data.mendeley.com/datasets/xh3ghf3jbg/2',
     'CC BY 4.0',
     'Bangladesh',
     CURRENT_DATE,
     'data/external/photos/manifest.csv',
     '',
     'Representative photography per dish-format archetype (dish_format_photo), restricted to single-item meal photos. Format-level only, never claimed as a photo of any specific recipe.');

-- GAP-029: what this pipeline actually delivers, distinct from GAP-025 (which stays open
-- and unmeasured by this table). severity 'minor' matches GAP-025's own severity -- see
-- migration 0023 for the current severity vocabulary.
INSERT INTO gap_register
  (gap_id, severity, area, source_table, source_column, description,
   affected_rows, measured_by, ui_behaviour, resolution_path, measured_at)
VALUES
  ('GAP-029', 'minor', 'book2', 'dish_format_photo', NULL,
   'No recipe has a per-recipe commissioned photograph (that remains GAP-025, unchanged). '
   'dish_format_photo instead holds representative stock photography per dish-format '
   'archetype (the same 11 marks.go archetypes), sourced from two verified, cleanly-'
   'licensed datasets and matched by format only -- the same order of claim the drawn '
   'marks already make, just photographic. A recipe whose archetype has no stored photo '
   'still prints its drawn mark.',
   11, 'importer',
   'A recipe prints the drawn mark for its dish format until that format has a stored '
   'photo, then prints the photo instead. Never a photo of the specific recipe.',
   'Commission a real per-recipe photograph (closes GAP-025 directly) or add more '
   'candidate images per archetype via cmd/photomatch -fetch (raises coverage here, does '
   'not touch GAP-025).',
   now());
```

- [ ] **Step 3: Write the down migration**

```sql
-- internal/db/migrations/0026_dish_format_photo.down.sql
DELETE FROM gap_register WHERE gap_id = 'GAP-029';
DELETE FROM external_source WHERE source_key IN ('BHARAT-INDIAN-FOODS', 'FOODBD');
DROP TABLE IF EXISTS photo_label_archetype_map;
DROP TABLE IF EXISTS dish_format_photo;
```

- [ ] **Step 4: Apply and roll back to confirm both directions work**

Run:
```bash
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import
```
Expected: import succeeds, `SELECT count(*) FROM dish_format_photo;` returns `0`, `SELECT count(*) FROM photo_label_archetype_map;` returns `40` (15 + 25 rows), `SELECT count(*) FROM gap_register;` returns `29`.

Then confirm rollback:
```bash
migrate -database "$DATABASE_URL" -path internal/db/migrations down 1
```
Expected: no error; `SELECT count(*) FROM gap_register;` back to `28`.

Re-apply before continuing:
```bash
migrate -database "$DATABASE_URL" -path internal/db/migrations up
```

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/0026_dish_format_photo.up.sql internal/db/migrations/0026_dish_format_photo.down.sql
git commit -m "Add dish_format_photo, the archetype-keyed home for real recipe photography"
```

---

## Task 2: `internal/photomatch` — types and the hand-written map reader

**Files:**
- Create: `internal/photomatch/photomatch.go`
- Test: `internal/photomatch/photomatch_test.go`

**Interfaces:**
- Consumes: `dish_format_photo`/`photo_label_archetype_map` schema from Task 1.
- Produces: `type LabelMap map[SourceLabel]string` (mark_id, empty string for excluded), `type SourceLabel struct { Dataset, Label string }`, `func LoadLabelMap(ctx context.Context, pool *pgxpool.Pool) (LabelMap, error)`, `func (m LabelMap) MarkID(dataset, label string) (markID string, ok bool)` — `ok` is `false` for both "not in the table" and "explicitly NULL", callers only need to know whether a candidate exists.
- Produces: `type ManifestRow struct { SourceDataset, SourceRowID, SourceLabel, MarkID, LocalFile, MediaType string }` and CSV read/write helpers `WriteManifest(path string, rows []ManifestRow) error`, `ReadManifest(path string) ([]ManifestRow, error)` — this is the file `data/external/photos/manifest.csv` that bridges the network-touching fetch stage and the DB-writing match stage.

- [ ] **Step 1: Write the failing test for `LoadLabelMap`**

```go
// internal/photomatch/photomatch_test.go
package photomatch

import (
	"context"
	"testing"

	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/dbtest" // use whatever helper internal/enrich's tests use to get a pool; if none exists, follow internal/db/integrity_test.go's TEST_DATABASE_URL skip pattern directly
)

func TestLoadLabelMapResolvesMappedAndExcludedLabels(t *testing.T) {
	pool := dbtest.Pool(t) // skips with t.Skip if TEST_DATABASE_URL is unset, matching the rest of the suite

	m, err := LoadLabelMap(context.Background(), pool)
	if err != nil {
		t.Fatalf("LoadLabelMap: %v", err)
	}

	markID, ok := m.MarkID("BHARAT-INDIAN-FOODS", "biryani")
	if !ok || markID != "bowl-grain" {
		t.Fatalf("biryani: got (%q, %v), want (bowl-grain, true)", markID, ok)
	}

	if _, ok := m.MarkID("BHARAT-INDIAN-FOODS", "dal"); ok {
		t.Fatalf("dal is explicitly excluded (mark_id NULL), MarkID must report ok=false")
	}

	if _, ok := m.MarkID("BHARAT-INDIAN-FOODS", "not-a-real-label"); ok {
		t.Fatalf("an unlisted label must also report ok=false")
	}
}
```

Check first whether `internal/enrich`'s own tests already have a pool-acquisition helper (`grep -rn "TEST_DATABASE_URL" internal/enrich/`) — if `internal/enrich` connects directly rather than through a shared `dbtest` package, copy that exact pattern instead of inventing a `dbtest` import that doesn't exist in this repo.

- [ ] **Step 2: Run it to verify it fails**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/photomatch/... -run TestLoadLabelMap -v`
Expected: FAIL — `LoadLabelMap` undefined (package doesn't exist yet).

- [ ] **Step 3: Write `photomatch.go`**

```go
// Package photomatch matches externally-sourced dish photography to the 11
// recipe_format_mark archetypes and stores it in dish_format_photo. It never touches a
// recipe_id: matching is format-bucket only, the same order of claim marks.go's drawn
// artwork already makes, and dish_format_photo is keyed to mark_id for exactly that
// reason -- see docs/superpowers/specs/2026-08-24-recipe-photo-pipeline-design.md.
package photomatch

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SourceLabel identifies one class label inside one source dataset -- the primary key
// shape of photo_label_archetype_map.
type SourceLabel struct {
	Dataset string
	Label   string
}

// LabelMap is the hand-written label -> archetype mapping, loaded once per run.
type LabelMap map[SourceLabel]string

// LoadLabelMap reads photo_label_archetype_map in full. The table is small (40 rows
// today) and hand-written, so loading it whole rather than querying per-label keeps the
// fetch and match stages from making one round trip per image.
func LoadLabelMap(ctx context.Context, pool *pgxpool.Pool) (LabelMap, error) {
	rows, err := pool.Query(ctx,
		`SELECT source_dataset, source_label, mark_id FROM photo_label_archetype_map`)
	if err != nil {
		return nil, fmt.Errorf("photomatch: query label map: %w", err)
	}
	defer rows.Close()

	m := make(LabelMap)
	for rows.Next() {
		var dataset, label string
		var markID *string
		if err := rows.Scan(&dataset, &label, &markID); err != nil {
			return nil, fmt.Errorf("photomatch: scan label map row: %w", err)
		}
		if markID != nil {
			m[SourceLabel{Dataset: dataset, Label: label}] = *markID
		}
		// A NULL mark_id (explicitly excluded) is simply never inserted -- MarkID's
		// two-value return below can't distinguish "excluded" from "unlisted" and
		// doesn't need to; both mean no candidate.
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photomatch: label map rows: %w", err)
	}
	return m, nil
}

// MarkID resolves a dataset+label pair to an archetype. ok is false whether the label is
// absent from the table (an unreviewed ingredient) or present with mark_id NULL
// (reviewed and excluded) -- callers only ever need "is this a usable candidate."
func (m LabelMap) MarkID(dataset, label string) (markID string, ok bool) {
	id, ok := m[SourceLabel{Dataset: dataset, Label: label}]
	return id, ok
}

// ManifestRow is one fetched image, bridging the network-touching fetch stage (Task 3)
// and the DB-writing match stage (Task 4). Persisted to data/external/photos/manifest.csv
// so the match stage never needs network access -- see the Global Constraints in the plan
// this package implements.
type ManifestRow struct {
	SourceDataset string
	SourceRowID   string // the source's own row/file identifier, for provenance
	SourceLabel   string
	MarkID        string
	LocalFile     string // relative to the data directory passed to the command
	MediaType     string
}

var manifestHeader = []string{"source_dataset", "source_row_id", "source_label", "mark_id", "local_file", "media_type"}

// WriteManifest overwrites the manifest file with exactly these rows -- a full rewrite
// on every fetch run, not an append, so a stale row from an earlier label mapping can
// never survive into a later match run.
func WriteManifest(path string, rows []ManifestRow) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("photomatch: create manifest: %w", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(manifestHeader); err != nil {
		return fmt.Errorf("photomatch: write manifest header: %w", err)
	}
	for _, r := range rows {
		rec := []string{r.SourceDataset, r.SourceRowID, r.SourceLabel, r.MarkID, r.LocalFile, r.MediaType}
		if err := w.Write(rec); err != nil {
			return fmt.Errorf("photomatch: write manifest row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("photomatch: flush manifest: %w", err)
	}
	return nil
}

// ReadManifest reads back what WriteManifest wrote.
func ReadManifest(path string) ([]ManifestRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("photomatch: open manifest: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("photomatch: read manifest header: %w", err)
	}
	col := make(map[string]int, len(head))
	for i, h := range head {
		col[h] = i
	}
	for _, need := range manifestHeader {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("photomatch: manifest missing column %q", need)
		}
	}

	var rows []ManifestRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("photomatch: read manifest row: %w", err)
		}
		rows = append(rows, ManifestRow{
			SourceDataset: rec[col["source_dataset"]],
			SourceRowID:   rec[col["source_row_id"]],
			SourceLabel:   rec[col["source_label"]],
			MarkID:        rec[col["mark_id"]],
			LocalFile:     rec[col["local_file"]],
			MediaType:     rec[col["media_type"]],
		})
	}
	return rows, nil
}
```

Add `"io"` to `photomatch.go`'s imports (needed for `io.EOF` here).

- [ ] **Step 4: Run the test again to verify it passes**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/photomatch/... -run TestLoadLabelMap -v`
Expected: PASS

- [ ] **Step 5: Add a manifest round-trip test (no database needed)**

```go
func TestManifestRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/manifest.csv"

	want := []ManifestRow{
		{SourceDataset: "BHARAT-INDIAN-FOODS", SourceRowID: "0", SourceLabel: "biryani", MarkID: "bowl-grain", LocalFile: "BHARAT-INDIAN-FOODS/0.jpg", MediaType: "image/jpeg"},
		{SourceDataset: "FOODBD", SourceRowID: "FoodBD-0120.jpg", SourceLabel: "khichuri", MarkID: "pot-khichdi", LocalFile: "FOODBD/FoodBD-0120.jpg", MediaType: "image/jpeg"},
	}
	if err := WriteManifest(path, want); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	got, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
```

Run: `go test ./internal/photomatch/... -run TestManifestRoundTrips -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/photomatch/photomatch.go internal/photomatch/photomatch_test.go
git commit -m "Add the photomatch package: label map and manifest read/write"
```

---

## Task 3: `internal/photomatch` — the fetch stage (network, offline, bounded)

**Files:**
- Create: `internal/photomatch/fetch.go`
- Test: `internal/photomatch/fetch_test.go`

**Interfaces:**
- Consumes: `LabelMap`, `ManifestRow`, `WriteManifest` from Task 2.
- Produces: `func FetchIndianFoods(ctx context.Context, client *http.Client, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error)`, `func FetchFoodBD(ctx context.Context, client *http.Client, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error)` — both take `*http.Client` (not a bare package-level `http.Get`) specifically so tests can point them at an `httptest.Server` instead of the real internet.

- [ ] **Step 1: Write the failing test for `FetchIndianFoods` against a fake server**

```go
// internal/photomatch/fetch_test.go
package photomatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchIndianFoodsDownloadsOnlyMappedLabels(t *testing.T) {
	// A tiny fake of datasets-server's /rows response shape, with one mapped label
	// (biryani -> bowl-grain) and one explicitly-excluded label (dal, no candidate).
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-jpeg-bytes"))
	}))
	defer imgSrv.Close()

	rowsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"features": []map[string]any{
				{"feature_idx": 0, "name": "image", "type": map[string]any{"_type": "Image"}},
				{"feature_idx": 1, "name": "label", "type": map[string]any{
					"names": []string{"biryani", "dal"}, "_type": "ClassLabel",
				}},
			},
			"rows": []map[string]any{
				{"row_idx": 0, "row": map[string]any{
					"image": map[string]any{"src": imgSrv.URL + "/0.jpg"}, "label": 0,
				}},
				{"row_idx": 1, "row": map[string]any{
					"image": map[string]any{"src": imgSrv.URL + "/1.jpg"}, "label": 1,
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer rowsSrv.Close()

	labels := LabelMap{{Dataset: "BHARAT-INDIAN-FOODS", Label: "biryani"}: "bowl-grain"}
	dir := t.TempDir()

	rows, err := fetchIndianFoodsFrom(context.Background(), rowsSrv.Client(), rowsSrv.URL, dir, labels, 10)
	if err != nil {
		t.Fatalf("fetchIndianFoodsFrom: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d manifest rows, want 1 (dal has no mapped archetype and must be skipped)", len(rows))
	}
	if rows[0].MarkID != "bowl-grain" || rows[0].SourceLabel != "biryani" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/photomatch/... -run TestFetchIndianFoodsDownloadsOnlyMappedLabels -v`
Expected: FAIL — `fetchIndianFoodsFrom` undefined.

- [ ] **Step 3: Write `fetch.go`**

```go
// internal/photomatch/fetch.go
package photomatch

import (
	"encoding/json"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

const (
	// indianFoodsDataset is verified live against the real endpoint while writing the
	// design spec this package implements -- see the spec's Decision 1.
	indianFoodsDataset  = "bharat-raghunathan/indian-foods-dataset"
	datasetsServerBase  = "https://datasets-server.huggingface.co"
	rowsPageSize        = 100
	foodBDDatasetID     = "xh3ghf3jbg"
	mendeleyAPIBase     = "https://data.mendeley.com/public-api/datasets"
)

// FetchIndianFoods downloads up to perLabelCap candidate images per mapped
// BHARAT-INDIAN-FOODS label into outDir/BHARAT-INDIAN-FOODS/, via HuggingFace's public
// datasets-server rows API. Unmapped labels (LabelMap.MarkID returns ok=false) are never
// downloaded -- fetching an image this pipeline could never use would just be storage
// spent on nothing.
func FetchIndianFoods(ctx context.Context, client *http.Client, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error) {
	return fetchIndianFoodsFrom(ctx, client, datasetsServerBase, outDir, labels, perLabelCap)
}

// fetchIndianFoodsFrom takes the API base URL as a parameter so tests can point it at an
// httptest.Server instead of the real internet.
func fetchIndianFoodsFrom(ctx context.Context, client *http.Client, base, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error) {
	type rowsResponse struct {
		Features []struct {
			Name string `json:"name"`
			Type struct {
				Names []string `json:"names"`
			} `json:"type"`
		} `json:"features"`
		Rows []struct {
			RowIdx int `json:"row_idx"`
			Row    struct {
				Image struct {
					Src string `json:"src"`
				} `json:"image"`
				Label int `json:"label"`
			} `json:"row"`
		} `json:"rows"`
	}

	subdir := filepath.Join(outDir, "BHARAT-INDIAN-FOODS")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		return nil, fmt.Errorf("photomatch: mkdir %s: %w", subdir, err)
	}

	var classNames []string
	var rows []ManifestRow
	counts := map[string]int{}

	for offset := 0; ; offset += rowsPageSize {
		q := url.Values{
			"dataset": {indianFoodsDataset},
			"config":  {"default"},
			"split":   {"train"},
			"offset":  {strconv.Itoa(offset)},
			"length":  {strconv.Itoa(rowsPageSize)},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/rows?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("photomatch: build rows request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("photomatch: fetch rows page at offset %d: %w", offset, err)
		}
		var page rowsResponse
		decErr := json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if decErr != nil {
			return nil, fmt.Errorf("photomatch: decode rows page at offset %d: %w", offset, decErr)
		}
		if len(page.Rows) == 0 {
			break // ran off the end of the dataset
		}
		if classNames == nil {
			for _, f := range page.Features {
				if f.Name == "label" {
					classNames = f.Type.Names
				}
			}
		}

		for _, r := range page.Rows {
			if r.Row.Label < 0 || r.Row.Label >= len(classNames) {
				continue
			}
			label := classNames[r.Row.Label]
			markID, ok := labels.MarkID("BHARAT-INDIAN-FOODS", label)
			if !ok || counts[label] >= perLabelCap {
				continue
			}

			localName := fmt.Sprintf("%d.jpg", r.Row.RowIdx)
			if err := downloadTo(ctx, client, r.Row.Image.Src, filepath.Join(subdir, localName)); err != nil {
				return nil, fmt.Errorf("photomatch: download row %d: %w", r.Row.RowIdx, err)
			}
			rows = append(rows, ManifestRow{
				SourceDataset: "BHARAT-INDIAN-FOODS",
				SourceRowID:   strconv.Itoa(r.Row.RowIdx),
				SourceLabel:   label,
				MarkID:        markID,
				LocalFile:     filepath.Join("BHARAT-INDIAN-FOODS", localName),
				MediaType:     "image/jpeg",
			})
			counts[label]++
		}

		if len(page.Rows) < rowsPageSize {
			break // short page means this was the last one
		}
	}
	return rows, nil
}

// downloadTo streams an HTTP response body to a local file. Used for both datasets'
// image bytes -- neither needs anything more than a plain GET and a file write.
func downloadTo(ctx context.Context, client *http.Client, srcURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: status %d", srcURL, resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test again to verify it passes**

Run: `go test ./internal/photomatch/... -run TestFetchIndianFoodsDownloadsOnlyMappedLabels -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for `FetchFoodBD`'s single-item filter**

```go
func TestFetchFoodBDOnlyTakesSingleItemPlates(t *testing.T) {
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("fake-jpeg-bytes"))
	}))
	defer imgSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"files": []map[string]any{
				{"filename": "FoodBD_Meta_data.csv", "content_details": map[string]any{
					"download_url": imgSrv.URL + "/meta.csv",
				}},
				{"filename": "FoodBD-0120.jpg", "content_details": map[string]any{
					"download_url": imgSrv.URL + "/0120.jpg",
				}},
				{"filename": "FoodBD-3026.jpg", "content_details": map[string]any{
					"download_url": imgSrv.URL + "/3026.jpg",
				}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer apiSrv.Close()

	// Registered separately because httptest.NewServer above already claims imgSrv's
	// routing for /meta.csv -- rebuild imgSrv as a mux so both the CSV and the images
	// resolve from one server, matching how a real fetch hits many URLs on one host.
	mux := http.NewServeMux()
	mux.HandleFunc("/meta.csv", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("filename,instances,Environmental conditions,preprocessing ,types\n" +
			"FoodBD-0120.jpg,\"khichuri\",indoor,resized,train\n" +
			"FoodBD-3026.jpg,\"fish,rice,shak,vaji\",indoor,resized,train\n"))
	})
	mux.HandleFunc("/0120.jpg", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fake-jpeg")) })
	mux.HandleFunc("/3026.jpg", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fake-jpeg")) })
	imgSrv2 := httptest.NewServer(mux)
	defer imgSrv2.Close()

	apiSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"files": []map[string]any{
				{"filename": "FoodBD_Meta_data.csv", "content_details": map[string]any{"download_url": imgSrv2.URL + "/meta.csv"}},
				{"filename": "FoodBD-0120.jpg", "content_details": map[string]any{"download_url": imgSrv2.URL + "/0120.jpg"}},
				{"filename": "FoodBD-3026.jpg", "content_details": map[string]any{"download_url": imgSrv2.URL + "/3026.jpg"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer apiSrv2.Close()

	labels := LabelMap{{Dataset: "FOODBD", Label: "khichuri"}: "pot-khichdi"}
	dir := t.TempDir()

	rows, err := fetchFoodBDFrom(context.Background(), apiSrv2.Client(), apiSrv2.URL, dir, labels, 10)
	if err != nil {
		t.Fatalf("fetchFoodBDFrom: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d manifest rows, want 1 (the multi-item plate must be rejected)", len(rows))
	}
	if rows[0].SourceLabel != "khichuri" || rows[0].MarkID != "pot-khichdi" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}
```

(The duplicated server setup above is deliberately left rough by design of this plan step — Task 3 Step 7's cleanup pass should collapse it to one mux-backed server before the code is considered done; shown this way here so the *behavior* being tested — CSV parsing driving which images get fetched — is unambiguous even before that tidy-up.)

- [ ] **Step 6: Write `FetchFoodBD` and `fetchFoodBDFrom` in `fetch.go`**

```go
// FetchFoodBD downloads up to perLabelCap candidate images per mapped FOODBD label into
// outDir/FOODBD/, via Mendeley's public dataset API. Only images whose
// FoodBD_Meta_data.csv "instances" column names exactly one dish are candidates -- most
// FoodBD photos are whole meal plates with several items, and a multi-item photo
// doesn't represent any single archetype cleanly (see the spec's Decision 1).
func FetchFoodBD(ctx context.Context, client *http.Client, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error) {
	return fetchFoodBDFrom(ctx, client, mendeleyAPIBase, outDir, labels, perLabelCap)
}

func fetchFoodBDFrom(ctx context.Context, client *http.Client, apiBase, outDir string, labels LabelMap, perLabelCap int) ([]ManifestRow, error) {
	type contentDetails struct {
		DownloadURL string `json:"download_url"`
	}
	type fileEntry struct {
		Filename       string         `json:"filename"`
		ContentDetails contentDetails `json:"content_details"`
	}
	type datasetResponse struct {
		Files []fileEntry `json:"files"`
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/"+foodBDDatasetID, nil)
	if err != nil {
		return nil, fmt.Errorf("photomatch: build FoodBD dataset request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("photomatch: fetch FoodBD dataset listing: %w", err)
	}
	var listing datasetResponse
	decErr := json.NewDecoder(resp.Body).Decode(&listing)
	resp.Body.Close()
	if decErr != nil {
		return nil, fmt.Errorf("photomatch: decode FoodBD dataset listing: %w", decErr)
	}

	byName := make(map[string]fileEntry, len(listing.Files))
	for _, f := range listing.Files {
		byName[f.Filename] = f
	}
	metaEntry, ok := byName["FoodBD_Meta_data.csv"]
	if !ok {
		return nil, fmt.Errorf("photomatch: FoodBD dataset listing has no FoodBD_Meta_data.csv")
	}

	metaReq, err := http.NewRequestWithContext(ctx, http.MethodGet, metaEntry.ContentDetails.DownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("photomatch: build metadata request: %w", err)
	}
	metaResp, err := client.Do(metaReq)
	if err != nil {
		return nil, fmt.Errorf("photomatch: fetch FoodBD metadata: %w", err)
	}
	defer metaResp.Body.Close()

	r := csv.NewReader(metaResp.Body)
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("photomatch: read FoodBD metadata header: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[h] = i
	}
	filenameCol, ok := col["filename"]
	if !ok {
		return nil, fmt.Errorf("photomatch: FoodBD metadata missing filename column")
	}
	instancesCol, ok := col["instances"]
	if !ok {
		return nil, fmt.Errorf("photomatch: FoodBD metadata missing instances column")
	}

	subdir := filepath.Join(outDir, "FOODBD")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		return nil, fmt.Errorf("photomatch: mkdir %s: %w", subdir, err)
	}

	var rows []ManifestRow
	counts := map[string]int{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("photomatch: read FoodBD metadata row: %w", err)
		}

		instances := strings.Split(rec[instancesCol], ",")
		if len(instances) != 1 {
			continue // a multi-item plate; not a candidate for any single archetype
		}
		label := strings.TrimSpace(instances[0])
		markID, ok := labels.MarkID("FOODBD", label)
		if !ok || counts[label] >= perLabelCap {
			continue
		}

		filename := rec[filenameCol]
		imgEntry, ok := byName[filename]
		if !ok {
			continue // metadata names a file the dataset listing doesn't have; skip rather than guess a URL
		}

		if err := downloadTo(ctx, client, imgEntry.ContentDetails.DownloadURL, filepath.Join(subdir, filename)); err != nil {
			return nil, fmt.Errorf("photomatch: download %s: %w", filename, err)
		}
		rows = append(rows, ManifestRow{
			SourceDataset: "FOODBD",
			SourceRowID:   filename,
			SourceLabel:   label,
			MarkID:        markID,
			LocalFile:     filepath.Join("FOODBD", filename),
			MediaType:     "image/jpeg",
		})
		counts[label]++
	}
	return rows, nil
}
```

Add `"encoding/csv"` and `"strings"` to `fetch.go`'s imports.

- [ ] **Step 7: Run both fetch tests to verify they pass, then clean up the duplicated test-server setup from Step 5**

Run: `go test ./internal/photomatch/... -run TestFetch -v`
Expected: PASS for both. Then edit `fetch_test.go` to remove the first (unused) `imgSrv`/`apiSrv` pair from Step 5's test — only `imgSrv2`/`apiSrv2` (the mux-backed pair) are actually used; leaving both was this step's own acknowledged rough edge.

Run again: `go test ./internal/photomatch/... -v`
Expected: PASS, no unused-variable vet warnings.

- [ ] **Step 8: Commit**

```bash
git add internal/photomatch/fetch.go internal/photomatch/fetch_test.go
git commit -m "Add the photomatch fetch stage for both source datasets"
```

---

## Task 4: `internal/photomatch` — the match stage (DB writes, no network)

**Files:**
- Create: `internal/photomatch/match.go`
- Test: `internal/photomatch/match_test.go`

**Interfaces:**
- Consumes: `ManifestRow`, `ReadManifest` from Task 2; `dish_format_photo` schema from Task 1.
- Produces: `type Summary struct { PhotosWritten int; ArchetypesCovered int }`, `func Match(ctx context.Context, pool *pgxpool.Pool, dataDir string, maxPerArchetype int) (Summary, error)` — reads the manifest, opens each local file, writes `dish_format_photo` rows, capped at `maxPerArchetype` per `mark_id` even when multiple labels feed the same archetype (e.g. several FoodBD `*-vaji` labels all map to `dish-mash`).

- [ ] **Step 1: Write the failing test**

```go
// internal/photomatch/match_test.go
package photomatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchWritesCappedRowsPerArchetype(t *testing.T) {
	pool := dbtest.Pool(t) // same helper as Task 2's test; skips without TEST_DATABASE_URL

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM dish_format_photo`); err != nil {
		t.Fatalf("clear dish_format_photo: %v", err)
	}

	dir := t.TempDir()
	imgDir := filepath.Join(dir, "FOODBD")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Three candidate images that all resolve to dish-mash (begun-vaji, korola-vaji,
	// kumra-vaji), to prove the per-archetype cap counts across labels, not per label.
	rows := []ManifestRow{
		{SourceDataset: "FOODBD", SourceRowID: "a.jpg", SourceLabel: "begun-vaji", MarkID: "dish-mash", LocalFile: "FOODBD/a.jpg", MediaType: "image/jpeg"},
		{SourceDataset: "FOODBD", SourceRowID: "b.jpg", SourceLabel: "korola-vaji", MarkID: "dish-mash", LocalFile: "FOODBD/b.jpg", MediaType: "image/jpeg"},
		{SourceDataset: "FOODBD", SourceRowID: "c.jpg", SourceLabel: "kumra-vaji", MarkID: "dish-mash", LocalFile: "FOODBD/c.jpg", MediaType: "image/jpeg"},
	}
	for _, r := range rows {
		if err := os.WriteFile(filepath.Join(dir, r.LocalFile), []byte("fake-jpeg-bytes"), 0o644); err != nil {
			t.Fatalf("write fake image: %v", err)
		}
	}
	if err := WriteManifest(filepath.Join(dir, "manifest.csv"), rows); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	summary, err := Match(ctx, pool, dir, 2) // cap of 2, three candidates for dish-mash
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if summary.PhotosWritten != 2 {
		t.Fatalf("got %d photos written, want 2 (the cap)", summary.PhotosWritten)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dish_format_photo WHERE mark_id = 'dish-mash'`).Scan(&count); err != nil {
		t.Fatalf("count dish-mash rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("got %d dish-mash rows in the database, want 2", count)
	}
}

func TestMatchIsIdempotent(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM dish_format_photo`); err != nil {
		t.Fatalf("clear: %v", err)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "BHARAT-INDIAN-FOODS"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	row := ManifestRow{SourceDataset: "BHARAT-INDIAN-FOODS", SourceRowID: "0", SourceLabel: "biryani", MarkID: "bowl-grain", LocalFile: "BHARAT-INDIAN-FOODS/0.jpg", MediaType: "image/jpeg"}
	if err := os.WriteFile(filepath.Join(dir, row.LocalFile), []byte("fake-jpeg-bytes"), 0o644); err != nil {
		t.Fatalf("write fake image: %v", err)
	}
	if err := WriteManifest(filepath.Join(dir, "manifest.csv"), []ManifestRow{row}); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	if _, err := Match(ctx, pool, dir, 5); err != nil {
		t.Fatalf("first Match: %v", err)
	}
	if _, err := Match(ctx, pool, dir, 5); err != nil {
		t.Fatalf("second Match: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM dish_format_photo WHERE mark_id = 'bowl-grain'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d rows after two runs, want 1 (re-running must clear and rewrite, not accumulate)", count)
	}
}
```

- [ ] **Step 2: Run to verify both fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/photomatch/... -run TestMatch -v`
Expected: FAIL — `Match` undefined.

- [ ] **Step 3: Write `match.go`**

```go
// internal/photomatch/match.go
package photomatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// placeholderCredit and placeholderLicence are deliberate, not researched-and-forgotten
// -- see the plan's Global Constraints and the spec's Decision 2. Both source datasets'
// real terms are already verified (CC0 and CC BY 4.0, spec Decision 1); this string is
// what the operator console actually shows, by explicit choice.
const (
	placeholderCredit  = "internal use — recipe photo match pipeline"
	placeholderLicence = "internal use — recipe photo match pipeline"
	addedBy            = "photo-match-pipeline"
)

// Summary is what one match run produced.
type Summary struct {
	PhotosWritten     int
	ArchetypesCovered int
}

// Match reads the manifest cmd/photomatch -fetch wrote, opens each local image file, and
// writes dish_format_photo -- capped at maxPerArchetype rows per mark_id, counted across
// every label that resolves to that archetype (several FoodBD "*-vaji" labels all feed
// dish-mash, for instance). Re-running clears and rewrites rather than accumulating, the
// same idempotency contract cmd/import and cmd/enrich already carry.
//
// This never touches the network -- every byte it writes already sits in dataDir from an
// earlier, separate -fetch run. See the plan's Global Constraints.
func Match(ctx context.Context, pool *pgxpool.Pool, dataDir string, maxPerArchetype int) (Summary, error) {
	var s Summary

	rows, err := ReadManifest(filepath.Join(dataDir, "manifest.csv"))
	if err != nil {
		return s, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return s, fmt.Errorf("photomatch: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.Exec(ctx, `DELETE FROM dish_format_photo`); err != nil {
		return s, fmt.Errorf("photomatch: clear dish_format_photo: %w", err)
	}

	perArchetype := map[string]int{}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.MarkID == "" {
			continue // shouldn't happen -- the fetch stage only writes rows with a resolved mark_id -- but never trust a file blindly
		}
		if perArchetype[r.MarkID] >= maxPerArchetype {
			continue
		}

		bytes, err := os.ReadFile(filepath.Join(dataDir, r.LocalFile))
		if err != nil {
			return s, fmt.Errorf("photomatch: read %s: %w", r.LocalFile, err)
		}

		if err := writePhoto(ctx, tx, r, bytes); err != nil {
			return s, err
		}
		perArchetype[r.MarkID]++
		seen[r.MarkID] = true
		s.PhotosWritten++
	}
	s.ArchetypesCovered = len(seen)

	if err := tx.Commit(ctx); err != nil {
		return s, fmt.Errorf("photomatch: commit: %w", err)
	}
	return s, nil
}

func writePhoto(ctx context.Context, tx pgx.Tx, r ManifestRow, imageBytes []byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO dish_format_photo
			(mark_id, media_type, bytes, credit, licence, source_dataset, source_row_id, source_label, added_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		r.MarkID, r.MediaType, imageBytes, placeholderCredit, placeholderLicence,
		r.SourceDataset, r.SourceRowID, r.SourceLabel, addedBy)
	if err != nil {
		return fmt.Errorf("photomatch: insert dish_format_photo for %s/%s: %w", r.SourceDataset, r.SourceRowID, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests again to verify they pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/photomatch/... -v`
Expected: PASS, all of Tasks 2-4's tests green together.

- [ ] **Step 5: Commit**

```bash
git add internal/photomatch/match.go internal/photomatch/match_test.go
git commit -m "Add the photomatch match stage: manifest to dish_format_photo, capped and idempotent"
```

---

## Task 5: `cmd/photomatch` — the CLI

**Files:**
- Create: `cmd/photomatch/main.go`

**Interfaces:**
- Consumes: `photomatch.FetchIndianFoods`, `photomatch.FetchFoodBD`, `photomatch.Match`, `photomatch.WriteManifest`, `photomatch.LoadLabelMap` (Tasks 2-4); `config.Load`, `db.Connect` (existing, `internal/config`, `internal/db`).

- [ ] **Step 1: Write `main.go`, mirroring `cmd/enrich/main.go`'s shape exactly**

```go
// Command photomatch fetches and matches real dish-format photography.
//
//	DATABASE_URL=... go run ./cmd/photomatch -fetch      # network stage: download candidates
//	DATABASE_URL=... go run ./cmd/photomatch              # match stage: manifest -> dish_format_photo
//	DATABASE_URL=... go run ./cmd/photomatch -sample 20   # hand-check what's stored
//
// The two stages are separate on purpose: -fetch is the only thing in this whole feature
// that touches the network, and it runs offline, ahead of any book being generated. The
// default (match) stage only ever reads local files already sitting in -data.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/madamgy/recipie/internal/config"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/photomatch"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("photomatch failed: %v", err)
	}
}

func run() error {
	fetch := flag.Bool("fetch", false, "run the network fetch stage instead of the match stage")
	sample := flag.Int("sample", 0, "print this many stored photos for hand-checking, then exit")
	dataDir := flag.String("data", "data/external/photos", "directory holding fetched images and the manifest")
	perLabel := flag.Int("per-label-cap", 15, "max candidate images to fetch per source label (-fetch only)")
	perArchetype := flag.Int("per-archetype-cap", 10, "max stored photos per archetype (match stage only)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if *sample > 0 {
		return printSample(ctx, pool, *sample)
	}

	if *fetch {
		return runFetch(ctx, pool, *dataDir, *perLabel)
	}
	return runMatch(ctx, pool, *dataDir, *perArchetype)
}

func runFetch(ctx context.Context, pool *pgxpool.Pool, dataDir string, perLabelCap int) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dataDir, err)
	}

	labels, err := photomatch.LoadLabelMap(ctx, pool)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}

	indianRows, err := photomatch.FetchIndianFoods(ctx, client, dataDir, labels, perLabelCap)
	if err != nil {
		return fmt.Errorf("fetch bharat-raghunathan: %w", err)
	}
	foodbdRows, err := photomatch.FetchFoodBD(ctx, client, dataDir, labels, perLabelCap)
	if err != nil {
		return fmt.Errorf("fetch FoodBD: %w", err)
	}

	all := append(indianRows, foodbdRows...)
	manifestPath := dataDir + "/manifest.csv"
	if err := photomatch.WriteManifest(manifestPath, all); err != nil {
		return err
	}

	sha, err := sha256File(manifestPath)
	if err != nil {
		return err
	}
	for _, key := range []string{"BHARAT-INDIAN-FOODS", "FOODBD"} {
		if _, err := pool.Exec(ctx,
			`UPDATE external_source SET sha256 = $1, rows_loaded = $2 WHERE source_key = $3`,
			sha, len(all), key); err != nil {
			return fmt.Errorf("record external_source for %s: %w", key, err)
		}
	}

	fmt.Printf("fetched %d candidate images (%d bharat-raghunathan, %d FoodBD)\n", len(all), len(indianRows), len(foodbdRows))
	fmt.Println("next: DATABASE_URL=... go run ./cmd/photomatch")
	return nil
}

func runMatch(ctx context.Context, pool *pgxpool.Pool, dataDir string, perArchetypeCap int) error {
	s, err := photomatch.Match(ctx, pool, dataDir, perArchetypeCap)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "photos written\t%d\t\n", s.PhotosWritten)
	fmt.Fprintf(w, "archetypes covered\t%d of 11\t\n", s.ArchetypesCovered)
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println("hand-check before trusting these:  go run ./cmd/photomatch -sample 20")
	return nil
}

func printSample(ctx context.Context, pool *pgxpool.Pool, n int) error {
	rows, err := pool.Query(ctx, `
		SELECT mark_id, source_dataset, source_label, added_at
		FROM dish_format_photo
		ORDER BY random()
		LIMIT $1`, n)
	if err != nil {
		return fmt.Errorf("sample: %w", err)
	}
	defer rows.Close()

	i := 0
	for rows.Next() {
		var markID, dataset, label string
		var addedAt time.Time
		if err := rows.Scan(&markID, &dataset, &label, &addedAt); err != nil {
			return fmt.Errorf("sample: %w", err)
		}
		i++
		fmt.Printf("%2d. %-14s <- %s / %s\n", i, markID, dataset, label)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sample: %w", err)
	}
	if i == 0 {
		fmt.Println("no photos stored; run -fetch then the match stage first")
	}
	return nil
}
```

- [ ] **Step 2: Add `sha256File` to `internal/photomatch/photomatch.go`**

```go
// sha256File hashes a file's contents, hex-encoded -- used to record the fetched
// manifest's checksum in external_source, the same way every other external dataset
// already records its own.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("photomatch: open %s for hashing: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("photomatch: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

Move this into `internal/photomatch/photomatch.go` (not `cmd/photomatch/main.go`) so it's exported as `photomatch.SHA256File` and testable there; update `main.go`'s call site to `photomatch.SHA256File(manifestPath)`. Add `"crypto/sha256"`, `"encoding/hex"`, `"io"` to `photomatch.go`'s imports.

- [ ] **Step 3: Build and run against a real (but tiny) fetch to confirm the CLI wiring works end to end**

Run:
```bash
go build ./...
go vet ./...
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/photomatch -fetch -per-label-cap 3
go run ./cmd/photomatch -per-archetype-cap 5
go run ./cmd/photomatch -sample 10
```
Expected: `-fetch` reports a small number of fetched images (real network calls to the two real APIs — this is the one step in this whole plan that legitimately needs internet access); the match run reports photos written and archetypes covered (fewer than 11, since a 3-per-label cap against ~15 mapped India labels and ~22 mapped FoodBD labels won't hit every archetype — that's fine, it's a smoke test); `-sample 10` prints real rows.

If `-fetch` fails because the execution environment has no internet access, note that explicitly rather than treating it as a code bug — re-run this step from an environment that does, before calling Task 5 done. The unit tests in Tasks 3-4 already prove the logic works against fake servers; this step is proving the two real, verified endpoints still behave the way they did when the spec was researched.

- [ ] **Step 4: Commit**

```bash
git add cmd/photomatch/main.go internal/photomatch/photomatch.go
git commit -m "Add the cmd/photomatch CLI"
```

---

## Task 6: `internal/book/photo.go` — the render-time lookup

**Files:**
- Modify: `internal/book/photo.go`
- Test: `internal/book/photo_test.go` (existing file — add to it)

**Interfaces:**
- Consumes: `dish_format_photo` schema from Task 1.
- Produces: `type RecipePhoto struct { DataURI template.URL; SourceLabel string }`, `func RepresentativePhoto(ctx context.Context, pool *pgxpool.Pool, markID string) (*RecipePhoto, error)` — returns `nil, nil` when the archetype has no stored photo, the same nil-is-ordinary-absence contract `Mark` already follows.

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/book/photo_test.go
func TestRepresentativePhotoReturnsNilWhenArchetypeHasNone(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM dish_format_photo WHERE mark_id = 'no-such-archetype'`); err != nil {
		t.Fatalf("clear: %v", err)
	}

	p, err := RepresentativePhoto(ctx, pool, "no-such-archetype")
	if err != nil {
		t.Fatalf("RepresentativePhoto: %v", err)
	}
	if p != nil {
		t.Fatalf("got %+v, want nil for an archetype with no stored photo", p)
	}
}

func TestRepresentativePhotoReturnsTheLowestIDDeterministically(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM dish_format_photo WHERE mark_id = 'test-archetype'`); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, `
			INSERT INTO dish_format_photo (mark_id, media_type, bytes, credit, licence, source_dataset, source_row_id, source_label, added_by)
			VALUES ('test-archetype', 'image/jpeg', $1, 'c', 'l', 'TEST', $2, 'label', 'test')`,
			[]byte("fake-bytes"), fmt.Sprintf("row-%d", i)); err != nil {
			t.Fatalf("insert fixture %d: %v", i, err)
		}
	}

	p1, err := RepresentativePhoto(ctx, pool, "test-archetype")
	if err != nil || p1 == nil {
		t.Fatalf("RepresentativePhoto: %+v, %v", p1, err)
	}
	p2, err := RepresentativePhoto(ctx, pool, "test-archetype")
	if err != nil || p2 == nil {
		t.Fatalf("RepresentativePhoto (second call): %+v, %v", p2, err)
	}
	if p1.DataURI != p2.DataURI {
		t.Fatalf("two calls returned different photos for the same archetype; must be deterministic")
	}
	if !strings.HasPrefix(string(p1.DataURI), "data:image/jpeg;base64,") {
		t.Fatalf("DataURI has the wrong prefix: %s", p1.DataURI)
	}
}
```

- [ ] **Step 2: Run to verify both fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/... -run TestRepresentativePhoto -v`
Expected: FAIL — `RepresentativePhoto` undefined.

- [ ] **Step 3: Add to `internal/book/photo.go`**

```go
// RecipePhoto is a real, stored dish-format photograph, ready to embed.
//
// It carries a data: URI for the same reason ChildPhoto does -- the print browser never
// fetches, so the image has to travel inside the document. See ChildPhoto.DataURI's own
// comment for why template.URL specifically, not string.
type RecipePhoto struct {
	DataURI     template.URL `json:"data_uri"`
	SourceLabel string       `json:"source_label,omitempty"`
}

// RepresentativePhoto returns the stored photo for a dish-format archetype, or nil if
// none has been matched yet.
//
// nil, not a placeholder image, for the same reason Mark returns nil when this
// repository carries no artwork for an id: a recipe whose archetype has no photo prints
// its drawn mark instead, and a missing photo is an ordinary, expected absence -- most
// archetypes won't have full coverage from a bounded fetch, by design (see the plan's
// Task 3 per-label caps).
//
// The lowest photo_id is picked deterministically rather than randomly: two renders of
// the same book should show the same photo, not a different one each time a chapter is
// regenerated.
func RepresentativePhoto(ctx context.Context, pool *pgxpool.Pool, markID string) (*RecipePhoto, error) {
	if markID == "" {
		return nil, nil
	}
	var (
		mediaType, label string
		imgBytes         []byte
	)
	err := pool.QueryRow(ctx, `
		SELECT media_type, bytes, source_label
		FROM dish_format_photo
		WHERE mark_id = $1
		ORDER BY photo_id
		LIMIT 1`, markID).Scan(&mediaType, &imgBytes, &label)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("book: representative photo for %s: %w", markID, err)
	}
	return &RecipePhoto{
		DataURI:     template.URL("data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(imgBytes)),
		SourceLabel: label,
	}, nil
}
```

Add `"context"`, `"github.com/jackc/pgx/v5"`, `"github.com/jackc/pgx/v5/pgxpool"` to `photo.go`'s imports.

- [ ] **Step 4: Run the tests again to verify they pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/... -run TestRepresentativePhoto -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/book/photo.go internal/book/photo_test.go
git commit -m "Add RepresentativePhoto: the render-time lookup by dish-format archetype"
```

---

## Task 7: Wire `Photo` into `RecipeCard`, both card-building paths, and the template

**Files:**
- Modify: `internal/book/types.go`
- Modify: `internal/book/book2.go`
- Modify: `internal/book/invented.go`
- Modify: `internal/book/templates/book2/recipe.html`
- Test: `internal/book/assemble_test.go`, `internal/book/invented_test.go`, `internal/book/render_test.go` (existing files — add to each)

**Interfaces:**
- Consumes: `RepresentativePhoto` from Task 6.
- Produces: `RecipeCard.Photo *RecipePhoto` field.

- [ ] **Step 1: Add the field to `RecipeCard` in `types.go`**

```go
	// Photo is the representative photograph for this recipe's dish-format archetype, or
	// nil. Real photography, but of the archetype, not necessarily this exact recipe --
	// see docs/superpowers/specs/2026-08-24-recipe-photo-pipeline-design.md. When present,
	// the template shows it instead of Mark; when nil, Mark's drawn artwork prints as
	// before. Both are always resolved together from the same mark_id, so a recipe is
	// never left with neither.
	Photo *RecipePhoto `json:"photo,omitempty"`
```

Add this immediately after the existing `Mark *DishMark` field so the two stay visually paired for the next reader.

- [ ] **Step 2: Wire it into `loadRecipeCards` in `book2.go`**

Find the existing line (per the query built in Task 1's review of the current code):
```go
			Mark: Mark(markID, formatLabel),
```
Change the surrounding block to resolve the photo alongside it:
```go
			Mark: Mark(markID, formatLabel),
```
stays as-is; after the `card := RecipeCard{...}` literal closes (right before the existing `card.ModificationNote = ...` line), add:
```go
		if markID != "" {
			photo, err := RepresentativePhoto(ctx, pool, markID)
			if err != nil {
				return nil, nil, fmt.Errorf("recipe %s: %w", recipeID, err)
			}
			card.Photo = photo
		}
```
This runs once per recipe inside the existing `for rows.Next()` loop — an extra query per recipe rather than a join, matching how `card.ModificationNote` is already resolved with its own separate call rather than folded into the main `SELECT`. (A left join against `dish_format_photo` would also work and would avoid N extra queries; note this as a follow-up optimization if `loadRecipeCards`' performance on a full 940-recipe run becomes a concern — not needed for correctness now, and `dish_format_photo` rows are small in count, so the extra round trips are cheap.)

- [ ] **Step 3: Wire it into `inventCard` in `invented.go`**

Find the existing line (per Task 1's grep):
```go
		Mark:             Mark(invented.DishFormatID, archetypeLabel(invented.DishFormatID)),
```
Immediately after the struct literal this line belongs to closes, add the same resolution:
```go
	if photo, err := RepresentativePhoto(ctx, pool, invented.DishFormatID); err != nil {
		return RecipeCard{}, fmt.Errorf("invented recipe photo lookup: %w", err)
	} else {
		card.Photo = photo // "card" is whatever local variable name the struct literal was assigned to -- match it exactly, don't introduce a new name
	}
```
Read the surrounding function fully before editing — confirm the exact variable name the `RecipeCard{...}` literal is assigned to (the earlier grep showed line 315 sits inside a larger literal; use whatever that literal's target variable actually is, not a guessed name).

- [ ] **Step 4: Update `recipe.html`'s `.dish-mark` block**

Find:
```html
  {{ with .Mark }}
  <figure class="dish-mark">
    {{ .SVG }}
    <figcaption class="dish-mark-caption">{{ .FormatLabel }}</figcaption>
  </figure>
  {{ end }}
```
Replace with:
```html
  {{ if .Photo }}
  <figure class="dish-mark">
    <img class="dish-photo" src="{{ .Photo.DataURI }}" alt="">
  </figure>
  {{ else }}
  {{ with .Mark }}
  <figure class="dish-mark">
    {{ .SVG }}
    <figcaption class="dish-mark-caption">{{ .FormatLabel }}</figcaption>
  </figure>
  {{ end }}
  {{ end }}
```
`alt=""` deliberately, matching how the cover photo's own `<img>` (check `book1/cover.html`) is written — read that file first and match its exact `alt` convention rather than inventing a different one here.

- [ ] **Step 5: Add `.dish-photo` sizing to `tokens.css`**

The photo needs to render at the same footprint as the mark it replaces (`22mm` per `.dish-mark svg`'s existing rule) so the page-fit budget documented at length in `tokens.css` doesn't shift. Add immediately after the existing `.dish-mark svg { width: 22mm; height: 22mm; display: block; }` rule:
```css
.dish-mark img.dish-photo {
  width: 22mm;
  height: 22mm;
  object-fit: cover;
  display: block;
}
```
Do not touch any other rule in the recipe-page section of `tokens.css` in this task — a bigger, warmer photo treatment is explicitly Plan 4's job (see the plan header), and this task's whole point is same-footprint parity so nothing here needs re-measuring against the page-fit budget.

- [ ] **Step 6: Write a render test proving both the photo and the mark-fallback paths**

Add to `internal/book/render_test.go` (read the file first to match its existing helper functions — likely something that renders a single `RecipeCard` through the real `book2/recipe.html` template and returns the HTML string; use that helper rather than re-implementing template execution):

```go
func TestRecipePageShowsPhotoWhenPresentAndMarkOtherwise(t *testing.T) {
	withPhoto := RecipeCard{ /* ... populate the minimum fields the existing render helper requires, following whatever an existing recipe-page render test already does ... */
		Photo: &RecipePhoto{DataURI: "data:image/jpeg;base64,ZmFrZQ=="},
	}
	html := renderRecipeCard(t, withPhoto) // use the actual existing helper name from render_test.go
	if !strings.Contains(html, `<img class="dish-photo"`) {
		t.Fatalf("expected a dish-photo img tag when Photo is set, got:\n%s", html)
	}
	if strings.Contains(html, `dish-mark-caption`) {
		t.Fatalf("must not print the drawn-mark caption when a real photo is shown")
	}

	withMarkOnly := RecipeCard{
		Mark: &DishMark{ID: "bowl-grain", FormatLabel: "test format", SVG: template.HTML("<svg></svg>")},
	}
	html2 := renderRecipeCard(t, withMarkOnly)
	if !strings.Contains(html2, "dish-mark-caption") {
		t.Fatalf("expected the drawn mark to print when Photo is nil, got:\n%s", html2)
	}
	if strings.Contains(html2, `class="dish-photo"`) {
		t.Fatalf("must not print an img tag when Photo is nil")
	}
}
```

Adjust the `RecipeCard{}` literals to include whatever other fields the real render helper requires to not error (check an existing recipe-page render test in the same file for the minimal required set).

- [ ] **Step 7: Run the full book test suite**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/book/... -v 2>&1 | tail -80`
Expected: PASS across the board, including the new tests and every pre-existing one (`TestEmptyChaptersAreOmittedAndReported`, the invented-recipe suite from Plan 2, `pagefit_test.go`'s guards — a 22mm same-footprint image swap should not move the page-fit budget, but confirm rather than assume).

- [ ] **Step 8: Commit**

```bash
git add internal/book/types.go internal/book/book2.go internal/book/invented.go \
        internal/book/templates/book2/recipe.html internal/book/templates/tokens.css \
        internal/book/render_test.go
git commit -m "Show a real dish-format photo on the recipe page when one is stored, mark otherwise"
```

---

## Task 8: Full verification pass

**Files:** none new — this task only runs checks.

- [ ] **Step 1: Full backend verification**

Run:
```bash
go build ./...
go vet ./...
gofmt -l internal/photomatch cmd/photomatch internal/book | tee /tmp/gofmt-check.txt
```
Expected: `go build`/`go vet` clean; `gofmt -l` prints nothing (empty file) — if it lists any file, run `gofmt -w` on it and re-check.

- [ ] **Step 2: Full test suite against a fresh database**

Run:
```bash
scripts/dev_db.fish down
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import
go run ./cmd/enrich
TEST_DATABASE_URL=$DATABASE_URL go test ./... -count=1
```
Expected: every package green, including `internal/photomatch` (new), `internal/book` (extended), `internal/db` (gap-register count now `29`).

- [ ] **Step 3: Frontend suite, unaffected**

Run: `cd web && npm test`
Expected: unchanged pass count — this plan touches no frontend file.

- [ ] **Step 4: Gap register sanity check**

Run:
```sql
SELECT gap_id, severity, affected_rows, measured_by FROM gap_register WHERE gap_id IN ('GAP-025', 'GAP-029');
```
Expected: `GAP-025` unchanged (`minor`, `940`, `importer`) — this pipeline must not have moved it. `GAP-029` present (`minor`, `11`, `importer`) with `affected_rows` reflecting archetypes still lacking a stored photo, if `internal/importer/gaps.go` was extended to measure it live (optional for this plan — if not wired to `gapMeasures`, `GAP-029` stays at its seeded value `11` and `measured_by = 'seed'`; note explicitly in the final report which of the two is true rather than assuming).

- [ ] **Step 5: Visual check — print a real book and look**

Run (needs Chromium on PATH):
```bash
BOOK_PAGE_DUMP=/tmp/photo-check DATABASE_URL=$DATABASE_URL go test ./internal/book/... -run TestBookSet -v
```
Look at the dumped Book 2 PDF: confirm at least one recipe (whichever archetype `cmd/photomatch` actually reached coverage for in Task 5 Step 3's real fetch) shows a real photograph, correctly sized, no layout regression on that page or its neighbors. If Chromium isn't available in this environment, note that explicitly and flag it for a follow-up check in an environment that has it — a skipped visual check is not a passed one, per this project's own stated discipline in `pagefit_test.go`'s comments.

- [ ] **Step 6: Final commit if Steps 1-5 turned up any fixups**

```bash
git add -A
git commit -m "Fix up formatting/test fallout from the photo pipeline verification pass"
```
(Only if there's something to commit — an empty `git status` here means skip this step entirely.)

---

## Notes for whoever executes this plan

- Task 5 Step 3 and Task 8 Step 5 are the only two steps in this whole plan that need real network access or a real Chromium install respectively. Every other step is self-contained and testable offline against fakes/a local database.
- The label→archetype map in Task 1 is a real editorial judgement call, written out plainly with a `note` per row specifically so it's reviewable. If the hand-check in Task 5 Step 3 (or a later look at real fetched images) shows a mapping is visually wrong — a `dosa` photo that doesn't actually look like a `pancake`-archetype dish, for instance — fix the migration's seed data in a follow-up migration (never edit `0026` after it has run in any shared environment) rather than papering over it in code.
- This plan deliberately stops at "one representative photo per archetype, same footprint as the drawn mark." Bigger photo-forward layout, multiple photos per archetype with rotation, warmer accent treatment — all Plan 4, and Plan 4 does not start until this plan is fully landed and the user has looked at real photos in a real printed book.
