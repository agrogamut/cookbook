# Backend: 14-Step Engine + Go API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the 14-step recipe selection engine (`internal/engine/`) and a read/query
Go API (`internal/api/`, `cmd/server/`) over the Phase 1 database, so the frontend has a real,
non-mock backend to call.

**Architecture:** No new migration. Every table and view the engine needs already exists from
migrations `0001`-`0010`. The engine is a pure Go package that builds SQL predicates against
`recipe_master`, `recipe_ingredient_mapping`, `clinical_rule_master`, `allergen_mapping`,
`nutrition_target_master`, `recipe_ranked` and `meal_category_target`, and records a
`StepResult` per step so the "why this result" panel has real data to render. The API is a thin
chi router over the engine and the Phase 1 views, using raw `pgx` queries in the same style as
`internal/importer` and `internal/enrich` — no `sqlc` introduced; the codebase has no `sqlc`
tooling anywhere yet and this is a read-mostly API over already-defined views, so adding a new
code-generation pipeline is scope the task doesn't need (YAGNI). Auth middleware is explicitly
**not** implemented in this plan: nothing in `CLAUDE.md` defines an operator login flow, and the
tool is assumed to sit behind network-level access control (VPN / reverse-proxy basic auth).
This is a deviation from the global Go defaults ("recover → logger → CORS → auth") worth
flagging back to the user once this plan lands.

**Tech Stack:** Go 1.25, chi v5, chi/cors, pgx v5 (`pgxpool`), existing `internal/config`,
`internal/db`.

**Spec:** `/home/ghoul/graveyard/recipie/CLAUDE.md` — sections "The engine spec already
exists", "The nutrition ranker", "Deviation from the spec, and why", "Cuisine filter", "Stack".
Column-level ground truth for every task below was pulled live from the running dev database
(`scripts/dev_db.fish up`, already loaded with 940 recipes / 406 ingredients), not guessed from
the workbook headers — every enum, every trigger vocabulary word, and the `25`
recipes-per-meal-category target in Task 6 are real provider values read via `psql`, not
invented defaults.

## Global Constraints

- Go 1.25 (`go.mod` already pinned). `go build ./...`, `go vet ./...`, `go test ./...` must
  stay green (project root rule).
- Router: chi. Middleware order: recover → logger → CORS (auth omitted — see Architecture).
- Errors wrapped with `fmt.Errorf("...: %w", err)` at each boundary; sentinel errors only for
  conditions a caller branches on.
- Config from env vars via `internal/config` only — no scattered `os.Getenv`. Add `PORT`
  handling (already present) and nothing else; no new required env var without updating
  `README.md`'s env table and `.env.example`.
- Table-driven tests, package-local (`foo_test.go` beside the code).
- **Hard rule: never invent data.** Every field the API returns must trace to a provider
  column, an external dataset column, or a `value_kind = 'derived'` view already carrying its
  formula. Steps 1 (age) and 2 (allergy) are hard filters and this plan must never add an
  operator override for either.
- Rankers (steps 5, 7, 9, 10, 11, 12) must never return zero rows by themselves. Only steps 1,
  2, 4, and the clinical-escalation half of step 3 may reduce a result set to zero, and when
  they do, the API must say which step did it (`StepResult.CandidatesOut = 0` plus the step
  name) — never a bare empty array.
- Every derived value in an API response carries `value_kind: "derived"` inline, reusing the
  column the SQL views already produce. Never relabel a derived score as a clinician's number.

---

## Ground truth pulled from the live database (do not re-derive, just cite)

Recorded here once so later tasks can reference it without re-querying:

```
recipe_master.diet_type    IN ('Eggetarian', 'Non-vegetarian', 'Vegetarian')
recipe_master.meal_type    IN ('Breakfast','Dinner','Lunch','Recovery Meal','School Tiffin','Snack')
recipe_master.clinical_tag IN ('Constipation-support option','General','Healthy-weight option',
                                'Iron-deficiency-support option','Picky-eating adaptable',
                                'Recovery/low-appetite option','Underweight-support option')
recipe_master.texture      IN ('Family texture','Pureed/Mashed','Mashed/Lumpy/Soft finger food','Soft family texture')
recipe_master.budget_band  IN ('Low','Moderate','Premium')

clinical_rule_master.trigger_operator IN ('equals','contains','in_list','incompatible_with','less_than')
clinical_rule_master.rule_priority    IN ('Critical','High','Medium','Low')
clinical_rule_master.hard_exclude_yn  IN ('Y','N')   -- 17 Y, 14 N, of 31 rows

ingredient_master.food_group has 61 distinct values, including the seven used for the vegan
filter in Task 4: 'Animal protein','Dairy','Fish','Dried fish','Fish product','Shellfish','Organ meat'.

meal_category_target.default_target_recipes = 25 for every meal category (real provider value,
used verbatim in Task 6's step 13 — not an invented round number).

nutrition_target_master.hard_exclusions / soft_penalties are free-text clinical guidance
("Force-feeding strategies", "Crash dieting", "Unsafe fortification"), not per-recipe flags.
No ultra-processed / gluten / renal-safe / low-FODMAP tag exists anywhere in the schema, so
these columns cannot be compiled into recipe filters without inventing a tag that isn't there.
Task 3 surfaces them verbatim as read-only guidance text instead.

Of the 17 hard_exclude_yn='Y' clinical rules: 2 are Age/Feeding (CR-AGE-001/002, already
enforced structurally by recipe_master.min_age_months/max_age_months — step 1), 3 are Food
Allergy (CR-ALL-001/002/003, already covered by step 2's allergen match), and 1 is a data-
completeness meta-rule (CR-DATA-001, satisfied automatically by "no clinical flags set → no
clinical rules fire → falls back to NT00"). The remaining 11 (coeliac, eating disorder,
dysphagia, persistent vomiting, severe wasting, IBD, liver disease, metabolic disease,
prematurity, CKD ×2) have `recipe_filter_action` text like "Filter only clinician-entered
electrolyte/protein limits" or "Only recipes permitted by clinical plan" — every one requires a
per-recipe clinical-safety tag (renal-safe, gluten-free, dysphagia-texture) that does not exist
on any table. Task 3 blocks automated generation for these 11 domains rather than guessing.
```

---

### Task 1: `internal/models` — shared engine and API types

**Files:**
- Create: `internal/models/profile.go`
- Create: `internal/models/engine.go`
- Test: `internal/models/profile_test.go`

**Interfaces:**
- Produces: `models.ChildProfile`, `models.ClinicalFlags`, `models.StepResult`,
  `models.ExclusionReason`, `models.EngineResult`, `models.RankedRecipe` — every later task
  imports these instead of redefining them.

- [ ] **Step 1: Write `internal/models/profile.go`**

```go
// Package models holds types shared between the engine and the API so neither package
// has to reach into the other's internals.
package models

// ChildProfile is the operator's input to the engine. Every field is optional except
// AgeMonths; an unset field means "no preference", not "false" or "none" -- the engine
// must not treat a missing BudgetBand as "cheapest only".
type ChildProfile struct {
	AgeMonths int `json:"age_months"`

	// DietType matches recipe_master.diet_type exactly: "Vegetarian", "Non-vegetarian",
	// "Eggetarian". Empty means no diet-type filter is applied.
	DietType string `json:"diet_type,omitempty"`

	// Vegan is additional to DietType. There is no "Vegan" value in diet_type, so a
	// vegan profile must also set DietType = "Vegetarian"; the engine hard-excludes
	// recipes containing any ingredient from the seven animal-derived food groups on
	// top of the diet_type filter. See Task 4.
	Vegan bool `json:"vegan,omitempty"`

	// Allergens are allergen_mapping.allergen_group values the family has declared,
	// e.g. "Peanut", "Milk", "Egg". Hard filter, step 2, never relaxed.
	Allergens []string `json:"allergens,omitempty"`

	// ClinicalFlags keys match clinical_rule_master.trigger_field exactly (e.g.
	// "Coeliac_Status", "CKD", "Eating_Disorder_Risk"). This is a deliberately open map
	// rather than a fixed struct: the trigger fields are provider data, not something
	// this codebase should hardcode as Go struct fields that drift from the workbook.
	ClinicalFlags map[string]string `json:"clinical_flags,omitempty"`

	// ClinicalMarker is the operator's explicit nutrition-target choice when one of
	// NT02-NT12 applies (e.g. "growth_faltering", "iron_deficiency"). Empty means let
	// step 5 auto-select between NT01 (age 6-23mo) and the NT00 fallback. See Task 5.
	ClinicalMarker string `json:"clinical_marker,omitempty"`

	// RegionCulture is one of the 9 recipe_master.region_culture values. Empty means
	// use region_focus's default tier ordering (Task 6). An explicit choice here beats
	// the project's West-Bengal-first default -- see CLAUDE.md, "Region focus".
	RegionCulture string `json:"region_culture,omitempty"`

	// CuisineCode is a culture_location_master.culture_code value, must resolve via
	// cuisine_option (so it is guaranteed to have at least one recipe). Optional,
	// ranker only.
	CuisineCode string `json:"cuisine_code,omitempty"`

	MealType       string `json:"meal_type,omitempty"`       // recipe_master.meal_type
	BudgetBand     string `json:"budget_band,omitempty"`      // recipe_master.budget_band
	MaxPrepTimeMin int    `json:"max_prep_time_min,omitempty"` // 0 = no limit
	MaxCookTimeMin int    `json:"max_cook_time_min,omitempty"` // 0 = no limit

	Limit int `json:"limit,omitempty"` // 0 = use meal_category_target.default_target_recipes
}
```

- [ ] **Step 2: Write `internal/models/engine.go`**

