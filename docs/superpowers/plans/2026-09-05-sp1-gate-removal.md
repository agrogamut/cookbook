# SP1: Gate Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** No declared condition, clinical flag or age band ever stops a book being generated; age becomes an ordering signal instead of a filter, and the printed scope caveats come off the family-facing page.

**Architecture:** The two blocking sites in `internal/engine` (`specialCareGate`, `clinicalFilter`) keep their provider-data lookups and stop returning a block. `ageFilter` returns the whole corpus and a new `applyAgeRank` stably partitions the ranked list into in-band and out-of-band halves. Everything downstream that existed only to carry a block - `ErrBlocked`, `BlockedDetail`, `writeBlocked`, the 409 mapping, `BookBlockedError`, the blocked Alert - is deleted.

**Tech Stack:** Go 1.x, pgx/v5, chi/v5, Next.js App Router + React + Tailwind, vitest.

**Spec:** `docs/superpowers/specs/2026-09-05-direct-generation-design.md`

## Global Constraints

- Never invent a data value. A gap is `null`, "not available", or a shorter list.
- Confirmed allergens (`allergyFilter`, step 2) and declared diet (`dietFilter`, step 4) stay hard filters. No task here touches either.
- `book1_content_block.ai_can_draft = 'N'` stays closed on all five blocks. The CHECK constraint and `TestAICanDraftGateIsPinned` are untouched.
- Provider per-row `Review_Status` / `Data_Quality` stay verbatim in the data model and the JSON API. This plan removes printed *caveats*, never a provider data flag.
- No emojis. No em dashes or en dashes in prose. No attribution to any AI tool in code, comments, or commit messages.
- `go build ./...`, `go vet ./...`, `go test ./...` green with a real `TEST_DATABASE_URL` before this sub-project is called done. Plus `npx tsc --noEmit`, `npm test`, `npm run build` in `web/`.
- Task 8 requires printing both books with `BOOK_PAGE_DUMP` and reading the sheets by eye. A count is not a substitute.

## File Structure

| File | Responsibility after this plan |
|---|---|
| `internal/engine/special_care.go` | Looks up the provider's condition row, records it as a step note. No block. `SpecialCareBlock` gone. |
| `internal/engine/clinical.go` | Validates flag keys, records fired rules as a step note. No escalation, no unclassified-rule error. |
| `internal/engine/steps_hard.go` | `ageFilter` returns the whole corpus with a step record. `allergyFilter` unchanged. |
| `internal/engine/rank.go` | Gains `applyAgeRank`, a stable in-band/out-of-band partition. |
| `internal/engine/pipeline.go` | No early return. Calls `applyAgeRank` after `rankByTarget`. |
| `internal/models/engine.go` | `EngineResult` loses `Blocked`/`BlockReason`. `RankedRecipe` gains `AgeInBand`. |
| `internal/book/book1.go`, `book2.go`, `set.go` | No gate consultation, no `ErrBlocked`. |
| `internal/book/blocked.go`, `blocked_test.go` | Deleted. |
| `internal/api/handlers/books.go`, `book_set.go` | No 409 path, no `writeBlocked`. |
| `internal/book/templates/book1/{illness,daily,stage,refs}.html` | No printed scope caveats. |
| `web/src/lib/api.ts`, `web/src/components/{book-generator,child-input-form}.tsx` | No blocked error class, variant, Alert or warning text. |

---

### Task 1: Special-care gate stops blocking

