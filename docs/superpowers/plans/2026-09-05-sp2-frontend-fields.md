# SP2: Frontend Field Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every input the backend already accepts is reachable from the console form, and engine step 8 stops being a no-op.

**Architecture:** Mostly a frontend sub-project. `handlers.profileDTO` already carries `vegan`, `religious_restriction`, `max_prep_time_min` and `max_cook_time_min`; `generateRequest.Conditions` already accepts any `trigger_field`; `GET /api/reference/clinical-markers` already lists all 28 of them. The form is the only thing that does not know. Two backend items ride along: stripping the dead `escalates` column from that endpoint after SP1 deletes the concept, and implementing the preference ranker.

**Tech Stack:** Go 1.x, pgx/v5, chi/v5, Next.js App Router + React + Tailwind + shadcn/ui, vitest.

**Spec:** `docs/superpowers/specs/2026-09-05-direct-generation-design.md`

**Depends on:** SP1 complete and merged. A clinical flag set before SP1 lands earns a 409.

## Global Constraints

- Never invent a data value. A blank field is sent as absent, never as a default. `num()` in `child-input-form.tsx` already models this correctly - follow it.
- Confirmed allergens and declared diet stay hard filters. Untouched here.
- Every option list is built from a database-backed endpoint, never a hardcoded array. This is the existing discipline in `child-input-form.tsx` and the reason is that provider vocabularies drift.
- No emojis. No em dashes or en dashes in prose. No attribution to any AI tool anywhere.
- Density first, per the frontend rules in `CLAUDE.md`: `Table` over `Card`, compact rows, no decorative anything. Operators run twenty lookups an hour.
- `go build ./...`, `go vet ./...`, `go test ./...` with a real `TEST_DATABASE_URL`; `npx tsc --noEmit`, `npm test`, `npm run build` in `web/`.

## File Structure

| File | Responsibility after this plan |
|---|---|
| `internal/api/handlers/reference.go` | `ReferenceClinicalMarkers` no longer computes or returns `escalates`. |
| `internal/engine/rank.go` | Gains `applyPreferenceRank`, engine step 8. |
| `internal/engine/pipeline.go` | Step 8 calls it instead of recording a no-op. |
| `internal/models/profile.go` | `ChildProfile` gains `LikedIngredients` / `DislikedIngredients`. |
| `internal/profile/profile.go` | `ToChildProfile` maps `Stored.Preferences` into them. |
| `web/src/lib/types.ts` | `ClinicalMarker` interface; `StoredProfile` gains the missing fields. |
| `web/src/lib/api.ts` | `getClinicalMarkers()`; `GenerateInput` gains the missing fields. |
| `web/src/components/child-input-form.tsx` | Clinical section, practice additions, preferences section. |

---

### Task 1: Drop the dead escalation column from the clinical-markers endpoint

**Files:**
- Modify: `internal/api/handlers/reference.go:219-280` (the `ReferenceClinicalMarkers` query and its output struct)
- Test: `internal/api/handlers/reference_test.go`

**Interfaces:**
- Consumes: SP1's deletion of `engine.escalationOnlyDomains` and `specialistApprovalLevel`.
- Produces: `/api/reference/clinical-markers` response rows with no `escalates` field. `loadable` stays.

- [ ] **Step 1: Write the failing test**

Add to `internal/api/handlers/reference_test.go`:

```go
func TestClinicalMarkersNoLongerReportEscalation(t *testing.T) {
	h := testHandlers(t)
	req := httptest.NewRequest(http.MethodGet, "/api/reference/clinical-markers", nil)
	rec := httptest.NewRecorder()
	h.ReferenceClinicalMarkers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	// Nothing escalates any more (SP1). A field naming a behaviour that no longer exists
	// is worse than an absent one: a client would render a badge for a state unreachable
	// in the system.
	if strings.Contains(rec.Body.String(), "escalates") {
		t.Error("the response still carries an escalates field")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/api/handlers/ -run TestClinicalMarkersNoLongerReportEscalation -v`