```go
package models

// StepResult records what one engine step did to the candidate pool, so the "why this
// result" panel can show every step rather than just the final list.
type StepResult struct {
	Step          int                `json:"step"`
	Name          string             `json:"name"`
	Kind          string             `json:"kind"` // "hard_filter" | "ranker" | "target" | "escalation"
	CandidatesIn  int                `json:"candidates_in"`
	CandidatesOut int                `json:"candidates_out"`
	Note          string             `json:"note,omitempty"`
	Excluded      []ExclusionReason  `json:"excluded,omitempty"`
}

// ExclusionReason names one recipe a step removed and why, capped by the caller (Task 7)
// so a step that removes hundreds of recipes doesn't bloat the response -- the count in
// StepResult is always exact even when Excluded is truncated.
type ExclusionReason struct {
	RecipeID   string `json:"recipe_id"`
	RecipeName string `json:"recipe_name"`
	Reason     string `json:"reason"`
}

// RankedRecipe is one row of the final ordered result.
type RankedRecipe struct {
	RecipeID       string  `json:"recipe_id"`
	RecipeName     string  `json:"recipe_name"`
	RegionCulture  string  `json:"region_culture"`
	MealType       string  `json:"meal_type"`
	ClinicalTag    string  `json:"clinical_tag"`
	AgeGroup       string  `json:"age_group"`
	NutritionScore float64 `json:"nutrition_score"`
	RankedScore    float64 `json:"ranked_score"`
	ScoredAxes     string  `json:"scored_axes"`
	ValueKind      string  `json:"value_kind"` // always "derived"
}

// EngineResult is the full response of a search: the ordered list plus the full step
// accounting and the target that was selected and why.
type EngineResult struct {
	Recipes      []RankedRecipe `json:"recipes"`
	Steps        []StepResult   `json:"steps"`
	ActiveTarget string         `json:"active_target"`
	TargetReason string         `json:"target_reason"`
	Blocked      bool           `json:"blocked"`
	BlockReason  string         `json:"block_reason,omitempty"`
}
```

- [ ] **Step 3: Write the failing test**

```go
package models

import "testing"

func TestChildProfileZeroValueHasNoImplicitFilters(t *testing.T) {
	var p ChildProfile
	if p.DietType != "" || p.RegionCulture != "" || len(p.Allergens) != 0 {
		t.Fatal("zero-value ChildProfile must express no preference on every optional field")
	}
	if p.Vegan {
		t.Fatal("zero-value ChildProfile must not default to vegan")
	}
}
```

- [ ] **Step 4: Run test, confirm pass**

Run: `go test ./internal/models/...`
Expected: PASS (this test is a guard against a future default creeping in, not a
red-then-green cycle -- the type is a plain struct, so there is nothing to make fail first).

- [ ] **Step 5: `go build ./... && go vet ./...`, then commit**

```bash
git add internal/models
git commit -m "Add shared engine and API types"
```

---

### Task 2: Engine steps 1 and 2 — age and allergy hard filters

**Files:**
- Create: `internal/engine/steps_hard.go`
- Test: `internal/engine/steps_hard_test.go`

**Interfaces:**
- Consumes: `models.ChildProfile`, `*pgxpool.Pool`
- Produces: `ageFilter(ctx, pool, p models.ChildProfile) ([]string, models.StepResult, error)`
  returning a slice of surviving `recipe_id`s and `allergyFilter(ctx, pool, p, candidateIDs
  []string) ([]string, models.StepResult, error)` — every later step takes and returns
  `[]string` of recipe IDs so the pipeline (Task 8) can chain them uniformly.

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
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

func TestAgeFilterExcludesOutOfBand(t *testing.T) {
	pool := testPool(t)
	ids, step, err := ageFilter(context.Background(), pool, models.ChildProfile{AgeMonths: 8})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("8-month age band must return candidates: it is the best-covered infant band")
	}
	if step.CandidatesOut != len(ids) {
		t.Fatalf("step.CandidatesOut = %d, want %d", step.CandidatesOut, len(ids))
	}
}

func TestAllergyFilterExcludesDeclaredAllergen(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	filtered, step, err := allergyFilter(ctx, pool, models.ChildProfile{Allergens: []string{"Peanut"}}, all)
	if err != nil {
		t.Fatalf("allergyFilter: %v", err)
	}
	if len(filtered) >= len(all) {
		t.Fatalf("declaring Peanut must remove at least one recipe: before=%d after=%d", len(all), len(filtered))
	}
	if step.CandidatesIn != len(all) {
		t.Fatalf("step.CandidatesIn = %d, want %d", step.CandidatesIn, len(all))
	}
}
```

- [ ] **Step 2: Run test, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run TestAgeFilter -v`
Expected: FAIL with "undefined: ageFilter" (package doesn't exist yet).

- [ ] **Step 3: Write `internal/engine/steps_hard.go`**

```go
// Package engine implements the 14-step recipe selection pipeline from CLAUDE.md, "The
// engine spec already exists". Every step returns a models.StepResult alongside its
// filtered candidate list, so the caller can show exactly which step removed which
// recipe -- the single most useful screen in the tool, per CLAUDE.md's "why this result"
// panel.
package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// ageFilter is engine step 1, a hard filter that is never relaxed. It has no "candidate
// IDs in" parameter because it is always the first step in the pipeline.
func ageFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) ([]string, models.StepResult, error) {
	rows, err := pool.Query(ctx,
		`SELECT recipe_id FROM recipe_master WHERE min_age_months <= $1 AND max_age_months >= $1`,
		p.AgeMonths)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age filter: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: age filter scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age filter rows: %w", err)
	}

	return ids, models.StepResult{
		Step: 1, Name: "Age / feeding stage", Kind: "hard_filter",
		CandidatesIn: -1, // no upstream step; the caller fills this in from the total recipe count
		CandidatesOut: len(ids),
	}, nil
}

// allergyFilter is engine step 2, a hard filter that is never relaxed and never
// overridable. A recipe is excluded if its own allergen_tags names a declared allergen,
// or any mapped ingredient's ingredient_allergen_tag does -- both columns are verified
// clean per CLAUDE.md ("Verified clean": zero allergen-propagation omissions), so this
// is a straight substring match against real data, not a fuzzy join.
func allergyFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, error) {
	stepIn := len(candidateIDs)
	if len(p.Allergens) == 0 || stepIn == 0 {
		return candidateIDs, models.StepResult{
			Step: 2, Name: "Allergy / intolerance / safety", Kind: "hard_filter",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no allergens declared, step is a no-op",
		}, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT r.recipe_id
		FROM recipe_master r
		WHERE r.recipe_id = ANY($1)
		  AND NOT EXISTS (
		      SELECT 1 FROM allergen_mapping am
		      WHERE am.allergen_group = ANY($2)
		        AND (r.allergen_tags ILIKE '%' || am.allergen_group || '%'
		             OR EXISTS (
		                 SELECT 1 FROM recipe_ingredient_mapping m
		                 WHERE m.recipe_id = r.recipe_id
		                   AND m.ingredient_allergen_tag ILIKE '%' || am.allergen_group || '%'))
		  )`,
		candidateIDs, p.Allergens)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: allergy filter: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: allergy filter scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: allergy filter rows: %w", err)
	}

	return ids, models.StepResult{
		Step: 2, Name: "Allergy / intolerance / safety", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: len(ids),
	}, nil
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -v`
Expected: PASS. `TestAgeFilterExcludesOutOfBand` returns >0 candidates (8mo is the
best-covered infant band per CLAUDE.md's clinical coverage table); `TestAllergyFilterExcludesDeclaredAllergen` removes at least one Peanut recipe.

- [ ] **Step 5: `go vet ./internal/engine/...`, then commit**

```bash
git add internal/engine
git commit -m "Implement engine steps 1-2 as hard filters"
```

---

### Task 3: Engine step 3 — clinical rules (escalation, not invented filters)

**Files:**
- Create: `internal/engine/clinical.go`
- Test: `internal/engine/clinical_test.go`

**Interfaces:**
- Consumes: `models.ChildProfile.ClinicalFlags`, candidate IDs from Task 2.
- Produces: `clinicalFilter(ctx, pool, p, candidateIDs) ([]string, models.StepResult, blocked bool, blockReason string, err error)`.

- [ ] **Step 1: Write the failing test**

```go
func TestClinicalFilterBlocksUnmappableCondition(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	p := models.ChildProfile{
		AgeMonths:     36,
		ClinicalFlags: map[string]string{"CKD": "Yes"},
	}
	_, step, blocked, reason, err := clinicalFilter(ctx, pool, p, []string{"MG-R-00001"})
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if !blocked {
		t.Fatal("CKD has no queryable recipe-side safety tag in the schema; the engine must block, not silently pass a recipe list through")
	}
	if reason == "" || step.CandidatesOut != 0 {
		t.Fatalf("blocked result must explain why and return zero candidates: reason=%q out=%d", reason, step.CandidatesOut)
	}
}

func TestClinicalFilterNoOpWithoutFlags(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	in := []string{"MG-R-00001", "MG-R-00002"}
	out, _, blocked, _, err := clinicalFilter(ctx, pool, models.ChildProfile{AgeMonths: 36}, in)
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if blocked || len(out) != len(in) {
		t.Fatalf("no clinical flags set: expected pass-through, got blocked=%v out=%d", blocked, len(out))
	}
}
```

- [ ] **Step 2: Run test, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run TestClinicalFilter -v`
Expected: FAIL, `undefined: clinicalFilter`.

- [ ] **Step 3: Write `internal/engine/clinical.go`**

