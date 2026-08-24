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

// inventedReviewStatus matches a real recipe's most common Review_Status verbatim
// (pdf_test.go pins the same string elsewhere). Every recipe in this corpus is still Draft
// either way, so this is true of an invented card too -- it simply stops being a printed tell
// that this particular card took a different path to the page.
const inventedReviewStatus = "Draft - Culinary/Nutrition/Clinical Review Required"

// topUpInvented tries to fill a category's shortfall against cards with an invented recipe per
// still-missing slot -- reused from ai_recipe where a stored one clears this child's own
// allow-list, drafted fresh with Gemini otherwise -- stopping at the first failure rather than
// retrying indefinitely. cards may already hold zero or more real recipes -- the returned
// slice is always at least as long as the input. The returned note is empty whenever the
// target was fully reached (by real recipes, invention, or both); otherwise it names how many
// recipes are still missing and why the fallback stopped. Never runs when cat.Target is 0,
// which means "no cap" on this category, not "zero is the target".
func topUpInvented(ctx context.Context, pool *pgxpool.Pool, drafter aidraft.Drafter,
	cp models.ChildProfile, cat mealCategory, version string, res models.EngineResult, cards []RecipeCard) ([]RecipeCard, string) {

	if cat.Target <= 0 || len(cards) >= cat.Target {
		return cards, ""
	}
	short := cat.Target - len(cards)
	// excluded stops the same stored row filling two slots of the same chapter: each call
	// into inventCard sees every id this call has already served, on top of whatever this
	// child's own allow-list rejects.
	excluded := make(map[string]bool, short)
	for i := 0; i < short; i++ {
		invented, err := inventCard(ctx, pool, drafter, cp, cat, version, res, excluded)
		if err != nil {
			return cards, fmt.Sprintf(
				"%s (%s) is short %d recipe(s) of its %d-recipe target after real corpus "+
					"recipes were exhausted; AI-invented fallback unavailable or failed "+
					"validation: %v", cat.ID, cat.Name, short-i, cat.Target, err)
		}
		excluded[invented.RecipeID] = true
		cards = append(cards, invented)
	}
	return cards, ""
}

// inventCard fills one fallback slot: it first looks for an already-persisted ai_recipe this
// child's own allow-list still clears, and only asks the model for a fresh one when the store
// has nothing usable. Either way the result runs through the same allow-list membership check
// (validateInventedRecipe) before it can become a card -- a recipe stored for a different
// child under different allergies is re-cleared here, never assumed safe because it printed
// once before.
//
// Any failure -- drafting unavailable, an ingredient outside the allow-list, a malformed
// response -- returns an error and no card. The caller reports the shortfall exactly the way
// an empty chapter always has; nothing here ever forces a failed-validation recipe through.
func inventCard(ctx context.Context, pool *pgxpool.Pool, drafter aidraft.Drafter,
	cp models.ChildProfile, cat mealCategory, version string, res models.EngineResult,
	excluded map[string]bool) (RecipeCard, error) {

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

	allowedIDs := make(map[string]bool, len(safe))
	nameByID := make(map[string]string, len(safe))
	for _, s := range safe {
		allowedIDs[s.IngredientID] = true
		nameByID[s.IngredientID] = s.Name
	}

	archetypes := MarkIDs()

	// Reuse only runs when this call actually opted into AI drafting. WithDrafter's own
	// contract is that an omitted drafter leaves AssembleBook2 behaving exactly as it did
	// before either feature existed -- a caller that never wired up a Drafter must not start
	// seeing AI-invented content just because some other caller's earlier run happened to
	// persist a row for the same category. aidraft.Disabled is themselves a comparable,
	// side-effect-free sentinel, so this is a plain equality check, not a capability probe.
	if drafter != aidraft.Disabled {
		card, ok, err := findStoredInventedCard(ctx, pool, cat, version, res, cp, allowedIDs, nameByID, archetypes, excluded)
		if err != nil {
			return RecipeCard{}, fmt.Errorf("stored invented recipe lookup: %w", err)
		}
		if ok {
			return card, nil
		}
	}

	return generateInventedCard(ctx, pool, drafter, cp, cat, version, res, safe, allowedIDs, nameByID, archetypes)
}

