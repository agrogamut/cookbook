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

// applySuspectedAllergenRank is the ranking half of engine step 2.
//
// Step 2's hard filter removes confirmed allergens and is never relaxed. This handles the
// other state the provider's masters distinguish and the flat allergen list cannot:
// AS-002 marks a suspected allergy hard_block = N, so it must rank down and raise a review
// flag rather than exclude.
//
// That is not the timid choice, it is the correct one. Unnecessary elimination is itself a
// recognised cause of faltering growth in children, so treating every suspicion as a
// confirmation trades one risk for a different one rather than removing risk.
//
// A group with no corpus tag demotes nothing, for the same reason it screens nothing --
// there is no tag to match. That is reported so it does not read as a working demotion.
func applySuspectedAllergenRank(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, recipes []models.RankedRecipe) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(recipes)
	if len(p.SuspectedAllergens) == 0 || stepIn == 0 {
		return recipes, models.StepResult{
			Step: 2, Name: "Allergy - suspected, ranker", Kind: "ranker",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no suspected allergens declared, step is a no-op",
		}, nil
	}

	ids := make([]string, len(recipes))
	for i, r := range recipes {
		ids[i] = r.RecipeID
	}

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT r.recipe_id
		FROM recipe_master r
		JOIN allergen_tag_vocabulary v
		  ON v.allergen_group = ANY($2) AND v.corpus_tag IS NOT NULL
		WHERE r.recipe_id = ANY($1)
		  AND (r.allergen_tags ILIKE '%' || v.corpus_tag || '%'
		       OR EXISTS (
		           SELECT 1 FROM recipe_ingredient_mapping m
		           WHERE m.recipe_id = r.recipe_id
		             AND m.ingredient_allergen_tag ILIKE '%' || v.corpus_tag || '%'))`,
		ids, p.SuspectedAllergens)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: suspected allergen rank: %w", err)
	}
	defer rows.Close()

	demote := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: suspected allergen scan: %w", err)
		}
		demote[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: suspected allergen rows: %w", err)
	}

	// Larger than every other adjustment in this file (culture 0.05, availability 0.05,
	// budget 0.03, duplicate -0.02) because a suspected allergen is a safety
	// signal rather than a preference, and it should push a recipe clearly down the list.
	// Still bounded, because it must not empty a page or override the nutrition ordering
	// outright -- that is what the confirmed state is for.
	const penalty = 0.15

	out := make([]models.RankedRecipe, len(recipes))
	copy(out, recipes)
	for i := range out {
		if demote[out[i].RecipeID] {
			out[i].RankedScore -= penalty
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RankedScore > out[j].RankedScore })

	unscreened, err := unscreenedGroups(ctx, pool, p.SuspectedAllergens)
	if err != nil {
		return nil, models.StepResult{}, err
	}

	// Unlike allergyFilter, this is a ranker and must never fail the request over an
	// unrecognized group -- rankers never empty a result set, including via an error path.
	// So a misspelled or unrecognized group (e.g. "Penut") is surfaced in the note rather
	// than rejected: without this, unscreenedGroups alone can't distinguish "recognized
	// group, no corpus tag" from "not a recognized group at all", and the note would read
	// like a working screen that found nothing.
	unknown, err := unknownAllergenGroups(ctx, pool, p.SuspectedAllergens)
	if err != nil {
		return nil, models.StepResult{}, err
	}

	note := fmt.Sprintf("%d of %d candidates carry a suspected allergen tag and were ranked down by %.2f; none were excluded, because AS-002 marks a suspected allergy hard_block = N",
		len(demote), stepIn, penalty)
	if len(unscreened) > 0 {
		note += fmt.Sprintf(". Suspected group(s) %v have no corpus tag, so they demoted nothing", unscreened)
	}
	if len(unknown) > 0 {
		note += fmt.Sprintf(". Suspected group(s) %v are not in allergen_mapping and were ignored", unknown)
	}

	return out, models.StepResult{
		Step: 2, Name: "Allergy - suspected, ranker", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: stepIn, Note: note,
	}, nil
}

// unknownAllergenGroups returns the subset of groups that do not exist anywhere in
// allergen_mapping at all -- distinct from unscreenedGroups, which only covers groups that
// are recognized but have no corpus tag. allergyFilter treats this case as fatal
// (ErrInvalidProfile) because it is a hard filter; this ranker cannot do the same without
// violating "rankers never empty a result set" by association with an error path, so it
// reports the unrecognized group in the step note instead of silently ignoring it.
func unknownAllergenGroups(ctx context.Context, pool *pgxpool.Pool, groups []string) ([]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `SELECT DISTINCT allergen_group FROM allergen_mapping WHERE allergen_group = ANY($1)`, groups)
	if err != nil {
		return nil, fmt.Errorf("engine: unknown allergen group lookup: %w", err)
	}
	defer rows.Close()
	valid := make(map[string]bool)
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, fmt.Errorf("engine: unknown allergen group scan: %w", err)
		}
		valid[g] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engine: unknown allergen group rows: %w", err)
	}
	var out []string
	for _, g := range groups {
		if !valid[g] {
			out = append(out, g)
		}
	}
	return out, nil
}

// unscreenedGroups returns the subset of groups with no corpus tag. Shared by the
// suspected-allergen ranker and step 2's hard filter so the two can never disagree about
// which groups the corpus cannot screen.
func unscreenedGroups(ctx context.Context, pool *pgxpool.Pool, groups []string) ([]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT allergen_group FROM allergen_tag_vocabulary
		WHERE allergen_group = ANY($1) AND corpus_tag IS NULL
		ORDER BY allergen_group`, groups)
	if err != nil {
		return nil, fmt.Errorf("engine: unscreened group lookup: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, fmt.Errorf("engine: unscreened group scan: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engine: unscreened group rows: %w", err)
	}
	return out, nil
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