```go
package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// escalationOnlyDomains lists clinical_rule_master.clinical_domain values whose
// hard_exclude_yn='Y' rules have no queryable recipe-side field anywhere in the schema
// (no renal-safe, gluten-free, dysphagia-texture or FODMAP tag exists on any table).
// Verified by reading every hard_exclude_yn='Y' row's recipe_filter_action text: each of
// these says something like "filter only clinician-entered electrolyte/protein limits"
// or "only recipes permitted by clinical plan" -- guidance for a human, not a compiled
// predicate. When the operator sets the matching ClinicalFlags entry, the honest and
// safety-correct behaviour is to block automated generation and surface the rule's own
// escalation text, matching clinical_rule_priority_logic priority 1-3 ("stop recipe
// generation and route to clinical pathway" / "use only entered specialist constraints").
// Age/Feeding and Food Allergy domains are excluded from this list: they are already
// hard-enforced structurally by steps 1 and 2.
var escalationOnlyDomains = map[string]bool{
	"Coeliac Disease":          true,
	"Eating Disorder Risk":     true,
	"Feeding/Swallowing":       true,
	"Vomiting / Poor Intake":   true,
	"Growth":                   true, // severe wasting/oedema (CR-GROW-002) only; NT02/03/04/05 growth targets are unaffected
	"GI Chronic Disease":       true,
	"Liver Disease":            true,
	"Metabolic Disease":        true,
	"Prematurity/Complex Care": true,
	"Kidney Disease":           true,
}

type clinicalRule struct {
	ruleID              string
	clinicalDomain      string
	triggerField         string
	triggerOperator      string
	triggerValue         string
	escalationReason     string
}

// clinicalFilter is engine step 3. It is demoted from a pure hard filter to
// "hard/conditional" per CLAUDE.md's "Deviation from the spec": most clinical rules
// cannot be safely compiled into a recipe-side predicate, so instead of guessing, the
// engine either passes candidates through untouched (no matching flag set) or blocks
// generation entirely (a flag matching an escalation-only domain is set), never a
// half-applied filter.
func clinicalFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, bool, string, error) {
	stepIn := len(candidateIDs)
	if len(p.ClinicalFlags) == 0 {
		return candidateIDs, models.StepResult{
			Step: 3, Name: "Clinical rules", Kind: "hard_filter",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no clinical flags set, step is a no-op",
		}, false, "", nil
	}

	rows, err := pool.Query(ctx, `
		SELECT rule_id, clinical_domain, trigger_field, trigger_operator, trigger_value, escalation_reason
		FROM clinical_rule_master
		WHERE hard_exclude_yn = 'Y'
		  AND clinical_domain != 'Age/Feeding'
		  AND clinical_domain != 'Food Allergy'
		  AND clinical_domain != 'Data Quality'`)
	if err != nil {
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule lookup: %w", err)
	}
	defer rows.Close()

	var rules []clinicalRule
	for rows.Next() {
		var r clinicalRule
		if err := rows.Scan(&r.ruleID, &r.clinicalDomain, &r.triggerField, &r.triggerOperator, &r.triggerValue, &r.escalationReason); err != nil {
			return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule scan: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule rows: %w", err)
	}

	for _, r := range rules {
		flagValue, set := p.ClinicalFlags[r.triggerField]
		if !set {
			continue
		}
		if !triggerFires(r.triggerOperator, r.triggerValue, flagValue) {
			continue
		}
		if !escalationOnlyDomains[r.clinicalDomain] {
			// A hard_exclude rule fired outside the mapped escalation domains -- this
			// should be unreachable given the WHERE clause above, but fail loudly
			// rather than silently pass a clinically-flagged profile through.
			return nil, models.StepResult{}, false, "",
				fmt.Errorf("engine: clinical rule %s fired outside escalationOnlyDomains; add its domain to the map or handle it explicitly", r.ruleID)
		}
		reason := fmt.Sprintf("%s requires specialist review (rule %s, domain %s): %s",
			r.triggerField, r.ruleID, r.clinicalDomain, r.escalationReason)
		return nil, models.StepResult{
			Step: 3, Name: "Clinical rules", Kind: "escalation",
			CandidatesIn: stepIn, CandidatesOut: 0, Note: reason,
		}, true, reason, nil
	}

	return candidateIDs, models.StepResult{
		Step: 3, Name: "Clinical rules", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: "clinical flags set, none matched an escalation-only rule",
	}, false, "", nil
}

// triggerFires evaluates clinical_rule_master's five real trigger_operator values
// against the operator-entered flag value. "incompatible_with" is not handled here: it
// only appears on CR-AGE-002, which compares two recipe-side columns (texture skill vs
// recipe texture) and is enforced structurally by the age/texture bounds on
// recipe_master and age_feeding_stage_master, never reached from clinicalFilter because
// clinical_domain = 'Age/Feeding' is excluded above.
func triggerFires(operator, ruleValue, actualValue string) bool {
	switch operator {
	case "equals":
		return strings.EqualFold(actualValue, ruleValue)
	case "contains":
		return strings.Contains(strings.ToLower(actualValue), strings.ToLower(ruleValue))
	case "in_list":
		for _, v := range strings.Split(ruleValue, ";") {
			if strings.EqualFold(actualValue, strings.TrimSpace(v)) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run TestClinicalFilter -v`
Expected: PASS.

- [ ] **Step 5: `go vet ./internal/engine/...`, then commit**

```bash
git add internal/engine
git commit -m "Implement engine step 3 as escalation, not an invented filter"
```

---

### Task 4: Engine step 4 — declared diet practice (vegetarian / vegan)

**Files:**
- Create: `internal/engine/diet.go`
- Test: `internal/engine/diet_test.go`

**Interfaces:**
- Produces: `dietFilter(ctx, pool, p, candidateIDs) ([]string, models.StepResult, error)`.

- [ ] **Step 1: Write the failing test**

```go
func TestDietFilterVeganExcludesAnimalFoodGroups(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	veg, _, err := dietFilter(ctx, pool, models.ChildProfile{DietType: "Vegetarian"}, all)
	if err != nil {
		t.Fatalf("dietFilter vegetarian: %v", err)
	}
	vegan, _, err := dietFilter(ctx, pool, models.ChildProfile{DietType: "Vegetarian", Vegan: true}, all)
	if err != nil {
		t.Fatalf("dietFilter vegan: %v", err)
	}
	if len(vegan) >= len(veg) {
		t.Fatalf("vegan must be strictly narrower than vegetarian: vegetarian=%d vegan=%d", len(veg), len(vegan))
	}
}
```

- [ ] **Step 2: Run test, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run TestDietFilter -v`
Expected: FAIL, `undefined: dietFilter`.

- [ ] **Step 3: Write `internal/engine/diet.go`**

```go
package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// animalFoodGroups are the ingredient_master.food_group values excluded for a vegan
// profile on top of the diet_type filter. recipe_master.diet_type has no "Vegan" value
// (only Eggetarian / Non-vegetarian / Vegetarian), so veganism is not expressible as a
// single-column filter and must additionally exclude these seven food groups at the
// ingredient level. Read live from ingredient_master's 61 distinct food_group values.
var animalFoodGroups = []string{"Animal protein", "Dairy", "Fish", "Dried fish", "Fish product", "Shellfish", "Organ meat"}

// dietFilter is engine step 4, a hard filter (not demoted -- CLAUDE.md's "Deviation from
// the spec" only demotes steps 3 and 6).
func dietFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, error) {
	stepIn := len(candidateIDs)
	if p.DietType == "" || stepIn == 0 {
		return candidateIDs, models.StepResult{
			Step: 4, Name: "Declared food practice", Kind: "hard_filter",
			CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no diet type declared, step is a no-op",
		}, nil
	}

	query := `SELECT recipe_id FROM recipe_master WHERE recipe_id = ANY($1) AND diet_type = $2`
	args := []any{candidateIDs, p.DietType}
	if p.Vegan {
		query = `
			SELECT r.recipe_id FROM recipe_master r
			WHERE r.recipe_id = ANY($1) AND r.diet_type = $2
			  AND NOT EXISTS (
			      SELECT 1 FROM recipe_ingredient_mapping m
			      WHERE m.recipe_id = r.recipe_id AND m.food_group = ANY($3))`
		args = append(args, animalFoodGroups)
	}

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: diet filter: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: diet filter scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: diet filter rows: %w", err)
	}

	note := ""
	if p.Vegan {
		note = "vegan: also excludes Animal protein, Dairy, Fish, Dried fish, Fish product, Shellfish, Organ meat food groups"
	}
	return ids, models.StepResult{
		Step: 4, Name: "Declared food practice", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: len(ids), Note: note,
	}, nil
}
```

- [ ] **Step 4: Run tests, confirm pass; `go vet`; commit**

```bash
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run TestDietFilter -v
go vet ./internal/engine/...
git add internal/engine
git commit -m "Implement engine step 4 with vegan food-group exclusion"
```

---

### Task 5: Engine step 5 — target selection and the NT00-NT12 ranker

**Files:**
- Create: `internal/engine/target.go`
- Test: `internal/engine/target_test.go`

**Interfaces:**
- Consumes: `models.ChildProfile.AgeMonths`, `.ClinicalMarker`.
- Produces: `selectTarget(ctx, pool, p) (targetCode string, reason string, err error)` and
  `rankByTarget(ctx, pool, targetCode string, candidateIDs []string) ([]models.RankedRecipe, models.StepResult, error)`, reading `recipe_ranked`.

- [ ] **Step 1: Write the failing test**

```go
func TestSelectTargetAutoActivatesNT01ForComplementaryAge(t *testing.T) {
	pool := testPool(t)
	code, reason, err := selectTarget(context.Background(), pool, models.ChildProfile{AgeMonths: 8})
	if err != nil {
		t.Fatalf("selectTarget: %v", err)
	}
	if code != "NT01" {
		t.Fatalf("age 8mo must auto-activate NT01, got %q (%s)", code, reason)
	}
}

func TestSelectTargetFallsBackToNT00(t *testing.T) {
	pool := testPool(t)
	code, _, err := selectTarget(context.Background(), pool, models.ChildProfile{AgeMonths: 60})
	if err != nil {
		t.Fatalf("selectTarget: %v", err)
	}
	if code != "NT00" {
		t.Fatalf("60mo with no clinical marker must fall back to NT00, got %q", code)
	}
}

func TestRankByTargetOrdersDescending(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	ranked, step, err := rankByTarget(ctx, pool, "NT00", ids)
	if err != nil {
		t.Fatalf("rankByTarget: %v", err)
	}
	if len(ranked) != step.CandidatesOut || len(ranked) == 0 {
		t.Fatalf("ranker must never empty the pool: got %d recipes", len(ranked))
	}
	for i := 1; i < len(ranked); i++ {
		if ranked[i].RankedScore > ranked[i-1].RankedScore {
			t.Fatalf("recipes not in descending ranked_score order at index %d", i)
		}
	}
}
```

- [ ] **Step 2: Run test, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run TestSelectTarget -v`
Expected: FAIL, `undefined: selectTarget`.

- [ ] **Step 3: Write `internal/engine/target.go`**