**Files:**
- Modify: `internal/engine/special_care.go`
- Modify: `internal/engine/pipeline.go:43-56`
- Test: `internal/engine/special_care_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `specialCareGate(ctx, pool, p) (models.StepResult, error)` - two returns, was four. `SpecialCareBlock` no longer exists.

- [ ] **Step 1: Write the failing test**

Replace `TestSpecialCareBlocksAProfileThatWouldOtherwiseSucceed` in `internal/engine/special_care_test.go` with:

```go
func TestSpecialCareConditionNoLongerStopsGeneration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	p := models.ChildProfile{AgeMonths: 36, SpecialCareCondition: "SC-CP"}
	res, err := Run(ctx, pool, p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Recipes) == 0 {
		t.Fatal("a declared special-care condition must not empty the result list")
	}

	var note string
	for _, s := range res.Steps {
		if s.Name == "Special-care condition" {
			note = s.Note
		}
	}
	if note == "" {
		t.Fatal("the special-care step must still record the provider's own text")
	}
	if !strings.Contains(note, "SC-CP") {
		t.Fatalf("step note must name the condition, got %q", note)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/engine/ -run TestSpecialCareConditionNoLongerStopsGeneration -v`
Expected: FAIL - `Run` currently returns an empty `Recipes` slice with `Blocked=true`.

- [ ] **Step 3: Rewrite the gate**

Delete `SpecialCareBlock` entirely from `internal/engine/special_care.go`. Replace `specialCareGate` with:

```go
// specialCareGate records the provider's special-care row for a declared condition. It no
// longer stops generation.
//
// The workbook's OR-001 reads "STOP_SPECIAL_CARE_GENERATION", and this used to honour it
// literally: six conditions returned zero recipes and named a mandatory reviewer. That was
// sized against a non-clinical operator who could not evaluate a recipe put in front of
// them, so blocking was the conservative direction. The input is now a verified doctor, and
// a 409 telling a doctor to route to a reviewer hands them nothing while telling the
// reviewer to find themselves. See docs/superpowers/specs/2026-09-05-direct-generation-design.md.
//
// The provider's own automatic_action, mandatory_reviewer and stop_if still travel, verbatim
// and unparaphrased, in the step note. Paraphrasing clinical instruction is how a summary
// becomes advice, and that has not changed just because the text no longer halts anything.
func specialCareGate(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (models.StepResult, error) {
	if p.SpecialCareCondition == "" {
		return models.StepResult{
			Step: 3, Name: "Special-care condition", Kind: "ranker",
			CandidatesIn: -1, CandidatesOut: -1,
			Note: "no special-care condition declared",
		}, nil
	}

	var condition, gateLevel, reviewer, automaticAction, stopIf string
	err := pool.QueryRow(ctx, `
		SELECT condition, coalesce(gate_level, ''), coalesce(mandatory_reviewer, ''),
		       coalesce(automatic_action, ''), coalesce(stop_if, '')
		FROM special_care_condition_gate
		WHERE condition_id = $1`, p.SpecialCareCondition).
		Scan(&condition, &gateLevel, &reviewer, &automaticAction, &stopIf)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.StepResult{}, fmt.Errorf(
			"engine: unknown special-care condition %q: %w", p.SpecialCareCondition, ErrInvalidProfile)
	}
	if err != nil {
		return models.StepResult{}, fmt.Errorf("engine: special-care gate lookup: %w", err)
	}

	return models.StepResult{
		Step: 3, Name: "Special-care condition", Kind: "ranker",
		CandidatesIn: -1, CandidatesOut: -1,
		Note: fmt.Sprintf(
			"%s (%s), gate level %s in the provider's Special-Care master. "+
				"Provider's stated action: %s Named reviewer: %s. Provider's stop condition: %s",
			condition, p.SpecialCareCondition, gateLevel, automaticAction, reviewer, stopIf),
	}, nil
}
```

Note the unknown-condition error is kept: an unrecognised `condition_id` is still a 400. That is input validation, not a gate.

The `gateLevel != "STOP-REVIEW"` refusal is deleted - nothing branches on the level any more, so refusing an unexpected one would reject a valid row for no effect.

- [ ] **Step 4: Update the pipeline call site**

In `internal/engine/pipeline.go`, replace lines 38-56 with:

```go
	// The special-care row is recorded, not enforced. See specialCareGate.
	scStep, err := specialCareGate(ctx, pool, p)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, scStep)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/engine/ -v`
Expected: PASS. `internal/book` and `internal/api` will not compile yet - that is Tasks 4 and 5.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/special_care.go internal/engine/special_care_test.go internal/engine/pipeline.go
git commit -m "engine: special-care condition is recorded, not a stop gate"
```

