package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
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
// default"): matching recipes get a flat boost that reorders within the pool the
// nutrition rubric already ranked, rather than a blended multiplier -- but per CLAUDE.md's
// "Region focus" principle (region preference "can never outweigh the nutrition rubric or
// remove a recipe"), that boost is sized as a tie-breaking nudge against
// recipe_ranked.ranked_score's live spread, not a magnitude large enough to override
// nutrition fitness outright. See the boost constant below for the exact sizing.
func applyCultureRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.RegionCulture == "" && p.CuisineCode == "" {
		return recipes, models.StepResult{Step: 7, Name: "Culture and location", Kind: "ranker", CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no region or cuisine preference, region_focus tiers apply as already scored by rank_weight in step 5"}, nil
	}

	region := p.RegionCulture
	if region == "" && p.CuisineCode != "" {
		err := pool.QueryRow(ctx, `SELECT region_culture FROM culture_region_map WHERE culture_code = $1`, p.CuisineCode).Scan(&region)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.StepResult{}, fmt.Errorf("engine: unrecognized cuisine_code %q: no matching row in culture_region_map: %w", p.CuisineCode, ErrInvalidProfile)
		}
		if err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: resolve cuisine code %s: %w", p.CuisineCode, err)
		}
	}

	// Rescaled from an earlier 1000.0 (found in the final whole-branch review to be
	// ~1500x recipe_ranked.ranked_score's live spread, which made region match an
	// unreported hard filter rather than a ranker nudge). 0.05 never exceeds ~8% of
	// that spread (ranked_score spans roughly 0.113-0.765, a 0.65 spread, verified
	// live) -- nutrition fitness stays the dominant ordering signal, this is a
	// tie-breaking nudge, not an override. The original intent (an explicit region
	// choice should visibly reorder the list) was right; only the magnitude was wrong.
	const boost = 0.05
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

	// Rescaled from an earlier 5.0 (found in the final whole-branch review to be up to
	// ~7.7x recipe_ranked.ranked_score's live spread at share=1.0, an unreported hard
	// filter in practice). 0.05 never exceeds ~8% of that spread (ranked_score spans
	// roughly 0.113-0.765, a 0.65 spread, verified live) -- nutrition fitness stays the
	// dominant ordering signal. Availability is still a real convenience signal, it just
	// nudges rather than overrides.
	const weight = 0.05
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

// applyDietRank is the ranking half of engine step 4. Step 4's hard filter decides what a
// family may eat; this decides what to show them first.
//
// recipe_master.diet_type states what a dish requires of whoever eats it, so the filter is
// a nested permission chain (vegan subset vegetarian subset eggetarian subset
// non-vegetarian -- see docs/decisions.md). A family declaring non-vegetarian is correctly
// permitted all 940 recipes, of which 828 are vegetarian, so without this step page one of
// their book is dal. Being permitted a dish is not the same as wanting it first.
//
// This is a nudge, not a re-sort: the boost sits between the budget boost (0.03) and the
// culture boost (0.05), so an explicit region choice still outranks a diet preference and
// neither outranks the nutrition score. Sized so it can reorder within a band of similar
// nutrition fitness and never across one.
func applyDietRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if p.DietType == "" || stepIn == 0 {
		return recipes, models.StepResult{
			Step: 4, Name: "Declared food practice - preference", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no diet type declared, step is a no-op",
		}, nil
	}

	const boost = 0.04

	matched := 0
	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		if out[i].DietType == p.DietType {
			out[i].RankedScore += boost
			matched++
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	note := fmt.Sprintf("%d of %d candidates match the declared practice %q exactly and were ranked up by %.2f",
		matched, stepIn, p.DietType, boost)
	if matched == stepIn {
		note = fmt.Sprintf("every candidate already matches the declared practice %q, so this step changed no ordering", p.DietType)
	}
	if matched == 0 {
		note = fmt.Sprintf("no candidate carries diet_type %q exactly; all of them are permitted by the nested diet chain but none is that practice's own dish", p.DietType)
	}

	return out, models.StepResult{
		Step: 4, Name: "Declared food practice - preference", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn, Note: note,
	}, nil
}

// regionCountry maps a recipe_master.region_culture value to the country word used in
// ingredient_master.region_availability free text. Hardcoded, not queried -- region_focus
// currently scopes to exactly India and Bangladesh (CLAUDE.md, "Region focus"), so a
// two-branch switch is a complete, correct mapping today. If region_focus ever gains a
// third country, this function must be revisited; it will not pick the change up on its
// own.
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

	// Rescaled from an earlier 2.0 (found in the final whole-branch review to be ~3x
	// recipe_ranked.ranked_score's live spread, which made an exact budget-band match
	// an unreported hard filter). 0.03 never exceeds ~5% of that spread (ranked_score
	// spans roughly 0.113-0.765, a 0.65 spread, verified live) -- the continuous cost
	// score from step 5 already does the real work; this is only a tie-breaking nudge
	// for an exact band match on top of it, never an override.
	const boost = 0.03
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
			// Rescaled from an earlier 0.5 (found in the final whole-branch review to be
			// ~77% of recipe_ranked.ranked_score's live spread, which made near-duplicate
			// demotion an unreported hard filter in practice). 0.02 never exceeds ~3% of
			// that spread (ranked_score spans roughly 0.113-0.765, a 0.65 spread, verified
			// live) -- nutrition fitness stays dominant; this only nudges a near-duplicate
			// a little further down among otherwise-similar candidates, never past a whole
			// tier of genuinely different recipes.
			out[i].RankedScore -= 0.02
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
	// Where the target came from, so the step note can attribute it honestly. Until the
	// console started sending `limit`, every target came from the provider's table and the
	// note said so unconditionally; an operator-entered number reported as a provider value
	// is a mislabelled source, which this project treats as a defect rather than a wording
	// nit.
	source := "operator-supplied limit"
	if limit <= 0 {
		limit = 25
		source = "engine default, no meal category given"
		if p.MealType != "" {
			var providedText string
			err := pool.QueryRow(ctx, `SELECT default_target_recipes FROM meal_category_target WHERE meal_category = $1`, p.MealType).Scan(&providedText)
			if err == nil {
				if provided, perr := strconv.Atoi(providedText); perr == nil {
					limit = provided
					source = "meal_category_target.default_target_recipes"
				} else {
					source = "engine default, meal_category_target value is not numeric"
				}
				// non-numeric stored value: keep the 25 default rather than error
			} else {
				source = "engine default, no meal_category_target row for this meal type"
			}
			// no row found (meal_category_target only covers named categories): keep the 25 default rather than error
		} else {
			source = "engine default, no meal category given"
		}
	}
	// The target and what was returned diverge when fewer candidates survived than the
	// target asked for. Report both rather than collapsing them, so an operator can tell a
	// short list caused by an earlier filter from one caused by the target itself.
	target := limit
	if limit > stepIn {
		limit = stepIn
	}
	return recipes[:limit], models.StepResult{
		Step: 13, Name: "Recipe count target", Kind: "target",
		CandidatesIn: stepIn, CandidatesOut: limit,
		Note: fmt.Sprintf("target %d (%s), returned %d", target, source, limit),
	}, nil
}