Expected: FAIL.

- [ ] **Step 3: Strip the query and the struct**

In `ReferenceClinicalMarkers`:
- Delete the `in_escalation_domain` CASE in the `loaded` CTE, including the ten-domain list and the comment explaining that it mirrors `engine.escalationOnlyDomains`. That mirror has nothing left to mirror.
- Delete the `human_approval_level` selection if nothing else uses it after the above, and the whole `scored` CTE if `escalates` was its only output - fold `loaded` straight into `per_value`.
- Delete `bool_or(escalates) AS escalates` from `per_value`, the `Escalates` field from the output struct, and its `Scan` target.
- `loadable` stays. It still means something real: whether the rule is one `clinicalFilter` loads and records.

- [ ] **Step 4: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/api/handlers/ -v`
Expected: PASS. Fix any existing test asserting on `escalates`.

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers/reference.go internal/api/handlers/reference_test.go
git commit -m "api: clinical markers stop reporting an escalation that no longer exists"
```

---

### Task 2: Implement engine step 8, the preference ranker

**Files:**
- Modify: `internal/models/profile.go`
- Modify: `internal/profile/profile.go` (`ToChildProfile`)
- Modify: `internal/engine/rank.go` (add `applyPreferenceRank`)
- Modify: `internal/engine/pipeline.go:122-131` (replace the no-op)
- Test: `internal/engine/rank_test.go`, `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `models.ChildProfile.LikedIngredients []string` and `.DislikedIngredients []string`, both `ingredient_master.ingredient_id` values; `applyPreferenceRank(ctx, pool, p, recipes) ([]models.RankedRecipe, models.StepResult, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/engine/rank_test.go`:

```go
func TestALikedIngredientRanksItsRecipesUp(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Pick a real ingredient that appears in at least one recipe, rather than hardcoding
	// an id: 304 of 406 ingredients appear in zero recipes (GAP-007), so a hardcoded pick
	// is a coin flip on whether this test measures anything.
	var ing string
	err := pool.QueryRow(ctx, `
		SELECT ingredient_id FROM recipe_ingredient_mapping
		GROUP BY ingredient_id ORDER BY count(*) DESC LIMIT 1`).Scan(&ing)
	if err != nil {
		t.Fatalf("pick ingredient: %v", err)
	}

	base := models.ChildProfile{AgeMonths: 36, Limit: 50}
	before, err := Run(ctx, pool, base)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	liked := base
	liked.LikedIngredients = []string{ing}
	after, err := Run(ctx, pool, liked)
	if err != nil {
		t.Fatalf("Run liked: %v", err)
	}

	if len(before.Recipes) != len(after.Recipes) {
		t.Fatalf("a preference is a ranker and must not change the count: %d then %d",
			len(before.Recipes), len(after.Recipes))
	}
	if before.Recipes[0].RecipeID == after.Recipes[0].RecipeID {
		t.Skip("the top recipe already contains the liked ingredient; ordering unchanged is correct here")
	}
}

func TestStep8IsNoLongerRecordedAsANoOp(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	res, err := Run(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, s := range res.Steps {
		if s.Step == 8 && strings.Contains(s.Note, "cannot run") {
			t.Fatal("step 8 still records itself as unable to run")
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/engine/ -run 'TestALikedIngredient|TestStep8' -v`
Expected: FAIL to compile (`LikedIngredients` undefined).

- [ ] **Step 3: Add the model fields**

In `internal/models/profile.go`, after `SuspectedAllergens`:

```go
	// LikedIngredients and DislikedIngredients are ingredient_master.ingredient_id values
	// the family has named. Ranker only, engine step 8, and never a filter in either
	// direction: a dislike that excluded recipes would narrow a child's diet on a
	// preference, which is the same over-restriction failure SuspectedAllergens'
	// rank-don't-filter rule exists to avoid.
	LikedIngredients    []string `json:"liked_ingredients,omitempty"`
	DislikedIngredients []string `json:"disliked_ingredients,omitempty"`
```

- [ ] **Step 4: Map them in the profile conversion**

In `internal/profile/profile.go`'s `ToChildProfile`, after the allergen loop, add a loop over `s.Preferences` routing each into `LikedIngredients` or `DislikedIngredients` by its stored kind. Read the `Preference` struct first and follow whatever field it actually uses to distinguish the two - do not assume a field name.

- [ ] **Step 5: Write the ranker**

Add to `internal/engine/rank.go`:

```go
// applyPreferenceRank is engine step 8. It was a recorded no-op from Phase 2 until now:
// child_preference has existed since migration 0014 but nothing read it into the engine's
// input.
//
// Ranker only, both directions. A dislike that excluded recipes would narrow a child's diet
// on a stated preference, which is the same over-restriction failure applySuspectedAllergenRank
// exists to avoid, and a preference is a far weaker signal than a suspected allergen. Hence
// the ordinary 0.05 magnitude rather than that step's 0.15.
func applyPreferenceRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if (len(p.LikedIngredients) == 0 && len(p.DislikedIngredients) == 0) || stepIn == 0 {
		return recipes, models.StepResult{
			Step: 8, Name: "Likes / dislikes / sensory", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no preferences recorded, step is a no-op",
		}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}

	// One query, both directions: a recipe can contain a liked and a disliked ingredient at
	// once, and the two adjustments must then cancel rather than one of them winning by
	// query order.
	rows, err := pool.Query(ctx, `
		SELECT recipe_id,
		       count(*) FILTER (WHERE ingredient_id = ANY($2)) > 0 AS liked,
		       count(*) FILTER (WHERE ingredient_id = ANY($3)) > 0 AS disliked
		FROM recipe_ingredient_mapping
		WHERE recipe_id = ANY($1)
		GROUP BY recipe_id`,
		ids, p.LikedIngredients, p.DislikedIngredients)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: preference rank: %w", err)
	}
	defer rows.Close()

	adjust := make(map[string]float64, len(recipes))
	var likedN, dislikedN int
	for rows.Next() {
		var id string
		var liked, disliked bool
		if err := rows.Scan(&id, &liked, &disliked); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: preference rank scan: %w", err)
		}
		const nudge = 0.05
		if liked {
			adjust[id] += nudge
			likedN++
		}
		if disliked {
			adjust[id] -= nudge
			dislikedN++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: preference rank rows: %w", err)
	}

	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		out[i].RankedScore += adjust[out[i].RecipeID]
	}
	// Sort within the age partition applyAgeRank set, never across it: a liked ingredient
	// must not lift an out-of-band recipe above an in-band one.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].AgeInBand != out[j].AgeInBand {
			return out[i].AgeInBand
		}
		return out[i].RankedScore > out[j].RankedScore
	})

	return out, models.StepResult{
		Step: 8, Name: "Likes / dislikes / sensory", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: fmt.Sprintf("%d recipes ranked up for a liked ingredient, %d ranked down for a disliked one", likedN, dislikedN),
	}, nil
}
```

**Read this before writing any later ranker:** every ranker after `applyAgeRank` must sort by the age partition first, exactly as above, or it silently undoes SP1 Task 3. Audit `applyCultureRank`, `applyAvailabilityRank`, `applyBudgetRank` and `dedupeNearDuplicates` for the same bug as part of this task and fix each the same way.

- [ ] **Step 6: Replace the no-op in the pipeline**

In `internal/engine/pipeline.go`, replace the hand-written step-8 `steps = append(...)` block (lines 122-131) with a call to `applyPreferenceRank`, following the shape of the calls around it.

- [ ] **Step 7: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/engine/ ./internal/profile/ -v`
Expected: PASS, including SP1's `TestAgeAppropriateRecipesSortAboveEveryOutOfBandOne`, which is the guard that catches the cross-partition sort bug.

