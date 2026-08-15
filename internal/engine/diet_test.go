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
