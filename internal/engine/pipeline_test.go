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
			if len(result.Steps) != 14 {
				t.Fatalf("persona %q: expected 14 recorded steps (1-13, with step 4 recorded "+
					"twice as a hard filter and a preference ranker; step 8 has no data source "+
					"and step 14 is a human release gate, neither runs in the engine), got %d",
					c.name, len(result.Steps))
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

// TestRunBlockedResultHasEmptyNotNilRecipes pins a fix found while building the
// frontend's TypeScript client: EngineResult.Recipes is typed RankedRecipe[] (never
// null) on the TS side, but the blocked early-return in pipeline.go used to leave
// Recipes at its Go zero value (nil), which marshals to JSON null rather than [] --
// exactly the "why is this empty" scenario the frontend's most important screen has to
// render, not crash on.
func TestRunBlockedResultHasEmptyNotNilRecipes(t *testing.T) {
	pool := testPool(t)
	result, err := Run(context.Background(), pool, models.ChildProfile{
		AgeMonths: 36, ClinicalFlags: map[string]string{"CKD": "Yes"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Blocked {
		t.Fatal("CKD must block generation; test assumption invalid if this changes")
	}
	if result.Recipes == nil {
		t.Fatal("Recipes must be an empty slice, not nil, so it marshals to JSON [] rather than null")
	}
	if len(result.Recipes) != 0 {
		t.Fatalf("a blocked result must carry zero recipes, got %d", len(result.Recipes))
	}
}

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