- [ ] **Step 8: Commit**

```bash
git add internal/models/profile.go internal/profile/ internal/engine/
git commit -m "engine: step 8 ranks on recorded likes and dislikes instead of recording a no-op"
```

---

### Task 3: Extend the frontend API client

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts:240-265` (`GenerateInput`)

**Interfaces:**
- Consumes: Task 1's endpoint shape.
- Produces: `ClinicalMarker` interface; `getClinicalMarkers(): Promise<ClinicalMarker[]>`; `GenerateInput` with `vegan`, `religious_restriction`, `max_prep_time_min`, `max_cook_time_min`, `preferences`.

- [ ] **Step 1: Add the types**

In `web/src/lib/types.ts`:

```typescript
/** One clinical trigger field and the values that fire a rule on it. Built from
 *  clinical_rule_master, never hardcoded: the trigger fields are provider data. */
export interface ClinicalMarker {
  trigger_field: string;
  trigger_operator: string;
  value: string;
  rule_id: string;
  clinical_domain: string;
  loadable: boolean;
}
```

Match the field names to what Task 1 leaves the endpoint returning - read the handler, do not assume. Add `vegan?`, `religious_restriction?`, `max_prep_time_min?`, `max_cook_time_min?` to `StoredProfile`.

- [ ] **Step 2: Add the client function**

In `web/src/lib/api.ts`, beside the other reference getters:

```typescript
export async function getClinicalMarkers(): Promise<ClinicalMarker[]> {
  return request<ClinicalMarker[]>("/api/reference/clinical-markers");
}
```

Extend `GenerateInput`:

```typescript
  vegan?: boolean;
  religious_restriction?: string;
  max_prep_time_min?: number;
  max_cook_time_min?: number;
  preferences?: { ingredient_id: string; kind: string }[];
