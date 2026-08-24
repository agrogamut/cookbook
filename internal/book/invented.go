package book

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
)

// inventedRecipeMinAgeMonths gates the whole fallback off below this age. There is no
// ingredient-level texture or choking-hazard rule anywhere in this codebase --
// engine.SafeIngredients's doc comment explains why one cannot safely be built today. A real
// recipe gets texture safety structurally, from recipe_master.texture and
// age_feeding_stage_master; an invented recipe has no row in either. Rather than invent a
// texture check this project has no verified source for, the fallback simply never runs below
// this age -- the same conservative direction steps 1 and 2 already take on real recipes.
const inventedRecipeMinAgeMonths = 24

// topUpInvented tries to fill a category's shortfall against cards with Gemini-invented
// recipes, one call per still-missing slot, stopping at the first failure rather than
// retrying indefinitely. cards may already hold zero or more real recipes -- the returned
// slice is always at least as long as the input. The returned note is empty whenever the
// target was fully reached (by real recipes, invention, or both); otherwise it names how many
// recipes are still missing and why the fallback stopped. Never runs when cat.Target is 0,
// which means "no cap" on this category, not "zero is the target".
func topUpInvented(ctx context.Context, pool *pgxpool.Pool, drafter aidraft.Drafter,
	cp models.ChildProfile, cat mealCategory, version string, cards []RecipeCard) ([]RecipeCard, string) {

	if cat.Target <= 0 || len(cards) >= cat.Target {
		return cards, ""
	}
	short := cat.Target - len(cards)
	for i := 0; i < short; i++ {
		invented, err := inventCard(ctx, pool, drafter, cp, cat, version, len(cards)+1)
		if err != nil {
			return cards, fmt.Sprintf(
				"%s (%s) is short %d recipe(s) of its %d-recipe target after real corpus "+
					"recipes were exhausted; AI-invented fallback unavailable or failed "+
					"validation: %v", cat.ID, cat.Name, short-i, cat.Target, err)
		}
		cards = append(cards, invented)
	}
	return cards, ""
}

