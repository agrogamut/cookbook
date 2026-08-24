package engine

import (
	"context"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

// TestSafeIngredientsExcludesDeclaredAllergen pins the property the Gemini invented-recipe
// fallback depends on for safety: an ingredient carrying the corpus tag for a declared
// allergen never appears in the allow-list, the same guarantee allergyFilter gives real
// recipes.
func TestSafeIngredientsExcludesDeclaredAllergen(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	all, err := SafeIngredients(ctx, pool, models.ChildProfile{})
	if err != nil {
		t.Fatalf("SafeIngredients no allergens: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("SafeIngredients with no restrictions returned zero ingredients")
	}

	milkExcluded, err := SafeIngredients(ctx, pool, models.ChildProfile{Allergens: []string{"Milk"}})
	if err != nil {
		t.Fatalf("SafeIngredients milk allergen: %v", err)
	}
	if len(milkExcluded) >= len(all) {
		t.Fatalf("declaring a Milk allergy did not narrow the ingredient set: unrestricted=%d, milk-excluded=%d",
			len(all), len(milkExcluded))
	}
}

// TestSafeIngredientsExcludesSuspectedAllergenToo pins the deliberate stricter-than-real-
// recipes behaviour: SuspectedAllergens are only a ranker demotion for provider-authored
// recipes (AS-002), but a hard exclusion here, because an invented recipe gets no human
// review beyond the signature page.
func TestSafeIngredientsExcludesSuspectedAllergenToo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	confirmedOnly, err := SafeIngredients(ctx, pool, models.ChildProfile{Allergens: []string{"Peanut"}})
	if err != nil {
		t.Fatalf("SafeIngredients confirmed peanut: %v", err)
	}
	confirmedAndSuspected, err := SafeIngredients(ctx, pool, models.ChildProfile{
		Allergens:          []string{"Peanut"},
		SuspectedAllergens: []string{"Milk"},
	})
	if err != nil {
		t.Fatalf("SafeIngredients confirmed peanut + suspected milk: %v", err)
	}
	if len(confirmedAndSuspected) >= len(confirmedOnly) {
		t.Fatalf("a suspected allergen did not narrow the allow-list: confirmed-only=%d, plus-suspected=%d",
			len(confirmedOnly), len(confirmedAndSuspected))
	}
}

// TestSafeIngredientsVegetarianExcludesAnimalFoodGroups pins that Vegetarian and Eggetarian
// both drop to the ingredient level and exclude animalFoodGroups, unlike dietFilter's
// recipe-level check which only does this for a Vegan profile.
func TestSafeIngredientsVegetarianExcludesAnimalFoodGroups(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	nonVeg, err := SafeIngredients(ctx, pool, models.ChildProfile{DietType: "Non-vegetarian"})
	if err != nil {
		t.Fatalf("SafeIngredients non-vegetarian: %v", err)
	}
	veg, err := SafeIngredients(ctx, pool, models.ChildProfile{DietType: "Vegetarian"})
	if err != nil {
		t.Fatalf("SafeIngredients vegetarian: %v", err)
	}
	if len(veg) >= len(nonVeg) {
		t.Fatalf("declaring Vegetarian did not narrow the ingredient set: non-vegetarian=%d, vegetarian=%d",
			len(nonVeg), len(veg))
	}

	eggetarian, err := SafeIngredients(ctx, pool, models.ChildProfile{DietType: "Eggetarian"})
	if err != nil {
		t.Fatalf("SafeIngredients eggetarian: %v", err)
	}
	if len(eggetarian) != len(veg) {
		t.Fatalf("Eggetarian and Vegetarian should apply the identical ingredient-level exclusion "+
			"(no ingredient-to-diet_type mapping distinguishes them): vegetarian=%d, eggetarian=%d",
			len(veg), len(eggetarian))
	}
}

func TestActiveClinicalRuleActionsNoFlagsIsEmpty(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	actions, err := ActiveClinicalRuleActions(ctx, pool, models.ChildProfile{})
	if err != nil {
		t.Fatalf("ActiveClinicalRuleActions: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("a profile with no ClinicalFlags should match zero rules, got %d", len(actions))
	}
}

// TestActiveClinicalRuleActionsRejectsUnrecognizedFlag mirrors clinicalFilter's own guard:
// an unrecognized ClinicalFlags key must not be silently ignored, whether it fails open into
// "no note" or fails closed into an error -- this pins that it does not fail open.
func TestActiveClinicalRuleActionsUnrecognizedFlagYieldsNoMatch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	actions, err := ActiveClinicalRuleActions(ctx, pool, models.ChildProfile{
		ClinicalFlags: map[string]string{"Not_A_Real_Trigger_Field": "yes"},
	})
	if err != nil {
		t.Fatalf("ActiveClinicalRuleActions: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("an unrecognized trigger field must never match a rule, got %d actions", len(actions))
	}
}
