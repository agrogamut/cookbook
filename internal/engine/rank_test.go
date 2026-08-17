package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"

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