// inventCard asks drafter for a fallback recipe constrained to this child's safe-ingredient
// allow-list, validates the response in Go, and returns a ready RecipeCard on success.
//
// Membership in engine.SafeIngredients's allow-list is the whole safety boundary here: it
// already excludes every ingredient carrying a declared or suspected allergen and, for a
// vegetarian/eggetarian/vegan diet, every animal-derived food group. Checking that every
// returned ingredient id is in that set (validateInventedRecipe) therefore re-verifies
// allergy and diet safety in one step, rather than needing a second allergen-overlap query --
// the allow-list was already built to be the answer to "is this ingredient safe for this
// child", not merely a convenience list.
//
// Any failure -- drafting unavailable, an ingredient outside the allow-list, a malformed
// response -- returns an error and no card. The caller reports the shortfall exactly the way
// an empty chapter always has; nothing here ever forces a failed-validation recipe through.
func inventCard(ctx context.Context, pool *pgxpool.Pool, drafter aidraft.Drafter,
	cp models.ChildProfile, cat mealCategory, version string, index int) (RecipeCard, error) {

	if cp.AgeMonths < inventedRecipeMinAgeMonths {
		return RecipeCard{}, fmt.Errorf(
			"invented recipe fallback does not run below %d months (no ingredient-level "+
				"texture/choking-hazard rule exists to check an invented recipe against)",
			inventedRecipeMinAgeMonths)
	}

	safe, err := engine.SafeIngredients(ctx, pool, cp)
	if err != nil {
		return RecipeCard{}, fmt.Errorf("safe ingredients: %w", err)
	}
	if len(safe) == 0 {
		return RecipeCard{}, fmt.Errorf("no safe ingredients for this child's profile")
	}

	allowed := make([]aidraft.AllowedIngredient, len(safe))
	allowedIDs := make(map[string]bool, len(safe))
	nameByID := make(map[string]string, len(safe))
	for i, s := range safe {
		allowed[i] = aidraft.AllowedIngredient{IngredientID: s.IngredientID, Name: s.Name}
		allowedIDs[s.IngredientID] = true
		nameByID[s.IngredientID] = s.Name
	}

	archetypes := MarkIDs()

	invented, err := drafter.DraftInventedRecipe(ctx, aidraft.InventedRecipeRequest{
		MealCategory:         cat.Name,
		ChildAgeMonths:       cp.AgeMonths,
		DietType:             cp.DietType,
		AllowedIngredients:   allowed,
		DishFormatArchetypes: archetypes,
	})
	if err != nil {
		return RecipeCard{}, fmt.Errorf("draft: %w", err)
	}

	if err := validateInventedRecipe(invented, allowedIDs, archetypes); err != nil {
		return RecipeCard{}, fmt.Errorf("validation: %w", err)
	}

	ingredients := make([]IngredientLine, 0, len(invented.Ingredients))
	for _, ing := range invented.Ingredients {
		ingredients = append(ingredients, IngredientLine{
			Name:     nameByID[ing.IngredientID],
			Quantity: fmt.Sprintf("%.0f g", ing.QuantityG),
		})
	}

	return RecipeCard{
		// AI- prefix keeps this out of the MG-R-##### namespace: it must never be mistaken
		// for a real recipe_master id, including by an operator searching for one later.
		// index is the card's position among this category's invented recipes, keeping the
		// id unique when a chapter needs more than one.
		RecipeID:       fmt.Sprintf("AI-%s-%02d", cat.ID, index),
		RecipeVersion:  version,
		Title:          invented.Name,
		MealCategoryID: cat.ID,
		SelectionReasons: []string{
			fmt.Sprintf("Invented by %s to fill %s (%s), which fell short of its target "+
				"after real corpus recipes were exhausted", invented.Model, cat.ID, cat.Name),
		},
		Ingredients:  ingredients,
		MethodSteps:  invented.MethodSteps,
		ReviewStatus: "AI-invented, not a provider recipe",
		Source:       "ai-invented",
		Mark:         Mark(invented.DishFormatID, archetypeLabel(invented.DishFormatID)),
	}, nil
}

// archetypeLabel turns a mark id (e.g. "pot-khichdi") into the drawing's own caption. It
// names the archetype the artwork depicts, never a specific dish -- there is no real
// format_pattern text for an invented recipe to caption itself with truthfully.
func archetypeLabel(markID string) string {
	words := strings.Split(strings.ReplaceAll(markID, "-", " "), " ")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// validateInventedRecipe re-checks the model's response in Go. The response schema
// (internal/aidraft/prompt.go) already constrains ingredient_id to an enum over allowedIDs and
// dish_format_id to an enum over archetypes, but a schema is not treated as sufficient on its
// own here -- the same defense-in-depth posture this project takes toward anything else that
// reaches a printed page.
func validateInventedRecipe(r aidraft.InventedRecipe, allowedIDs map[string]bool, archetypes []string) error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("empty recipe name")
	}
	if len(r.MethodSteps) == 0 {
		return fmt.Errorf("no method steps")
	}
	if len(r.Ingredients) == 0 {
		return fmt.Errorf("no ingredients")
	}

	seen := make(map[string]bool, len(r.Ingredients))
	for _, ing := range r.Ingredients {
		if !allowedIDs[ing.IngredientID] {
			return fmt.Errorf("ingredient %q is outside the allowed set", ing.IngredientID)
		}
		if seen[ing.IngredientID] {
			return fmt.Errorf("ingredient %q listed more than once", ing.IngredientID)
		}
		seen[ing.IngredientID] = true
		if ing.QuantityG <= 0 {
			return fmt.Errorf("ingredient %q has a non-positive quantity", ing.IngredientID)
		}
	}

	for _, a := range archetypes {
		if a == r.DishFormatID {
			return nil
		}
	}
	return fmt.Errorf("dish_format_id %q is not one of the allowed archetypes", r.DishFormatID)
}
