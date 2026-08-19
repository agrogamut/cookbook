package book

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
	"github.com/madamgy/recipie/internal/profile"
)

// ErrBlocked is returned by both assemblers when generation is stopped for this child. A
// blocked result must never become a book: the special-care stop gate exists because the
// feeding decision for those children is a clinician's, and a book issued in the child's
// name is exactly the artifact that would override it.
//
// Both books, not only the recipe book. The provider's own wording is "Condition is a STOP
// GATE, not a simple recipe filter", and a Book 1 carrying general-population milestone
// tables under the name of a child with a STOP-REVIEW diagnosis is the same override in a
// different binding.
var ErrBlocked = errors.New("book: engine blocked generation")

// methodStepPattern splits the provider's numbered preparation text ("1) ... 2) ... 3) ...")
// into individual steps. Whatever the numbering, the words themselves are the provider's own
// -- this only breaks one string into several, it never rewrites a syllable of it.
var methodStepPattern = regexp.MustCompile(`\d+\)\s*`)

// mealCategory is one row of meal_category_target, with default_target_recipes parsed to an
// int. The column is text in the workbook; a value this code cannot parse means "no cap"
// rather than a guessed number.
type mealCategory struct {
	ID     string
	Name   string
	Target int
}

// AssembleBook2 builds the recipe book for one child.
//
// A chapter with no recipes is omitted rather than rendered empty, and the omission is
// reported. Four of the seven meal categories have no mapped recipes today (GAP-023), and
// meal_category_target.include_logic marks several of them conditional anyway, so an omitted
// chapter is frequently the correct output rather than a failure.
//
// RotationPlan is never built here: assembling a seven-day rotation from a category that may
// hold as few as one surviving recipe would mean repeating a recipe or inventing a schedule
// this project has no data to construct honestly, so it stays nil (its documented meaning
// in types.go) until a real rotation logic is designed against real diversity data.
func AssembleBook2(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time) (Book2, []string, error) {
	cp, dropped, err := s.ToChildProfile(asOf)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: derive engine input: %w", err)
	}

	res, err := engine.Run(ctx, pool, cp)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: run engine: %w", err)
	}
	if res.Blocked {
		return Book2{}, nil, fmt.Errorf("%w: %s", ErrBlocked, res.BlockReason)
	}

	version, err := recipeMasterVersion(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: recipe version (GAP-024): %w", err)
	}

	boilerplate, err := boilerplateMethods(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: boilerplate methods (GAP-001): %w", err)
	}

	ageStages, err := ageStagesByAgeGroup(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: age stage map: %w", err)
	}

	categories, err := loadMealCategories(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: load meal categories: %w", err)
	}

	bengaliNames, err := ingredientBengaliNames(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: load ingredient bengali names: %w", err)
	}

	mapped, err := loadMealCategoryRecipeIDs(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: load meal category recipe map: %w", err)
	}

	// rank orders every recipe the engine returned, best first. byID recovers the engine's
	// own recorded region and diet fields for a recipe, which is where selection_reasons
	// comes from -- the engine's accounting, not new prose.
	rank := make(map[string]int, len(res.Recipes))
	byID := make(map[string]models.RankedRecipe, len(res.Recipes))
	for i, r := range res.Recipes {
		rank[r.RecipeID] = i
		byID[r.RecipeID] = r
	}

	skipped := append([]string{}, dropped...)
	sections := []MealSection{}

	for _, cat := range categories {
		candidateIDs := mapped[cat.ID]
		if len(candidateIDs) == 0 {
			skipped = append(skipped, fmt.Sprintf(
				"meal category %s (%s) has no recipes mapped to it at all (GAP-023)",
				cat.ID, cat.Name))
			continue
		}

		var survivors []string
		for _, id := range candidateIDs {
			if _, ok := rank[id]; ok {
				survivors = append(survivors, id)
			}
		}
		if len(survivors) == 0 {
			skipped = append(skipped, fmt.Sprintf(
				"meal category %s (%s) has %d recipes mapped to it, but none survived "+
					"this child's age, allergy, clinical or diet filters",
				cat.ID, cat.Name, len(candidateIDs)))
			continue
		}

		// Ranked best-first, the engine's own ordering, then capped to the provider's
		// per-category target.
		sort.Slice(survivors, func(i, j int) bool { return rank[survivors[i]] < rank[survivors[j]] })
		if cat.Target > 0 && len(survivors) > cat.Target {
			survivors = survivors[:cat.Target]
		}

		cards, cardSkips, err := loadRecipeCards(ctx, pool, survivors, cat.ID, version, boilerplate, ageStages, bengaliNames, byID, cp, res)
		if err != nil {
			return Book2{}, nil, fmt.Errorf("book: load recipe cards for %s: %w", cat.ID, err)
		}
		skipped = append(skipped, cardSkips...)
		if len(cards) == 0 {
			// Every id in survivors came from the engine's own result, so the join to
			// recipe_method_card/recipe_master should never drop one -- reaching this
			// means an id is orphaned somewhere upstream, and the honest response is to
			// report the omission rather than render a heading with nothing under it.
			skipped = append(skipped, fmt.Sprintf(
				"meal category %s (%s) had %d surviving candidates but none could be "+
					"loaded as a recipe card", cat.ID, cat.Name, len(survivors)))
			continue
		}

		sections = append(sections, MealSection{
			MealCategoryID:    cat.ID,
			Title:             cat.Name,
			TargetRecipeCount: cat.Target,
			Recipes:           cards,
		})
	}

	b := Book2{
		Metadata: Metadata{
			Title:          "My Child's Personalized Recipe Book",
			BookVersion:    "V1",
			GenerationDate: asOf,
			Language:       "en",
			ReviewStatus:   "Draft - Culinary/Nutrition/Clinical Review Required",
		},
		Child: ChildSummary{
			DisplayName:   s.DisplayName,
			AgeMonths:     cp.AgeMonths,
			AgeLabel:      ageLabel(cp.AgeMonths),
			FoodPractice:  cp.DietType,
			AllergyStatus: allergyStatus(cp.Allergens, cp.SuspectedAllergens),
		},
		MealSections: sections,
		RotationPlan: nil,
	}
	return b, skipped, nil
}

