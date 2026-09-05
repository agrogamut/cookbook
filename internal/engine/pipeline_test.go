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
			if len(result.Recipes) == 0 {
				t.Fatalf("persona %q returned zero recipes; ranker steps must never collapse a result set. Steps: %+v", c.name, result.Steps)
			}
			if len(result.Steps) != 17 {
				t.Fatalf("persona %q: expected 17 recorded steps (1-13, with steps 1, 2 and 4 "+
					"each recorded twice -- a first half plus a ranker half -- and step 3 "+
					"recorded twice as the special-care row plus the clinical rule record; "+
					"step 8 has no data source and step 14 is a human release gate, neither "+
					"runs in the engine), got %d", c.name, len(result.Steps))
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

// TestRunNeverReturnsNilRecipes pins a fix found while building the frontend's TypeScript
// client: EngineResult.Recipes is typed RankedRecipe[] (never null) on the TS side, but a
// pipeline path that leaves Recipes at its Go zero value (nil) marshals to JSON null rather
// than [] -- exactly the "why is this empty" scenario the frontend's most important screen
// has to render, not crash on.
//
// It used to reach that path through the clinical block, which is gone (SP1). The property
// itself is not: the confirmed-allergen and diet hard filters are untouched and can still
// collapse the pool to nothing, and the guard at the end of Run is what covers them. This
// version drives the narrowest profile the engine still supports rather than a blocked one,
// and asserts non-nil whatever the length turns out to be -- the length is not the point,
// the JSON shape is.
func TestRunNeverReturnsNilRecipes(t *testing.T) {
	pool := testPool(t)

	// Every allergen group the corpus actually screens on, so the step-2 hard filter
	// excludes as much as it is capable of excluding. Read live rather than hardcoded: a
	// group with a NULL corpus_tag screens nothing, and listing one would make this profile
	// look narrower than it is.
	rows, err := pool.Query(context.Background(), `
		SELECT DISTINCT allergen_group FROM allergen_tag_vocabulary WHERE corpus_tag IS NOT NULL`)
	if err != nil {
		t.Fatalf("allergen groups: %v", err)
	}
	var groups []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			rows.Close()
			t.Fatalf("scan: %v", err)
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	result, err := Run(context.Background(), pool, models.ChildProfile{
		AgeMonths: 36, DietType: "Vegetarian", Vegan: true, Allergens: groups,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Recipes == nil {
		t.Fatal("Recipes must be an empty slice, not nil, so it marshals to JSON [] rather than null")
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
