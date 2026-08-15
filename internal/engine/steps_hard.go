// Package engine implements the 14-step recipe selection pipeline from CLAUDE.md, "The
// engine spec already exists". Every step returns a models.StepResult alongside its
// filtered candidate list, so the caller can show exactly which step removed which
// recipe -- the single most useful screen in the tool, per CLAUDE.md's "why this result"
// panel.
package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// ageFilter is engine step 1, a hard filter that is never relaxed. It has no "candidate
// IDs in" parameter because it is always the first step in the pipeline.
func ageFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) ([]string, models.StepResult, error) {
	rows, err := pool.Query(ctx,
		`SELECT recipe_id FROM recipe_master WHERE min_age_months <= $1 AND max_age_months >= $1`,
		p.AgeMonths)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age filter: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: age filter scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age filter rows: %w", err)
	}

	return ids, models.StepResult{
		Step: 1, Name: "Age / feeding stage", Kind: "hard_filter",
		CandidatesIn: -1, // no upstream step; the caller fills this in from the total recipe count
		CandidatesOut: len(ids),
	}, nil
}

// allergyFilter is engine step 2, a hard filter that is never relaxed and never
// overridable. A recipe is excluded if its own allergen_tags names a declared allergen,
// or any mapped ingredient's ingredient_allergen_tag does -- both columns are verified
// clean per CLAUDE.md ("Verified clean": zero allergen-propagation omissions), so this
// is a straight substring match against real data, not a fuzzy join.
func allergyFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, error) {
	stepIn := len(candidateIDs)
	if len(p.Allergens) == 0 || stepIn == 0 {
		return candidateIDs, models.StepResult{
			Step: 2, Name: "Allergy / intolerance / safety", Kind: "hard_filter",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no allergens declared, step is a no-op",
		}, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT r.recipe_id
		FROM recipe_master r
		WHERE r.recipe_id = ANY($1)
		  AND NOT EXISTS (
		      SELECT 1 FROM allergen_mapping am
		      WHERE am.allergen_group = ANY($2)
		        AND (r.allergen_tags ILIKE '%' || am.allergen_group || '%'
		             OR EXISTS (
		                 SELECT 1 FROM recipe_ingredient_mapping m
		                 WHERE m.recipe_id = r.recipe_id
		                   AND m.ingredient_allergen_tag ILIKE '%' || am.allergen_group || '%'))
		  )`,
		candidateIDs, p.Allergens)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: allergy filter: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: allergy filter scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: allergy filter rows: %w", err)
	}

	return ids, models.StepResult{
		Step: 2, Name: "Allergy / intolerance / safety", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: len(ids),
	}, nil
}
