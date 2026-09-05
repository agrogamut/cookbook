# SP4: Fact-Check Pass Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every drafted and inferred value in an assembled book is checked against the corpus row it claims to come from, before the book is returned. A failing check drops the unit and records a finding. It never withholds the book.

**Architecture:** A new `internal/factcheck` package with one entry point, run in `AssembleSet` after both books are assembled. Four independent checkers, each a pure function over an already-assembled book plus a pool, each returning findings. No network calls - every check is a local query. Findings surface to the operator and never print in either book.

**Tech Stack:** Go 1.x, pgx/v5, Next.js.

**Spec:** `docs/superpowers/specs/2026-09-05-direct-generation-design.md`

**Depends on:** SP3 complete and merged. Inferred fields are one of the four things being checked.

## Global Constraints

- **A finding drops a unit, never the book.** One recipe card, one note, one inferred field. This is the same omit-rather-than-half-build convention every `Drafter` caller already follows, and it is deliberately not a new gate: SP1 exists to remove gates and re-introducing one under a different name would undo it.
- **Findings never print.** They are operator-facing only, beside the existing omissions panels.
- **No network calls.** Every check is a query against the pool the assembler already holds. A fact-check that costs a Gemini round trip is a second thing to be wrong.
- Drafting disabled must produce zero findings and a book byte-identical to one generated before this sub-project. That is the regression guard for the whole package.
- Never invent a data value, including in a finding's own text. A finding names the value, the table it was checked against, and the verdict. It does not suggest a correction.
- No emojis. No em dashes or en dashes in prose. No AI attribution anywhere.
- `go build ./...`, `go vet ./...`, `go test ./...` with `TEST_DATABASE_URL`; `npx tsc --noEmit`, `npm test`, `npm run build`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/factcheck/factcheck.go` | `Finding`, `Verdict`, and `Run(ctx, pool, in Input) []Finding` dispatching the four checkers. |
| `internal/factcheck/inferred.go` | Inferred profile fields against the live vocabularies, re-read independently of SP3. |
| `internal/factcheck/invented.go` | Invented-recipe ingredients against `ingredient_master`, plus the age band and allergen set a real recipe passes. |
| `internal/factcheck/notes.go` | Drafted modification notes against the `clinical_rule_master` text they paraphrase. |
| `internal/factcheck/nutrition.go` | Printed nutrition figures against `recipe_nutrition_recomputed`. |
| `internal/book/set.go` | Calls `factcheck.Run`, applies the drops, returns the findings. |
| `internal/api/handlers/`, `web/src/` | `factcheck_findings` in JSON, one more operator panel. |

---

### Task 1: The package skeleton and the finding type

**Files:**
- Create: `internal/factcheck/factcheck.go`
- Test: `internal/factcheck/factcheck_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: everything below depends on these types.

```go
// Verdict is what a check concluded about one value.
type Verdict string

const (
	// Verified means the value was found in the source it claims to come from.
	Verified Verdict = "verified"
	// Unsupported means the source was checked and does not carry this value. The unit is
	// dropped.
	Unsupported Verdict = "unsupported"
	// Uncheckable means no source exists to check this value against. The unit is kept:
	// an absent source is not evidence the value is wrong, and dropping on it would remove
	// most of a book the provider genuinely supplied.
	Uncheckable Verdict = "uncheckable"
)

// Finding is one checked value.
type Finding struct {
	Unit    string  // what would be dropped, e.g. "recipe card MG-R-00042"
	Field   string  // the value checked, e.g. "ingredient_id ING9999"
	Source  string  // the table checked against, e.g. "ingredient_master"
	Verdict Verdict
	Detail  string // what was found, never a suggested correction
}
```

- [ ] **Step 1: Write the failing test**

```go
func TestOnlyUnsupportedDropsAUnit(t *testing.T) {
	// Uncheckable is the load-bearing distinction. Most of this corpus has no independent
	// source to check against - 267 of 406 ingredients have no IFCT counterpart, and
	// GAP-026 records 27 blocks the workbook ships no text for. Treating "I could not
	// check this" as "this is wrong" would empty most of both books.
	for _, tc := range []struct {
		verdict Verdict
		drops   bool
	}{
		{Verified, false},
		{Unsupported, true},
		{Uncheckable, false},
	} {
		if got := Finding{Verdict: tc.verdict}.Drops(); got != tc.drops {
			t.Errorf("%s: Drops() = %v, want %v", tc.verdict, got, tc.drops)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/factcheck/ -v`
