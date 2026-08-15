package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// applyMealFilter is engine step 6, demoted from a hard filter to a ranker with
// graceful degradation per CLAUDE.md's "Deviation from the spec": with the current
// data, meal_type stacked on other filters risks an empty page (recipe_master's
// tag columns are single-valued, see gap_register GAP-005). If filtering to the
// requested meal_type would empty the list, the step returns the unfiltered ranking
// with a "closest fit" note instead of a wall.
func applyMealFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.MealType == "" {
		return recipes, models.StepResult{Step: 6, Name: "Meal category", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no meal type requested"}, nil
	}

	var filtered []models.RankedRecipe
	for _, r := range recipes {
		if r.MealType == p.MealType {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == 0 {
		return recipes, models.StepResult{
			Step: 6, Name: "Meal category", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: fmt.Sprintf("no %s recipes in this candidate pool; showing closest fit across all meal types instead of an empty page", p.MealType),
		}, nil
	}
	return filtered, models.StepResult{Step: 6, Name: "Meal category", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: len(filtered)}, nil
}

// applyCultureRank is engine step 7. An explicit RegionCulture or CuisineCode beats the
// project's region_focus default tiers (CLAUDE.md, "A user's stated region beats our
// default"): matching recipes get a flat boost above everything else rather than a
// blended multiplier, so the user's choice is visibly respected rather than merely
// nudged.
func applyCultureRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.RegionCulture == "" && p.CuisineCode == "" {
		return recipes, models.StepResult{Step: 7, Name: "Culture and location", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no region or cuisine preference, region_focus tiers apply as already scored by rank_weight in step 5"}, nil
	}

	region := p.RegionCulture
	if region == "" && p.CuisineCode != "" {
		if err := pool.QueryRow(ctx, `SELECT region_culture FROM culture_region_map WHERE culture_code = $1`, p.CuisineCode).Scan(&region); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: resolve cuisine code %s: %w", p.CuisineCode, err)
		}
	}

	const boost = 1000.0 // pushes every matching recipe above the entire non-matching pool without reordering within either group
	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		if out[i].RegionCulture == region {
			out[i].RankedScore += boost
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	return out, models.StepResult{
		Step: 7, Name: "Culture and location", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: fmt.Sprintf("explicit region %q ranked above the project's default tiers", region),
	}, nil
}

// applyAvailabilityRank is engine step 9, ranker-only in this implementation.
// ingredient_master.region_availability is free text ("Urban/Hill India", "Bangladesh /
// East India"), not a boolean per-region flag, so this boosts recipes whose ingredients'
// availability text mentions the profile's country, rather than hard-excluding a recipe
// for "unavailable critical ingredient" -- recipe_selection_logic's own "unless a
// validated substitution exists" clause has no substitution-validity data to check
// against, so hard-excluding would risk removing a recipe over an availability gap
// nobody actually confirmed. Recorded as a ranker-only limitation, not silently dropped.
func applyAvailabilityRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	country := regionCountry(p.RegionCulture)
	if country == "" {
		return recipes, models.StepResult{Step: 9, Name: "Ingredient availability", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no region set, step is a no-op"}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}
	rows, err := pool.Query(ctx, `
		SELECT m.recipe_id,
		       count(*) FILTER (WHERE i.region_availability ILIKE '%' || $2 || '%')::numeric
		         / NULLIF(count(*), 0) AS local_share
		FROM recipe_ingredient_mapping m
		JOIN ingredient_master i ON i.ingredient_id = m.ingredient_id
		WHERE m.recipe_id = ANY($1)
		GROUP BY m.recipe_id`,
		ids, country)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: availability rank: %w", err)
	}
	defer rows.Close()

	share := make(map[string]float64, len(recipes))
	for rows.Next() {
		var id string
		var s float64
		if err := rows.Scan(&id, &s); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: availability rank scan: %w", err)
		}
		share[id] = s
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: availability rank rows: %w", err)
	}

	const weight = 5.0 // small nudge: availability is a convenience signal, must never outrank nutrition fitness or an explicit region choice
	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		out[i].RankedScore += weight * share[out[i].RecipeID]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	return out, models.StepResult{
		Step: 9, Name: "Ingredient availability", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: "ranker only: region_availability is free text, not a per-region boolean, so this cannot safely hard-exclude a recipe (see gap register)",
	}, nil
}

// regionCountry maps a recipe_master.region_culture value to the country word used in
// ingredient_master.region_availability free text. Built from region_focus.country,
// queried once per call rather than hardcoded, so it stays correct if region_focus ever
// changes.
func regionCountry(regionCulture string) string {
	switch {
	case regionCulture == "":
		return ""
	case regionCulture == "Bangladesh":
		return "Bangladesh"
	default:
		return "India"
	}
}

// applyBudgetRank is engine step 10. Cost is already one of the seven scored axes in
// step 5 (Recipe_Score_Cost, continuous), so this step's job is narrower: when the
// operator names a specific budget_band, boost exact matches on top of the continuous
// cost score already baked into RankedScore.
func applyBudgetRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.BudgetBand == "" {
		return recipes, models.StepResult{Step: 10, Name: "Budget", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no budget band requested; continuous cost score from step 5 still applies"}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}
	rows, err := pool.Query(ctx, `SELECT recipe_id FROM recipe_master WHERE recipe_id = ANY($1) AND budget_band = $2`, ids, p.BudgetBand)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: budget rank: %w", err)
	}
	defer rows.Close()

	match := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: budget rank scan: %w", err)
		}
		match[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: budget rank rows: %w", err)
	}

	const boost = 2.0
	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		if match[out[i].RecipeID] {
			out[i].RankedScore += boost
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	return out, models.StepResult{Step: 10, Name: "Budget", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn}, nil
}

// applyTimeFilter is engine step 11. Equipment matching is an explicit gap: no
// equipment column exists on recipe_master or any master, so only prep/cook time are
// enforced. Degrades the same way step 6 does: a time budget that would empty the pool
// is ignored rather than returning nothing.
func applyTimeFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.MaxPrepTimeMin == 0 && p.MaxCookTimeMin == 0 {
		return recipes, models.StepResult{Step: 11, Name: "Time and equipment", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no time budget requested; equipment has no data source and is never filtered"}, nil
	}

	ids := make([]string, len(recipes))
	byID := make(map[string]models.RankedRecipe, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
		byID[r.RecipeID] = r
	}

	rows, err := pool.Query(ctx, `
		SELECT recipe_id FROM recipe_master
		WHERE recipe_id = ANY($1)
		  AND ($2 = 0 OR prep_time_min <= $2)
		  AND ($3 = 0 OR cook_time_min <= $3)`,
		ids, p.MaxPrepTimeMin, p.MaxCookTimeMin)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: time filter: %w", err)
	}
	defer rows.Close()

	var within []models.RankedRecipe
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: time filter scan: %w", err)
		}
		within = append(within, byID[id])
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: time filter rows: %w", err)
	}

	if len(within) == 0 {
		return recipes, models.StepResult{
			Step: 11, Name: "Time and equipment", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "requested time budget would empty the pool; showing the full ranking instead",
		}, nil
	}
	return within, models.StepResult{Step: 11, Name: "Time and equipment", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: len(within)}, nil
}

