package engine

import (
	"context"
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
