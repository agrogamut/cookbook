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

	query := `SELECT recipe_id FROM recipe_master WHERE recipe_id = ANY($1) AND diet_type = $2`
	args := []any{candidateIDs, p.DietType}
	if p.Vegan {
		query = `
			SELECT r.recipe_id FROM recipe_master r
			WHERE r.recipe_id = ANY($1) AND r.diet_type = $2
			  AND NOT EXISTS (
			      SELECT 1 FROM recipe_ingredient_mapping m
			      WHERE m.recipe_id = r.recipe_id AND m.food_group = ANY($3))`
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
		note = "vegan: also excludes Animal protein, Dairy, Fish, Dried fish, Fish product, Shellfish, Organ meat food groups"
	}
	return ids, models.StepResult{
		Step: 4, Name: "Declared food practice", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: len(ids), Note: note,
	}, nil
}