// findStoredInventedCard looks in ai_recipe for a previously-drafted recipe in this meal
// category and re-runs validateInventedRecipe against this call's own allowedIDs -- the same
// child-safe set generateInventedCard constrains a fresh draft to. A row drafted for an
// earlier child is only ever reused if every one of its ingredients is also in *this* child's
// allow-list; anything else is skipped, not served. Rows already handed out earlier in this
// same topUpInvented call (excluded) are skipped without a query, so one chapter never prints
// the same stored recipe twice.
func findStoredInventedCard(ctx context.Context, pool *pgxpool.Pool, cat mealCategory, version string,
	res models.EngineResult, cp models.ChildProfile, allowedIDs map[string]bool, nameByID map[string]string,
	archetypes []string, excluded map[string]bool) (RecipeCard, bool, error) {

	rows, err := pool.Query(ctx, `
		SELECT recipe_id, title, dish_format_id, method_steps
		FROM ai_recipe
		WHERE meal_category_id = $1
		ORDER BY created_at ASC`, cat.ID)
	if err != nil {
		return RecipeCard{}, false, fmt.Errorf("query stored invented recipes: %w", err)
	}

	type candidate struct {
		id, title, dishFormatID string
		methodSteps             []string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.title, &c.dishFormatID, &c.methodSteps); err != nil {
			rows.Close()
			return RecipeCard{}, false, fmt.Errorf("scan stored invented recipe: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return RecipeCard{}, false, fmt.Errorf("stored invented recipe rows: %w", err)
	}
	rows.Close()

	for _, c := range candidates {
		if excluded[c.id] {
			continue
		}

		ingRows, err := pool.Query(ctx,
			`SELECT ingredient_id, quantity_g FROM ai_recipe_ingredient WHERE recipe_id = $1`, c.id)
		if err != nil {
			return RecipeCard{}, false, fmt.Errorf("query stored invented ingredients for %s: %w", c.id, err)
		}
		invented := aidraft.InventedRecipe{Name: c.title, MethodSteps: c.methodSteps, DishFormatID: c.dishFormatID}
		for ingRows.Next() {
			var id string
			var qty float64
			if err := ingRows.Scan(&id, &qty); err != nil {
				ingRows.Close()
				return RecipeCard{}, false, fmt.Errorf("scan stored invented ingredient for %s: %w", c.id, err)
			}
			invented.Ingredients = append(invented.Ingredients, aidraft.InventedIngredientLine{IngredientID: id, QuantityG: qty})
		}
		if err := ingRows.Err(); err != nil {
			ingRows.Close()
			return RecipeCard{}, false, fmt.Errorf("stored invented ingredient rows for %s: %w", c.id, err)
		}
		ingRows.Close()

		// A row drafted for a different child fails here whenever this child's own allow-list
		// (allergies, diet) excludes an ingredient the earlier child's did not -- re-cleared,
		// never assumed safe. Skip and try the next stored candidate rather than treat this as
		// a hard failure; the store may hold several rows for the same category.
		if err := validateInventedRecipe(invented, allowedIDs, archetypes); err != nil {
			continue
		}

		ingredients := make([]IngredientLine, 0, len(invented.Ingredients))
		for _, ing := range invented.Ingredients {
			ingredients = append(ingredients, IngredientLine{
				Name:     nameByID[ing.IngredientID],
				Quantity: fmt.Sprintf("%.0f g", ing.QuantityG),
			})
		}
		card, err := buildInventedCard(ctx, pool, c.id, version, cat, invented, ingredients, res, cp, archetypes)
		if err != nil {
			return RecipeCard{}, false, err
		}
		return card, true, nil
	}
	return RecipeCard{}, false, nil
}

// generateInventedCard asks drafter for a fallback recipe constrained to this child's
// safe-ingredient allow-list, validates the response in Go, persists it to ai_recipe /
// ai_recipe_ingredient, and returns a ready RecipeCard. Persistence happens before this
// returns, on the same call that generated the recipe, so a drafted recipe is never handed
// back without also being saved for reuse.
//
// Membership in engine.SafeIngredients's allow-list is the whole safety boundary here: it
// already excludes every ingredient carrying a declared or suspected allergen and, for a
// vegetarian/eggetarian/vegan diet, every animal-derived food group. Checking that every
// returned ingredient id is in that set (validateInventedRecipe) therefore re-verifies
// allergy and diet safety in one step, rather than needing a second allergen-overlap query --
// the allow-list was already built to be the answer to "is this ingredient safe for this
// child", not merely a convenience list.
func generateInventedCard(ctx context.Context, pool *pgxpool.Pool, drafter aidraft.Drafter,
	cp models.ChildProfile, cat mealCategory, version string, res models.EngineResult,
	safe []engine.SafeIngredient, allowedIDs map[string]bool, nameByID map[string]string,
	archetypes []string) (RecipeCard, error) {

	allowed := make([]aidraft.AllowedIngredient, len(safe))
	for i, s := range safe {
		allowed[i] = aidraft.AllowedIngredient{IngredientID: s.IngredientID, Name: s.Name}
	}

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

	recipeID, err := persistInventedRecipe(ctx, pool, cat, cp, invented)
	if err != nil {
		return RecipeCard{}, fmt.Errorf("persist: %w", err)
	}

	ingredients := make([]IngredientLine, 0, len(invented.Ingredients))
	for _, ing := range invented.Ingredients {
		ingredients = append(ingredients, IngredientLine{
			Name:     nameByID[ing.IngredientID],
			Quantity: fmt.Sprintf("%.0f g", ing.QuantityG),
		})
	}

	return buildInventedCard(ctx, pool, recipeID, version, cat, invented, ingredients, res, cp, archetypes)
}

// persistInventedRecipe writes a freshly-drafted, already-validated recipe to ai_recipe and
// ai_recipe_ingredient in one transaction, so a recipe row is never left without its
// ingredient rows (or the reverse) if either insert fails partway through.
func persistInventedRecipe(ctx context.Context, pool *pgxpool.Pool, cat mealCategory,
	cp models.ChildProfile, invented aidraft.InventedRecipe) (string, error) {

	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var recipeID string
	err = tx.QueryRow(ctx, `
		INSERT INTO ai_recipe
			(title, meal_category_id, dish_format_id, diet_type, min_age_months, method_steps, model)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING recipe_id`,
		invented.Name, cat.ID, invented.DishFormatID, cp.DietType, cp.AgeMonths,
		invented.MethodSteps, invented.Model,
	).Scan(&recipeID)
	if err != nil {
		return "", fmt.Errorf("insert ai_recipe: %w", err)
	}

	for _, ing := range invented.Ingredients {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
			VALUES ($1, $2, $3)`, recipeID, ing.IngredientID, ing.QuantityG); err != nil {
			return "", fmt.Errorf("insert ai_recipe_ingredient: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return recipeID, nil
}

// buildInventedCard is the one place an invented recipe -- stored or freshly drafted -- turns
// into a RecipeCard, so the two paths can never drift into printing the card differently.
// Source stays "ai-invented" on the struct and in the JSON API, where the operator console's
// fact-check pass needs it; recipe.html never reads this field, so the printed page carries no
// distinction from a provider recipe.
func buildInventedCard(ctx context.Context, pool *pgxpool.Pool, recipeID, version string, cat mealCategory,
	invented aidraft.InventedRecipe, ingredients []IngredientLine, res models.EngineResult, cp models.ChildProfile,
	archetypes []string) (RecipeCard, error) {

	card := RecipeCard{
		RecipeID:         recipeID,
		RecipeVersion:    version,
		Title:            invented.Name,
		MealCategoryID:   cat.ID,
		SelectionReasons: inventedSelectionReasons(res, cp),
		Ingredients:      ingredients,
		MethodSteps:      invented.MethodSteps,
		ReviewStatus:     inventedReviewStatus,
		Source:           "ai-invented",
		Mark:             Mark(invented.DishFormatID, archetypeLabel(invented.DishFormatID)),
	}
	photo, err := RepresentativePhoto(ctx, pool, invented.DishFormatID)
	if err != nil {
		return RecipeCard{}, fmt.Errorf("invented recipe %s photo lookup for dish format %s: %w",
			recipeID, invented.DishFormatID, err)
	}
	card.Photo = photo
	return card, nil
}

// inventedSelectionReasons draws the same kind of reason selectionReasons builds for a real
// card -- the active nutrition target and a stated diet-practice match -- and never a sentence
// naming how the card was produced. There is no region field to compare here: an invented
// recipe carries no recipe_master row with its own recorded Region_Culture, so the region line
// selectionReasons prints for a real card simply has nothing to compare against and is omitted
// rather than faked.
func inventedSelectionReasons(res models.EngineResult, cp models.ChildProfile) []string {
	reasons := []string{
		fmt.Sprintf("Nutrition target %s: %s", res.ActiveTarget, res.TargetReason),
	}
	if cp.DietType != "" {
		reasons = append(reasons, fmt.Sprintf("Diet practice: %s", cp.DietType))
	}
	return reasons
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

// validateInventedRecipe re-checks a recipe -- freshly drafted or pulled from ai_recipe -- in
// Go. The response schema (internal/aidraft/prompt.go) already constrains ingredient_id to an
// enum over allowedIDs and dish_format_id to an enum over archetypes at draft time, but a
// schema is not treated as sufficient on its own here, and a stored row never went through
// that schema check against *this* call's allowedIDs at all -- the same defense-in-depth
// posture this project takes toward anything else that reaches a printed page.
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