// recipeMasterVersion reads the content hash of the most recent import of recipe_master.
// See GAP-024 (migration 0017): the provider ships no version column, so this derived value
// stands in for one and is always labelled as an import hash, never as a provider version.
func recipeMasterVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var hash string
	err := pool.QueryRow(ctx, `
		SELECT content_hash FROM import_table_stat
		WHERE table_name = 'recipe_master'
		  AND run_id = (SELECT max(run_id) FROM import_table_stat WHERE table_name = 'recipe_master')`).
		Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no import run recorded for recipe_master; run the importer first")
	}
	if err != nil {
		return "", fmt.Errorf("query content hash: %w", err)
	}
	return hash, nil
}

// boilerplateMethods returns the set of preparation_method_full texts that more than one
// recipe shares -- the GAP-001 measure, computed the same way internal/importer/gaps.go
// counts it, so a card's MethodIsProviderBoilerplate flag agrees with the register.
func boilerplateMethods(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `
		SELECT preparation_method_full FROM recipe_master
		GROUP BY preparation_method_full
		HAVING count(*) > 1`)
	if err != nil {
		return nil, fmt.Errorf("query boilerplate methods: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var method string
		if err := rows.Scan(&method); err != nil {
			return nil, fmt.Errorf("scan boilerplate method: %w", err)
		}
		out[method] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("boilerplate method rows: %w", err)
	}
	return out, nil
}