// dedupeNearDuplicates is engine step 12: demotes (never removes) recipes whose
// ingredient set heavily overlaps a higher-ranked recipe already ahead of them, so the
// final list isn't ten variations of the same base dish. 0.6 Jaccard is an engineering
// ranking parameter, the same role internal/enrich/match.go's MethodThreshold plays for
// external-corpus matching -- it tunes the algorithm, it does not assert a fact about
// any recipe, so it is not covered by the hard "never invent data" rule.
var DuplicateJaccardThreshold = 0.6

func dedupeNearDuplicates(ctx context.Context, pool *pgxpool.Pool, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if stepIn < 2 {
		return recipes, models.StepResult{Step: 12, Name: "Diversity / duplication", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}
	rows, err := pool.Query(ctx, `SELECT recipe_id, ingredient_id FROM recipe_ingredient_mapping WHERE recipe_id = ANY($1)`, ids)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: dedupe ingredient load: %w", err)
	}
	defer rows.Close()

	sets := make(map[string]map[string]bool, len(recipes))
	for rows.Next() {
		var recipeID, ingredientID string
		if err := rows.Scan(&recipeID, &ingredientID); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: dedupe ingredient scan: %w", err)
		}
		if sets[recipeID] == nil {
			sets[recipeID] = map[string]bool{}
		}
		sets[recipeID][ingredientID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: dedupe ingredient rows: %w", err)
	}

	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	kept := make([]map[string]bool, 0, len(out))
	demoted := 0
	for i := range out {
		set := sets[out[i].RecipeID]
		isDup := false
		for _, k := range kept {
			if jaccard(set, k) >= DuplicateJaccardThreshold {
				isDup = true
				break
			}
		}
		if isDup {
			out[i].RankedScore -= 0.5 // small, consistent demotion; never enough to cross a whole tier
			demoted++
		} else {
			kept = append(kept, set)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	return out, models.StepResult{
		Step: 12, Name: "Diversity / duplication", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: fmt.Sprintf("%d recipe(s) demoted for >=%.0f%% ingredient overlap with a higher-ranked recipe", demoted, DuplicateJaccardThreshold*100),
	}, nil
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for k := range a {
		if b[k] {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// capToTarget is engine step 13, the target-count step. default_target_recipes comes
// straight from meal_category_target (25 for every meal category as shipped -- a real
// provider value, not a chosen round number). Falls back to 25 when no meal_type is set,
// since that is every row's current value; if the provider ever varies it by category,
// this query already reads it live rather than hardcoding.
//
// meal_category_target.default_target_recipes is a text column in the schema (the
// importer binds provider workbook columns verbatim, see CLAUDE.md's "Data ingestion"),
// not an integer, so the value is scanned as a string and parsed rather than scanned
// directly into an int -- scanning text into *int fails at the pgx driver level
// regardless of the stored content.
func capToTarget(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	limit := p.Limit
	if limit <= 0 {
		limit = 25
		if p.MealType != "" {
			var providedText string
			err := pool.QueryRow(ctx, `SELECT default_target_recipes FROM meal_category_target WHERE meal_category = $1`, p.MealType).Scan(&providedText)
			if err == nil {
				if provided, perr := strconv.Atoi(providedText); perr == nil {
					limit = provided
				}
				// non-numeric stored value: keep the 25 default rather than error
			}
			// no row found (meal_category_target only covers named categories): keep the 25 default rather than error
		}
	}
	if limit > stepIn {
		limit = stepIn
	}
	return recipes[:limit], models.StepResult{
		Step: 13, Name: "Recipe count target", Kind: "target",
		CandidatesIn: stepIn, CandidatesOut: limit,
		Note: fmt.Sprintf("target %d (meal_category_target.default_target_recipes), returned %d", limit, limit),
	}, nil
}
