package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// SafeIngredient is one ingredient_master row cleared for a specific child -- the allow-list
// internal/aidraft's invented-recipe fallback is constrained to.
type SafeIngredient struct {
	IngredientID string
	Name         string
}

// SafeIngredients returns every ingredient_master row a fallback (AI-invented) recipe may
// draw from for this child. It applies the same two provider-sourced signals real recipes are
// already filtered on -- food_group and allergen_tags -- at the ingredient level instead of
// the recipe level, because an invented recipe has no recipe_master row of its own for
// allergyFilter (steps_hard.go) or dietFilter (diet.go) to run against.
//
// Two places this is deliberately stricter than the real-recipe path, both because an
// invented recipe has been through no human review beyond the document-level signature page,
// so it does not get the benefit of the doubt a provider-authored recipe does:
//
//   - SuspectedAllergens are a ranker demotion for real recipes (AS-002, hard_block='N'),
//     never a hard exclusion. Here they are excluded outright, alongside confirmed Allergens.
//   - Vegetarian and Eggetarian are treated identically -- both exclude every one of
//     animalFoodGroups (diet.go), dairy included. dietFilter only drops to the ingredient
//     level for a Vegan profile; there is no ingredient-to-diet_type mapping table for the
//     ordinary Vegetarian/Eggetarian split, so this reuses the vegan exclusion list rather
//     than inventing a finer one. That over-excludes dairy for a lacto-vegetarian child
//     (narrower pool, not an unsafe one) rather than under-excluding meat for anybody.
func SafeIngredients(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) ([]SafeIngredient, error) {
	excludedGroups := append(append([]string{}, p.Allergens...), p.SuspectedAllergens...)
	restrictDiet := p.Vegan || p.DietType == "Vegetarian" || p.DietType == "Eggetarian"

	rows, err := pool.Query(ctx, `
		SELECT i.ingredient_id, i.english_name
		FROM ingredient_master i
		WHERE (NOT $2::boolean OR i.food_group != ALL($3))
		  AND (NOT $2::boolean OR i.allergen_tags NOT ILIKE '%Milk%')
		  AND NOT EXISTS (
		      SELECT 1 FROM allergen_tag_vocabulary v
		      WHERE v.allergen_group = ANY($1)
		        AND v.corpus_tag IS NOT NULL
		        AND i.allergen_tags ILIKE '%' || v.corpus_tag || '%'
		  )
		ORDER BY i.ingredient_id`,
		excludedGroups, restrictDiet, animalFoodGroups)
	if err != nil {
		return nil, fmt.Errorf("engine: safe ingredients: %w", err)
	}
	defer rows.Close()

	var out []SafeIngredient
	for rows.Next() {
		var s SafeIngredient
		if err := rows.Scan(&s.IngredientID, &s.Name); err != nil {
			return nil, fmt.Errorf("engine: safe ingredient scan: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engine: safe ingredient rows: %w", err)
	}
	return out, nil
}
