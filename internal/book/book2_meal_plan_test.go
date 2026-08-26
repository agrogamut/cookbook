package book

import (
	"reflect"
	"testing"
)

// TestWeeklyMealPlanFromSectionsSkipsEmptyCategories pins the pure list logic behind B1-006's
// "This week's plan" table: a category with real cards becomes one MealPlanCategory carrying
// its real dish titles and serving sizes, and a category with zero cards (a mapped chapter this
// child's own filters emptied) is left out rather than printed as an empty group.
func TestWeeklyMealPlanFromSectionsSkipsEmptyCategories(t *testing.T) {
	sections := []MealSection{
		{
			Title: "Breakfast",
			Recipes: []RecipeCard{
				{Title: "Bengali Rice & Fish Khichuri", Serving: "150 g"},
				{Title: "Soft Egg Bhurji", Serving: "100 g"},
			},
		},
		{Title: "Lunch", Recipes: nil},
		{
			Title:   "Dinner",
			Recipes: []RecipeCard{{Title: "Dal & Vegetable Bowl", Serving: "180 g"}},
		},
	}

	got := WeeklyMealPlanFromSections(sections)

	want := []MealPlanCategory{
		{Title: "Breakfast", Rows: []MealPlanRow{
			{Dish: "Bengali Rice & Fish Khichuri", Serving: "150 g"},
			{Dish: "Soft Egg Bhurji", Serving: "100 g"},
		}},
		{Title: "Dinner", Rows: []MealPlanRow{
			{Dish: "Dal & Vegetable Bowl", Serving: "180 g"},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("WeeklyMealPlanFromSections =\n%+v\nwant\n%+v", got, want)
	}
}

func TestWeeklyMealPlanFromSectionsReturnsNilForNoRecipesAnywhere(t *testing.T) {
	got := WeeklyMealPlanFromSections([]MealSection{{Title: "Breakfast"}, {Title: "Lunch"}})
	if len(got) != 0 {
		t.Fatalf("WeeklyMealPlanFromSections with no recipes anywhere = %+v, want empty", got)
	}
}
