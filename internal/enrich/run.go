package enrich

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Summary is what one enrichment pass produced.
type Summary struct {
	CorpusRowsRead     int
	CorpusRowsKept     int
	CompositionRows    int
	RecipesMatched     int
	RecipesTotal       int
	IngredientsMatched int
	IngredientsTotal   int
}

// Run loads the external datasets and joins them to the provider data. It runs in one
// transaction: a partial enrichment, where some recipes carry a suggestion from an older
// corpus and others from a newer one, would make every coverage number meaningless.
//
// Re-running is safe and produces the same result: each stage clears its own output
// before rewriting it.
func Run(ctx context.Context, pool *pgxpool.Pool, dataDir string) (Summary, error) {
	var s Summary

	tx, err := pool.Begin(ctx)
	if err != nil {
		return s, fmt.Errorf("enrich: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	var runID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO enrichment_run (method_threshold, nutrition_threshold)
		 VALUES ($1,$2) RETURNING run_id`, MethodThreshold, NutritionThreshold,
	).Scan(&runID); err != nil {
		return s, fmt.Errorf("enrich: open run: %w", err)
	}

	// Clear the join outputs before reloading their sources. They hold foreign keys into
	// external_recipe and external_food_composition, so the dependent rows go first.
	for _, t := range []string{"recipe_method_external", "ingredient_nutrition_audit"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+t); err != nil {
			return s, fmt.Errorf("enrich: clear %s: %w", t, err)
		}
	}

	s.CorpusRowsRead, s.CorpusRowsKept, err = loadExternalRecipes(ctx, tx, dataDir)
	if err != nil {
		return s, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE external_source SET rows_loaded = $1 WHERE source_key = 'INDIAN-RECIPES'`,
		s.CorpusRowsKept); err != nil {
		return s, fmt.Errorf("enrich: record corpus size: %w", err)
	}

	s.CompositionRows, err = loadFoodComposition(ctx, tx, dataDir)
	if err != nil {
		return s, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE external_source SET rows_loaded = $1 WHERE source_key = 'IFCT-2017'`,
		s.CompositionRows); err != nil {
		return s, fmt.Errorf("enrich: record composition size: %w", err)
	}

	s.RecipesMatched, s.RecipesTotal, err = matchMethods(ctx, tx)
	if err != nil {
		return s, err
	}

	s.IngredientsMatched, s.IngredientsTotal, err = auditNutrition(ctx, tx)
	if err != nil {
		return s, err
	}

	if err := refreshGaps(ctx, tx); err != nil {
		return s, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE enrichment_run SET finished_at = now(), ok = true,
		    recipes_matched = $2, recipes_total = $3,
		    ingredients_matched = $4, ingredients_total = $5
		WHERE run_id = $1`,
		runID, s.RecipesMatched, s.RecipesTotal, s.IngredientsMatched, s.IngredientsTotal,
	); err != nil {
		return s, fmt.Errorf("enrich: close run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return s, fmt.Errorf("enrich: commit: %w", err)
	}
	return s, nil
}
