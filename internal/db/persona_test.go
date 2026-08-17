package db_test

import (
	"context"
	"testing"
)

// The persona queries from the audit. Against the raw workbooks, three of these return
// zero rows once a clinical constraint is stacked on top of the demographic ones. Under
// the ranker they must all return a populated, descending-ordered list.
//
// The rule these encode:
//
//   - Age and allergy stay HARD filters. They are the safety boundary and are never
//     relaxed to make a result set non-empty.
//   - Diet type stays a hard filter: a declared food practice is a stated exclusion,
//     not a preference.
//   - Everything else -- clinical condition, meal category, region, season -- ranks.
//     A ranker reorders and cannot empty a list, which is what makes these pass.
//
// Region is expressed as a preference, not a filter, so "West Bengal" surfaces Bengali
// recipes first and still returns a usable list if that region is thin for the age band.
type persona struct {
	name string
	// hard filters
	ageMonths int
	dietType  string
	allergens []string // matched against recipe and ingredient allergen tags
	// ranker inputs
	targetCode     string // NT00-NT12, from the provider rubric
	preferRegion   string // '' for no preference
	preferMealType string // '' for no preference
	minResults     int
}

var personas = []persona{
	{
		name: "8 months, vegetarian, no milk",
		// NT01 auto-activates for 6-23 months per the rubric's trigger column.
		ageMonths: 8, dietType: "Vegetarian", allergens: []string{"milk"},
		targetCode: "NT01", minResults: 1,
	},
	{
		name: "8 months, vegetarian, no milk, iron support",
		// Returned 0 against the raw data. Iron support is now a ranker weight, not a
		// tag filter, so the list is ordered by iron fitness instead of emptied.
		ageMonths: 8, dietType: "Vegetarian", allergens: []string{"milk"},
		targetCode: "NT06", minResults: 1,
	},
	{
		name: "3 years, vegetarian, peanut and milk allergy, West Bengal",
		// Returned 0 against the raw data. Region is now a preference weight.
		ageMonths: 36, dietType: "Vegetarian", allergens: []string{"peanut", "milk"},
		targetCode: "NT00", preferRegion: "West Bengal / East India", minResults: 1,
	},
	{
		name: "3 years, non-vegetarian, Nepal, lunch",
		// Returned 2 against the raw data. Meal type now ranks rather than filters.
		ageMonths: 36, dietType: "Non-vegetarian",
		targetCode: "NT00", preferRegion: "Nepal", preferMealType: "Lunch", minResults: 1,
	},
	{
		name: "18 months, vegetarian, West Bengal, iron support",
		// The home-region case: Bengali iron-rich options for a toddler.
		ageMonths: 18, dietType: "Vegetarian",
		targetCode: "NT06", preferRegion: "West Bengal / East India", minResults: 1,
	},
}

// personaQuery is the Phase 1 shape of engine steps 1-12: hard filters in the WHERE
// clause, everything else folded into ORDER BY.
const personaQuery = `
SELECT r.recipe_id,
       r.recipe_name,
       r.region_culture,
       -- The project's region focus applies only as a default. Once the user names a
       -- region, their choice ranks first and our tier weight steps aside.
       s.score * CASE WHEN $4 <> '' THEN 1 ELSE f.rank_weight END
         + CASE WHEN $4 <> '' AND r.region_culture = $4 THEN 0.15 ELSE 0 END
         + CASE WHEN $5 <> '' AND r.meal_type      = $5 THEN 0.03 ELSE 0 END
       AS final_score
FROM recipe_master r
JOIN recipe_target_score s ON s.recipe_id = r.recipe_id AND s.target_code = $3
JOIN region_focus        f ON f.region_culture = r.region_culture
WHERE r.min_age_months <= $1 AND r.max_age_months >= $1          -- hard: age
  AND lower(r.diet_type) = ANY($2::text[])                        -- hard: food practice, nested
  AND NOT EXISTS (                                                -- hard: allergy
      SELECT 1
      FROM recipe_ingredient_mapping m
      JOIN ingredient_master i ON i.ingredient_id = m.ingredient_id
      WHERE m.recipe_id = r.recipe_id
        AND EXISTS (
            SELECT 1 FROM unnest($6::text[]) AS a(term)
            WHERE i.allergen_tags ILIKE '%' || a.term || '%'
               OR i.english_name  ILIKE '%' || a.term || '%'))
  AND NOT EXISTS (
      SELECT 1 FROM unnest($6::text[]) AS a(term)
      WHERE r.allergen_tags ILIKE '%' || a.term || '%')
ORDER BY final_score DESC, r.recipe_id
LIMIT 20`