```

Match `kind`'s accepted values to what `profile.Preference` actually stores - read it.

- [ ] **Step 3: Verify**

Run: `cd web && npx tsc --noEmit`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/
git commit -m "console: client types for the clinical, practice and preference inputs"
```

---

### Task 4: The clinical section

**Files:**
- Modify: `web/src/components/child-input-form.tsx:416-439`
- Test: `web/src/components/child-input-form.test.tsx`

**Interfaces:**
- Consumes: `getClinicalMarkers`, `ClinicalMarker` from Task 3.
- Produces: `conditions[]` in the submitted payload carrying one entry per set clinical flag, plus the special-care entry when set.

- [ ] **Step 1: Write the failing test**

```typescript
it("renders a clinical control per trigger field from the endpoint, not a constant", async () => {
  vi.mocked(getClinicalMarkers).mockResolvedValue([
    { trigger_field: "CKD", trigger_operator: "equals", value: "Yes",
      rule_id: "CR-REN-001", clinical_domain: "Kidney Disease", loadable: true },
  ]);
  render(<ChildInputForm busy={false} onGenerate={() => {}} />);
  expect(await screen.findByLabelText("CKD")).toBeInTheDocument();
});
```

Follow `profile-form.test.tsx`'s `vi.mock("@/lib/api", ...)` pattern - it is the real sibling pattern in this repo.

- [ ] **Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/child-input-form.test.tsx`
Expected: FAIL, no such label.

- [ ] **Step 3: Build the section**

- Add `markers` state and load it in the existing reference-loading `useEffect` alongside the other five getters.
- Add `clinicalFlags: Record<string, string>` state.
- Group `markers` by `trigger_field`. For each group render a control by `trigger_operator`: `in_list` and `equals` render a `Select` over the group's distinct `value`s plus a blank "not recorded" option; `contains` renders an `Input`. Label each with the trigger field. Show the `clinical_domain` as muted helper text - an operator needs to know which domain a flag belongs to.
- Exclude `Special_Care_Condition` from the generated controls; it keeps its own dedicated select.
- In `submit()`, extend the existing `conditions` array: keep the special-care entry, and append one `{ trigger_field, flag_value, class: "chronic" }` per non-empty entry in `clinicalFlags`.

- [ ] **Step 4: Run tests**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/child-input-form.tsx web/src/components/child-input-form.test.tsx
git commit -m "console: clinical flags are enterable, built from the rule master"
```