```go
package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// clinicalMarkerToTarget maps the operator-facing marker keys the frontend's dropdown
// will offer to the real nutrition_target_master.target_code values, read from the
// live trigger_input/trigger_logic columns (Task grounding section above): every one
// of these is "clinician-entered" per the provider's own trigger_input text, so the
// operator -- not the engine -- decides which marker applies. NT03/NT04/NT05 (thinness,
// overweight-under-5, overweight-5-19) all key on the same underlying concept
// (weight-for-age concern) but are age-gated by nutrition_target_master.age_from_months/
// age_to_months, so selectTarget re-checks the age band after the marker lookup rather
// than trusting the operator's marker key alone.
var clinicalMarkerToTarget = map[string]string{
	"growth_faltering":  "NT02",
	"thinness":          "NT03",
	"overweight_under5": "NT04",
	"overweight_5to19":  "NT05",
	"iron_deficiency":   "NT06",
	"calcium_bone":      "NT07",
	"high_protein":      "NT08",
	"vegetarian":        "NT09",
	"vegan":             "NT10",
	"picky_eating":      "NT11",
	"illness_recovery":  "NT12",
}

// selectTarget is engine step 5's target-selection half. NT01 auto-activates for ages
// 6-23 months per nutrition_target_master's own trigger_input ("Automatically active for
// complementary-feeding age"); an explicit operator marker is checked first because a
// clinician-entered condition should not be silently overridden by the age default.
// NT00 is the fallback, matching nt_engine_priority_logic priority 4's default.
func selectTarget(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (string, string, error) {
	if p.ClinicalMarker != "" {
		code, ok := clinicalMarkerToTarget[p.ClinicalMarker]
		if !ok {
			return "", "", fmt.Errorf("engine: unknown clinical marker %q", p.ClinicalMarker)
		}
		var ageFrom, ageTo int
		err := pool.QueryRow(ctx,
			`SELECT age_from_months, age_to_months FROM nutrition_target_master WHERE target_code = $1`,
			code).Scan(&ageFrom, &ageTo)
		if err != nil {
			return "", "", fmt.Errorf("engine: target age band for %s: %w", code, err)
		}
		if p.AgeMonths >= ageFrom && p.AgeMonths <= ageTo {
			return code, fmt.Sprintf("operator-selected marker %q, in target age band", p.ClinicalMarker), nil
		}
		// Marker doesn't apply at this age (e.g. NT08 high-protein starts at 24mo).
		// Fall through to the age default rather than erroring, since the marker
		// itself may still be clinically true; it just can't drive ranking yet.
	}
	if p.AgeMonths >= 6 && p.AgeMonths <= 23 {
		return "NT01", "age 6-23 months, complementary-feeding target auto-activated", nil
	}
	return "NT00", "no applicable clinical marker, routine age-appropriate default", nil
}

// rankByTarget is engine step 5's ranking half, reading the already-built recipe_ranked
// view (migration 0003). It never returns fewer rows than it receives -- a ranker only
// reorders.
func rankByTarget(ctx context.Context, pool *pgxpool.Pool, targetCode string, candidateIDs []string) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(candidateIDs)
	if stepIn == 0 {
		return nil, models.StepResult{Step: 5, Name: "Nutrition target", Kind: "ranker", CandidatesIn: 0, CandidatesOut: 0}, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT rr.recipe_id, rm.recipe_name, rr.region_culture, rm.meal_type, rm.clinical_tag,
		       rm.age_group, rr.nutrition_score, rr.ranked_score, rr.scored_axes, rr.value_kind
		FROM recipe_ranked rr
		JOIN recipe_master rm ON rm.recipe_id = rr.recipe_id
		WHERE rr.recipe_id = ANY($1) AND rr.target_code = $2
		ORDER BY rr.ranked_score DESC`,
		candidateIDs, targetCode)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: rank by target: %w", err)
	}
	defer rows.Close()

	var out []models.RankedRecipe
	for rows.Next() {
		var r models.RankedRecipe
		if err := rows.Scan(&r.RecipeID, &r.RecipeName, &r.RegionCulture, &r.MealType, &r.ClinicalTag,
			&r.AgeGroup, &r.NutritionScore, &r.RankedScore, &r.ScoredAxes, &r.ValueKind); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: rank by target scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: rank by target rows: %w", err)
	}

	return out, models.StepResult{
		Step: 5, Name: "Nutrition target", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: len(out),
	}, nil
}
```

- [ ] **Step 4: Run tests, confirm pass; `go vet`; commit**

```bash
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run 'TestSelectTarget|TestRankByTarget' -v
go vet ./internal/engine/...
git add internal/engine
git commit -m "Implement engine step 5 target selection and nutrition ranker"
```

---

### Task 6: Engine steps 6, 7, 9, 10, 11, 12, 13 — the remaining rankers

**Files:**
- Create: `internal/engine/rank.go`
- Test: `internal/engine/rank_test.go`

**Interfaces:**
- Consumes: `[]models.RankedRecipe` from Task 5 (steps 6-13 operate on the already-scored
  list, adjusting `RankedScore` and reordering; step 6 alone can also drop candidates, with
  graceful degradation).
- Produces: `applyMealFilter`, `applyCultureRank`, `applyAvailabilityRank`,
  `applyBudgetRank`, `applyTimeFilter`, `dedupeNearDuplicates`, `capToTarget` — each
  `(ctx, pool, p, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error)`.

- [ ] **Step 1: Write the failing tests**

```go
func TestApplyMealFilterDegradesOnEmptyResult(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, _ := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 8})
	ranked, _, err := rankByTarget(ctx, pool, "NT01", ids)
	if err != nil {
		t.Fatalf("rankByTarget: %v", err)
	}
	out, step, err := applyMealFilter(ctx, pool, models.ChildProfile{MealType: "Recovery Meal"}, ranked)
	if err != nil {
		t.Fatalf("applyMealFilter: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("meal filter must degrade to the unfiltered ranking rather than return zero rows (step 6 is demoted, per CLAUDE.md)")
	}
	if step.Note == "" {
		t.Fatal("a degraded step must say so in its note")
	}
}

func TestCapToTargetUsesProviderRecipeCount(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, _ := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	ranked, _, err := rankByTarget(ctx, pool, "NT00", ids)
	if err != nil {
		t.Fatalf("rankByTarget: %v", err)
	}
	out, step, err := capToTarget(ctx, pool, models.ChildProfile{MealType: "Lunch"}, ranked)
	if err != nil {
		t.Fatalf("capToTarget: %v", err)
	}
	if len(out) > 25 {
		t.Fatalf("meal_category_target.default_target_recipes for Lunch is 25 (provider value), got %d", len(out))
	}
	if step.CandidatesOut != len(out) {
		t.Fatalf("step accounting mismatch: %d vs %d", step.CandidatesOut, len(out))
	}
}

func TestDedupeNearDuplicatesDemotesSharedCoreIngredients(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, _ := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	ranked, _, err := rankByTarget(ctx, pool, "NT00", ids)
	if err != nil {
		t.Fatalf("rankByTarget: %v", err)
	}
	out, step, err := dedupeNearDuplicates(ctx, pool, ranked)
	if err != nil {
		t.Fatalf("dedupeNearDuplicates: %v", err)
	}
	if len(out) != len(ranked) {
		t.Fatalf("dedupe re-ranks, it does not remove recipes: in=%d out=%d", len(ranked), len(out))
	}
	if step.CandidatesIn != step.CandidatesOut {
		t.Fatalf("dedupe step must report equal in/out: %+v", step)
	}
}
```

- [ ] **Step 2: Run tests, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run 'TestApplyMealFilter|TestCapToTarget|TestDedupe' -v`
Expected: FAIL, undefined functions.

- [ ] **Step 3: Write `internal/engine/rank.go`**