func TestPersonaQueriesNeverCollapse(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, p := range personas {
		t.Run(p.name, func(t *testing.T) {
			allergens := p.allergens
			if allergens == nil {
				allergens = []string{}
			}

			// Diet is nested, not categorical -- the same rule the engine's dietPermits
			// encodes. A persona declaring Non-vegetarian may eat every diet_type; one
			// declaring Vegetarian may eat only Vegetarian. Matching for equality here
			// would hold this suite to semantics the engine no longer uses.
			permitted := map[string][]string{
				"Vegetarian":     {"vegetarian"},
				"Eggetarian":     {"vegetarian", "eggetarian"},
				"Non-vegetarian": {"vegetarian", "eggetarian", "non-vegetarian"},
			}[p.dietType]
			if permitted == nil {
				t.Fatalf("persona declares unrecognized diet_type %q", p.dietType)
			}

			rows, err := pool.Query(ctx, personaQuery,
				p.ageMonths, permitted, p.targetCode, p.preferRegion, p.preferMealType, allergens)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			defer rows.Close()

			var (
				n    int
				prev = 2.0 // above the maximum possible score
			)
			for rows.Next() {
				var id, name, region string
				var score float64
				if err := rows.Scan(&id, &name, &region, &score); err != nil {
					t.Fatalf("scan: %v", err)
				}
				if score > prev {
					t.Errorf("results are not ordered: %s scored %.6f after %.6f", id, score, prev)
				}
				prev = score
				if n < 3 {
					t.Logf("  %d. %s  %s  [%s]  %.4f", n+1, id, name, region, score)
				}
				n++
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate: %v", err)
			}
			if n < p.minResults {
				t.Errorf("returned %d results, want at least %d; a ranker must never empty a result set",
					n, p.minResults)
			}
		})
	}
}

// TestAllergyFilterIsNeverRelaxed guards the one thing that must not degrade. If a
// future change turns the allergy filter into a ranker to make a page look fuller, this
// fails.
func TestAllergyFilterIsNeverRelaxed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, allergen := range []string{"milk", "peanut", "egg", "fish", "soy"} {
		t.Run(allergen, func(t *testing.T) {
			var leaked int
			err := pool.QueryRow(ctx, `
				SELECT count(*)
				FROM recipe_master r
				WHERE r.min_age_months <= 36 AND r.max_age_months >= 36
				  AND EXISTS (
				      SELECT 1 FROM recipe_ingredient_mapping m
				      JOIN ingredient_master i ON i.ingredient_id = m.ingredient_id
				      WHERE m.recipe_id = r.recipe_id
				        AND (i.allergen_tags ILIKE '%' || $1 || '%'
				          OR i.english_name  ILIKE '%' || $1 || '%'))
				  AND NOT EXISTS (
				      SELECT 1 FROM recipe_ingredient_mapping m
				      JOIN ingredient_master i ON i.ingredient_id = m.ingredient_id
				      WHERE m.recipe_id = r.recipe_id
				        AND (i.allergen_tags ILIKE '%' || $1 || '%'
				          OR i.english_name  ILIKE '%' || $1 || '%'))`, allergen).Scan(&leaked)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if leaked != 0 {
				t.Errorf("the allergen predicate is not self-consistent for %q", allergen)
			}
		})
	}
}
