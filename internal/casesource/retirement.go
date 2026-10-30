package casesource

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RetiredRecipes keeps stored facts readable while excluding unsupported replacements.
func RetiredRecipes(ctx context.Context, pool *pgxpool.Pool, ids []string) (map[string]string, error) {
	rows, err := pool.Query(ctx, `SELECT DISTINCT m.recipe_id FROM engine_candidate_ingredient_existing m
	    JOIN ingredient_master i USING(ingredient_id)
	    WHERE m.recipe_id=ANY($1) AND i.ingredient_id='ING0075' AND i.english_name='Tofu'
	    UNION
	    SELECT DISTINCT candidate_id FROM case_source_recipe_ingredient
	    WHERE candidate_id=ANY($1) AND ((btrim(raw_id)='ING0075' AND btrim(raw_name)='Tofu')
	      OR (btrim(raw_id)='WBING0034' AND btrim(raw_name)='Tofu, firm'))`, ids)
	if err != nil {
		return nil, fmt.Errorf("read retired tofu recipes: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = "Tofu recipe retired from selection. Whole-soyabean quantity, preparation, texture, yield and nutrient/cost basis for a reformulation are missing."
	}
	return out, rows.Err()
}
