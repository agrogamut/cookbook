package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

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

	// Prove the value was genuinely read from the database, not silently defaulted:
	// confirm it matches the live column exactly, and that the input pool was large
	// enough that a broken read (falling through to some other value) would show up.
	var providedText string
	if err := pool.QueryRow(ctx, `SELECT default_target_recipes FROM meal_category_target WHERE meal_category = $1`, "Lunch").Scan(&providedText); err != nil {
		t.Fatalf("reading meal_category_target directly: %v", err)
	}
	provided, err := strconv.Atoi(providedText)
	if err != nil {
		t.Fatalf("meal_category_target.default_target_recipes not numeric: %q", providedText)
	}
	if len(ranked) <= provided {
		t.Skip("candidate pool not larger than the provider target; cannot distinguish a genuine live read from a coincidental match")
	}
	if len(out) != provided {
		t.Fatalf("capToTarget must return exactly meal_category_target.default_target_recipes (%d) when the pool is larger, got %d", provided, len(out))
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

	// Prove real work happened, not a no-op pass-through: the note must report at
	// least one demotion, and the resulting order must differ from the input order
	// somewhere (a genuine demotion always moves at least one recipe).
	if strings.Contains(step.Note, "0 recipe(s) demoted") {
		t.Fatalf("expected at least one demotion on this dataset, got note: %q", step.Note)
	}
	reordered := false
	for i := range out {
		if out[i].RecipeID != ranked[i].RecipeID {
			reordered = true
			break
		}
	}
	if !reordered {
		t.Fatal("dedupe produced identical order to the input; expected at least one recipe to move after a demotion")
	}
}

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