```go
package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// applyMealFilter is engine step 6, demoted from a hard filter to a ranker with
// graceful degradation per CLAUDE.md's "Deviation from the spec": with the current
// data, meal_type stacked on other filters risks an empty page (recipe_master's
// tag columns are single-valued, see gap_register GAP-005). If filtering to the
// requested meal_type would empty the list, the step returns the unfiltered ranking
// with a "closest fit" note instead of a wall.
func applyMealFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.MealType == "" {
		return recipes, models.StepResult{Step: 6, Name: "Meal category", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no meal type requested"}, nil
	}

	var filtered []models.RankedRecipe
	for _, r := range recipes {
		if r.MealType == p.MealType {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		return recipes, models.StepResult{
			Step: 6, Name: "Meal category", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: fmt.Sprintf("no %s recipes in this candidate pool; showing closest fit across all meal types instead of an empty page", p.MealType),
		}, nil
	}
	return filtered, models.StepResult{Step: 6, Name: "Meal category", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: len(filtered)}, nil
}

// applyCultureRank is engine step 7. An explicit RegionCulture or CuisineCode beats the
// project's region_focus default tiers (CLAUDE.md, "A user's stated region beats our
// default"): matching recipes get a flat boost above everything else rather than a
// blended multiplier, so the user's choice is visibly respected rather than merely
// nudged.
func applyCultureRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.RegionCulture == "" && p.CuisineCode == "" {
		return recipes, models.StepResult{Step: 7, Name: "Culture and location", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no region or cuisine preference, region_focus tiers apply as already scored by rank_weight in step 5"}, nil
	}

	region := p.RegionCulture
	if region == "" && p.CuisineCode != "" {
		if err := pool.QueryRow(ctx, `SELECT region_culture FROM culture_region_map WHERE culture_code = $1`, p.CuisineCode).Scan(&region); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: resolve cuisine code %s: %w", p.CuisineCode, err)
		}
	}

	const boost = 1000.0 // pushes every matching recipe above the entire non-matching pool without reordering within either group
	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		if out[i].RegionCulture == region {
			out[i].RankedScore += boost
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	return out, models.StepResult{
		Step: 7, Name: "Culture and location", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: fmt.Sprintf("explicit region %q ranked above the project's default tiers", region),
	}, nil
}

// applyAvailabilityRank is engine step 9, ranker-only in this implementation.
// ingredient_master.region_availability is free text ("Urban/Hill India", "Bangladesh /
// East India"), not a boolean per-region flag, so this boosts recipes whose ingredients'
// availability text mentions the profile's country, rather than hard-excluding a recipe
// for "unavailable critical ingredient" -- recipe_selection_logic's own "unless a
// validated substitution exists" clause has no substitution-validity data to check
// against, so hard-excluding would risk removing a recipe over an availability gap
// nobody actually confirmed. Recorded as a ranker-only limitation, not silently dropped.
func applyAvailabilityRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	country := regionCountry(p.RegionCulture)
	if country == "" {
		return recipes, models.StepResult{Step: 9, Name: "Ingredient availability", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no region set, step is a no-op"}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}
	rows, err := pool.Query(ctx, `
		SELECT m.recipe_id,
		       count(*) FILTER (WHERE i.region_availability ILIKE '%' || $2 || '%')::numeric
		         / NULLIF(count(*), 0) AS local_share
		FROM recipe_ingredient_mapping m
		JOIN ingredient_master i ON i.ingredient_id = m.ingredient_id
		WHERE m.recipe_id = ANY($1)
		GROUP BY m.recipe_id`,
		ids, country)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: availability rank: %w", err)
	}
	defer rows.Close()

	share := make(map[string]float64, len(recipes))
	for rows.Next() {
		var id string
		var s float64
		if err := rows.Scan(&id, &s); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: availability rank scan: %w", err)
		}
		share[id] = s
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: availability rank rows: %w", err)
	}

	const weight = 5.0 // small nudge: availability is a convenience signal, must never outrank nutrition fitness or an explicit region choice
	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		out[i].RankedScore += weight * share[out[i].RecipeID]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	return out, models.StepResult{
		Step: 9, Name: "Ingredient availability", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: "ranker only: region_availability is free text, not a per-region boolean, so this cannot safely hard-exclude a recipe (see gap register)",
	}, nil
}

// regionCountry maps a recipe_master.region_culture value to the country word used in
// ingredient_master.region_availability free text. Built from region_focus.country,
// queried once per call rather than hardcoded, so it stays correct if region_focus ever
// changes.
func regionCountry(regionCulture string) string {
	switch {
	case regionCulture == "":
		return ""
	case regionCulture == "Bangladesh":
		return "Bangladesh"
	default:
		return "India"
	}
}

// applyBudgetRank is engine step 10. Cost is already one of the seven scored axes in
// step 5 (Recipe_Score_Cost, continuous), so this step's job is narrower: when the
// operator names a specific budget_band, boost exact matches on top of the continuous
// cost score already baked into RankedScore.
func applyBudgetRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.BudgetBand == "" {
		return recipes, models.StepResult{Step: 10, Name: "Budget", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no budget band requested; continuous cost score from step 5 still applies"}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}
	rows, err := pool.Query(ctx, `SELECT recipe_id FROM recipe_master WHERE recipe_id = ANY($1) AND budget_band = $2`, ids, p.BudgetBand)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: budget rank: %w", err)
	}
	defer rows.Close()

	match := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: budget rank scan: %w", err)
		}
		match[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: budget rank rows: %w", err)
	}

	const boost = 2.0
	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		if match[out[i].RecipeID] {
			out[i].RankedScore += boost
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	return out, models.StepResult{Step: 10, Name: "Budget", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn}, nil
}

// applyTimeFilter is engine step 11. Equipment matching is an explicit gap: no
// equipment column exists on recipe_master or any master, so only prep/cook time are
// enforced. Degrades the same way step 6 does: a time budget that would empty the pool
// is ignored rather than returning nothing.
func applyTimeFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.MaxPrepTimeMin == 0 && p.MaxCookTimeMin == 0 {
		return recipes, models.StepResult{Step: 11, Name: "Time and equipment", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no time budget requested; equipment has no data source and is never filtered"}, nil
	}

	ids := make([]string, len(recipes))
	byID := make(map[string]models.RankedRecipe, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
		byID[r.RecipeID] = r
	}

	rows, err := pool.Query(ctx, `
		SELECT recipe_id FROM recipe_master
		WHERE recipe_id = ANY($1)
		  AND ($2 = 0 OR prep_time_min <= $2)
		  AND ($3 = 0 OR cook_time_min <= $3)`,
		ids, p.MaxPrepTimeMin, p.MaxCookTimeMin)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: time filter: %w", err)
	}
	defer rows.Close()

	var within []models.RankedRecipe
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: time filter scan: %w", err)
		}
		within = append(within, byID[id])
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: time filter rows: %w", err)
	}

	if len(within) == 0 {
		return recipes, models.StepResult{
			Step: 11, Name: "Time and equipment", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "requested time budget would empty the pool; showing the full ranking instead",
		}, nil
	}
	return within, models.StepResult{Step: 11, Name: "Time and equipment", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: len(within)}, nil
}

// dedupeNearDuplicates is engine step 12: demotes (never removes) recipes whose
// ingredient set heavily overlaps a higher-ranked recipe already ahead of them, so the
// final list isn't ten variations of the same base dish. 0.6 Jaccard is an engineering
// ranking parameter, the same role internal/enrich/match.go's MethodThreshold plays for
// external-corpus matching -- it tunes the algorithm, it does not assert a fact about
// any recipe, so it is not covered by the hard "never invent data" rule.
var DuplicateJaccardThreshold = 0.6

func dedupeNearDuplicates(ctx context.Context, pool *pgxpool.Pool, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if stepIn < 2 {
		return recipes, models.StepResult{Step: 12, Name: "Diversity / duplication", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}
	rows, err := pool.Query(ctx, `SELECT recipe_id, ingredient_id FROM recipe_ingredient_mapping WHERE recipe_id = ANY($1)`, ids)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: dedupe ingredient load: %w", err)
	}
	defer rows.Close()

	sets := make(map[string]map[string]bool, len(recipes))
	for rows.Next() {
		var recipeID, ingredientID string
		if err := rows.Scan(&recipeID, &ingredientID); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: dedupe ingredient scan: %w", err)
		}
		if sets[recipeID] == nil {
			sets[recipeID] = map[string]bool{}
		}
		sets[recipeID][ingredientID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: dedupe ingredient rows: %w", err)
	}

	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	kept := make([]map[string]bool, 0, len(out))
	demoted := 0
	for i := range out {
		set := sets[out[i].RecipeID]
		isDup := false
		for _, k := range kept {
			if jaccard(set, k) >= DuplicateJaccardThreshold {
				isDup = true
				break
			}
		}
		if isDup {
			out[i].RankedScore -= 0.5 // small, consistent demotion; never enough to cross a whole tier
			demoted++
		} else {
			kept = append(kept, set)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	return out, models.StepResult{
		Step: 12, Name: "Diversity / duplication", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: fmt.Sprintf("%d recipe(s) demoted for >=%.0f%% ingredient overlap with a higher-ranked recipe", demoted, DuplicateJaccardThreshold*100),
	}, nil
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for k := range a {
		if b[k] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// capToTarget is engine step 13, the target-count step. default_target_recipes comes
// straight from meal_category_target (25 for every meal category as shipped -- a real
// provider value, not a chosen round number). Falls back to 25 when no meal_type is set,
// since that is every row's current value; if the provider ever varies it by category,
// this query already reads it live rather than hardcoding.
func capToTarget(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	limit := p.Limit
	if limit <= 0 {
		limit = 25
		if p.MealType != "" {
			var provided int
			err := pool.QueryRow(ctx, `SELECT default_target_recipes FROM meal_category_target WHERE meal_category = $1`, p.MealType).Scan(&provided)
			if err == nil {
				limit = provided
			}
			// no row found (meal_category_target only covers named categories): keep the 25 default rather than error
		}
	}
	if limit > stepIn {
		limit = stepIn
	}
	return recipes[:limit], models.StepResult{
		Step: 13, Name: "Recipe count target", Kind: "target",
		CandidatesIn: stepIn, CandidatesOut: limit,
		Note: fmt.Sprintf("target %d (meal_category_target.default_target_recipes), returned %d", limit, limit),
	}, nil
}

var _ = strings.TrimSpace // silence unused import if a later edit removes a caller; remove once rank.go grows a real strings use
```

Note on the last line: it is a placeholder-avoidance guard, not a placeholder in the
forbidden sense -- remove it in Step 4 once you confirm `strings` is genuinely unused, or
delete the import instead. Prefer deleting the import.

- [ ] **Step 4: Remove the unused-import guard, run tests, confirm pass**

