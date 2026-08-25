package aidraft

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestNewClientWithoutKeyIsDisabled pins the contract internal/book relies on: an empty
// GEMINI_API_KEY never panics or requires a nil check downstream, it just always reports
// ErrDraftingUnavailable.
func TestNewClientWithoutKeyIsDisabled(t *testing.T) {
	d, err := NewClient(context.Background(), "")
	if err != nil {
		t.Fatalf("NewClient(\"\") returned an error, want a disabled Drafter: %v", err)
	}

	if _, err := d.DraftModificationNote(context.Background(), ModificationRequest{}); !errors.Is(err, ErrDraftingUnavailable) {
		t.Errorf("DraftModificationNote on disabled client = %v, want ErrDraftingUnavailable", err)
	}
	if _, err := d.DraftInventedRecipe(context.Background(), InventedRecipeRequest{}); !errors.Is(err, ErrDraftingUnavailable) {
		t.Errorf("DraftInventedRecipe on disabled client = %v, want ErrDraftingUnavailable", err)
	}
}

// TestModificationPromptGroundsOnProviderTextOnly pins that the prompt carries the provider's
// own BookAction/RequiredModification text verbatim, and nothing invented is stitched in
// around it -- the whole safety argument for this feature rests on the grounding block being
// exactly the DB row, so a change here needs to be a visible diff.
func TestModificationPromptGroundsOnProviderTextOnly(t *testing.T) {
	req := ModificationRequest{
		RuleID:               "CR-014",
		ClinicalDomain:       "Constipation",
		BookAction:           "Increase fibre and fluids gradually.",
		RequiredModification: "Offer stewed fruit instead of raw where texture allows.",
		RecipeName:           "West Bengal Rice & Lentil Khichuri",
		ChildAgeMonths:       14,
	}
	prompt := buildModificationPrompt(req)

	for _, want := range []string{req.BookAction, req.RequiredModification, req.RecipeName, "Constipation"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing verbatim grounding text %q\nprompt:\n%s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "Do not add any clinical claim") {
		t.Error("prompt is missing the paraphrase-only instruction")
	}
}

// TestInventedRecipeSchemaConstrainsToAllowedIngredients pins the API-level guardrail: the
// schema's ingredient_id enum is exactly the allowed set handed in, nothing more. This is the
// first of two checks (the second is a hard reject in internal/book) -- this test only proves
// the schema construction itself doesn't silently widen or drop an id.
func TestInventedRecipeSchemaConstrainsToAllowedIngredients(t *testing.T) {
	allowed := []AllowedIngredient{
		{IngredientID: "ING0012", Name: "Moong dal"},
		{IngredientID: "ING0044", Name: "Rice"},
	}
	archetypes := []string{"MARK-KHICHURI", "MARK-PUREE"}

	schema := inventedRecipeSchema(allowed, archetypes)

	ingredientsProp := schema.Properties["ingredients"]
	if ingredientsProp == nil {
		t.Fatal("schema has no \"ingredients\" property")
	}
	idEnum := ingredientsProp.Items.Properties["ingredient_id"].Enum
	if len(idEnum) != len(allowed) {
		t.Fatalf("ingredient_id enum has %d entries, want %d", len(idEnum), len(allowed))
	}
	for i, a := range allowed {
		if idEnum[i] != a.IngredientID {
			t.Errorf("ingredient_id enum[%d] = %q, want %q", i, idEnum[i], a.IngredientID)
		}
	}

	formatEnum := schema.Properties["dish_format_id"].Enum
	if len(formatEnum) != len(archetypes) {
		t.Fatalf("dish_format_id enum has %d entries, want %d", len(formatEnum), len(archetypes))
	}
}

// TestInventedRecipePromptListsOnlyAllowedIngredients guards against the prose half of the
// prompt drifting out of sync with the schema -- both need to name the same allowed set, or
// the model gets a mixed signal about what it may use.
func TestInventedRecipePromptListsOnlyAllowedIngredients(t *testing.T) {
	req := InventedRecipeRequest{
		MealCategory:         "Breakfast",
		ChildAgeMonths:       18,
		DietType:             "Vegetarian",
		RequiredTexture:      "Soft mashed",
		AllowedIngredients:   []AllowedIngredient{{IngredientID: "ING0012", Name: "Moong dal"}},
		DishFormatArchetypes: []string{"MARK-KHICHURI"},
	}
	prompt := buildInventedRecipePrompt(req)

	if !strings.Contains(prompt, "ING0012") || !strings.Contains(prompt, "Moong dal") {
		t.Errorf("prompt does not list the allowed ingredient:\n%s", prompt)
	}
	if !strings.Contains(prompt, "MARK-KHICHURI") {
		t.Errorf("prompt does not list the allowed dish format:\n%s", prompt)
	}
	if !strings.Contains(prompt, "ONLY ingredients from the allowed list") {
		t.Error("prompt is missing the allowed-ingredients-only instruction")
	}
}

// TestFoodGroupPriorityPromptGroundsOnRealSourcesOnly pins that the prompt carries every
// fed-in nutrient action verbatim and the exact closed macro-group vocabulary, plus the
// paraphrase-only / closed-vocabulary instructions -- the same "grounding must be a visible
// diff" discipline TestModificationPromptGroundsOnProviderTextOnly holds.
func TestFoodGroupPriorityPromptGroundsOnRealSourcesOnly(t *testing.T) {
	req := FoodGroupPriorityRequest{
		TargetCode: "NT01",
		TargetName: "Complementary-feeding nutrient density",
		Actions: map[string]string{
			"Iron":    "High priority",
			"Calcium": "High priority",
		},
		MacroGroups: []string{"Pulse & legume", "Dairy", "Vegetable"},
	}
	prompt := buildFoodGroupPriorityPrompt(req)

	for _, want := range []string{
		req.TargetName, req.TargetCode, "Iron: High priority", "Calcium: High priority",
		"Pulse & legume", "Dairy", "Vegetable",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing verbatim grounding text %q\nprompt:\n%s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "ONLY food-group names from the list") {
		t.Error("prompt is missing the closed-vocabulary instruction")
	}
	if !strings.Contains(prompt, "Do not add a claim about a nutrient") {
		t.Error("prompt is missing the paraphrase-only instruction")
	}
}

// TestFoodGroupPrioritySchemaConstrainsToRealMacroGroups pins the API-level guardrail: the
// schema's food_group enum is exactly the real macro-group list handed in, nothing more --
// the same enum-construction check TestInventedRecipeSchemaConstrainsToAllowedIngredients
// runs for ingredient_id.
func TestFoodGroupPrioritySchemaConstrainsToRealMacroGroups(t *testing.T) {
	macroGroups := []string{"Pulse & legume", "Dairy", "Vegetable"}
	schema := foodGroupPrioritySchema(macroGroups)

	rowsProp := schema.Properties["rows"]
	if rowsProp == nil || rowsProp.Items == nil {
		t.Fatal("schema has no rows/items definition")
	}
	foodGroupEnum := rowsProp.Items.Properties["food_group"].Enum
	if len(foodGroupEnum) != len(macroGroups) {
		t.Fatalf("food_group enum has %d entries, want %d", len(foodGroupEnum), len(macroGroups))
	}
	for i, g := range macroGroups {
		if foodGroupEnum[i] != g {
			t.Errorf("food_group enum[%d] = %q, want %q", i, foodGroupEnum[i], g)
		}
	}
}
