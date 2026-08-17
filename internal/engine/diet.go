package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// animalFoodGroups are the ingredient_master.food_group values excluded for a vegan
// profile on top of the diet_type filter. recipe_master.diet_type has no "Vegan" value
// (only Eggetarian / Non-vegetarian / Vegetarian), so veganism is not expressible as a
// single-column filter and must additionally exclude these seven food groups at the
// ingredient level. Read live from ingredient_master's 61 distinct food_group values.
var animalFoodGroups = []string{"Animal protein", "Dairy", "Fish", "Dried fish", "Fish product", "Shellfish", "Organ meat"}

// dietPermits maps a family's declared food practice to the recipe_master.diet_type
// values that practice may eat.
//
// Diet is nested, not categorical: vegetarian ⊂ eggetarian ⊂ non-vegetarian. A recipe's
// diet_type states what the dish requires of whoever eats it, not which kind of eater it
// belongs to -- so a non-vegetarian child may eat a vegetarian dish, while the reverse is
// never true. Matching diet_type for equality reads the column as a category and starves
// the result list: a non-vegetarian profile saw 111 of 940 recipes and an eggetarian one
// saw a single recipe, because the corpus holds exactly one dish using an egg ingredient.
//
// The old behaviour failed safe -- it never served a family food their practice forbids,
// it only withheld food they could have eaten -- which is why the persona suite, written
// to catch empty result sets rather than short ones, did not flag it.
var dietPermits = map[string][]string{
	"Vegetarian":     {"Vegetarian"},
	"Eggetarian":     {"Vegetarian", "Eggetarian"},
	"Non-vegetarian": {"Vegetarian", "Eggetarian", "Non-vegetarian"},
}

// dietFilter is engine step 4, a hard filter (not demoted -- CLAUDE.md's "Deviation from
// the spec" only demotes steps 3 and 6).
func dietFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, error) {
	stepIn := len(candidateIDs)
	if p.DietType == "" || stepIn == 0 {
		return candidateIDs, models.StepResult{
			Step: 4, Name: "Declared food practice", Kind: "hard_filter",
			CandidatesIn: stepIn, CandidatesOut: stepIn, Note: "no diet type declared, step is a no-op",
		}, nil
	}

	permitted, ok := dietPermits[p.DietType]
	if !ok {
		return nil, models.StepResult{}, fmt.Errorf("engine: diet filter: unrecognized diet_type %q: %w", p.DietType, ErrInvalidProfile)
	}

	query := `SELECT recipe_id FROM recipe_master WHERE recipe_id = ANY($1) AND diet_type = ANY($2)`
	args := []any{candidateIDs, permitted}
	if p.Vegan {
		// food_group alone misses ghee (ING0060): it is bucketed under "Fat", not
		// "Dairy", so it survives the food-group exclusion -- verified live, 103
		// recipes contain it. The provider did correctly tag it
		// ingredient_allergen_tag = 'Milk' on the mapping row even though the
		// food-group bucketing missed it, so also excluding on that tag catches ghee
		// (and anything else tagged as a milk allergen but bucketed outside Dairy)
		// without touching the vegetarian path, which still allows dairy.
		query = `
			SELECT r.recipe_id FROM recipe_master r
			WHERE r.recipe_id = ANY($1) AND r.diet_type = ANY($2)
			  AND NOT EXISTS (
			      SELECT 1 FROM recipe_ingredient_mapping m
			      WHERE m.recipe_id = r.recipe_id
			        AND (m.food_group = ANY($3) OR m.ingredient_allergen_tag ILIKE '%Milk%'))`
		args = append(args, animalFoodGroups)
	}

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: diet filter: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: diet filter scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: diet filter rows: %w", err)
	}

	note := ""
	if p.Vegan {
		note = "vegan: also excludes Animal protein, Dairy, Fish, Dried fish, Fish product, Shellfish, Organ meat food groups, and any ingredient carrying a Milk allergen tag regardless of food_group (catches ghee)"
	}
	return ids, models.StepResult{
		Step: 4, Name: "Declared food practice", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: len(ids), Note: note,
	}, nil
}