---

### Task 2: Clinical filter stops blocking

**Files:**
- Modify: `internal/engine/clinical.go`
- Modify: `internal/engine/pipeline.go:58-73`
- Test: `internal/engine/clinical_test.go`

**Interfaces:**
- Consumes: Task 1's pipeline shape.
- Produces: `clinicalFilter(ctx, pool, p, candidateIDs) ([]string, models.StepResult, error)` - three returns, was five.

- [ ] **Step 1: Write the failing test**

Add to `internal/engine/clinical_test.go`:

```go
func TestAnEscalatingClinicalFlagNoLongerStopsGeneration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// CKD fires CR-REN-001/002, both of which used to return an escalation block.
	p := models.ChildProfile{AgeMonths: 60, ClinicalFlags: map[string]string{"CKD": "Yes"}}
	res, err := Run(ctx, pool, p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Recipes) == 0 {
		t.Fatal("a clinical flag must not empty the result list")
	}

	var note string
	for _, s := range res.Steps {
		if s.Step == 3 && s.Name == "Clinical rules" {
			note = s.Note
		}
	}
	if !strings.Contains(note, "CR-REN") {
		t.Fatalf("the step note must name the rules that fired, got %q", note)
	}
}

func TestAnUnrecognizedClinicalFlagKeyIsStillRejected(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// A typo must not fail open into a full list. This is input validation, not a gate.
	p := models.ChildProfile{AgeMonths: 60, ClinicalFlags: map[string]string{"CDK": "Yes"}}
	if _, err := Run(ctx, pool, p); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("want ErrInvalidProfile, got %v", err)
	}
}
```

Delete any existing test asserting an escalation block or the unclassified-rule error.

- [ ] **Step 2: Run tests to verify they fail**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/engine/ -run 'TestAnEscalatingClinicalFlag|TestAnUnrecognized' -v`
Expected: the first FAILs (empty list today); the second PASSes already and must keep passing.

- [ ] **Step 3: Rewrite the filter**

In `internal/engine/clinical.go`: delete `specialistApprovalLevel` (line 22), delete `escalationOnlyDomains` (lines 61-72), and keep `clinicalRule`, `triggerFires`.

Replace the escalation and unclassified-rule logic (everything from `var firstEscalating` through the end of the block-returning branch) with a note-building loop:

```go
	// Every rule that fires is recorded by id and domain. None of them filters.
	//
	// This is not a loss of function. escalationOnlyDomains, deleted with this change, existed
	// precisely because these rules have no compilable recipe-side predicate - no renal-safe,
	// gluten-free, dysphagia-texture or FODMAP tag exists on any table - so the block was
	// standing in for a filter that could never be written. The clinical signal still reaches
	// the output twice over: SelectTarget picks the nutrition target from the child's
	// condition, and ActiveClinicalRuleActions feeds the per-recipe modification notes.
	var fired []string
	for i := range rules {
		r := rules[i]
		flagValue, set := p.ClinicalFlags[r.triggerField]
		if !set || !triggerFires(r.triggerOperator, r.triggerValue, flagValue) {
			continue
		}
		fired = append(fired, fmt.Sprintf("%s (%s)", r.ruleID, r.clinicalDomain))
	}

	note := "clinical flags set, no rule fired"
	if len(fired) > 0 {
		note = "rules fired, recorded not filtered: " + strings.Join(fired, ", ")
	}
	return candidateIDs, models.StepResult{
		Step: 3, Name: "Clinical rules", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn, Note: note,
	}, nil
