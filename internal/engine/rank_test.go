package engine

import (
	"context"
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