// ageStagesByAgeGroup maps recipe_master.age_group to the Age/Feeding Stage master's stage
// codes (AF01..AF09) via age_feeding_stage_master.recipe_master_age_group, the join CLAUDE.md
// already lists as verified clean ("covers all 7 recipe age groups"). Several stage codes can
// share one age_group (AF04/AF05 both read "2-5 years", AF08/AF09 both read "13-18 years"),
// so a recipe legitimately carries more than one age_stage_id.
func ageStagesByAgeGroup(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT recipe_master_age_group, stage_code
		FROM age_feeding_stage_master
		WHERE recipe_master_age_group IS NOT NULL
		ORDER BY stage_code`)
	if err != nil {
		return nil, fmt.Errorf("query age stage map: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var ageGroup, stageCode string
		if err := rows.Scan(&ageGroup, &stageCode); err != nil {
			return nil, fmt.Errorf("scan age stage row: %w", err)
		}
		out[ageGroup] = append(out[ageGroup], stageCode)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("age stage rows: %w", err)
	}
	return out, nil
}

// ingredientBengaliNames maps ingredient_master.ingredient_id to its bengali_name, when the
// provider supplied one. 406 of 406 ingredients carry a Bengali name today; an id with none
// (or not found here) renders with the English name alone, never a transliteration this
// project has no source for.
func ingredientBengaliNames(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT ingredient_id, coalesce(bengali_name, '')
		FROM ingredient_master`)
	if err != nil {
		return nil, fmt.Errorf("query ingredient bengali names: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var id, bengali string
		if err := rows.Scan(&id, &bengali); err != nil {
			return nil, fmt.Errorf("scan ingredient bengali name: %w", err)
		}
		if bengali != "" {
			out[id] = bengali
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingredient bengali name rows: %w", err)
	}
	return out, nil
}

func loadMealCategories(ctx context.Context, pool *pgxpool.Pool) ([]mealCategory, error) {
	rows, err := pool.Query(ctx, `
		SELECT meal_category_id, meal_category, coalesce(default_target_recipes, '')
		FROM meal_category_target
		ORDER BY meal_category_id`)
	if err != nil {
		return nil, fmt.Errorf("query meal categories: %w", err)
	}
	defer rows.Close()

	var out []mealCategory
	for rows.Next() {
		var id, name, targetText string
		if err := rows.Scan(&id, &name, &targetText); err != nil {
			return nil, fmt.Errorf("scan meal category: %w", err)
		}
		// A target this code cannot parse means no cap is applied, not a guessed number.
		target, _ := strconv.Atoi(strings.TrimSpace(targetText))
		out = append(out, mealCategory{ID: id, Name: name, Target: target})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("meal category rows: %w", err)
	}
	return out, nil
}

// loadMealCategoryRecipeIDs reads the whole meal_category_recipe view (Task 1's join of
// meal_category_recipe_map, meal_category_target and recipe_master). A category absent from
// this map has no mapped meal_type at all -- GAP-023 -- and is distinct from a category that
// is mapped but whose recipes were all filtered out by this particular child's profile.
func loadMealCategoryRecipeIDs(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `SELECT meal_category_id, recipe_id FROM meal_category_recipe`)
	if err != nil {
		return nil, fmt.Errorf("query meal category recipe map: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var categoryID, recipeID string
		if err := rows.Scan(&categoryID, &recipeID); err != nil {
			return nil, fmt.Errorf("scan meal category recipe row: %w", err)
		}
		out[categoryID] = append(out[categoryID], recipeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("meal category recipe rows: %w", err)
	}
	return out, nil
}

// loadRecipeCards fetches the full card content for one meal section's chosen recipe ids,
// joining recipe_method_card (provider method text and review status) to recipe_master (the
// fields the card view does not carry) and left-joining recipe_nutrition_recomputed (the
// IFCT-verification band). Following the same shape as
// internal/api/handlers/recipes.go's RecipeDetail: query, scan into a local struct, wrap
// every error at the boundary.
//
// Every id in ids ends up either in the returned cards or named in the returned skip slice --
// never silently absent. The join is 1:1 across all 940 recipes today, so the skip slice is
// empty on current data; it exists so a future join miss is reported rather than rendering a
// chapter one recipe short of what was asked for.
func loadRecipeCards(ctx context.Context, pool *pgxpool.Pool, ids []string, categoryID, version string,
	boilerplate map[string]bool, ageStages map[string][]string, bengaliNames map[string]string,
	byID map[string]models.RankedRecipe, cp models.ChildProfile, res models.EngineResult) ([]RecipeCard, []string, error) {

	rows, err := pool.Query(ctx, `
		SELECT c.recipe_id, c.recipe_name, c.provider_method, c.provider_review_status,
		       r.age_group, r.texture, r.serving_size_g, r.ingredient_ids, r.ingredient_names,
		       r.ingredient_quantities_g,
		       r.safety_rule, r.clinical_tag, r.growth_target, r.prep_time_min, r.cook_time_min,
		       r.budget_band,
		       n.ingredient_coverage, n.fully_verified
		FROM recipe_method_card c
		JOIN recipe_master r ON r.recipe_id = c.recipe_id
		LEFT JOIN recipe_nutrition_recomputed n ON n.recipe_id = c.recipe_id
		WHERE c.recipe_id = ANY($1)`, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("query recipe cards: %w", err)
	}
	defer rows.Close()

	byRecipeID := make(map[string]RecipeCard, len(ids))
	for rows.Next() {
		var (
			recipeID, recipeName, method, reviewStatus    string
			ageGroup, texture, safetyRule                 string
			clinicalTag, growthTarget                     string
			servingSizeG, prepTimeMin, cookTimeMin        int
			budgetBand                                    string
			ingredientIDs, ingredientNames, ingredientQty string
			coverage                                      *float64
			fullyVerified                                 *bool
		)
		if err := rows.Scan(&recipeID, &recipeName, &method, &reviewStatus,
			&ageGroup, &texture, &servingSizeG, &ingredientIDs, &ingredientNames, &ingredientQty,
			&safetyRule, &clinicalTag, &growthTarget, &prepTimeMin, &cookTimeMin,
			&budgetBand, &coverage, &fullyVerified); err != nil {
			return nil, nil, fmt.Errorf("scan recipe card: %w", err)
		}

		prep, cook, band, tex := prepTimeMin, cookTimeMin, budgetBand, texture
		card := RecipeCard{
			RecipeID:                    recipeID,
			RecipeVersion:               version,
			Title:                       recipeName,
			MealCategoryID:              categoryID,
			AgeStageIDs:                 ageStages[ageGroup],
			SelectionReasons:            selectionReasons(res, cp, byID[recipeID]),
			NutritionTags:               nutritionTags(clinicalTag, growthTarget, coverage, fullyVerified),
			PrepTimeMinutes:             &prep,
			CookTimeMinutes:             &cook,
			CostBand:                    &band,
			Ingredients:                 splitIngredients(ingredientIDs, ingredientNames, ingredientQty, bengaliNames),
			MethodSteps:                 splitMethodSteps(method),
			TextureServing:              &tex,
			Serving:                     fmt.Sprintf("%d g", servingSizeG),
			Safety:                      safetyRule,
			ReviewStatus:                reviewStatus,
			MethodIsProviderBoilerplate: boilerplate[method],
		}
		byRecipeID[recipeID] = card
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("recipe card rows: %w", err)
	}

	// Rebuild in the caller's rank order rather than the query's arbitrary row order, so a
	// chapter's recipes stay best-first. An id with no matching row is named in the skip
	// slice rather than dropped -- a chapter that asked for twelve recipes and got eleven
	// must say so, not just render eleven.
	cards := make([]RecipeCard, 0, len(ids))
	var skipped []string
	for _, id := range ids {
		c, ok := byRecipeID[id]
		if !ok {
			skipped = append(skipped, fmt.Sprintf(
				"recipe %s has no method card row and was not rendered", id))
			continue
		}
		cards = append(cards, c)
	}
	return cards, skipped, nil
}

// selectionReasons draws only from the engine's own recorded decisions -- never invented
// prose. The active nutrition target and why it was chosen come straight from EngineResult;
// the region and diet lines only appear when the ranked recipe's own recorded fields agree
// with what the family declared, so an unstated preference never gets reported as a "match".
func selectionReasons(res models.EngineResult, cp models.ChildProfile, r models.RankedRecipe) []string {
	reasons := []string{
		fmt.Sprintf("Nutrition target %s: %s", res.ActiveTarget, res.TargetReason),
	}
	if cp.RegionCulture != "" && r.RegionCulture == cp.RegionCulture {
		reasons = append(reasons, fmt.Sprintf(
			"Region match: recipe is %s, the family's stated region", r.RegionCulture))
	}
	if cp.DietType != "" && r.DietType == cp.DietType {
		reasons = append(reasons, fmt.Sprintf("Diet practice: %s", r.DietType))
	}
	return reasons
}

// nutritionTags carries only the provider's own tags (clinical_tag, growth_target) plus the
// IFCT-verification band from recipe_nutrition_recomputed -- never a computed nutrition
// figure, only the labels that already exist or that state how verified an existing figure is.
func nutritionTags(clinicalTag, growthTarget string, coverage *float64, fullyVerified *bool) []string {
	var tags []string
	if clinicalTag != "" {
		tags = append(tags, "Clinical tag: "+clinicalTag)
	}
	if growthTarget != "" {
		tags = append(tags, "Growth target: "+growthTarget)
	}
	if fullyVerified != nil {
		if *fullyVerified {
			tags = append(tags, "Nutrition: fully IFCT-verified")
		} else if coverage != nil {
			tags = append(tags, fmt.Sprintf("Nutrition: %.0f%% IFCT-verified by mass", *coverage*100))
		}
	}
	return tags
}

// splitIngredients pairs recipe_master's semicolon-delimited ingredient_ids, ingredient_names
// and ingredient_quantities_g lists, which CLAUDE.md's "Verified clean" section already
// establishes align 1:1 for all 1000 recipes. Names and quantities are transcribed verbatim;
// nothing here estimates a quantity for a name with no matching entry. bengaliNames is keyed
// by ingredient_id rather than by name, because matching on the provider's own id is exact
// where matching on free-text name would not be.
func splitIngredients(ids, names, quantities string, bengaliNames map[string]string) []IngredientLine {
	idParts := strings.Split(ids, ";")
	nameParts := strings.Split(names, ";")
	qtyParts := strings.Split(quantities, ";")
	out := make([]IngredientLine, 0, len(nameParts))
	for i, name := range nameParts {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		line := IngredientLine{Name: name}
		if i < len(qtyParts) {
			if qty := strings.TrimSpace(qtyParts[i]); qty != "" {
				line.Quantity = qty + " g"
			}
		}
		if i < len(idParts) {
			if id := strings.TrimSpace(idParts[i]); id != "" {
				line.Bengali = bengaliNames[id]
			}
		}
		out = append(out, line)
	}
	return out
}

// splitMethodSteps breaks the provider's single preparation_method_full string into the
// numbered steps it already contains ("1) ... 2) ... 3) ..."). This only segments existing
// text; it introduces no new instruction and rewrites no word of the provider's own.
func splitMethodSteps(method string) []string {
	parts := methodStepPattern.Split(method, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		if trimmed := strings.TrimSpace(method); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