```

Change the function signature to `([]string, models.StepResult, error)` and drop the `false, ""` from the two early returns (the no-flags no-op and the validation error path).

- [ ] **Step 4: Update the pipeline call site**

In `internal/engine/pipeline.go`, replace lines 58-73 with:

```go
	ids, step3, err := clinicalFilter(ctx, pool, p, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step3)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/engine/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/clinical.go internal/engine/clinical_test.go internal/engine/pipeline.go
git commit -m "engine: clinical rules are recorded, not an escalation block"
```

---

### Task 3: Age becomes a stable partition

**Files:**
- Modify: `internal/engine/steps_hard.go:16-44`
- Modify: `internal/engine/rank.go` (add `applyAgeRank`)
- Modify: `internal/engine/pipeline.go`
- Modify: `internal/models/engine.go:25-37`
- Test: `internal/engine/steps_hard_test.go`, `internal/engine/rank_test.go`

**Interfaces:**
- Consumes: Tasks 1 and 2's pipeline shape.
- Produces: `models.RankedRecipe.AgeInBand bool` (JSON `age_in_band`); `applyAgeRank(ctx, pool, p, recipes) ([]models.RankedRecipe, models.StepResult, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/engine/rank_test.go`:

```go
func TestAgeAppropriateRecipesSortAboveEveryOutOfBandOne(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	p := models.ChildProfile{AgeMonths: 7}
	res, err := Run(ctx, pool, p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Recipes) == 0 {
		t.Fatal("empty result list")
	}

	seenOutOfBand := false
	for i, r := range res.Recipes {
		if !r.AgeInBand {
			seenOutOfBand = true
			continue
		}
		if seenOutOfBand {
			t.Fatalf("recipe %d (%s) is in-band but sits below an out-of-band recipe",
				i, r.RecipeID)
		}
	}
}

func TestAgeNeverRemovesARecipe(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_master`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}

	// Limit 0 means "use the category default", so ask for everything explicitly.
	p := models.ChildProfile{AgeMonths: 7, Limit: total}
	res, err := Run(ctx, pool, p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Recipes) != total {
		t.Fatalf("age must not remove a recipe: got %d of %d", len(res.Recipes), total)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/engine/ -run TestAge -v`
Expected: FAIL - `AgeInBand` does not compile yet.

- [ ] **Step 3: Add the model field**

In `internal/models/engine.go`, add to `RankedRecipe` after `AgeGroup`:

```go
	// AgeInBand reports whether this recipe's [min_age_months, max_age_months] contains
	// the child's age. Age is an ordering signal, not a filter: every in-band recipe sorts
	// above every out-of-band one, and an out-of-band recipe is reachable only once the
	// in-band pool is exhausted. See applyAgeRank.
	AgeInBand bool `json:"age_in_band"`
```

- [ ] **Step 4: Widen the age step**

Replace `ageFilter` in `internal/engine/steps_hard.go` with:

```go
// ageStep is engine step 1. It returns the whole corpus and removes nothing.
//
// It was a hard filter, and the filter was the right shape for a non-clinical operator: a
// recipe outside a child's age band is not for that child, and nobody downstream could be
// relied on to notice. The input is now a verified doctor, and an empty page is worse than
// a short one that starts with the closest fits. Age still decides the ordering - see
// applyAgeRank, which partitions rather than removes - so an out-of-band recipe surfaces
// only when the in-band pool has run out.
func ageStep(ctx context.Context, pool *pgxpool.Pool) ([]string, models.StepResult, error) {
	rows, err := pool.Query(ctx, `SELECT recipe_id FROM recipe_master`)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age step: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: age step scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age step rows: %w", err)
	}

	return ids, models.StepResult{
		Step: 1, Name: "Age / feeding stage", Kind: "ranker",
		CandidatesIn: len(ids), CandidatesOut: len(ids),
		Note: "age orders the list rather than filtering it; see step 1's ranker half",
	}, nil
}
```

- [ ] **Step 5: Add the partition ranker**

Add to `internal/engine/rank.go`:

```go
// applyAgeRank is step 1's ranker half. It stably partitions the ranked list: every recipe
// whose age band contains the child's age first, in the score order rankByTarget produced,
// then everything else, in the same order.
//
// A partition rather than a score adjustment, unlike every other ranker in this file.
// recipe_target_score normalises within an age band, so a teenage recipe scoring 0.9 among
// teenage recipes and a six-month puree scoring 0.7 among purees are not comparable numbers.
// No constant in this file's family (culture 0.05, availability 0.05, budget 0.03, suspected
// allergen -0.15, against a live ranked_score spread of about 0.65) can guarantee the puree
// wins, and a constant large enough to guarantee it (>= 1.0) is a hard filter wearing a
// ranker's clothes. The partition states the actual intent and keeps scores undistorted.
func applyAgeRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if stepIn == 0 {
		return recipes, models.StepResult{
			Step: 1, Name: "Age / feeding stage, ranker", Kind: "ranker",
			CandidatesIn: 0, CandidatesOut: 0, Note: "empty pool, step is a no-op",
		}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}

	rows, err := pool.Query(ctx, `
		SELECT recipe_id FROM recipe_master
		WHERE recipe_id = ANY($1) AND min_age_months <= $2 AND max_age_months >= $2`,
		ids, p.AgeMonths)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age rank: %w", err)
	}
	defer rows.Close()

	inBand := make(map[string]bool, len(recipes))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: age rank scan: %w", err)
		}
		inBand[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age rank rows: %w", err)
	}

	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		out[i].AgeInBand = inBand[out[i].RecipeID]
	}
	// Only the partition is compared. SliceStable preserves the incoming score order inside
	// each half, so nothing needs to re-sort by score.
	sort.SliceStable(out, func(i, j int) bool { return out[i].AgeInBand && !out[j].AgeInBand })

	return out, models.StepResult{
		Step: 1, Name: "Age / feeding stage, ranker", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: fmt.Sprintf("%d of %d recipes are in this child's age band and sort first; none removed",
			len(inBand), stepIn),
	}, nil
}
```

- [ ] **Step 6: Wire it into the pipeline**

In `internal/engine/pipeline.go`, replace the `ageFilter` call and its `totalInBand` query (lines 21-30) with:

```go
	ids, step1, err := ageStep(ctx, pool)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step1)
```

Then, immediately after the `rankByTarget` call and its `steps = append(steps, step5)`, insert:

```go
	// Age's ranker half runs first among the post-scoring rankers: it is the coarsest
	// relevance signal, and every later ranker adjusts scores within the halves it sets.
	ranked, step1rank, err := applyAgeRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step1rank)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/engine/ -v`
Expected: PASS. Update `internal/engine/steps_hard_test.go` - any test asserting `ageFilter` removes rows now asserts `ageStep` returns the full corpus.

- [ ] **Step 8: Commit**

```bash
git add internal/engine/steps_hard.go internal/engine/rank.go internal/engine/pipeline.go internal/models/engine.go internal/engine/steps_hard_test.go internal/engine/rank_test.go
git commit -m "engine: age orders the candidate list instead of filtering it"
```

---

### Task 4: Remove the block from the result model and the book package

**Files:**
- Modify: `internal/models/engine.go:41-56`
- Modify: `internal/book/book1.go:160-196`, `internal/book/book2.go:21-30,128-137`, `internal/book/set.go:41`
- Delete: `internal/book/blocked.go`, `internal/book/blocked_test.go`
- Test: `internal/book/assemble_test.go:370-382`, `internal/book/set_test.go:120-130`

**Interfaces:**
- Consumes: `models.EngineResult` without `Blocked`, from Tasks 1-3.
- Produces: `book.ErrBlocked` and `book.BlockedDetail` no longer exist. `AssembleBook1` / `AssembleBook2` / `AssembleSet` keep their signatures and simply never return a block error.

- [ ] **Step 1: Write the failing test**

Replace the two `ErrBlocked` assertions in `internal/book/assemble_test.go` (around line 373) with:

```go
func TestASpecialCareChildStillGetsBothBooks(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := storedProfileWithCondition(t, "SC-DS")
	asOf := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

	b1, _, err := AssembleBook1(ctx, pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	if len(b1.Sections) == 0 {
		t.Fatal("Book 1 is empty for a special-care child")
	}

	b2, _, err := AssembleBook2(ctx, pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleBook2: %v", err)
	}
	if len(b2.Chapters) == 0 {
		t.Fatal("Book 2 has no chapters for a special-care child")
	}
}
```

Use whatever helper `assemble_test.go` already provides for building a `profile.Stored` with a condition; if none exists, build one inline the way the neighbouring tests do. Delete `internal/book/set_test.go`'s stop-gate test entirely.

- [ ] **Step 2: Run it to verify it fails**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -run TestASpecialCareChildStillGetsBothBooks -v`
Expected: FAIL with `book: engine blocked generation`.

- [ ] **Step 3: Strip the model fields**

In `internal/models/engine.go`, delete `Blocked bool` and `BlockReason string` from `EngineResult`. Leave `UnscreenedAllergens` and its comment exactly as they are - that field is a screening-coverage fact, not a gate.

- [ ] **Step 4: Strip the book package**

- `internal/book/book1.go`: delete the `engine.SpecialCareBlock` call and its `if blocked` branch (lines 190-196). Rewrite the `AssembleBook1` doc comment paragraph that describes the gate, replacing it with one sentence saying Book 1 consults no gate and calls `engine.SelectTarget` only.
- `internal/book/book2.go`: delete the `ErrBlocked` declaration (line 30) and its comment; delete the `if res.Blocked` branch (lines 135-137). The preliminary `engine.Run` above it existed only for the block check, so delete that too and let the per-category loop do its own runs. Confirm by reading the loop that `res` is not referenced afterwards; if `clinicalActions` or anything else needs it, keep the run and delete only the branch.
- `internal/book/set.go`: delete the `ErrBlocked` sentence from the doc comment on line 41.
- Delete `internal/book/blocked.go` and `internal/book/blocked_test.go`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -v`
Expected: PASS. `internal/api` will not compile yet - that is Task 5.

- [ ] **Step 6: Commit**

```bash
git add internal/models/engine.go internal/book/
git commit -m "book: a declared condition no longer withholds either book"
```

---

### Task 5: Remove the 409 path from the API

**Files:**
- Modify: `internal/api/handlers/books.go:51-110`
- Modify: `internal/api/handlers/book_set.go:37-60`
- Test: `internal/api/handlers/books_test.go`, `internal/api/handlers/book_generate_test.go`

**Interfaces:**
- Consumes: `book.ErrBlocked` gone, from Task 4.
- Produces: no endpoint returns 409. 503 (`ErrChromiumUnavailable`) and 500 (`ErrPrintFailed`) are unchanged and must stay distinct.

- [ ] **Step 1: Write the failing test**

Add to `internal/api/handlers/books_test.go`:

```go
func TestGenerateReturns200ForASpecialCareChild(t *testing.T) {
	h := testHandlers(t)
	body := `{"date_of_birth":"2022-01-01","conditions":[
		{"trigger_field":"Special_Care_Condition","flag_value":"SC-DS","class":"chronic"}]}`

	req := httptest.NewRequest(http.MethodPost, "/api/books/generate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.BookGenerate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

Match `testHandlers` to whatever the existing tests in this file use.

- [ ] **Step 2: Run it to verify it fails**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/api/handlers/ -run TestGenerateReturns200ForASpecialCareChild -v`
Expected: FAIL with 409 (or a compile error, since Task 4 removed `ErrBlocked`).

- [ ] **Step 3: Strip the handlers**

- `internal/api/handlers/books.go`: delete `writeBlocked` (lines 103-110) and both `errors.Is(err, book.ErrBlocked)` branches (lines 66-68, 78-80). Update the doc comment on the handler that enumerates the status codes so it no longer mentions 409.
- `internal/api/handlers/book_set.go`: delete the `errors.Is` branch (lines 56-58) and the `ErrBlocked` sentence from the doc comment.
- Delete any now-unused `errors` import.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && TEST_DATABASE_URL=$DATABASE_URL go test ./... -v`
Expected: the whole Go suite green. Delete any remaining test asserting a 409.

- [ ] **Step 5: Commit**

```bash
git add internal/api/
git commit -m "api: drop the 409 stop-gate response, no request is refused for a declared condition"
```

---

### Task 6: Remove the blocked path from the frontend

**Files:**
- Modify: `web/src/lib/api.ts:150-155,178`
- Modify: `web/src/components/book-generator.tsx:14-18,68-79,257-269`
- Modify: `web/src/components/child-input-form.tsx:431-436`
- Test: `web/src/components/book-generator.test.tsx`

**Interfaces:**
- Consumes: no 409 from the API, from Task 5.
- Produces: `BookBlockedError` no longer exported. `Problem` is `Unavailable | PrintFailed | Failed`.

- [ ] **Step 1: Write the failing test**

Add to `web/src/components/book-generator.test.tsx`:

```typescript
it("shows no stop-gate warning when a special-care condition is selected", async () => {
  render(<BookGenerator />);
  // The dropdown's old helper text told the operator generation would halt. Nothing
  // halts now, so nothing should say it does.
  expect(screen.queryByText(/generation will halt/i)).toBeNull();
  expect(screen.queryByText(/no book is produced/i)).toBeNull();
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/components/book-generator.test.tsx`
Expected: this specific assertion passes only once the text is gone; if the dropdown text is not rendered until a condition is picked, extend the test to pick one first via the existing select-interaction helper in this file.

- [ ] **Step 3: Strip the frontend**

- `web/src/lib/api.ts`: delete the `BookBlockedError` class and the `if (res.status === 409)` line. Leave `RendererUnavailableError` (503) and `PrintFailedError` (500) untouched - those are operational faults and still need to read differently from each other.
- `web/src/components/book-generator.tsx`: delete the `Blocked` type, remove it from the `Problem` union, delete the `BookBlockedError` branch in `classify`, delete the `problem?.kind === "blocked"` Alert block, and remove `BookBlockedError` from the import.
- `web/src/components/child-input-form.tsx`: delete the `{specialCare && (<p>…</p>)}` helper paragraph under the special-care select (lines 431-436). The select itself stays: the condition is still real input that drives target selection and drafting.

- [ ] **Step 4: Run the suite**

Run: `cd web && npx tsc --noEmit && npm test && npm run build`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "console: drop the blocked-generation state, nothing stops a run now"
```

---

### Task 7: Remove the printed scope caveats

**Files:**
- Modify: `internal/book/templates/book1/illness.html:22`
- Modify: `internal/book/templates/book1/daily.html:48`
- Modify: `internal/book/templates/book1/stage.html:179`
- Modify: `internal/book/templates/book1/refs.html:15-27`
- Modify: `internal/book/colwidth.go` (references table widths)
- Test: `internal/book/render_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks; independent of the engine work.
- Produces: `IllnessBlock.EngineLimit`, `DailyLifeModule.AILimit` and the evidence row's `Limitation` remain populated on the structs and in the JSON. Only the printed output changes.

- [ ] **Step 1: Write the failing test**

Add to `internal/book/render_test.go`:

```go
func TestScopeCaveatsDoNotPrint(t *testing.T) {
	// The struct fields stay populated for an operator reading the JSON. The family-facing
	// page is where they stop appearing. Same line the 2026-08-25 amendment drew for the
	// per-recipe Draft label: a repeated status caveat is this project's call to print or not.
	html := renderBook1ForTest(t)
	for _, phrase := range []string{
		"Scope of this page:",
		"This is the last stage the provider's feeding master defines.",
		"Stated limitation",
	} {
		if strings.Contains(html, phrase) {
			t.Errorf("%q still prints", phrase)
		}
	}
}
```

Use whatever full-render helper `render_test.go` already has; if it renders per-template, render the four templates in question and check each.

- [ ] **Step 2: Run it to verify it fails**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -run TestScopeCaveatsDoNotPrint -v`
Expected: FAIL on all three phrases.

- [ ] **Step 3: Edit the templates**

- `illness.html`: delete line 22, `{{ with .EngineLimit }}<p class="domain-limit">{{ . }}</p>{{ end }}`, and rewrite the file-head comment above it that explains why it printed per situation.
- `daily.html`: delete line 48, `{{ with .AILimit }}<p class="domain-limit">Scope of this page: {{ . }}</p>{{ end }}`, and its file-head comment paragraph.
- `stage.html`: delete line 179 and the surrounding `<p class="domain-limit">` element.
- `refs.html`: delete the `<th style="width:23%">Stated limitation</th>` and the matching `<td>{{ if .Limitation }}…{{ end }}</td>`, and rewrite the file-head comment, which currently argues at length for why the column exists.

- [ ] **Step 4: Resize the references table**

`refs.html`'s remaining four columns were sized against a five-column layout with an explicit 23% on the limitation column. Recompute in `colwidth.go` for four columns, following the existing pattern in that file, and delete the now-dangling 23%.

- [ ] **Step 5: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -v`
Expected: PASS. `.domain-limit` may now be unused in `tokens.css` - leave the class definition alone, it costs nothing and Task 8 may want it back if a print reads badly.

- [ ] **Step 6: Commit**

```bash
git add internal/book/templates/ internal/book/colwidth.go internal/book/render_test.go
git commit -m "book1: scope caveats stay on the struct and come off the printed page"
```

---

### Task 8: Print both books and read them

**Files:**
- Modify: `internal/book/pagefit_test.go` (budget only, if it moved)

**Interfaces:**
- Consumes: every earlier task.
- Produces: a verified printed document. No new code.

This task exists because `rendered + reported == total` held perfectly while Book 1 printed nearly empty, and every layout defect this project has found was found by printing a book and reading it.

- [ ] **Step 1: Print both books**

```bash
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
BOOK_PAGE_DUMP=/tmp/bookdump TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -run TestPDF -v
```

- [ ] **Step 2: Read every sheet**

Open both PDFs. Confirm, by eye:
- No scope caveat on the illness page, the daily-life pages, the last feeding stage, or the references table.
- The references table's four columns are sized sensibly and no identifier is broken across a line (`IAP-STG-CONSTIPATIO/N` is the failure this project has seen before).
- Removing a paragraph has not orphaned a heading at the foot of a sheet.
- Bengali ingredient names still shape correctly. Unaffected by this change, but it is the whole reason this renderer exists, so it gets a glance.

- [ ] **Step 3: Check the page-fit budget**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -run TestPageFit -v`
Expected: PASS. The underfilled-page budget was 8 of 60. Removing text can only reduce ink, so a regression here is real and must be fixed by adjusting where the break falls, not by raising the budget. If the count improved, lower the budget to the new number - the guard is documented as ratcheting down.

- [ ] **Step 4: Full verification**

```bash
go build ./... && go vet ./...
TEST_DATABASE_URL=$DATABASE_URL go test ./...
cd web && npx tsc --noEmit && npm test && npm run build
```

- [ ] **Step 5: Commit**

```bash
git add internal/book/pagefit_test.go
git commit -m "book: re-measure the page-fit budget after the caveat removal"
```

If the budget did not move, there is nothing to commit and that is a valid outcome - say so rather than inventing a change.
