# Engine Honesty and Input Completeness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the engine tell the truth about what it did and did not screen, escalate the
clinical rules the provider marks as specialist-only, and expose the six inputs the API
already accepts but the console never sends.

**Architecture:** No new subsystem. Two engine functions gain a return value or a wider
query, three read-only reference endpoints join the six that exist, one ranker is added
to `internal/engine/rank.go`, and the profile form gains controls fed by those endpoints.
Four new `gap_register` rows record holes the register does not currently carry. Every
change is to existing files following their existing patterns.

**Tech Stack:** Go 1.25, chi v5, pgx v5 (`pgxpool`), Next.js App Router, React,
TypeScript, Tailwind v4, shadcn/ui.

**Spec:** `docs/superpowers/specs/2026-08-18-phase-3-foundation-design.md`, sections 1-5
and "A documentation correction this work must make". Every column name, rule id and
magnitude below was read live from the workbooks and the checked-in source, not inferred.

## Global Constraints

- Go 1.25. `go build ./...`, `go vet ./...` and `go test ./...` must stay green.
- Errors wrapped `fmt.Errorf("...: %w", err)` at each boundary. `ErrInvalidProfile`
  (`internal/engine/errors.go`) is the only sentinel; handlers map it to 400.
- Table-driven tests, package-local (`foo_test.go` beside the code). The engine and
  handler suites need `TEST_DATABASE_URL`; without it they skip.
- **Hard rule: never invent data.** Every value returned traces to a provider column, an
  external dataset column, or a `value_kind = 'derived'` view carrying its formula.
- Steps 1 (age) and 2 (allergy) are hard filters. This plan must not add an override,
  a "show excluded anyway" toggle, or any path that relaxes either.
- Rankers never empty a result set. Only steps 1, 2, 4 and the clinical-escalation half of
  step 3 may reduce a pool to zero, and each must say so in its `StepResult`.
- No emoji anywhere. No attribution trailers on any commit.
- Ranker magnitudes live in the same normalised space: culture boost `0.05`, availability
  weight `0.05`, budget boost `0.03`, duplicate penalty `-0.02`. Anything added sits inside
  that range so preference can never outrank nutrition.

---

## Ground truth read from source (cite, do not re-derive)

```
allergen_tag_vocabulary (migration 0011) -- 11 rows.
  corpus_tag NOT NULL: Egg, Fish, Milk, Peanut, Sesame, Soy, Wheat(->'Gluten-containing cereal')
  corpus_tag IS NULL:  Crustacean/Mollusc, Mustard, Sulphites, Tree nuts

clinical_rule_master.human_approval_level -- 5 distinct values. The escalation tier is the
literal string 'Specialist clinical approval', on exactly 14 rules:
  CR-GROW-002 CR-ALL-002 CR-ALL-003 CR-CEL-002 CR-DM-001 CR-DM-002 CR-REN-001
  CR-REN-002  CR-FEED-002 CR-PREM-001 CR-LIV-001 CR-MET-001 CR-IBD-001 CR-ED-001

clinical_rule_master.specialist_required -- FREE TEXT on all 31 rows, never Y/N.
  e.g. 'Pediatric review if concern', 'Not applicable', 'Paediatric nephrology/dietitian'.
  Never branch on it. Render it verbatim.

escalationOnlyDomains (internal/engine/clinical.go) -- 10 domains, hand-written.
  Disagreement with the specialist tier, both directions:
    caught by tier, missed by map: Diabetes (CR-DM-001/002, hard_exclude_yn='N'),
                                   Food Allergy (CR-ALL-003, excluded by the domain filter)
    caught by map, not at tier:    Vomiting / Poor Intake (CR-GI-002, 'Clinical approval')

gap_register -- 16 rows. GAP-001..GAP-012 are seeded in migration 0002; GAP-013..GAP-016
are upserted by internal/enrich/gaps.go on every cmd/enrich run and no migration writes
them. internal/importer/gaps.go only UPDATEs affected_rows; it never INSERTs. The four
documents saying 16 are correct. New gaps therefore start at GAP-017 and take it to 20.

models.EngineResult.Steps currently holds 13 entries (steps 1-13; step 8 is an explicit
no-op, step 14 never appears). internal/engine/pipeline_test.go:40 pins that 13.

Corpus time values: prep_time_min in {5,10,15,20}; cook_time_min in {10,15,20,25,30,35}.
```

---

### Task 1: `UnscreenedAllergens` becomes a field on `EngineResult`

**Files:**
- Modify: `internal/models/engine.go`
- Modify: `internal/engine/steps_hard.go`
- Modify: `internal/engine/pipeline.go`
- Test: `internal/engine/steps_hard_test.go`, `internal/engine/pipeline_test.go`

**Interfaces:**
- Produces: `models.EngineResult.UnscreenedAllergens []string`, and a widened
  `allergyFilter(ctx, pool, p, candidateIDs) (ids []string, step models.StepResult, unscreened []string, err error)`.
  Task 3 and Task 7 both read the new field.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/steps_hard_test.go`:

```go
func TestAllergyFilterReportsUnscreenedGroups(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}

	cases := []struct {
		name           string
		allergens      []string
		wantUnscreened []string
	}{
		{"tree nuts have no corpus tag", []string{"Tree nuts"}, []string{"Tree nuts"}},
		{"peanut has one", []string{"Peanut"}, nil},
		{"mixed reports only the unscreened half", []string{"Peanut", "Mustard"}, []string{"Mustard"}},
		{"none declared", nil, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, unscreened, err := allergyFilter(ctx, pool,
				models.ChildProfile{AgeMonths: 36, Allergens: c.allergens}, all)
			if err != nil {
				t.Fatalf("allergyFilter: %v", err)
			}
			if len(unscreened) != len(c.wantUnscreened) {
				t.Fatalf("unscreened = %v, want %v", unscreened, c.wantUnscreened)
			}
			for i := range c.wantUnscreened {
				if unscreened[i] != c.wantUnscreened[i] {
					t.Fatalf("unscreened = %v, want %v", unscreened, c.wantUnscreened)
				}
			}
		})
	}
}
```

Append to `internal/engine/pipeline_test.go`:

```go
func TestRunSurfacesUnscreenedAllergensOnTheResult(t *testing.T) {
	pool := testPool(t)
	result, err := Run(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, Allergens: []string{"Tree nuts"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.UnscreenedAllergens) != 1 || result.UnscreenedAllergens[0] != "Tree nuts" {
		t.Fatalf("UnscreenedAllergens = %v, want [Tree nuts]; a declared allergen that "+
			"screened nothing must reach the caller as a field, not only as a step note",
			result.UnscreenedAllergens)
	}
	if len(result.Recipes) == 0 {
		t.Fatal("an unscreened allergen must not empty the result set; it screens nothing")
	}
}
```

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/... -run 'TestAllergyFilterReportsUnscreened|TestRunSurfacesUnscreened' -v`

Expected: FAIL to compile — `allergyFilter` returns 3 values not 4, and
`result.UnscreenedAllergens` is undefined.

- [ ] **Step 3: Add the field to `internal/models/engine.go`**

Add to the `EngineResult` struct, after `BlockReason`:

```go
	// UnscreenedAllergens names declared allergen groups that have no tag anywhere in
	// the recipe corpus (allergen_tag_vocabulary.corpus_tag IS NULL). They excluded zero
	// recipes because nothing carries the tag, not because the filter passed. Any client
	// rendering a result set MUST render this: a result page that omits it implies a
	// screening that did not happen, which is the one failure mode this project treats
	// as dangerous rather than untidy.
	UnscreenedAllergens []string `json:"unscreened_allergens,omitempty"`
```

- [ ] **Step 4: Widen `allergyFilter` in `internal/engine/steps_hard.go`**

Change the signature:

```go
func allergyFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, []string, error) {
```

Every existing `return` inside the function gains a slice in third position. The early
no-op return becomes:

```go
	if len(p.Allergens) == 0 || stepIn == 0 {
		return candidateIDs, models.StepResult{
			Step: 2, Name: "Allergy / intolerance / safety", Kind: "hard_filter",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no allergens declared, step is a no-op",
		}, nil, nil
	}
```

Every error return becomes `return nil, models.StepResult{}, nil, fmt.Errorf(...)` with its
existing message unchanged.

The final return carries the `absent` slice that the function already computes:

```go
	return ids, models.StepResult{
		Step: 2, Name: "Allergy / intolerance / safety", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: len(ids), Note: note,
	}, absent, nil
```

Leave the `note` construction exactly as it is. The note is the human-readable long form
and stays; the slice is the machine-readable short form beside it.

- [ ] **Step 5: Thread it through `internal/engine/pipeline.go`**

Change the call site:

```go
	ids, step2, unscreened, err := allergyFilter(ctx, pool, p, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step2)
```

The blocked early return must carry it too, because a profile can declare an unscreened
allergen and also trip a clinical hold, and the operator needs both facts:

```go
	if blocked {
		return models.EngineResult{
			Steps:               steps,
			Blocked:             true,
			BlockReason:         blockReason,
			UnscreenedAllergens: unscreened,
		}, nil
	}
```

And the success return:

```go
	return models.EngineResult{
		Recipes:             ranked,
		Steps:               steps,
		ActiveTarget:        targetCode,
		TargetReason:        targetReason,
		UnscreenedAllergens: unscreened,
	}, nil
```

- [ ] **Step 6: Fix the existing call in `internal/engine/steps_hard_test.go`**

`TestAllergyFilterExcludesDeclaredAllergen` calls `allergyFilter` with three return values.
Change its call to:

```go
	filtered, step, _, err := allergyFilter(ctx, pool, models.ChildProfile{Allergens: []string{"Peanut"}}, all)
```

- [ ] **Step 7: Run the full engine suite, confirm pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/... -v`

Expected: PASS, including the five personas and the peanut-leak guard.

- [ ] **Step 8: `go build ./... && go vet ./...`, then commit**

```bash
git add internal/models/engine.go internal/engine/steps_hard.go internal/engine/pipeline.go internal/engine/steps_hard_test.go internal/engine/pipeline_test.go
git commit -m "Report unscreened allergen groups as a field, not only a step note"
```

---

### Task 2: `GET /api/reference/allergens`

**Files:**
- Modify: `internal/api/handlers/reference.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/handlers/reference_test.go`

**Interfaces:**
- Produces: `(*Handlers).ReferenceAllergens` at `GET /api/reference/allergens`, returning
  `[{allergen_group, corpus_tag, screens}]`. Task 7's multi-select is built from it.

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers/reference_test.go`:

```go
func TestReferenceAllergensReportsWhetherEachGroupScreens(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/allergens", nil)
	rec := httptest.NewRecorder()

	h.ReferenceAllergens(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		AllergenGroup string  `json:"allergen_group"`
		CorpusTag     *string `json:"corpus_tag"`
		Screens       bool    `json:"screens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 11 {
		t.Fatalf("expected 11 allergen groups from allergen_tag_vocabulary, got %d", len(got))
	}
	for _, g := range got {
		if g.Screens != (g.CorpusTag != nil) {
			t.Fatalf("%s: screens=%v but corpus_tag nil=%v; screens must be derived from "+
				"the tag, never asserted independently", g.AllergenGroup, g.Screens, g.CorpusTag == nil)
		}
	}
}