Expected: FAIL, no package.

- [ ] **Step 3: Implement**

Write the types above plus `func (f Finding) Drops() bool { return f.Verdict == Unsupported }` and a `Run(ctx, pool, in Input) []Finding` that, for now, returns nil. `Input` carries the assembled `Book1`, `Book2` and the SP3 inference notes - define it against those real types, importing `internal/book`. If that import cycles (because `internal/book` will call `factcheck`), invert it: `factcheck` defines narrow interfaces describing only what it reads, and `internal/book` satisfies them. Check for the cycle before writing either version.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/factcheck/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/factcheck/
git commit -m "factcheck: finding type, with uncheckable kept apart from unsupported"
```

---

### Task 2: Check invented-recipe ingredients

**Files:**
- Create: `internal/factcheck/invented.go`
- Test: `internal/factcheck/invented_test.go`

**Interfaces:**
- Consumes: Task 1's `Finding`.
- Produces: `checkInvented(ctx, pool, in Input) []Finding`.

This is the highest-value check in the package: `DraftInventedRecipe`'s own doc comment says it "performs no safety validation" and that `internal/book` must re-check every ingredient id, the allergen set and the required texture. That re-check exists in `internal/book/invented.go` today; this makes it a reported finding rather than a silent drop, and adds the age band.

- [ ] **Step 1: Write the failing test**

```go
func TestAnInventedRecipeWithAnUnknownIngredientIsUnsupported(t *testing.T) {
	pool := testPool(t)
	in := inputWithInventedCard(t, "MG-INV-0001", []string{"ING9999"})

	findings := checkInvented(context.Background(), pool, in)
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %d", len(findings))
	}
	if findings[0].Verdict != Unsupported {
		t.Fatalf("want Unsupported, got %s", findings[0].Verdict)
	}
	if !findings[0].Drops() {
		t.Error("an invented recipe naming an ingredient that does not exist must be dropped")
	}
}