---

### Task 5: Practice and preference fields

**Files:**
- Modify: `web/src/components/child-input-form.tsx`
- Test: `web/src/components/child-input-form.test.tsx`

**Interfaces:**
- Consumes: Task 3's `GenerateInput` fields.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Write the failing test**

```typescript
it("disables the vegan checkbox unless the diet is Vegetarian", async () => {
  render(<ChildInputForm busy={false} onGenerate={() => {}} />);
  // models.ChildProfile's own rule: there is no "Vegan" diet_type value, so a vegan
  // profile must also declare DietType = "Vegetarian". The form mirrors that rather
  // than letting an operator submit a combination the engine will reject.
  expect(await screen.findByLabelText(/vegan/i)).toBeDisabled();
});
```

- [ ] **Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/child-input-form.test.tsx`
Expected: FAIL, no such control.

- [ ] **Step 3: Add the fields**

In the "Food practice and place" section, extend the grid with:
- A vegan checkbox, `disabled={diet !== "Vegetarian"}`, forced back to false whenever `diet` changes away from Vegetarian.
- A religious restriction `Input`.
- Two numeric `Input`s for max prep and max cook minutes, both routed through the existing `num()` helper so a blank stays `undefined` rather than becoming `0`. `0` means "no limit" to `applyTimeFilter`, so the distinction does not bite here, but `num()` is the house rule and a later field where it does bite should not have to relearn it.

Add a new "Preferences" section: two ingredient pickers, liked and disliked, built from a new `getIngredients()` call if one does not already exist in `api.ts` (check first - `/api/ingredients` is a real route). Render as the same badge-toggle pattern the allergens section already uses, which handles a long list densely.

Wire all five into `submit()`.

- [ ] **Step 4: Run tests and build**

Run: `cd web && npx tsc --noEmit && npm test && npm run build`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/
git commit -m "console: vegan, religious restriction, time budget and ingredient preferences"
```

---

### Task 6: End-to-end round trip

**Files:**
- Test: `internal/api/handlers/book_generate_test.go`

**Interfaces:**
- Consumes: every earlier task.
- Produces: no code, one guard.

- [ ] **Step 1: Write the test**

```go
func TestEveryFormFieldReachesTheEngineInput(t *testing.T) {
	h := testHandlers(t)
	body := `{
		"date_of_birth":"2022-01-01",
		"diet_type":"Vegetarian","vegan":true,
		"religious_restriction":"Halal",
		"max_prep_time_min":20,"max_cook_time_min":30,
		"conditions":[{"trigger_field":"CKD","flag_value":"Yes","class":"chronic"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/books/generate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.BookGenerate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

If `BookGenerate`'s response does not expose the derived `ChildProfile`, assert instead at the `profile.Stored` -> `ToChildProfile` boundary in `internal/profile/profile_test.go`, where every field is directly observable. Do not add an endpoint just to make this assertion convenient.

- [ ] **Step 2: Run the full suite**

```bash
go build ./... && go vet ./...
TEST_DATABASE_URL=$DATABASE_URL go test ./...
cd web && npx tsc --noEmit && npm test && npm run build
```

- [ ] **Step 3: Generate one book by hand and read it**

Start the server, open `/books`, fill every new field, generate. Confirm the run succeeds, the step list in the response names the clinical rules that fired, and neither book prints anything about the clinical flags that was not there before - SP2 adds inputs, not printed content.

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers/book_generate_test.go
git commit -m "api: pin that every console field reaches the engine input"
```