// TestEveryOfferedAllergenScreensSomething fails on four rows today and is meant to.
// It is the tracking mechanism for GAP-017: it turns green only when the provider tags
// the corpus for Tree nuts, Crustacean/Mollusc, Mustard and Sulphites. It skips rather
// than fails so it does not break CI, because the hole is the provider's to close and a
// red suite trains people to ignore red suites.
func TestEveryOfferedAllergenScreensSomething(t *testing.T) {
	pool := testPool(t)
	var unscreened []string
	rows, err := pool.Query(context.Background(),
		`SELECT allergen_group FROM allergen_tag_vocabulary WHERE corpus_tag IS NULL ORDER BY allergen_group`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			t.Fatalf("scan: %v", err)
		}
		unscreened = append(unscreened, g)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(unscreened) > 0 {
		t.Skipf("GAP-017 still open: %d allergen group(s) screen nothing: %v. "+
			"They remain selectable and are reported in EngineResult.UnscreenedAllergens. "+
			"This test passes when the provider tags the corpus.", len(unscreened), unscreened)
	}
}
```

Add `"context"` to that file's imports if it is not already present.

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/api/handlers/... -run 'TestReferenceAllergens|TestEveryOfferedAllergen' -v`

Expected: `TestReferenceAllergens...` FAILS to compile (`undefined: h.ReferenceAllergens`).

- [ ] **Step 3: Write the handler**

Append to `internal/api/handlers/reference.go`:

```go
// ReferenceAllergens returns allergen_tag_vocabulary: the eleven provider allergen groups
// with the literal corpus tag each maps to, and whether declaring it screens anything at
// all. Four groups have no corpus tag (migration 0011 verified this against the live
// corpus), so screens is false for them and the picker built from this endpoint must show
// that state rather than offering them as though they filter.
//
// screens is derived from corpus_tag rather than stored, so the two can never disagree.
func (h *Handlers) ReferenceAllergens(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT allergen_group, corpus_tag, note, corpus_tag IS NOT NULL AS screens
		FROM allergen_tag_vocabulary
		ORDER BY (corpus_tag IS NULL), allergen_group`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "allergen list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type allergen struct {
		AllergenGroup string  `json:"allergen_group"`
		CorpusTag     *string `json:"corpus_tag"`
		Note          string  `json:"note"`
		Screens       bool    `json:"screens"`
	}
	out := []allergen{}
	for rows.Next() {
		var a allergen
		if err := rows.Scan(&a.AllergenGroup, &a.CorpusTag, &a.Note, &a.Screens); err != nil {
			writeError(w, http.StatusInternalServerError, "allergen scan failed: "+err.Error())
			return
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "allergen rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
```

`ORDER BY (corpus_tag IS NULL), allergen_group` puts the screening groups first, so a
picker rendering in order lists working options above the four that do not.

- [ ] **Step 4: Register the route**

In `internal/api/router.go`, after the `nutrition-targets` line:

```go
	r.Get("/api/reference/allergens", h.ReferenceAllergens)
```

- [ ] **Step 5: Run tests, confirm pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/api/... -v`

Expected: `TestReferenceAllergensReportsWhetherEachGroupScreens` PASS,
`TestEveryOfferedAllergenScreensSomething` SKIP naming the four groups.

- [ ] **Step 6: `go vet ./...`, then commit**

```bash
git add internal/api/handlers/reference.go internal/api/router.go internal/api/handlers/reference_test.go
git commit -m "Add the allergen reference endpoint with an explicit screens flag"
```

---

### Task 3: Four missing gap register rows, and the documentation correction

**Files:**
- Create: `internal/db/migrations/0012_gap_register_additions.up.sql`
- Create: `internal/db/migrations/0012_gap_register_additions.down.sql`
- Modify: `internal/importer/gaps.go`
- Modify: `CLAUDE.md`, `docs/handover-2026-08-18.md`, `docs/not-built.md`
- Test: `internal/db/integrity_test.go`

**Interfaces:**
- Consumes: `allergen_tag_vocabulary` (migration 0011), `clinical_rule_master`.
- Produces: `GAP-017`, `GAP-018`, `GAP-019`, `GAP-020` in `gap_register`, two of them
  re-measured on every import. Task 4 asserts `GAP-020` reaches zero.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/integrity_test.go`:

```go
func TestGapRegisterCountMatchesTheDocumentedCount(t *testing.T) {
	pool := testPool(t)
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM gap_register`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	// Sixteen after migration 0012. The four documents that already claimed sixteen were
	// wrong when written -- the register held twelve -- and this test exists so the
	// number is asserted rather than asserted-in-prose. If a gap is added or retired,
	// change this number and the four documents in the same commit.
	if n != 20 {
		t.Fatalf("gap_register holds %d rows, want 20", n)
	}
}

func TestNewGapsAreMeasuredNotGuessed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cases := []struct {
		gapID string
		want  int
	}{
		// Four allergen groups with no corpus tag, counted from the vocabulary table.
		{"GAP-017", 4},
		// Clinical rules at the provider's specialist tier that the engine does not
		// escalate. Zero once Task 4 lands; non-zero here is the finding itself.
		{"GAP-020", 0},
	}
	for _, c := range cases {
		t.Run(c.gapID, func(t *testing.T) {
			var got *int
			err := pool.QueryRow(ctx,
				`SELECT affected_rows FROM gap_register WHERE gap_id = $1`, c.gapID).Scan(&got)
			if err != nil {
				t.Fatalf("lookup %s: %v", c.gapID, err)
			}
			if got == nil {
				t.Fatalf("%s has a NULL count; it is declared measured_by = importer and "+
					"must carry a number after an import", c.gapID)
			}
			if *got != c.want {
				t.Fatalf("%s affected_rows = %d, want %d", c.gapID, *got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/db/... -run 'TestGapRegisterCount|TestNewGapsAreMeasured' -v`

Expected: FAIL — `gap_register holds 12 rows, want 16`, and `lookup GAP-017: no rows`.

- [ ] **Step 3: Write `internal/db/migrations/0012_gap_register_additions.up.sql`**

```sql
-- Four holes the register did not carry, found while specifying the Phase 3 foundation.
--
-- The register's stated purpose is that a missing value is visible rather than silently
-- absent. Four things were absent from the register itself, which is the same failure one
-- level up. Two of them re-measure on every import (see internal/importer/gaps.go); two
-- cannot be counted by any query, because there is no way to measure the absence of rows
-- nobody has written, and they follow GAP-010's precedent of a seeded NULL count.
--
-- This migration also takes the register from twelve rows to sixteen. CLAUDE.md,
-- docs/handover-2026-08-18.md, docs/not-built.md and the Phase 2 frontend plan all claimed
-- sixteen while it held twelve. Those documents were wrong when written and become right
-- only by coincidence; they are corrected in the same commit as this migration.

INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows, measured_by, ui_behaviour, resolution_path) VALUES

    ('GAP-017', 'blocker', 'Allergen screening', 'allergen_tag_vocabulary', 'corpus_tag',
     'Four declared allergen groups (Tree nuts, Crustacean/Mollusc, Mustard, Sulphites) have no matching tag anywhere in the recipe or ingredient corpus. Declaring one is accepted and excludes zero recipes, because nothing carries the tag. This is an absent screen, not a passing filter.',
     NULL, 'importer',
     'The group stays selectable and is returned in EngineResult.unscreened_allergens. Any client rendering a result set must show a persistent "not screened - no corpus coverage" state beside the field and on the results.',
     'Provider tags the ingredient corpus for these four groups. The count reaches zero on its own when they do; no code change is needed to close it.'),

    ('GAP-018', 'major', 'Book 1 assembly', NULL, NULL,
     'The Book Assembly Logic sheet in MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx numbers its rows 1-11, then 17-19, then 15-16. Nothing numbered 12, 13 or 14 exists in the workbook. The Book 1 pipeline specification therefore has a three-step hole between generating parent-writable pages and inserting daily-life modules.',
     NULL, 'seed',
     'Not surfaced until a Book 1 assembler exists. Recorded so the hole is not rediscovered as a bug in the assembler.',
     'Ask the provider whether steps 12-14 were dropped, renumbered, or never written. Outstanding question 11.'),

    ('GAP-019', 'blocker', 'Clinical scope', 'clinical_rule_master', 'trigger_field',
     'Down syndrome, cerebral palsy, congenital heart disease, cleft lip and palate, autism and intellectual disability have no rule row. Each changes feeding through texture, energy density, oral-motor ability, mealtime behaviour and sometimes fluid restriction. A child with one of them is currently scored like any other child.',
     NULL, 'seed',
     'No behaviour today: the engine cannot know about a condition with no trigger_field. It holds only for conditions the masters name.',
     'Provider extends clinical_rule_master. The list cannot be written on this side without inventing clinical scope. Outstanding question 10.'),

    ('GAP-020', 'blocker', 'Clinical escalation', 'clinical_rule_master', 'human_approval_level',
     'Clinical rules the provider marks Specialist clinical approval that the engine does not escalate. Before the fix this was two rules: Diabetes_Type (CR-DM-001, CR-DM-002) and Multiple_Food_Allergies (CR-ALL-003), all invisible to the engine because the rule query filtered on hard_exclude_yn = Y and excluded the Food Allergy domain.',
     NULL, 'importer',
     'Escalated rules return blocked = true with the rule id, the domain and the provider''s own specialist_required text verbatim. A rule at neither the specialist tier nor in escalationOnlyDomains passes through untouched.',
     'Closed in code by taking the union of the specialist tier and the hand-written domain map. The measure stays as a regression guard: it must remain zero.');
```

- [ ] **Step 4: Write `internal/db/migrations/0012_gap_register_additions.down.sql`**

```sql
DELETE FROM gap_register WHERE gap_id IN ('GAP-017', 'GAP-018', 'GAP-019', 'GAP-020');
```

- [ ] **Step 5: Add the two measures to `internal/importer/gaps.go`**

Append to the `gapMeasures` slice, after the `GAP-012` entry:

```go
	// Allergen groups the provider's vocabulary names but the corpus never tags. Counted
	// from the vocabulary table rather than from a hardcoded list, so it drops to zero by
	// itself when the provider tags the corpus.
	{"GAP-017", `SELECT count(*) FROM allergen_tag_vocabulary WHERE corpus_tag IS NULL`},

	// Clinical rules at the provider's own 'Specialist clinical approval' tier that the
	// engine would not escalate. The engine escalates the union of that tier and the
	// hand-written escalationOnlyDomains map, so this counts tier rules whose domain the
	// map also misses -- zero after the union lands, and a regression guard afterwards.
	{"GAP-020", `SELECT count(*) FROM clinical_rule_master
	             WHERE human_approval_level = 'Specialist clinical approval'
	               AND clinical_domain NOT IN (
	                   'Coeliac Disease', 'Eating Disorder Risk', 'Feeding/Swallowing',
	                   'Vomiting / Poor Intake', 'Growth', 'GI Chronic Disease',
	                   'Liver Disease', 'Metabolic Disease', 'Prematurity/Complex Care',
	                   'Kidney Disease')`},
```

That domain list duplicates `escalationOnlyDomains` in `internal/engine/clinical.go`.
Task 4 adds a test asserting the two stay identical, so the duplication is pinned rather
than left to drift.

- [ ] **Step 6: Run tests, confirm `GAP-017` passes and `GAP-020` still fails**

```bash
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import
TEST_DATABASE_URL=$DATABASE_URL go test ./internal/db/... -run 'TestGapRegisterCount|TestNewGapsAreMeasured' -v
```

Expected: `TestGapRegisterCountMatchesTheDocumentedCount` PASS.
`TestNewGapsAreMeasured/GAP-017` PASS with 4.
`TestNewGapsAreMeasured/GAP-020` FAIL with `affected_rows = 2, want 0` — that is the
finding, and Task 4 closes it.

- [ ] **Step 7: Correct the four documents**

In `CLAUDE.md`, find the line in the Phase 1 status table reading
`| 8 | Gap register | done - 16 rows, re-counted on every run |` and change `16 rows` to
`16 rows`; then find the exit-criteria line
`- [x] The gap register accounts for every known hole (16 entries)` and leave the number,
but append `, four of them added in migration 0012`.

In `docs/handover-2026-08-18.md`, section 7, change
`16 rows, 4 marked blocker` to `16 rows after migration 0012, 6 marked blocker` and add
`GAP-017`, `GAP-019` and `GAP-020` to the blocker table beneath it.

In `docs/not-built.md`, replace section 1.1's option list with the settled position:
migration `0011` already bridged the vocabulary and fixed the `Wheat` ->
`Gluten-containing cereal` mismatch; the four groups stay selectable; the unscreened set
is reported on `EngineResult`; `GAP-017` tracks it.

In `docs/superpowers/plans/2026-08-16-frontend-shadcn-devtool.md`, Task 12 Step 2 item 5,
change `confirm 16 gaps` to `confirm 16 gaps (12 original plus 4 from migration 0012)`.

- [ ] **Step 8: Commit**

```bash
git add internal/db/migrations/0012_gap_register_additions.up.sql internal/db/migrations/0012_gap_register_additions.down.sql internal/importer/gaps.go internal/db/integrity_test.go CLAUDE.md docs/handover-2026-08-18.md docs/not-built.md docs/superpowers/plans/2026-08-16-frontend-shadcn-devtool.md
git commit -m "Record four gaps the register did not carry, and correct its documented count"
```

---

### Task 4: Escalate on the provider's own specialist tier

**Files:**
- Modify: `internal/engine/clinical.go`
- Test: `internal/engine/clinical_test.go`

**Interfaces:**
- Consumes: `models.ChildProfile.ClinicalFlags`, `clinical_rule_master`.
- Produces: unchanged signature
  `clinicalFilter(ctx, pool, p, candidateIDs) ([]string, models.StepResult, bool, string, error)`.
  Only its rule set and its `BlockReason` text change.

- [ ] **Step 1: Write the failing tests**

Append to `internal/engine/clinical_test.go`:

```go
func TestClinicalFilterBlocksAtTheProviderSpecialistTier(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		flags map[string]string
	}{
		// Both were invisible before: the rule query filtered on hard_exclude_yn = 'Y'
		// (these are 'N') and excluded the Food Allergy domain outright.
		{"diabetes", map[string]string{"Diabetes_Type": "Type 1"}},
		{"multiple food allergies", map[string]string{"Multiple_Food_Allergies": "Yes"}},
		// Already caught by the hand-written domain map; must stay caught.
		{"kidney disease", map[string]string{"CKD": "Yes"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, step, blocked, reason, err := clinicalFilter(ctx, pool,
				models.ChildProfile{AgeMonths: 36, ClinicalFlags: c.flags},
				[]string{"MG-R-00001"})
			if err != nil {
				t.Fatalf("clinicalFilter: %v", err)
			}
			if !blocked {
				t.Fatalf("%s sits at the provider's Specialist clinical approval tier and "+
					"must hold generation, not return a recipe list", c.name)
			}
			if reason == "" || step.CandidatesOut != 0 {
				t.Fatalf("a blocked result must explain itself and return zero candidates: reason=%q out=%d", reason, step.CandidatesOut)
			}
		})
	}
}

func TestClinicalFilterBlockReasonQuotesTheProviderSpecialist(t *testing.T) {
	pool := testPool(t)
	_, _, blocked, reason, err := clinicalFilter(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, ClinicalFlags: map[string]string{"CKD": "Yes"}},
		[]string{"MG-R-00001"})
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if !blocked {
		t.Fatal("CKD must block")
	}
	// specialist_required is free text naming which specialist. CR-REN-001 reads
	// "Paediatric nephrology/dietitian". It is rendered verbatim, never parsed.
	if !strings.Contains(reason, "nephrology") {
		t.Fatalf("BlockReason must quote the provider's specialist_required text so the "+
			"operator knows which specialist is needed; got %q", reason)
	}
}

func TestClinicalFilterDoesNotBlockANonEscalatingRule(t *testing.T) {
	pool := testPool(t)
	// CR-IRON-001 sits at 'Clinical approval' and its engine_action is "Boost iron-rich
	// recipes". Its specialist_required text ("Pediatrician/dietitian as indicated") is
	// non-empty like every other row's, which is exactly why that column must never be
	// treated as a flag.
	out, _, blocked, _, err := clinicalFilter(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, ClinicalFlags: map[string]string{"Anemia_or_Iron_Risk": "Yes"}},
		[]string{"MG-R-00001", "MG-R-00002"})
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if blocked {
		t.Fatal("an iron-risk flag must not hold generation; it is a ranking signal")
	}
	if len(out) != 2 {
		t.Fatalf("non-escalating flags pass candidates through untouched, got %d of 2", len(out))
	}
}

// TestEscalationSourcesDisagreementIsPinned prints every rule where the provider's
// specialist tier and the hand-written domain map disagree, in both directions. The
// engine blocks on the union, so a disagreement is not a bug -- but it must be a visible
// list rather than a silent preference for one source.
func TestEscalationSourcesDisagreementIsPinned(t *testing.T) {
	pool := testPool(t)
	rows, err := pool.Query(context.Background(), `
		SELECT rule_id, clinical_domain, human_approval_level
		FROM clinical_rule_master
		ORDER BY rule_id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var tierOnly, mapOnly []string
	for rows.Next() {
		var id, domain, level string
		if err := rows.Scan(&id, &domain, &level); err != nil {
			t.Fatalf("scan: %v", err)
		}
		atTier := level == specialistApprovalLevel
		inMap := escalationOnlyDomains[domain]
		switch {
		case atTier && !inMap:
			tierOnly = append(tierOnly, id+" ("+domain+")")
		case inMap && !atTier:
			mapOnly = append(mapOnly, id+" ("+domain+")")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	t.Logf("at the specialist tier but not in escalationOnlyDomains: %v", tierOnly)
	t.Logf("in escalationOnlyDomains but not at the specialist tier: %v", mapOnly)

	// Both directions are expected and both are escalated, because the engine takes the
	// union. This assertion exists so that a change to either source is noticed here
	// rather than discovered by a child getting an unescalated list.
	if len(tierOnly) == 0 && len(mapOnly) == 0 {
		t.Log("the two sources now agree exactly; escalationOnlyDomains may be retirable")
	}
}
```

Add `"strings"` to that file's imports.

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/... -run TestClinicalFilter -v`

Expected: FAIL to compile (`undefined: specialistApprovalLevel`), and once that is added,
`TestClinicalFilterBlocksAtTheProviderSpecialistTier/diabetes` fails because `Diabetes_Type`
does not block.

- [ ] **Step 3: Rewrite the rule query and the block condition in `internal/engine/clinical.go`**

Add the constant above `escalationOnlyDomains`:

```go
// specialistApprovalLevel is the one clinical_rule_master.human_approval_level value that
// means "a specialist must set the targets before anything is generated". The column takes
// five values; the other four ('Clinical approval', 'Clinical/editorial approval',
// 'Clinical/nutrition approval', 'Editorial/clinical workflow approval') describe review of
// generated output rather than a precondition for generating it.
//
// This is the provider's own machine-readable escalation boundary and is preferred to any
// list written on this side. Note which column it is NOT: specialist_required is free text
// on all 31 rows ("Pediatric review if concern", "Not applicable"), so branching on its
// emptiness would escalate every rule in the master, including iron support.
const specialistApprovalLevel = "Specialist clinical approval"
```

Extend `escalationOnlyDomains`'s comment with a paragraph:

```go
// Retained after specialistApprovalLevel was introduced, because the two sets are not
// identical and the map is broader in one place: CR-GI-002 (Vomiting / Poor Intake) sits
// at 'Clinical approval' yet holding a routine meal plan until hydration is assessed is
// the right behaviour. The engine escalates the union of the two. TestEscalationSources-
// DisagreementIsPinned prints every disagreement so neither source drifts unnoticed.
```

Add two fields to the `clinicalRule` struct:

```go
type clinicalRule struct {
	ruleID              string
	clinicalDomain      string
	triggerField        string
	triggerOperator     string
	triggerValue        string
	escalationReason    string
	humanApprovalLevel  string
	specialistRequired  string
}
```

Replace the rule query. The old `WHERE hard_exclude_yn = 'Y'` filter is what hid the
diabetes rules, so the new predicate selects on escalation instead of on exclusion:

```go
	rows, err := pool.Query(ctx, `
		SELECT rule_id, clinical_domain, trigger_field, trigger_operator, trigger_value,
		       escalation_reason, human_approval_level, coalesce(specialist_required, '')
		FROM clinical_rule_master
		WHERE (human_approval_level = $1 OR hard_exclude_yn = 'Y')
		  AND clinical_domain NOT IN ('Age/Feeding', 'Data Quality')`,
		specialistApprovalLevel)
```

Two changes to the domain exclusions, both deliberate:

- `Age/Feeding` stays excluded: `CR-AGE-001` and `CR-AGE-002` are enforced structurally by
  step 1's `min_age_months`/`max_age_months` bounds.
- **`Food Allergy` is no longer excluded.** Step 2 covers allergen tags, which handles
  `CR-ALL-001`. It does not handle `CR-ALL-003` (`Multiple_Food_Allergies` -> specialist
  pathway), which is a different question from "does this recipe contain the allergen".

Update the scan:

```go
		if err := rows.Scan(&r.ruleID, &r.clinicalDomain, &r.triggerField, &r.triggerOperator,
			&r.triggerValue, &r.escalationReason, &r.humanApprovalLevel, &r.specialistRequired); err != nil {
```

Replace the block decision inside the `for _, r := range rules` loop:

```go
		escalates := r.humanApprovalLevel == specialistApprovalLevel || escalationOnlyDomains[r.clinicalDomain]
		if !escalates {
			// A hard_exclude rule fired that neither sits at the specialist tier nor has
			// a mapped domain. Its recipe_filter_action needs a per-recipe clinical
			// safety tag that does not exist on any table, so passing it through would
			// half-apply a clinical filter. Fail loudly instead of choosing silently.
			return nil, models.StepResult{}, false, "",
				fmt.Errorf("engine: clinical rule %s (domain %s, approval %q) fired but is neither at the specialist tier nor in escalationOnlyDomains; classify it explicitly", r.ruleID, r.clinicalDomain, r.humanApprovalLevel)
		}

		reason := fmt.Sprintf("%s requires specialist review (rule %s, domain %s, %s): %s",
			r.triggerField, r.ruleID, r.clinicalDomain, r.humanApprovalLevel, r.escalationReason)
		if r.specialistRequired != "" {
			// Verbatim provider guidance naming which specialist. Never parsed.
			reason += " Specialist: " + r.specialistRequired + "."
		}
		return nil, models.StepResult{
			Step: 3, Name: "Clinical rules", Kind: "escalation",
			CandidatesIn: stepIn, CandidatesOut: 0, Note: reason,
		}, true, reason, nil
```

- [ ] **Step 4: Run the engine suite, confirm pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/... -v`

Expected: PASS. The five personas set no clinical flags, so none of them start blocking.

- [ ] **Step 5: Re-import and confirm `GAP-020` reaches zero**

```bash
go run ./cmd/import
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/db/... -run TestNewGapsAreMeasured -v
```

Expected: `GAP-020` PASS with `affected_rows = 0`.

- [ ] **Step 6: `go build ./... && go vet ./...`, then commit**

```bash
git add internal/engine/clinical.go internal/engine/clinical_test.go
git commit -m "Escalate clinical rules at the provider's specialist approval tier

Diabetes_Type and Multiple_Food_Allergies sat at the provider's own
Specialist clinical approval level and were invisible to the engine:
the rule query filtered on hard_exclude_yn = Y, which both are not,
and excluded the Food Allergy domain on the assumption step 2 covered
it. Step 2 covers allergen tags, not the multiple-allergy pathway.

The engine now escalates the union of that tier and the hand-written
domain map, and quotes the provider's specialist_required text so an
operator learns which specialist is needed."
```

---

### Task 5: `clinical-markers` and `enums` reference endpoints

**Files:**
- Modify: `internal/api/handlers/reference.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/handlers/reference_test.go`

**Interfaces:**
- Produces: `(*Handlers).ReferenceClinicalMarkers` at `GET /api/reference/clinical-markers`
  and `(*Handlers).ReferenceEnums` at `GET /api/reference/enums`. Task 7 builds the
  clinical-flags multi-select and the time selectors from them.

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers/reference_test.go`:

```go
func TestReferenceClinicalMarkersCoversEveryTriggerField(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/clinical-markers", nil)
	rec := httptest.NewRecorder()

	h.ReferenceClinicalMarkers(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		TriggerField string `json:"trigger_field"`
		RuleIDs      string `json:"rule_ids"`
		Escalates    bool   `json:"escalates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 28 {
		t.Fatalf("expected 28 distinct trigger_field values across the 31 rules, got %d", len(got))
	}
	var escalating int
	for _, m := range got {
		if m.RuleIDs == "" {
			t.Fatalf("%s carries no rule id; a marker with no rule cannot be offered", m.TriggerField)
		}
		if m.Escalates {
			escalating++
		}
	}
	if escalating == 0 {
		t.Fatal("no marker reports escalates=true, but the specialist tier is non-empty")
	}
}

func TestReferenceEnumsCarryLiveCounts(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/enums", nil)
	rec := httptest.NewRecorder()

	h.ReferenceEnums(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string][]struct {
		Value string `json:"value"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"diet_type", "meal_type", "budget_band", "season",
		"texture", "growth_target", "post_vaccine_context", "prep_time_min", "cook_time_min"} {
		if len(got[key]) == 0 {
			t.Fatalf("enum %q is empty; every one of these columns is populated on all 940 rows", key)
		}
	}
	if len(got["diet_type"]) != 3 {
		t.Fatalf("diet_type has 3 values in scope, got %d", len(got["diet_type"]))
	}
	if len(got["prep_time_min"]) != 4 {
		t.Fatalf("prep_time_min has 4 distinct corpus values, got %d", len(got["prep_time_min"]))
	}
	if len(got["cook_time_min"]) != 6 {
		t.Fatalf("cook_time_min has 6 distinct corpus values, got %d", len(got["cook_time_min"]))
	}
	var total int
	for _, v := range got["diet_type"] {
		total += v.Count
	}
	if total != 940 {
		t.Fatalf("diet_type counts sum to %d, want 940: counts must be live, not stored", total)
	}
}
```

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/api/handlers/... -run 'TestReferenceClinicalMarkers|TestReferenceEnums' -v`

Expected: FAIL to compile — both handlers undefined.

- [ ] **Step 3: Write `ReferenceClinicalMarkers`**

Append to `internal/api/handlers/reference.go`:

```go
// ReferenceClinicalMarkers returns the 28 distinct clinical_rule_master.trigger_field
// values with the rules behind each, so a client can offer a clinical-flags control whose
// keys are guaranteed to resolve. ChildProfile.ClinicalFlags is validated against exactly
// this vocabulary and an unrecognized key returns 400, so a free-text input is a trap.
//
// escalates says whether setting this marker will hold generation rather than filter it.
// An operator deserves to know that before typing, not after an empty result page.
func (h *Handlers) ReferenceClinicalMarkers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT trigger_field,
		       string_agg(DISTINCT rule_id, ', ' ORDER BY rule_id)          AS rule_ids,
		       string_agg(DISTINCT clinical_domain, ', ')                   AS domains,
		       string_agg(DISTINCT engine_action, ' | ')                    AS engine_actions,
		       string_agg(DISTINCT coalesce(specialist_required, ''), ' | ') AS specialist_required,
		       bool_or(human_approval_level = 'Specialist clinical approval') AS escalates
		FROM clinical_rule_master
		GROUP BY trigger_field
		ORDER BY trigger_field`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "clinical marker list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type marker struct {
		TriggerField       string `json:"trigger_field"`
		RuleIDs            string `json:"rule_ids"`
		Domains            string `json:"domains"`
		EngineActions      string `json:"engine_actions"`
		SpecialistRequired string `json:"specialist_required"`
		Escalates          bool   `json:"escalates"`
	}
	out := []marker{}
	for rows.Next() {
		var m marker
		if err := rows.Scan(&m.TriggerField, &m.RuleIDs, &m.Domains, &m.EngineActions,
			&m.SpecialistRequired, &m.Escalates); err != nil {
			writeError(w, http.StatusInternalServerError, "clinical marker scan failed: "+err.Error())
			return
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "clinical marker rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
```

`escalates` hardcodes the tier string rather than importing `engine.specialistApprovalLevel`,
because that constant is unexported and `internal/api/handlers` must not depend on engine
internals. The value is asserted in both packages' tests, so a change is caught.

- [ ] **Step 4: Write `ReferenceEnums`**

Append to `internal/api/handlers/reference.go`:

```go
// enumColumns are the recipe_master columns a client may offer as a filter or a ranker
// input. Each is returned with a live count so a control can show how much corpus sits
// behind each option, and so a zero-count value is visibly zero rather than absent.
//
// prep_time_min and cook_time_min are included as enums on purpose: the corpus holds four
// and six distinct values respectively, so a free minute entry would imply a precision the
// data has not got. A client renders them as stop selectors.
var enumColumns = []string{
	"diet_type", "meal_type", "budget_band", "season",
	"texture", "growth_target", "post_vaccine_context",
	"prep_time_min", "cook_time_min",
}

// ReferenceEnums returns every offerable recipe_master vocabulary with live counts.
//
// Unlike cuisine_option, which filters on COUNT(*) > 0 inside the view because a cuisine
// with no recipes is a broken option, a zero-count enum value is kept: season and texture
// are facts about the corpus an operator may want to see even at zero. The client decides
// whether to hide or disable.
func (h *Handlers) ReferenceEnums(w http.ResponseWriter, r *http.Request) {
	type value struct {
		Value string `json:"value"`
		Count int    `json:"count"`
	}
	out := map[string][]value{}

	for _, col := range enumColumns {
		// col is not user input: it comes from the package-level enumColumns slice, and
		// is sanitized anyway so a future edit cannot turn this into an injection.
		q := fmt.Sprintf(`
			SELECT %s::text AS value, count(*) AS n
			FROM recipe_master
			WHERE %s IS NOT NULL
			GROUP BY 1
			ORDER BY n DESC, 1`,
			pgx.Identifier{col}.Sanitize(), pgx.Identifier{col}.Sanitize())

		rows, err := h.pool.Query(r.Context(), q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "enum list failed for "+col+": "+err.Error())
			return
		}
		vals := []value{}
		for rows.Next() {
			var v value
			if err := rows.Scan(&v.Value, &v.Count); err != nil {
				rows.Close()
				writeError(w, http.StatusInternalServerError, "enum scan failed for "+col+": "+err.Error())
				return
			}
			vals = append(vals, v)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "enum rows failed for "+col+": "+err.Error())
			return
		}
		rows.Close()
		out[col] = vals
	}

	writeJSON(w, http.StatusOK, out)
}
```

Add `"fmt"` and `"github.com/jackc/pgx/v5"` to the file's imports.

- [ ] **Step 5: Register both routes**

In `internal/api/router.go`, after the allergens line from Task 2:

```go
	r.Get("/api/reference/clinical-markers", h.ReferenceClinicalMarkers)
	r.Get("/api/reference/enums", h.ReferenceEnums)
```

- [ ] **Step 6: Run tests, confirm pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/api/... -v`

Expected: PASS.

- [ ] **Step 7: `go vet ./...`, then commit**

```bash
git add internal/api/handlers/reference.go internal/api/router.go internal/api/handlers/reference_test.go
git commit -m "Add clinical-marker and enum reference endpoints with live counts"
```

---

### Task 6: The diet ranker

**Files:**
- Modify: `internal/engine/rank.go`
- Modify: `internal/engine/pipeline.go`
- Test: `internal/engine/rank_test.go`, `internal/engine/pipeline_test.go`

**Interfaces:**
- Consumes: `[]models.RankedRecipe` from `rankByTarget`.
- Produces: `applyDietRank(ctx, pool, p, recipes) ([]models.RankedRecipe, models.StepResult, error)`,
  matching the signature of every other ranker in the file.

- [ ] **Step 1: Write the failing tests**

Append to `internal/engine/rank_test.go`:

```go
func TestApplyDietRankConcentratesTheDeclaredPracticeAtTheTop(t *testing.T) {
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

	out, step, err := applyDietRank(ctx, pool,
		models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian"}, ranked)
	if err != nil {
		t.Fatalf("applyDietRank: %v", err)
	}
	if len(out) != len(ranked) {
		t.Fatalf("a ranker reorders, it never removes: in=%d out=%d", len(ranked), len(out))
	}
	if step.CandidatesIn != step.CandidatesOut {
		t.Fatalf("diet ranker must report equal in/out: %+v", step)
	}

	// The declared practice should be denser in the top quarter than in the pool overall.
	// Asserting density rather than a strict prefix keeps the test honest: the boost is a
	// nudge inside the nutrition ordering, not a re-sort that overrides it.
	quarter := len(out) / 4
	if quarter < 4 {
		t.Skip("candidate pool too small to measure density")
	}
	var topMatch, allMatch int
	for i, r := range out {
		if r.MealType == "" { // guard against a zero-value row sneaking in
			t.Fatalf("row %d has no meal_type; the ranker must not fabricate rows", i)
		}
		if i < quarter && r.DietType == "Non-vegetarian" {
			topMatch++
		}
		if r.DietType == "Non-vegetarian" {
			allMatch++
		}
	}
	if allMatch == 0 {
		t.Fatal("no non-vegetarian recipes in the 36-month pool; the fixture assumption is wrong")
	}
	topRate := float64(topMatch) / float64(quarter)
	poolRate := float64(allMatch) / float64(len(out))
	if topRate <= poolRate {
		t.Fatalf("declared practice not concentrated: top-quarter rate %.3f, pool rate %.3f", topRate, poolRate)
	}
}

func TestApplyDietRankLeavesAVegetarianProfileUntouched(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, _ := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	ranked, _, err := rankByTarget(ctx, pool, "NT00", ids)
	if err != nil {
		t.Fatalf("rankByTarget: %v", err)
	}
	// A vegetarian declaration permits only Vegetarian recipes, so step 4 has already
	// narrowed the pool to rows that all match. Boosting every row equally changes no
	// ordering, and the step must say so rather than claim work it did not do.
	vegOnly := make([]models.RankedRecipe, 0, len(ranked))
	for _, r := range ranked {
		if r.DietType == "Vegetarian" {
			vegOnly = append(vegOnly, r)
		}
	}
	out, step, err := applyDietRank(ctx, pool,
		models.ChildProfile{AgeMonths: 36, DietType: "Vegetarian"}, vegOnly)
	if err != nil {
		t.Fatalf("applyDietRank: %v", err)
	}
	for i := range out {
		if out[i].RecipeID != vegOnly[i].RecipeID {
			t.Fatalf("ordering changed at index %d when every candidate matches the declared practice", i)
		}
	}
	if step.Note == "" {
		t.Fatal("a step that changed no ordering must say why")
	}
}

func TestApplyDietRankIsANoOpWithNoDeclaredPractice(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, _ := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	ranked, _, _ := rankByTarget(ctx, pool, "NT00", ids)

	out, step, err := applyDietRank(ctx, pool, models.ChildProfile{AgeMonths: 36}, ranked)
	if err != nil {
		t.Fatalf("applyDietRank: %v", err)
	}
	if len(out) != len(ranked) || step.CandidatesIn != step.CandidatesOut {
		t.Fatalf("no declared practice must be a pure no-op: %+v", step)
	}
}
```

Update the step-count assertion in `internal/engine/pipeline_test.go`:

```go
			if len(result.Steps) != 14 {
				t.Fatalf("persona %q: expected 14 recorded steps (1-13, with step 4 recorded "+
					"twice as a hard filter and a preference ranker; step 8 has no data source "+
					"and step 14 is a human release gate, neither runs in the engine), got %d",
					c.name, len(result.Steps))
			}
```

- [ ] **Step 2: Run tests, confirm they fail**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/... -run 'TestApplyDietRank|TestRunPersona' -v`

Expected: FAIL — `undefined: applyDietRank`, and `r.DietType` undefined on
`models.RankedRecipe`.

- [ ] **Step 3: Add `DietType` to `models.RankedRecipe`**

In `internal/models/engine.go`, add to the struct after `MealType`:

```go
	DietType       string  `json:"diet_type"`
```

In `internal/engine/target.go`, `rankByTarget`'s query and scan must carry it. Add
`rm.diet_type` to the SELECT list immediately after `rm.meal_type`, and `&r.DietType`
to the `rows.Scan` call in the same position.

- [ ] **Step 4: Write `applyDietRank` in `internal/engine/rank.go`**

Insert after `applyCultureRank`:

```go
// applyDietRank is the ranking half of engine step 4. Step 4's hard filter decides what a
// family may eat; this decides what to show them first.
//
// recipe_master.diet_type states what a dish requires of whoever eats it, so the filter is
// a nested permission chain (vegan subset vegetarian subset eggetarian subset
// non-vegetarian -- see docs/decisions.md). A family declaring non-vegetarian is correctly
// permitted all 940 recipes, of which 828 are vegetarian, so without this step page one of
// their book is dal. Being permitted a dish is not the same as wanting it first.
//
// This is a nudge, not a re-sort: the boost sits between the budget boost (0.03) and the
// culture boost (0.05), so an explicit region choice still outranks a diet preference and
// neither outranks the nutrition score. Sized so it can reorder within a band of similar
// nutrition fitness and never across one.
func applyDietRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.DietType == "" || stepIn == 0 {
		return recipes, models.StepResult{
			Step: 4, Name: "Declared food practice - preference", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no diet type declared, step is a no-op",
		}, nil
	}

	const boost = 0.04

	matched := 0
	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		if out[i].DietType == p.DietType {
			out[i].RankedScore += boost
			matched++
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	note := fmt.Sprintf("%d of %d candidates match the declared practice %q exactly and were ranked up by %.2f",
		matched, stepIn, p.DietType, boost)
	if matched == stepIn {
		note = fmt.Sprintf("every candidate already matches the declared practice %q, so this step changed no ordering", p.DietType)
	}
	if matched == 0 {
		note = fmt.Sprintf("no candidate carries diet_type %q exactly; all of them are permitted by the nested diet chain but none is that practice's own dish", p.DietType)
	}

	return out, models.StepResult{
		Step: 4, Name: "Declared food practice - preference", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn, Note: note,
	}, nil
}
```

- [ ] **Step 5: Wire it into `internal/engine/pipeline.go`**

Immediately after the step-5 block (`steps = append(steps, step5)`) and before
`applyMealFilter`:

```go
	// Step 4's ranking half runs here rather than beside its hard filter, because it
	// adjusts a RankedScore that does not exist until step 5 has scored the pool. Both
	// halves are recorded as step 4 so the why-panel shows the filter and the preference
	// as one concept with two effects.
	ranked, step4rank, err := applyDietRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step4rank)
```

- [ ] **Step 6: Run the full suite, confirm pass**

Run: `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./... -v`

Expected: PASS everywhere, including the five personas at 14 steps.

- [ ] **Step 7: `go build ./... && go vet ./...`, then commit**

```bash
git add internal/models/engine.go internal/engine/rank.go internal/engine/target.go internal/engine/pipeline.go internal/engine/rank_test.go internal/engine/pipeline_test.go
git commit -m "Rank the declared diet practice above what the nesting merely permits"
```

---

### Task 7: Console — allergen multi-select and the six unexposed inputs

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/components/profile-form.tsx`
- Create: `web/src/components/unscreened-allergen-alert.tsx`
- Modify: `web/src/app/page.tsx`

**Interfaces:**
- Consumes: `/api/reference/allergens`, `/clinical-markers`, `/enums`, `/regions`,
  `/cuisines`, and `EngineResult.unscreened_allergens` from Task 1.
- Produces: a `ProfileForm` that sends all 13 `ChildProfile` fields and cannot offer a
  value the corpus does not have.

- [ ] **Step 1: Add the types**

In `web/src/lib/types.ts`, add `unscreened_allergens` to `EngineResult`, `diet_type` to
`RankedRecipe`, and the three new response types:

```ts
export interface RankedRecipe {
  recipe_id: string;
  recipe_name: string;
  region_culture: string;
  meal_type: string;
  diet_type: string;
  clinical_tag: string;
  age_group: string;
  nutrition_score: number;
  ranked_score: number;
  scored_axes: string;
  value_kind: "derived";
}

export interface EngineResult {
  recipes: RankedRecipe[];
  steps: StepResult[];
  active_target: string;
  target_reason: string;
  blocked: boolean;
  block_reason?: string;
  /** Declared allergen groups with no tag anywhere in the corpus. They screened nothing.
   *  Rendering a result set without surfacing these implies a screening that did not
   *  happen, which is the one thing this UI must never do. */
  unscreened_allergens?: string[];
}

export interface Allergen {
  allergen_group: string;
  corpus_tag: string | null;
  note: string;
  screens: boolean;
}

export interface ClinicalMarker {
  trigger_field: string;
  rule_ids: string;
  domains: string;
  engine_actions: string;
  specialist_required: string;
  escalates: boolean;
}

export interface EnumValue {
  value: string;
  count: number;
}

export type ReferenceEnums = Record<string, EnumValue[]>;
```

- [ ] **Step 2: Add the client functions**

In `web/src/lib/api.ts`, import the new types and append:

```ts
export function getAllergens(): Promise<Allergen[]> {
  return request<Allergen[]>("/api/reference/allergens");
}

export function getClinicalMarkers(): Promise<ClinicalMarker[]> {
  return request<ClinicalMarker[]>("/api/reference/clinical-markers");
}

export function getEnums(): Promise<ReferenceEnums> {
  return request<ReferenceEnums>("/api/reference/enums");
}
```

- [ ] **Step 3: Write the unscreened-allergen alert**

Create `web/src/components/unscreened-allergen-alert.tsx`:

```tsx
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

/**
 * Renders beside every result set when a declared allergen screened nothing.
 *
 * This is not a nicety. Four allergen groups (Tree nuts, Crustacean/Mollusc, Mustard,
 * Sulphites) have no tag anywhere in the corpus, so declaring one is accepted and removes
 * zero recipes. A results page that looks identical to a screened one implies a protection
 * that does not exist, which is the failure mode CLAUDE.md's hard rule exists to prevent.
 * See gap GAP-017.
 */
export function UnscreenedAllergenAlert({ groups }: { groups: string[] }) {
  if (groups.length === 0) return null;
  return (
    <Alert variant="destructive" className="mb-4">
      <AlertTitle className="font-mono text-xs uppercase">
        Not screened - no corpus coverage
      </AlertTitle>
      <AlertDescription className="space-y-1 text-sm">
        <p>
          <span className="font-mono">{groups.join(", ")}</span>{" "}
          {groups.length === 1 ? "has" : "have"} no matching tag on any recipe or
          ingredient. {groups.length === 1 ? "It" : "They"} excluded zero recipes because
          nothing carries the tag, not because the filter passed.
        </p>
        <p className="text-xs">
          Every recipe below is unscreened for{" "}
          {groups.length === 1 ? "this allergen" : "these allergens"}. Check ingredients
          directly before serving.
        </p>
      </AlertDescription>
    </Alert>
  );
}
```

- [ ] **Step 4: Rewrite `profile-form.tsx`**

Replace `web/src/components/profile-form.tsx` entirely. The form now loads its
vocabularies rather than hardcoding them, so it cannot offer a value the corpus lacks:

```tsx
"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { getAllergens, getClinicalMarkers, getEnums, getRegions, getCuisines } from "@/lib/api";
import type {
  Allergen, ChildProfile, ClinicalMarker, Cuisine, Region, ReferenceEnums,
} from "@/lib/types";

interface ProfileFormProps {
  onSubmit: (profile: ChildProfile) => void;
  loading: boolean;
}

// The operator-facing marker keys the engine's selectTarget understands. These are engine
// concepts (which nutrition target to activate), not clinical_rule_master trigger fields,
// which is why they are a separate control from clinical flags below.
const CLINICAL_MARKERS = [
  { value: "growth_faltering", label: "Growth faltering" },
  { value: "thinness", label: "Thinness (BMI-for-age)" },
  { value: "overweight_under5", label: "Overweight risk under 5" },
  { value: "overweight_5to19", label: "Overweight / obesity 5-19" },
  { value: "iron_deficiency", label: "Iron-deficiency risk" },
  { value: "calcium_bone", label: "Calcium / bone health" },
  { value: "high_protein", label: "High-protein emphasis" },
  { value: "vegetarian", label: "Vegetarian adequacy" },
  { value: "vegan", label: "Vegan adequacy" },
  { value: "picky_eating", label: "Picky eating / low variety" },
  { value: "illness_recovery", label: "Illness / recovery" },
];

const NONE = "__none__"; // Radix Select forbids an empty-string item value

export function ProfileForm({ onSubmit, loading }: ProfileFormProps) {
  const [ageMonths, setAgeMonths] = useState<number | "">("");
  const [dietType, setDietType] = useState("");
  const [vegan, setVegan] = useState(false);
  const [allergens, setAllergens] = useState<string[]>([]);
  const [clinicalMarker, setClinicalMarker] = useState("");
  const [clinicalFlags, setClinicalFlags] = useState<Record<string, string>>({});
  const [mealType, setMealType] = useState("");
  const [budgetBand, setBudgetBand] = useState("");
  const [regionCulture, setRegionCulture] = useState("");
  const [cuisineCode, setCuisineCode] = useState("");
  const [maxPrep, setMaxPrep] = useState("");
  const [maxCook, setMaxCook] = useState("");
  const [limit, setLimit] = useState("");

  const [allergenOptions, setAllergenOptions] = useState<Allergen[]>([]);
  const [markerOptions, setMarkerOptions] = useState<ClinicalMarker[]>([]);
  const [enums, setEnums] = useState<ReferenceEnums>({});
  const [regions, setRegions] = useState<Region[]>([]);
  const [cuisines, setCuisines] = useState<Cuisine[]>([]);
  const [refError, setRefError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([getAllergens(), getClinicalMarkers(), getEnums(), getRegions(), getCuisines()])
      .then(([a, m, e, r, c]) => {
        setAllergenOptions(a);
        setMarkerOptions(m);
        setEnums(e);
        setRegions(r);
        setCuisines(c);
      })
      .catch((err) => setRefError(err instanceof Error ? err.message : String(err)));
  }, []);

  function toggleAllergen(group: string) {
    setAllergens((prev) =>
      prev.includes(group) ? prev.filter((g) => g !== group) : [...prev, group]);
  }

  function toggleFlag(field: string) {
    setClinicalFlags((prev) => {
      const next = { ...prev };
      if (field in next) delete next[field];
      else next[field] = "Yes";
      return next;
    });
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (ageMonths === "") return;
    onSubmit({
      age_months: Number(ageMonths),
      diet_type: dietType ? (dietType as ChildProfile["diet_type"]) : undefined,
      vegan: vegan || undefined,
      allergens: allergens.length ? allergens : undefined,
      clinical_marker: clinicalMarker || undefined,
      clinical_flags: Object.keys(clinicalFlags).length ? clinicalFlags : undefined,
      meal_type: mealType || undefined,
      budget_band: budgetBand ? (budgetBand as ChildProfile["budget_band"]) : undefined,
      region_culture: regionCulture || undefined,
      cuisine_code: cuisineCode || undefined,
      max_prep_time_min: maxPrep ? Number(maxPrep) : undefined,
      max_cook_time_min: maxCook ? Number(maxCook) : undefined,
      limit: limit ? Number(limit) : undefined,
    });
  }

  const enumValues = (key: string) => enums[key] ?? [];
  const label = "text-xs uppercase text-muted-foreground";

  return (
    <form onSubmit={handleSubmit} className="space-y-4 font-mono text-sm">
      {refError && (
        <p className="text-xs text-destructive">
          Reference vocabularies failed to load ({refError}). Controls below are empty
          rather than guessed; confirm the API server is running.
        </p>
      )}

      <div className="space-y-1">
        <label htmlFor="age" className={label}>Age (months) *</label>
        <Input
          id="age" type="number" min={0} max={216} required
          value={ageMonths}
          onChange={(e) => setAgeMonths(e.target.value === "" ? "" : Number(e.target.value))}
        />
      </div>

      <div className="space-y-1">
        <span className={label}>Diet type</span>
        <Select value={dietType || NONE} onValueChange={(v) => setDietType(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference</SelectItem>
            {enumValues("diet_type").map((v) => (
              <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <label className="flex items-start gap-2 text-xs">
        <input type="checkbox" checked={vegan} onChange={(e) => setVegan(e.target.checked)} className="mt-0.5" />
        <span>Vegan - additional to diet type; excludes dairy, fish and animal-protein food groups</span>
      </label>

      <fieldset className="space-y-1">
        <legend className={label}>Declared allergens</legend>
        <div className="flex flex-wrap gap-1">
          {allergenOptions.map((a) => {
            const on = allergens.includes(a.allergen_group);
            return (
              <button
                key={a.allergen_group}
                type="button"
                onClick={() => toggleAllergen(a.allergen_group)}
                title={a.note}
                className="focus-visible:ring-ring rounded focus-visible:outline-none focus-visible:ring-2"
              >
                <Badge
                  variant={on ? "default" : "outline"}
                  className={a.screens ? "" : "border-dashed opacity-70"}
                >
                  {a.allergen_group}
                  {!a.screens && " - not screened"}
                </Badge>
              </button>
            );
          })}
        </div>
        <p className="text-xs text-muted-foreground">
          Dashed groups have no tag anywhere in the corpus. They stay selectable so the
          allergy can be recorded, and every result is labelled unscreened for them.
        </p>
      </fieldset>

      <div className="space-y-1">
        <span className={label}>Nutrition target marker</span>
        <Select value={clinicalMarker || NONE} onValueChange={(v) => setClinicalMarker(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="None - age-default ranking" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>None - age-default ranking</SelectItem>
            {CLINICAL_MARKERS.map((m) => (
              <SelectItem key={m.value} value={m.value}>{m.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <fieldset className="space-y-1">
        <legend className={label}>Clinical flags</legend>
        <div className="flex flex-wrap gap-1">
          {markerOptions.map((m) => {
            const on = m.trigger_field in clinicalFlags;
            return (
              <button
                key={m.trigger_field}
                type="button"
                onClick={() => toggleFlag(m.trigger_field)}
                title={`${m.rule_ids} - ${m.engine_actions}`}
                className="focus-visible:ring-ring rounded focus-visible:outline-none focus-visible:ring-2"
              >
                <Badge variant={on ? "default" : "outline"} className={m.escalates ? "border-destructive" : ""}>
                  {m.trigger_field}
                  {m.escalates && " - holds"}
                </Badge>
              </button>
            );
          })}
        </div>
        <p className="text-xs text-muted-foreground">
          Flags outlined in red hold generation for specialist review rather than filtering
          it. No recipe list is returned for those.
        </p>
      </fieldset>

      <div className="space-y-1">
        <span className={label}>Region</span>
        <Select value={regionCulture || NONE} onValueChange={(v) => setRegionCulture(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference - West Bengal first" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference - West Bengal first</SelectItem>
            {regions.map((r) => (
              <SelectItem key={r.region_culture} value={r.region_culture}>
                {r.region_culture} (tier {r.focus_tier})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1">
        <span className={label}>Cuisine cluster</span>
        <Select value={cuisineCode || NONE} onValueChange={(v) => setCuisineCode(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference</SelectItem>
            {cuisines.map((c) => (
              <SelectItem key={c.culture_code} value={c.culture_code}>
                {c.cuisine_cluster} - {c.region_culture} ({c.recipe_count})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1">
        <span className={label}>Meal type</span>
        <Select value={mealType || NONE} onValueChange={(v) => setMealType(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference</SelectItem>
            {enumValues("meal_type").map((v) => (
              <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1">
        <span className={label}>Budget band</span>
        <Select value={budgetBand || NONE} onValueChange={(v) => setBudgetBand(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference</SelectItem>
            {enumValues("budget_band").map((v) => (
              <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <div className="space-y-1">
          <span className={label}>Max prep (min)</span>
          <Select value={maxPrep || NONE} onValueChange={(v) => setMaxPrep(v === NONE ? "" : v)}>
            <SelectTrigger><SelectValue placeholder="Any" /></SelectTrigger>
            <SelectContent>
              <SelectItem value={NONE}>Any</SelectItem>
              {enumValues("prep_time_min").map((v) => (
                <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1">
          <span className={label}>Max cook (min)</span>
          <Select value={maxCook || NONE} onValueChange={(v) => setMaxCook(v === NONE ? "" : v)}>
            <SelectTrigger><SelectValue placeholder="Any" /></SelectTrigger>
            <SelectContent>
              <SelectItem value={NONE}>Any</SelectItem>
              {enumValues("cook_time_min").map((v) => (
                <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="space-y-1">
        <label htmlFor="limit" className={label}>Result limit</label>
        <Input
          id="limit" type="number" min={1} max={200} placeholder="25 (meal_category_target)"
          value={limit} onChange={(e) => setLimit(e.target.value)}
        />
      </div>

      <Button type="submit" disabled={ageMonths === "" || loading} className="w-full">
        {loading ? "Running engine..." : "Search"}
      </Button>
    </form>
  );
}
```

The time controls read their stops from `/api/reference/enums` rather than the hardcoded
`5/10/15/20` and `10/15/20/25/30/35`, so a re-import that changes the corpus changes the
control without a code edit.

- [ ] **Step 5: Render the alert on the results page**

In `web/src/app/page.tsx`, import the component and render it above every non-loading
result branch, including the blocked one:

```tsx
import { UnscreenedAllergenAlert } from "@/components/unscreened-allergen-alert";

// ...inside <section>, immediately after the {error && ...} block:
{!loading && result?.unscreened_allergens && (
  <UnscreenedAllergenAlert groups={result.unscreened_allergens} />
)}
```

It renders above the blocked alert, the empty-state alert and the results table alike,
because an unscreened allergen is true in all three cases.

- [ ] **Step 6: Typecheck and verify in the browser**

```bash
cd web && pnpm exec tsc --noEmit && pnpm dev
```

With `go run ./cmd/server` running against the imported database:

1. Age 36, allergen `Tree nuts` — confirm the destructive "not screened" alert renders
   above the table and the row count is unchanged from declaring nothing.
2. Age 36, allergen `Peanut` — confirm no alert and a smaller row count.
3. Set clinical flag `Diabetes_Type` — confirm the badge is outlined in red before
   clicking, and that submitting returns the blocked alert naming the specialist.
4. Set region `South India` — confirm South Indian recipes move to the top and step 7 in
   the why-panel reports the explicit region.
5. Set cuisine `Tamil` — confirm a non-empty list; a hard filter here would return zero.
6. Set max prep 5 — confirm the row count drops and step 11 reports it.
7. Diet type `Non-vegetarian` — confirm 940 candidates at step 4's filter and the new
   step-4 preference ranker reporting how many matched.

- [ ] **Step 7: Commit**

```bash
cd /home/ghoul/graveyard/recipie
git add web/src/lib/types.ts web/src/lib/api.ts web/src/components/profile-form.tsx web/src/components/unscreened-allergen-alert.tsx web/src/app/page.tsx
git commit -m "Send all thirteen profile fields and surface unscreened allergens"
```

---

### Task 8: Full-stack verification and documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/engine-inputs.md`

**Interfaces:** none. Verification only.

- [ ] **Step 1: Run the whole suite from a clean database**

```bash
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import
go run ./cmd/enrich
go build ./...
go vet ./...
TEST_DATABASE_URL=$DATABASE_URL go test ./... -v
```

Expected: every package PASS. `TestEveryOfferedAllergenScreensSomething` SKIP naming four
groups. `TestGapRegisterCountMatchesTheDocumentedCount` PASS at 20.

- [ ] **Step 2: Confirm the importer is still idempotent**

```bash
go run ./cmd/import
psql $DATABASE_URL -c "SELECT a.table_name FROM import_table_stat a JOIN import_table_stat b USING (table_name) WHERE a.run_id = (SELECT max(run_id) FROM import_table_stat) AND b.run_id = (SELECT max(run_id) - 1 FROM import_table_stat) AND a.content_hash <> b.content_hash"
```

Expected: zero rows.

- [ ] **Step 3: Update `README.md`'s endpoint table**

Add three rows to the "Running the API" table:

```markdown
| `/api/reference/allergens` | GET | The 11 allergen groups, their corpus tag, and whether declaring each screens anything |
| `/api/reference/clinical-markers` | GET | The 28 clinical trigger fields, their rules, and whether each holds generation |
| `/api/reference/enums` | GET | Every offerable recipe_master vocabulary with live counts |
```

- [ ] **Step 4: Update `docs/engine-inputs.md`**

Change the "Exposed in UI" column to `yes` for `region_culture`, `cuisine_code`,
`clinical_flags`, `max_prep_time_min`, `max_cook_time_min` and `limit`.

In the allergens table, replace the "Filters anything" column footnote with a pointer to
`EngineResult.unscreened_allergens` and `GAP-017`, and note that `Wheat` resolves through
`allergen_tag_vocabulary` to `Gluten-containing cereal`.

Add a row to the field table for the step-4 preference ranker, and change the step count
in any prose that says the engine records 13 steps to 14.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/engine-inputs.md
git commit -m "Document the three new reference endpoints and the wired inputs"
```

---

## Self-review notes

Checked against the spec, sections 1-5 plus the documentation correction:

- Spec §1 (allergen field, keep selectable, reference endpoint, failing-today test,
  `GAP-017`): Tasks 1, 2, 3, 7.
- Spec §2 (specialist tier union, keep the map, verbatim `specialist_required`,
  disagreement test, `GAP-020`): Task 4.
- Spec §3 (six inputs, stop selectors not free entry): Tasks 5, 7.
- Spec §4 (three endpoints, live counts, zero-count enums kept): Tasks 2, 5.
- Spec §5 (diet ranker, magnitude beside the existing adjustments, new recorded step):
  Task 6.
- Spec "documentation correction" (`GAP-018`, `GAP-019`, count of 20, `not-built.md` §1.1):
  Task 3.

Type consistency: `allergyFilter` returns four values from Task 1 onward and Task 1 fixes
its only other caller. `models.RankedRecipe.DietType` is added in Task 6 step 3 before
Task 6 step 4 reads it, and mirrored into `web/src/lib/types.ts` in Task 7 step 1.
`specialistApprovalLevel` is defined in Task 4 step 3 and used by the Task 4 tests written
in step 1, which is why step 2 expects a compile failure first. The literal
`'Specialist clinical approval'` appears in three places — the Go constant, the `GAP-020`
measure query and the `clinical-markers` handler — and each is asserted by a test that
would fail if one changed alone.