func TestAnInventedRecipeCarryingADeclaredAllergenIsUnsupported(t *testing.T) {
	// The allergen filter is the one thing SP1 explicitly did not relax. An invented
	// recipe is the only path by which a peanut ingredient could reach a peanut-allergic
	// child's book, because it never went through step 2 - it was written after the
	// filter ran.
	pool := testPool(t)
	in := inputWithInventedCardForAllergicChild(t, "Peanut")

	findings := checkInvented(context.Background(), pool, in)
	if len(findings) == 0 || !findings[0].Drops() {
		t.Fatal("an invented recipe containing a declared allergen must be dropped")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/factcheck/ -run TestAnInvented -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Implement**

For each card flagged invented (`internal/book` already carries the internal source flag the 2026-08-24 amendment mandates - find it, do not add a second one):
- Every `ingredient_id` must exist in `ingredient_master`. Missing: `Unsupported`.
- No ingredient may carry a corpus tag matching a confirmed allergen, checked with the same `allergen_tag_vocabulary` plus `ingredient_allergen_override` join `allergyFilter` uses. Do not re-write that query from memory - read `internal/engine/steps_hard.go` and mirror it. A match is `Unsupported`.
- The recipe's declared min age must not exceed the child's age. Exceeded: `Unsupported`.

- [ ] **Step 4: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/factcheck/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/factcheck/invented.go internal/factcheck/invented_test.go
git commit -m "factcheck: invented recipes are re-checked against the ingredient master and the allergen set"
```

---

### Task 3: Check inferred fields and drafted notes

**Files:**
- Create: `internal/factcheck/inferred.go`, `internal/factcheck/notes.go`
- Test: `internal/factcheck/inferred_test.go`, `internal/factcheck/notes_test.go`

**Interfaces:**
- Consumes: Task 1's `Finding`, SP3's inference notes.
- Produces: `checkInferred(...)`, `checkNotes(...)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestAnInferredRegionOutsideTheCorpusIsUnsupported(t *testing.T) {
	// SP3 already validates this. Checking it again here, from an independently re-read
	// vocabulary, is the point: a check that trusts the same in-memory list the producer
	// used is not a check.
	pool := testPool(t)
	in := inputWithInferred(t, "region_culture", "Atlantis")

	findings := checkInferred(context.Background(), pool, in)
	if len(findings) != 1 || findings[0].Verdict != Unsupported {
		t.Fatalf("want one Unsupported finding, got %+v", findings)
	}
}

func TestAModificationNoteForAConditionWithNoRuleIsUnsupported(t *testing.T) {
	pool := testPool(t)
	in := inputWithModificationNote(t, "Condition_That_Does_Not_Exist", "eat smaller portions")

	findings := checkNotes(context.Background(), pool, in)
	if len(findings) != 1 || findings[0].Verdict != Unsupported {
		t.Fatalf("want one Unsupported finding, got %+v", findings)
	}
}

func TestAGenericNoteForAZeroBackingConditionIsUncheckable(t *testing.T) {
	// Gas/bloating has no provider row at all. The 2026-08-24 amendment authorises
	// generic, non-specific safe text for exactly that case, so its absence from
	// clinical_rule_master is expected and must not read as a defect.
	pool := testPool(t)
	in := inputWithModificationNote(t, "gas_bloating", "offer smaller portions")

	findings := checkNotes(context.Background(), pool, in)
	if len(findings) != 1 || findings[0].Verdict != Uncheckable {
		t.Fatalf("want one Uncheckable finding, got %+v", findings)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/factcheck/ -run 'TestAnInferred|TestAModification|TestAGeneric' -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Implement `checkInferred`**

Re-read `region_focus`, `cuisine_option`, the distinct `budget_band`, and the distinct time values from the database. Independently - do not accept a vocabulary map from SP3. Each inferred value in the corpus vocabulary is `Verified`; outside it, `Unsupported`.

- [ ] **Step 4: Implement `checkNotes`**

For each drafted modification note, look up the condition it was drafted for in `clinical_rule_master`. A matching row with non-empty `book2_action` or `Required_Modification`: `Verified`. A matching row with both empty, or no matching row for a condition on the known zero-backing list: `Uncheckable`. A note naming a condition that is neither in the master nor on that list: `Unsupported`.

The zero-backing list is the one from the 2026-08-24 amendment: gas/bloating, and "wanting to eat more". Write it as a named constant with a comment pointing at the amendment. Do not derive it - it is a policy decision, not data.

- [ ] **Step 5: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/factcheck/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/factcheck/inferred.go internal/factcheck/notes.go internal/factcheck/inferred_test.go internal/factcheck/notes_test.go
git commit -m "factcheck: inferred fields and drafted notes checked against independently re-read sources"
```

---

### Task 4: Check printed nutrition figures

**Files:**
- Create: `internal/factcheck/nutrition.go`
- Test: `internal/factcheck/nutrition_test.go`

**Interfaces:**
- Consumes: Task 1's `Finding`.
- Produces: `checkNutrition(ctx, pool, in Input) []Finding`.

- [ ] **Step 1: Write the failing test**

```go
func TestAPrintedCoverageThatDisagreesWithTheViewIsUnsupported(t *testing.T) {
	// The IFCT coverage percentage is the one derived confidence figure the 2026-08-25
	// amendment kept on the page after removing every other caveat. If it drifts from
	// recipe_nutrition_recomputed, the page is claiming a verification level that row
	// does not support - which is the exact failure the hard rule is about.
	pool := testPool(t)
	in := inputWithRecipeCard(t, "MG-R-00042", withCoverage(0.99))

	findings := checkNutrition(context.Background(), pool, in)
	if len(findings) != 1 || findings[0].Verdict != Unsupported {
		t.Fatalf("want one Unsupported finding, got %+v", findings)
	}
}
```

Pick a real recipe id whose actual coverage is known not to be 0.99, by querying for one in the test rather than hardcoding.

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/factcheck/ -run TestAPrintedCoverage -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Implement**

For every recipe card carrying nutrition, compare the printed energy, protein, iron, calcium and `ingredient_coverage` against `recipe_nutrition_recomputed` for that `recipe_id`. Equal within float tolerance: `Verified`. Different: `Unsupported`. No row in the view: `Uncheckable` - some recipes genuinely have none, and that is a gap already counted, not a defect this check discovered.

A nutrition mismatch drops the **card**, not the chapter. State that in the `Unit` field.

- [ ] **Step 4: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/factcheck/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/factcheck/nutrition.go internal/factcheck/nutrition_test.go
git commit -m "factcheck: printed nutrition figures checked against the recomputed view"
```

---

### Task 5: Run the pass and apply the drops

**Files:**
- Modify: `internal/factcheck/factcheck.go` (`Run` dispatches all four)
- Modify: `internal/book/set.go`
- Test: `internal/book/set_test.go`

**Interfaces:**
- Consumes: Tasks 2, 3, 4.
- Produces: `AssembleSet` returns `FactCheckFindings []factcheck.Finding` and books with dropped units removed.

- [ ] **Step 1: Write the failing test**

```go
func TestDraftingDisabledProducesNoFindings(t *testing.T) {
	// The regression guard for the whole package. Nothing is drafted or inferred, so
	// nothing is checkable, so nothing may be reported - and the books must be exactly
	// what they were before this sub-project existed.
	pool := testPool(t)
	set, err := AssembleSet(context.Background(), pool, storedProfileForTest(t), someTime)
	if err != nil {
		t.Fatalf("AssembleSet: %v", err)
	}
	if len(set.FactCheckFindings) != 0 {
		t.Fatalf("want zero findings with drafting disabled, got %+v", set.FactCheckFindings)
	}
}

func TestADroppedCardDoesNotWithholdTheBook(t *testing.T) {
	pool := testPool(t)
	drafter := drafterThatInventsABadRecipe(t)
	set, err := AssembleSet(context.Background(), pool, storedProfileForTest(t), someTime,
		WithDrafter(drafter))
	if err != nil {
		t.Fatalf("a dropped card must not fail the run: %v", err)
	}
	if len(set.Book2.Chapters) == 0 {
		t.Fatal("the book was withheld rather than the card dropped")
	}
	if len(set.FactCheckFindings) == 0 {
		t.Fatal("the drop was silent")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -run 'TestDraftingDisabledProducesNoFindings|TestADroppedCard' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`factcheck.Run` calls the four checkers and concatenates. In `AssembleSet`, after both assemblers return and before the result is built: run the pass, remove every unit whose finding `Drops()`, and attach the findings. Removing a card may take a chapter below its recipe-count target - that is correct and must not trigger `topUpInvented` a second time, which would invent a replacement for a card the check just rejected. Guard against that explicitly.

- [ ] **Step 4: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ ./internal/factcheck/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/factcheck/factcheck.go internal/book/set.go internal/book/set_test.go
git commit -m "book: run the fact-check pass and drop failing units without withholding the book"
```

---

### Task 6: Surface findings to the operator

**Files:**
- Modify: `internal/api/handlers/book_set.go`, `internal/api/handlers/book_generate.go`
- Modify: `web/src/lib/types.ts`, `web/src/lib/api.ts`, `web/src/components/book-generator.tsx`
- Test: `internal/api/handlers/book_generate_test.go`, `web/src/components/book-generator.test.tsx`

**Interfaces:**
- Consumes: Task 5's findings.
- Produces: `factcheck_findings` in the JSON; one more operator panel on `/books`.

- [ ] **Step 1: Write the failing tests**

Go: the generate response carries `factcheck_findings`, each with unit, field, source, verdict and detail.

Frontend: the panel renders one row per finding, groups by verdict, and does not render when the array is empty.

- [ ] **Step 2: Run to verify failure**

Run both suites. Expected: FAIL.

- [ ] **Step 3: Implement**

Serialize beside the existing omissions and (from SP3) inferred fields. On the frontend, add the type and a fourth panel following the same markup as the other three. Verdict is the sort key: `Unsupported` first, since that is the one that changed the book. Monospace for the unit and field identifiers, per the frontend rules.

Nothing about a finding reaches either book's HTML. Assert it.

- [ ] **Step 4: Run the full suite**

```bash
go build ./... && go vet ./...
TEST_DATABASE_URL=$DATABASE_URL go test ./...
cd web && npx tsc --noEmit && npm test && npm run build
```

- [ ] **Step 5: Generate with a real key and read the result**

Set `GEMINI_API_KEY`. Generate against the worst-case profile the 2026-08-26 amendment describes - two allergen exclusions plus an active clinical condition, narrow enough to trip the invented-recipe fallback in more than one chapter. Confirm: the run completes; any dropped card is named in the panel with its source; the printed books contain no dropped card and no mention of the pass; a chapter short a card is short rather than topped up with a replacement.

Then print both books with `BOOK_PAGE_DUMP` and read them, because a chapter losing a card changes where every later break falls, and `pagefit_test.go`'s budget is the thing that notices.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers/ web/src/
git commit -m "console: show fact-check findings beside the omissions"
```