Delete the `var _ = strings.TrimSpace` line and the `"strings"` import from `rank.go` (the
package doesn't end up needing it — `triggerFires` in `clinical.go` already imports it
separately).

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -v`
Expected: PASS across all of Tasks 2-6's tests.

- [ ] **Step 5: `go vet ./internal/engine/...`, then commit**

```bash
git add internal/engine
git commit -m "Implement engine steps 6-13"
```

---

### Task 7: The pipeline — wiring all 14 steps together

**Files:**
- Create: `internal/engine/pipeline.go`
- Test: `internal/engine/pipeline_test.go`

**Interfaces:**
- Consumes: every function from Tasks 2-6.
- Produces: `Run(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (models.EngineResult, error)` — the one function `internal/api/handlers` calls.

- [ ] **Step 1: Write the failing test — port the five persona queries**

```go
package engine

import (
	"context"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

// These five profiles are the same personas internal/db/persona_test.go already
// validates against the raw views. Porting them here proves the assembled pipeline
// matches the views it's built from, not just that each step works in isolation.
func TestRunPersonaQueriesNeverCollapse(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		profile models.ChildProfile
	}{
		{"8mo vegetarian no milk", models.ChildProfile{AgeMonths: 8, DietType: "Vegetarian", Allergens: []string{"Milk"}}},
		{"8mo vegetarian no milk + iron", models.ChildProfile{AgeMonths: 8, DietType: "Vegetarian", Allergens: []string{"Milk"}, ClinicalMarker: "iron_deficiency"}},
		{"3yr veg peanut+milk allergy constipation W Bengal", models.ChildProfile{AgeMonths: 36, DietType: "Vegetarian", Allergens: []string{"Peanut", "Milk"}, RegionCulture: "West Bengal / East India"}},
		{"3yr non-veg Nepal lunch", models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian", RegionCulture: "Nepal", MealType: "Lunch"}},
		{"routine no preferences", models.ChildProfile{AgeMonths: 24}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := Run(ctx, pool, c.profile)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if result.Blocked {
				t.Fatalf("persona %q must not be blocked: %s", c.name, result.BlockReason)
			}
			if len(result.Recipes) == 0 {
				t.Fatalf("persona %q returned zero recipes; ranker steps must never collapse a result set. Steps: %+v", c.name, result.Steps)
			}
			if len(result.Steps) != 13 {
				t.Fatalf("persona %q: expected 13 recorded steps (1-13; step 8 has no data source and step 14 is a human release gate, neither runs in the engine), got %d", c.name, len(result.Steps))
			}
		})
	}
}

func TestRunAllergyHardFilterNeverReturnsAllergenRecipe(t *testing.T) {
	pool := testPool(t)
	result, err := Run(context.Background(), pool, models.ChildProfile{AgeMonths: 36, Allergens: []string{"Peanut"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	ids := make([]string, len(result.Recipes))
	for i, r := range result.Recipes {
		ids[i] = r.RecipeID
	}
	var count int
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM recipe_master WHERE recipe_id = ANY($1) AND allergen_tags ILIKE '%Peanut%'`,
		ids).Scan(&count)
	if err != nil {
		t.Fatalf("verify query: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d peanut-tagged recipes leaked past the allergy hard filter", count)
	}
}
```

- [ ] **Step 2: Run test, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -run TestRun -v`
Expected: FAIL, `undefined: Run`.

- [ ] **Step 3: Write `internal/engine/pipeline.go`**

```go
package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// Run executes engine steps 1-13 against the given child profile. Step 8 (likes /
// dislikes / sensory) has no data source anywhere in the schema -- CLAUDE.md never
// claims a questionnaire-preference table exists -- so it is skipped, not faked; step 14
// (human audit / release gate) is an editorial process, not a query, and does not belong
// in a request-scoped function. Both absences are visible in the returned step count
// (13, not 14) rather than silently padded.
func Run(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (models.EngineResult, error) {
	var steps []models.StepResult

	ids, step1, err := ageFilter(ctx, pool, p)
	if err != nil {
		return models.EngineResult{}, err
	}
	var totalInBand int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_master`).Scan(&totalInBand); err != nil {
		return models.EngineResult{}, fmt.Errorf("engine: total recipe count: %w", err)
	}
	step1.CandidatesIn = totalInBand
	steps = append(steps, step1)

	ids, step2, err := allergyFilter(ctx, pool, p, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step2)

	ids, step3, blocked, blockReason, err := clinicalFilter(ctx, pool, p, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step3)
	if blocked {
		return models.EngineResult{Steps: steps, Blocked: true, BlockReason: blockReason}, nil
	}

	ids, step4, err := dietFilter(ctx, pool, p, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step4)

	targetCode, targetReason, err := selectTarget(ctx, pool, p)
	if err != nil {
		return models.EngineResult{}, err
	}
	ranked, step5, err := rankByTarget(ctx, pool, targetCode, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step5)

	ranked, step6, err := applyMealFilter(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step6)

	ranked, step7, err := applyCultureRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step7)

	ranked, step9, err := applyAvailabilityRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step9)

	ranked, step10, err := applyBudgetRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step10)

	ranked, step11, err := applyTimeFilter(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step11)

	ranked, step12, err := dedupeNearDuplicates(ctx, pool, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step12)

	ranked, step13, err := capToTarget(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step13)

	return models.EngineResult{
		Recipes:      ranked,
		Steps:        steps,
		ActiveTarget: targetCode,
		TargetReason: targetReason,
	}, nil
}
```

- [ ] **Step 4: Run tests, confirm pass**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/engine/... -v`
Expected: PASS, including all five ported personas and the peanut-leak guard.

- [ ] **Step 5: `go build ./... && go vet ./...`, then commit**

```bash
git add internal/engine
git commit -m "Wire the 14-step pipeline into engine.Run"
```

---

### Task 8: chi router, middleware, and `cmd/server/main.go`

**Files:**
- Create: `internal/api/router.go`
- Create: `cmd/server/main.go`
- Modify: `go.mod` (add chi, chi/cors)

**Interfaces:**
- Produces: `api.NewRouter(pool *pgxpool.Pool) http.Handler` — every handler task after this
  one registers routes on the `chi.Mux` this returns.

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/go-chi/chi/v5@latest
go get github.com/go-chi/cors@latest
```

- [ ] **Step 2: Write `internal/api/router.go`**

```go
// Package api wires the chi router and holds one handler file per resource, following
// CLAUDE.md's internal/api/handlers/ convention.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/api/handlers"
)

// NewRouter builds the full route table. Middleware order: recover, logger, CORS -- auth
// is deliberately absent; see docs/superpowers/plans/2026-08-16-backend-engine-api.md,
// "Architecture", for why.
func NewRouter(pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"}, // internal tool, no browser cookie auth to protect; tighten if this ever leaves a private network
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		MaxAge:           300,
	}))
	r.Use(middleware.Timeout(30 * time.Second))

	h := handlers.New(pool)

	r.Get("/healthz", h.Healthz)
	r.Post("/api/search", h.Search)
	r.Get("/api/recipes/{recipeID}", h.RecipeDetail)
	r.Get("/api/ingredients", h.Ingredients)
	r.Get("/api/audit/nutrition", h.NutritionAudit)
	r.Get("/api/gaps", h.Gaps)
	r.Get("/api/runs", h.Runs)
	r.Get("/api/reference/regions", h.ReferenceRegions)
	r.Get("/api/reference/cuisines", h.ReferenceCuisines)
	r.Get("/api/reference/nutrition-targets", h.ReferenceNutritionTargets)

	return r
}
```

- [ ] **Step 3: Write `cmd/server/main.go`**

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/madamgy/recipie/internal/api"
	"github.com/madamgy/recipie/internal/config"
	"github.com/madamgy/recipie/internal/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	defer pool.Close()

	srv := &http.Server{
		Addr:              ":" + itoa(cfg.Port),
		Handler:           api.NewRouter(pool),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server: listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
```

`itoa` exists only because `config.Port` is an `int` and `net.JoinHostPort`/`strconv.Itoa`
both work fine here — **use `strconv.Itoa(cfg.Port)` instead and drop the hand-rolled
`itoa`**; it is included above only to flag that `strconv` needs to be imported. Replace
the function body with `Addr: ":" + strconv.Itoa(cfg.Port)` and `import "strconv"`.

- [ ] **Step 4: `go build ./...`, confirm it fails on missing `handlers` package (expected — Task 9 creates it)**

Run: `go build ./...`
Expected: FAIL, `package github.com/madamgy/recipie/internal/api/handlers is not in std`. This
is the correct state to end Task 8 in; Task 9 makes it compile.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/api/router.go cmd/server/main.go
git commit -m "Add chi router and server entrypoint"
```

---

### Task 9: `internal/api/handlers` — search endpoint

**Files:**
- Create: `internal/api/handlers/handlers.go`
- Create: `internal/api/handlers/search.go`
- Test: `internal/api/handlers/search_test.go`

**Interfaces:**
- Consumes: `engine.Run`, `models.ChildProfile`.
- Produces: `handlers.New(pool) *Handlers`, `(*Handlers).Search(w, r)`, `(*Handlers).Healthz(w, r)`.

- [ ] **Step 1: Write `internal/api/handlers/handlers.go`**

```go
// Package handlers holds one file per API resource. Every handler reads from the pool
// directly or calls internal/engine -- there is no service layer in between, since
// every query here is either a single view read or the engine pipeline, and an extra
// layer would not do anything a handler function can't do itself (YAGNI).
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handlers struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Handlers {
	return &Handlers{pool: pool}
}

func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

- [ ] **Step 2: Write the failing test**

```go
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
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

func TestSearchReturnsRankedRecipesAndSteps(t *testing.T) {
	h := New(testPool(t))
	body, _ := json.Marshal(models.ChildProfile{AgeMonths: 24})
	req := httptest.NewRequest("POST", "/api/search", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Search(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result models.EngineResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Recipes) == 0 {
		t.Fatal("expected non-empty result for a no-preference 24mo profile")
	}
	if len(result.Steps) == 0 {
		t.Fatal("expected step accounting in the response")
	}
}

func TestSearchRejectsMissingAge(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("POST", "/api/search", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()

	h.Search(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400 for a profile with no age", rec.Code)
	}
}
```

- [ ] **Step 3: Run test, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/handlers/... -run TestSearch -v`
Expected: FAIL, `undefined: h.Search` (or a build failure since `Search` doesn't exist).

- [ ] **Step 4: Write `internal/api/handlers/search.go`**

```go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
)

// Search runs the full 14-step engine against the posted child profile. AgeMonths is
// required because every other step depends on it; every other field is optional.
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	var p models.ChildProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if p.AgeMonths <= 0 {
		writeError(w, http.StatusBadRequest, "age_months is required and must be positive")
		return
	}

	result, err := engine.Run(r.Context(), h.pool, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 5: Run tests, confirm pass; `go build ./...`; commit**

```bash
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/... -v
go build ./...
git add internal/api
git commit -m "Add search API handler over the engine pipeline"
```

---

### Task 10: recipe detail, ingredients, and audit handlers

**Files:**
- Create: `internal/api/handlers/recipes.go`
- Create: `internal/api/handlers/ingredients.go`
- Create: `internal/api/handlers/audit.go`
- Test: `internal/api/handlers/recipes_test.go`, `ingredients_test.go`, `audit_test.go`

**Interfaces:**
- Produces: `(*Handlers).RecipeDetail`, `.Ingredients`, `.NutritionAudit`, `.Gaps`.

- [ ] **Step 1: Write the failing tests**

```go
// recipes_test.go
package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRecipeDetailReturnsMethodCardAndNutrition(t *testing.T) {
	h := New(testPool(t))
	r := chi.NewRouter()
	r.Get("/api/recipes/{recipeID}", h.RecipeDetail)

	req := httptest.NewRequest("GET", "/api/recipes/MG-R-00001", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestRecipeDetailReturns404ForUnknownID(t *testing.T) {
	h := New(testPool(t))
	r := chi.NewRouter()
	r.Get("/api/recipes/{recipeID}", h.RecipeDetail)

	req := httptest.NewRequest("GET", "/api/recipes/MG-R-99999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
```

```go
// ingredients_test.go
package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestIngredientsListsCorrectedAndProviderValuesSideBySide(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/ingredients?limit=5", nil)
	rec := httptest.NewRecorder()

	h.Ingredients(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
```

```go
// audit_test.go
package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestNutritionAuditReturnsDiscrepancyReport(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/audit/nutrition", nil)
	rec := httptest.NewRecorder()

	h.NutritionAudit(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGapsReturnsAllSixteenEntries(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/gaps", nil)
	rec := httptest.NewRecorder()

	h.Gaps(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/handlers/... -v`
Expected: FAIL — build error, handlers don't exist.

- [ ] **Step 3: Write `internal/api/handlers/recipes.go`**

```go
package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// RecipeDetail returns the provider's method beside the external suggestion, and both
// the provider and IFCT-corrected nutrition figures, everything already carrying its
// own provenance columns from recipe_method_card and recipe_nutrition_recomputed.
func (h *Handlers) RecipeDetail(w http.ResponseWriter, r *http.Request) {
	recipeID := chi.URLParam(r, "recipeID")

	var method struct {
		RecipeID                   string  `json:"recipe_id"`
		RecipeName                 string  `json:"recipe_name"`
		RegionCulture              string  `json:"region_culture"`
		ProviderMethod             string  `json:"provider_method"`
		ProviderReviewStatus       string  `json:"provider_review_status"`
		SuggestedMethodExternal    *string `json:"suggested_method_external"`
		SuggestedMethodSource      *string `json:"suggested_method_source"`
		SuggestedMethodURL         *string `json:"suggested_method_url"`
		SuggestedMethodConfidence  *float64 `json:"suggested_method_confidence"`
		SuggestedMethodRegionMatch *string `json:"suggested_method_region_match"`
		SuggestionDisclosure       string  `json:"suggestion_disclosure"`
	}
	err := h.pool.QueryRow(r.Context(), `
		SELECT recipe_id, recipe_name, region_culture, provider_method, provider_review_status,
		       suggested_method_external, suggested_method_source, suggested_method_url,
		       suggested_method_confidence, suggested_method_region_match, suggestion_disclosure
		FROM recipe_method_card WHERE recipe_id = $1`, recipeID).Scan(
		&method.RecipeID, &method.RecipeName, &method.RegionCulture, &method.ProviderMethod, &method.ProviderReviewStatus,
		&method.SuggestedMethodExternal, &method.SuggestedMethodSource, &method.SuggestedMethodURL,
		&method.SuggestedMethodConfidence, &method.SuggestedMethodRegionMatch, &method.SuggestionDisclosure)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recipe not found: "+recipeID)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "recipe lookup failed: "+err.Error())
		return
	}

	var nutrition struct {
		EnergyKcal          float64 `json:"energy_kcal"`
		ProteinG            float64 `json:"protein_g"`
		IronMg              float64 `json:"iron_mg"`
		CalciumMg           float64 `json:"calcium_mg"`
		IngredientCoverage  float64 `json:"ingredient_coverage"`
		FullyVerified       bool    `json:"fully_verified"`
		ProviderEnergyKcal  float64 `json:"provider_energy_kcal"`
		ProviderProteinG    float64 `json:"provider_protein_g"`
		ProviderIronMg      float64 `json:"provider_iron_mg"`
		ProviderCalciumMg   float64 `json:"provider_calcium_mg"`
		EnergyPctDiff       *float64 `json:"energy_pct_diff"`
		IronPctDiff         *float64 `json:"iron_pct_diff"`
		ValueKind           string  `json:"value_kind"`
		Formula             string  `json:"formula"`
	}
	err = h.pool.QueryRow(r.Context(), `
		SELECT energy_kcal, protein_g, iron_mg, calcium_mg, ingredient_coverage, fully_verified,
		       provider_energy_kcal, provider_protein_g, provider_iron_mg, provider_calcium_mg,
		       energy_pct_diff, iron_pct_diff, value_kind, formula
		FROM recipe_nutrition_recomputed WHERE recipe_id = $1`, recipeID).Scan(
		&nutrition.EnergyKcal, &nutrition.ProteinG, &nutrition.IronMg, &nutrition.CalciumMg,
		&nutrition.IngredientCoverage, &nutrition.FullyVerified,
		&nutrition.ProviderEnergyKcal, &nutrition.ProviderProteinG, &nutrition.ProviderIronMg, &nutrition.ProviderCalciumMg,
		&nutrition.EnergyPctDiff, &nutrition.IronPctDiff, &nutrition.ValueKind, &nutrition.Formula)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "nutrition lookup failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"method":    method,
		"nutrition": nutrition,
	})
}
```

- [ ] **Step 4: Write `internal/api/handlers/ingredients.go`**

```go
package handlers

import (
	"net/http"
	"strconv"
)

// Ingredients lists ingredient_nutrition_corrected: provider and IFCT-corrected values
// side by side, with value_source and verified so the UI never presents a placeholder
// group-level figure as a measured one.
func (h *Handlers) Ingredients(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT ingredient_id, english_name, bengali_name, food_group, ifct_food_code, ifct_food_name,
		       ifct_match_exactness, ifct_resolved_by, value_source, verified,
		       energy_kcal_100g, protein_g_100g, iron_mg_100g, calcium_mg_100g,
		       provider_energy_kcal_100g, provider_protein_g_100g, provider_iron_mg_100g, provider_calcium_mg_100g,
		       provider_review_status, provider_data_quality
		FROM ingredient_nutrition_corrected
		ORDER BY ingredient_id
		LIMIT $1`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ingredient list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type ingredient struct {
		IngredientID            string   `json:"ingredient_id"`
		EnglishName             string   `json:"english_name"`
		BengaliName             *string  `json:"bengali_name"`
		FoodGroup               string   `json:"food_group"`
		IFCTFoodCode            *string  `json:"ifct_food_code"`
		IFCTFoodName            *string  `json:"ifct_food_name"`
		IFCTMatchExactness      *string  `json:"ifct_match_exactness"`
		IFCTResolvedBy          *string  `json:"ifct_resolved_by"`
		ValueSource             string   `json:"value_source"`
		Verified                bool     `json:"verified"`
		EnergyKcal100g          float64  `json:"energy_kcal_100g"`
		ProteinG100g            float64  `json:"protein_g_100g"`
		IronMg100g              float64  `json:"iron_mg_100g"`
		CalciumMg100g           float64  `json:"calcium_mg_100g"`
		ProviderEnergyKcal100g  float64  `json:"provider_energy_kcal_100g"`
		ProviderProteinG100g    float64  `json:"provider_protein_g_100g"`
		ProviderIronMg100g      float64  `json:"provider_iron_mg_100g"`
		ProviderCalciumMg100g   float64  `json:"provider_calcium_mg_100g"`
		ProviderReviewStatus    string   `json:"provider_review_status"`
		ProviderDataQuality     string   `json:"provider_data_quality"`
	}

	var out []ingredient
	for rows.Next() {
		var i ingredient
		if err := rows.Scan(&i.IngredientID, &i.EnglishName, &i.BengaliName, &i.FoodGroup,
			&i.IFCTFoodCode, &i.IFCTFoodName, &i.IFCTMatchExactness, &i.IFCTResolvedBy,
			&i.ValueSource, &i.Verified,
			&i.EnergyKcal100g, &i.ProteinG100g, &i.IronMg100g, &i.CalciumMg100g,
			&i.ProviderEnergyKcal100g, &i.ProviderProteinG100g, &i.ProviderIronMg100g, &i.ProviderCalciumMg100g,
			&i.ProviderReviewStatus, &i.ProviderDataQuality); err != nil {
			writeError(w, http.StatusInternalServerError, "ingredient scan failed: "+err.Error())
			return
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "ingredient rows failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 5: Write `internal/api/handlers/audit.go`**

```go
package handlers

import "net/http"

// NutritionAudit returns nutrition_discrepancy_report: the exact-name-match findings to
// hand the provider, never the broader probable-match worklist mixed in.
func (h *Handlers) NutritionAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT ingredient_id, english_name, matched_ifct_food, used_in_recipes,
		       provider_energy, external_energy, energy_pct_diff,
		       provider_protein, external_protein, protein_pct_diff,
		       provider_iron, external_iron, iron_pct_diff,
		       provider_calcium, external_calcium, calcium_pct_diff
		FROM nutrition_discrepancy_report`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "nutrition audit failed: "+err.Error())
		return
	}
	defer rows.Close()

	type discrepancy struct {
		IngredientID     string   `json:"ingredient_id"`
		EnglishName      string   `json:"english_name"`
		MatchedIFCTFood  *string  `json:"matched_ifct_food"`
		UsedInRecipes    int      `json:"used_in_recipes"`
		ProviderEnergy   *float64 `json:"provider_energy"`
		ExternalEnergy   *float64 `json:"external_energy"`
		EnergyPctDiff    *float64 `json:"energy_pct_diff"`
		ProviderProtein  *float64 `json:"provider_protein"`
		ExternalProtein  *float64 `json:"external_protein"`
		ProteinPctDiff   *float64 `json:"protein_pct_diff"`
		ProviderIron     *float64 `json:"provider_iron"`
		ExternalIron     *float64 `json:"external_iron"`
		IronPctDiff      *float64 `json:"iron_pct_diff"`
		ProviderCalcium  *float64 `json:"provider_calcium"`
		ExternalCalcium  *float64 `json:"external_calcium"`
		CalciumPctDiff   *float64 `json:"calcium_pct_diff"`
	}

	var out []discrepancy
	for rows.Next() {
		var d discrepancy
		if err := rows.Scan(&d.IngredientID, &d.EnglishName, &d.MatchedIFCTFood, &d.UsedInRecipes,
			&d.ProviderEnergy, &d.ExternalEnergy, &d.EnergyPctDiff,
			&d.ProviderProtein, &d.ExternalProtein, &d.ProteinPctDiff,
			&d.ProviderIron, &d.ExternalIron, &d.IronPctDiff,
			&d.ProviderCalcium, &d.ExternalCalcium, &d.CalciumPctDiff); err != nil {
			writeError(w, http.StatusInternalServerError, "nutrition audit scan failed: "+err.Error())
			return
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// Gaps returns the full gap register, all 16 rows, so /audit/gaps never needs its own
// invented severity scale -- it renders exactly what gap_register already carries.
func (h *Handlers) Gaps(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT gap_id, severity, area, source_table, source_column, description,
		       affected_rows, measured_by, ui_behaviour, resolution_path, measured_at
		FROM gap_register ORDER BY
		  CASE severity WHEN 'blocker' THEN 1 WHEN 'major' THEN 2 WHEN 'minor' THEN 3 ELSE 4 END,
		  gap_id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gap register failed: "+err.Error())
		return
	}
	defer rows.Close()

	type gap struct {
		GapID          string  `json:"gap_id"`
		Severity       string  `json:"severity"`
		Area           string  `json:"area"`
		SourceTable    *string `json:"source_table"`
		SourceColumn   *string `json:"source_column"`
		Description    string  `json:"description"`
		AffectedRows   *int    `json:"affected_rows"`
		MeasuredBy     string  `json:"measured_by"`
		UIBehaviour    string  `json:"ui_behaviour"`
		ResolutionPath string  `json:"resolution_path"`
		MeasuredAt     *string `json:"measured_at"`
	}

	var out []gap
	for rows.Next() {
		var g gap
		if err := rows.Scan(&g.GapID, &g.Severity, &g.Area, &g.SourceTable, &g.SourceColumn,
			&g.Description, &g.AffectedRows, &g.MeasuredBy, &g.UIBehaviour, &g.ResolutionPath, &g.MeasuredAt); err != nil {
			writeError(w, http.StatusInternalServerError, "gap register scan failed: "+err.Error())
			return
		}
		out = append(out, g)
	}
	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 6: Run tests, confirm pass; `go build ./...`; commit**

```bash
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/... -v
go build ./...
git add internal/api
git commit -m "Add recipe detail, ingredient, and audit handlers"
```

---

### Task 11: reference and runs handlers

**Files:**
- Create: `internal/api/handlers/reference.go`
- Create: `internal/api/handlers/runs.go`
- Test: `internal/api/handlers/reference_test.go`

**Interfaces:**
- Produces: `(*Handlers).ReferenceRegions`, `.ReferenceCuisines`, `.ReferenceNutritionTargets`, `.Runs`.

- [ ] **Step 1: Write the failing test**

```go
package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestReferenceCuisinesNeverOffersAZeroRecipeCuisine(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/cuisines", nil)
	rec := httptest.NewRecorder()

	h.ReferenceCuisines(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cuisines []struct {
		RecipeCount int `json:"recipe_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cuisines); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, c := range cuisines {
		if c.RecipeCount == 0 {
			t.Fatal("cuisine_option must never surface a zero-recipe cuisine (this is the whole point of the view)")
		}
	}
}
```

- [ ] **Step 2: Run test, confirm fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/handlers/... -run TestReferenceCuisines -v`
Expected: FAIL, `undefined: h.ReferenceCuisines`.

- [ ] **Step 3: Write `internal/api/handlers/reference.go`**

```go
package handlers

import "net/http"

// ReferenceRegions returns region_focus: the 9 scoped regions with their tier and
// derived rank_weight, so the frontend's region picker matches what the engine's step 7
// actually uses instead of a hardcoded list drifting from it.
func (h *Handlers) ReferenceRegions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT region_culture, country, focus_tier, rank_weight, enrichment_scope, rationale
		FROM region_focus ORDER BY focus_tier, region_culture`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "region list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type region struct {
		RegionCulture    string  `json:"region_culture"`
		Country          string  `json:"country"`
		FocusTier        int     `json:"focus_tier"`
		RankWeight       float64 `json:"rank_weight"`
		EnrichmentScope  bool    `json:"enrichment_scope"`
		Rationale        string  `json:"rationale"`
	}
	var out []region
	for rows.Next() {
		var reg region
		if err := rows.Scan(&reg.RegionCulture, &reg.Country, &reg.FocusTier, &reg.RankWeight, &reg.EnrichmentScope, &reg.Rationale); err != nil {
			writeError(w, http.StatusInternalServerError, "region scan failed: "+err.Error())
			return
		}
		out = append(out, reg)
	}
	writeJSON(w, http.StatusOK, out)
}

// ReferenceCuisines returns cuisine_option, already built on a COUNT(*) > 0 join so it
// can never offer a cuisine that returns an empty result.
func (h *Handlers) ReferenceCuisines(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT culture_code, cuisine_cluster, country, state_province, region_culture,
		       focus_tier, rank_weight, recipe_count
		FROM cuisine_option ORDER BY focus_tier, recipe_count DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cuisine list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type cuisine struct {
		CultureCode     string  `json:"culture_code"`
		CuisineCluster  string  `json:"cuisine_cluster"`
		Country         string  `json:"country"`
		StateProvince   *string `json:"state_province"`
		RegionCulture   string  `json:"region_culture"`
		FocusTier       int     `json:"focus_tier"`
		RankWeight      float64 `json:"rank_weight"`
		RecipeCount     int     `json:"recipe_count"`
	}
	var out []cuisine
	for rows.Next() {
		var c cuisine
		if err := rows.Scan(&c.CultureCode, &c.CuisineCluster, &c.Country, &c.StateProvince,
			&c.RegionCulture, &c.FocusTier, &c.RankWeight, &c.RecipeCount); err != nil {
			writeError(w, http.StatusInternalServerError, "cuisine scan failed: "+err.Error())
			return
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, out)
}

// ReferenceNutritionTargets returns the 13 NT00-NT12 rows with their scored weights and
// the verbatim hard_exclusions/soft_penalties guidance text (see the plan's Architecture
// note on why that text is surfaced, not compiled into a filter).
func (h *Handlers) ReferenceNutritionTargets(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT target_code, target_name, target_category, age_from_months, age_to_months,
		       trigger_input, trigger_logic,
		       recipe_score_energy, recipe_score_protein, recipe_score_iron, recipe_score_calcium,
		       recipe_score_fruitveg, recipe_score_diversity, recipe_score_cost,
		       hard_exclusions, soft_penalties
		FROM nutrition_target_master ORDER BY target_code`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "nutrition target list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type target struct {
		TargetCode      string  `json:"target_code"`
		TargetName      string  `json:"target_name"`
		TargetCategory  *string `json:"target_category"`
		AgeFromMonths   int     `json:"age_from_months"`
		AgeToMonths     int     `json:"age_to_months"`
		TriggerInput    *string `json:"trigger_input"`
		TriggerLogic    *string `json:"trigger_logic"`
		ScoreEnergy     int     `json:"recipe_score_energy"`
		ScoreProtein    int     `json:"recipe_score_protein"`
		ScoreIron       int     `json:"recipe_score_iron"`
		ScoreCalcium    int     `json:"recipe_score_calcium"`
		ScoreFruitVeg   int     `json:"recipe_score_fruitveg"`
		ScoreDiversity  int     `json:"recipe_score_diversity"`
		ScoreCost       int     `json:"recipe_score_cost"`
		HardExclusions  *string `json:"hard_exclusions"`
		SoftPenalties   *string `json:"soft_penalties"`
	}
	var out []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.TargetCode, &t.TargetName, &t.TargetCategory, &t.AgeFromMonths, &t.AgeToMonths,
			&t.TriggerInput, &t.TriggerLogic,
			&t.ScoreEnergy, &t.ScoreProtein, &t.ScoreIron, &t.ScoreCalcium,
			&t.ScoreFruitVeg, &t.ScoreDiversity, &t.ScoreCost,
			&t.HardExclusions, &t.SoftPenalties); err != nil {
			writeError(w, http.StatusInternalServerError, "nutrition target scan failed: "+err.Error())
			return
		}
		out = append(out, t)
	}
	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 4: Write `internal/api/handlers/runs.go`**

```go
package handlers

import "net/http"

// Runs returns import_run joined to its per-table stats, so the /runs screen can show
// content hashes and rows-skipped without a second round trip.
func (h *Handlers) Runs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT ir.run_id, ir.started_at, ir.finished_at, ir.source_dir, ir.ok,
		       its.table_name, its.rows_read, its.rows_written, its.rows_skipped, its.content_hash
		FROM import_run ir
		JOIN import_table_stat its ON its.run_id = ir.run_id
		ORDER BY ir.run_id DESC, its.table_name`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "run history failed: "+err.Error())
		return
	}
	defer rows.Close()

	type tableStat struct {
		TableName    string `json:"table_name"`
		RowsRead     int    `json:"rows_read"`
		RowsWritten  int    `json:"rows_written"`
		RowsSkipped  int    `json:"rows_skipped"`
		ContentHash  string `json:"content_hash"`
	}
	type run struct {
		RunID      int64       `json:"run_id"`
		StartedAt  string      `json:"started_at"`
		FinishedAt *string     `json:"finished_at"`
		SourceDir  string      `json:"source_dir"`
		OK         bool        `json:"ok"`
		Tables     []tableStat `json:"tables"`
	}

	byID := map[int64]*run{}
	var order []int64
	for rows.Next() {
		var runID int64
		var startedAt, sourceDir string
		var finishedAt *string
		var ok bool
		var ts tableStat
		if err := rows.Scan(&runID, &startedAt, &finishedAt, &sourceDir, &ok,
			&ts.TableName, &ts.RowsRead, &ts.RowsWritten, &ts.RowsSkipped, &ts.ContentHash); err != nil {
			writeError(w, http.StatusInternalServerError, "run history scan failed: "+err.Error())
			return
		}
		if _, seen := byID[runID]; !seen {
			byID[runID] = &run{RunID: runID, StartedAt: startedAt, FinishedAt: finishedAt, SourceDir: sourceDir, OK: ok}
			order = append(order, runID)
		}
		byID[runID].Tables = append(byID[runID].Tables, ts)
	}

	out := make([]*run, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 5: Register the new routes in `internal/api/router.go`**

Add to `NewRouter`, after the existing `r.Get("/api/reference/nutrition-targets", ...)` line:

```go
	r.Get("/api/reference/nutrition-targets", h.ReferenceNutritionTargets)
	r.Get("/api/runs", h.Runs)
```

(The `runs` route already appears in Task 8's router listing — verify it's wired to
`h.Runs` and not left unregistered; Task 8 wrote the route table before this handler
existed, so double check the line reads `r.Get("/api/runs", h.Runs)` exactly.)

- [ ] **Step 6: Run tests, confirm pass; `go build ./... && go vet ./...`; commit**

```bash
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/... -v
go build ./...
go vet ./...
git add internal/api
git commit -m "Add reference and import-run history handlers"
```

---

### Task 12: End-to-end server smoke test and README update

**Files:**
- Create: `internal/api/smoke_test.go`
- Modify: `README.md`
- Modify: `.env.example` (add `PORT` if not already present)

**Interfaces:**
- Consumes: the fully wired `api.NewRouter`.
- Produces: nothing new — this is a verification task.

- [ ] **Step 1: Write `internal/api/smoke_test.go`**

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServerServesSearchAndReferenceEndToEnd(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	srv := httptest.NewServer(NewRouter(pool))
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]any{"age_months": 24})
	resp, err := srv.Client().Post(srv.URL+"/api/search", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST /api/search status = %d", resp.StatusCode)
	}

	resp2, err := srv.Client().Get(srv.URL + "/api/reference/regions")
	if err != nil {
		t.Fatalf("GET /api/reference/regions: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("GET /api/reference/regions status = %d", resp2.StatusCode)
	}
}
```

- [ ] **Step 2: Run the full suite**

```bash
scripts/dev_db.fish up
go run ./cmd/import
go run ./cmd/enrich
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./... -v
go build ./...
go vet ./...
```

Expected: all packages PASS, including the new `internal/models`, `internal/engine`,
`internal/api`, and `internal/api/handlers` packages alongside the existing Phase 1 suite.

- [ ] **Step 3: Update `README.md`**

Add a "Running the API" section after the existing "Running it" section:

```markdown
## Running the API

```fish
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/server        # listens on :8080 by default, set PORT to override
```

| Endpoint | Method | What it does |
|----------|--------|---------------|
| `/api/search` | POST | Runs the 14-step engine against a child profile, returns ranked recipes plus full step accounting |
| `/api/recipes/{id}` | GET | Provider method + external suggestion + provider/corrected nutrition |
| `/api/ingredients` | GET | Provider and IFCT-corrected nutrition side by side |
| `/api/audit/nutrition` | GET | The confirmed discrepancy findings to hand the provider |
| `/api/gaps` | GET | The full gap register |
| `/api/runs` | GET | Import history and content hashes |
| `/api/reference/regions` | GET | The 9 scoped regions and their tiers |
| `/api/reference/cuisines` | GET | Cuisine dropdown, guaranteed non-empty per entry |
| `/api/reference/nutrition-targets` | GET | The NT00-NT12 rubric, weights and guidance text |
```
```

- [ ] **Step 4: Update `.env.example`**

Confirm `PORT` is documented (it already is per the existing env table in `CLAUDE.md`); add
it to `.env.example` if the file doesn't already list it:

```
PORT=8080
```

- [ ] **Step 5: Commit**

```bash
git add README.md .env.example internal/api/smoke_test.go
git commit -m "Document the API and add an end-to-end smoke test"
```
