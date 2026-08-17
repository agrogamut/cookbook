package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

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

// TestDietFilterIsNestedNotCategorical pins the nesting rule: a declared practice may eat
// every diet_type at or below its own level, so each step up the chain is a superset of
// the one below. Before this fix the filter matched diet_type for equality, and a
// non-vegetarian profile received only the 111 dishes tagged Non-vegetarian rather than
// all 940 it is entitled to.
func TestDietFilterIsNestedNotCategorical(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Age 84 months: above the 6-9 year floor of the corpus's only egg recipe
	// (MG-R-00692), so the eggetarian superset is observable rather than filtered out
	// by age before the diet step ever runs.
	all, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 84})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}

	set := func(dietType string) map[string]bool {
		t.Helper()
		ids, _, err := dietFilter(ctx, pool, models.ChildProfile{DietType: dietType}, all)
		if err != nil {
			t.Fatalf("dietFilter %s: %v", dietType, err)
		}
		out := make(map[string]bool, len(ids))
		for _, id := range ids {
			out[id] = true
		}
		return out
	}

	veg := set("Vegetarian")
	egg := set("Eggetarian")
	non := set("Non-vegetarian")

	for _, c := range []struct {
		name             string
		subset, superset map[string]bool
	}{
		{"vegetarian ⊂ eggetarian", veg, egg},
		{"eggetarian ⊂ non-vegetarian", egg, non},
	} {
		if len(c.superset) < len(c.subset) {
			t.Errorf("%s: superset is smaller (%d) than subset (%d)", c.name, len(c.superset), len(c.subset))
		}
		for id := range c.subset {
			if !c.superset[id] {
				t.Errorf("%s: recipe %s is eatable by the narrower practice but missing from the wider one", c.name, id)
				break
			}
		}
	}

	// A non-vegetarian family is entitled to the whole age-eligible pool -- no dish in
	// this corpus requires more than a non-vegetarian eater.
	if len(non) != len(all) {
		t.Errorf("non-vegetarian must see every age-eligible recipe: got %d of %d", len(non), len(all))
	}
}

// TestDietFilterRejectsUnknownPractice keeps an unrecognized diet_type a 400 rather than
// a silently empty result list -- the failure mode the equality match had, where a typo
// returned zero recipes and looked like a legitimately narrow corpus.
func TestDietFilterRejectsUnknownPractice(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	all, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	_, _, err = dietFilter(ctx, pool, models.ChildProfile{DietType: "Pescatarian"}, all)
	if !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("unknown diet_type must return ErrInvalidProfile, got %v", err)
	}
}

// TestDietFilterVeganExcludesGheeByAllergenTag pins the fix for the final whole-branch
// review's ghee leak: Ghee (ING0060) is bucketed food_group='Fat', not 'Dairy', so the
// food-group exclusion alone misses it. It is correctly tagged
// ingredient_allergen_tag='Milk' on its recipe_ingredient_mapping rows, so the vegan
// filter must also exclude on that tag. Before this fix, 75 recipes containing ghee
// passed the vegan filter untouched.
func TestDietFilterVeganExcludesGheeByAllergenTag(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var containsGhee int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_ingredient_mapping WHERE ingredient_id = 'ING0060'`).Scan(&containsGhee)
	if err != nil {
		t.Fatalf("count ghee recipes: %v", err)
	}
	if containsGhee == 0 {
		t.Skip("no recipe in this dataset contains ghee (ING0060); nothing to assert")
	}

	all, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	vegan, _, err := dietFilter(ctx, pool, models.ChildProfile{DietType: "Vegetarian", Vegan: true}, all)
	if err != nil {
		t.Fatalf("dietFilter vegan: %v", err)
	}

	ids := make(map[string]bool, len(vegan))
	for _, id := range vegan {
		ids[id] = true
	}
	rows, err := pool.Query(ctx, `SELECT recipe_id FROM recipe_ingredient_mapping WHERE ingredient_id = 'ING0060'`)
	if err != nil {
		t.Fatalf("query ghee recipe ids: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if ids[id] {
			t.Fatalf("recipe %s contains ghee (a Milk allergen) and must not pass the vegan filter", id)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
}
