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
		CandidatesIn:  -1, // no upstream step; the caller fills this in from the total recipe count
		CandidatesOut: len(ids),
	}, nil
}

// allergyFilter is engine step 2, a hard filter that is never relaxed and never
// overridable. A recipe is excluded if its own allergen_tags names a declared allergen,
// or any mapped ingredient's ingredient_allergen_tag does -- both columns are verified
// clean per CLAUDE.md ("Verified clean": zero allergen-propagation omissions), so this
// is a straight substring match against real data, not a fuzzy join.
func allergyFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, []string, error) {
	stepIn := len(candidateIDs)
	if len(p.Allergens) == 0 || stepIn == 0 {
		return candidateIDs, models.StepResult{
			Step: 2, Name: "Allergy / intolerance / safety", Kind: "hard_filter",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no allergens declared, step is a no-op",
		}, nil, nil
	}

	validRows, err := pool.Query(ctx, `SELECT DISTINCT allergen_group FROM allergen_mapping WHERE allergen_group = ANY($1)`, p.Allergens)
	if err != nil {
		return nil, models.StepResult{}, nil, fmt.Errorf("engine: allergy filter validate: %w", err)
	}
	valid := make(map[string]bool)
	for validRows.Next() {
		var g string
		if err := validRows.Scan(&g); err != nil {
			validRows.Close()
			return nil, models.StepResult{}, nil, fmt.Errorf("engine: allergy filter validate scan: %w", err)
		}
		valid[g] = true
	}
	if err := validRows.Err(); err != nil {
		validRows.Close()
		return nil, models.StepResult{}, nil, fmt.Errorf("engine: allergy filter validate rows: %w", err)
	}
	validRows.Close()

	var unmatched []string
	for _, a := range p.Allergens {
		if !valid[a] {
			unmatched = append(unmatched, a)
		}
	}
	if len(unmatched) > 0 {
		return nil, models.StepResult{}, nil, fmt.Errorf("engine: allergy filter: unrecognized allergen(s) %v — must match allergen_mapping.allergen_group exactly: %w", unmatched, ErrInvalidProfile)
	}

	// Join through allergen_tag_vocabulary rather than matching am.allergen_group
	// directly: allergen_mapping's vocabulary (e.g. "Wheat") and the corpus's actual tag
	// strings (e.g. "Gluten-containing cereal") differ for some groups. Matching only
	// rows with a non-NULL corpus_tag means a declared allergen whose group has no
	// corpus tag correctly excludes nothing, rather than being silently coerced into a
	// (wrong) direct match against a vocabulary word the corpus never uses.
	rows, err := pool.Query(ctx, `
		SELECT r.recipe_id
		FROM recipe_master r
		WHERE r.recipe_id = ANY($1)
		  AND NOT EXISTS (
		      SELECT 1 FROM allergen_tag_vocabulary v
		      WHERE v.allergen_group = ANY($2)
		        AND v.corpus_tag IS NOT NULL
		        AND (r.allergen_tags ILIKE '%' || v.corpus_tag || '%'
		             OR EXISTS (
		                 SELECT 1 FROM recipe_ingredient_mapping m
		                 WHERE m.recipe_id = r.recipe_id
		                   AND m.ingredient_allergen_tag ILIKE '%' || v.corpus_tag || '%'))
		  )`,
		candidateIDs, p.Allergens)
	if err != nil {
		return nil, models.StepResult{}, nil, fmt.Errorf("engine: allergy filter: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, nil, fmt.Errorf("engine: allergy filter scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, nil, fmt.Errorf("engine: allergy filter rows: %w", err)
	}

	// A declared allergen whose allergen_group has no corpus_tag at all (Crustacean/
	// Mollusc, Mustard, Sulphites, Tree nuts as verified live) correctly excludes zero
	// recipes -- there's genuinely nothing tagged. That is indistinguishable from an
	// ordinary no-op exclusion unless it's called out explicitly, so name it here.
	absent, err := unscreenedGroups(ctx, pool, p.Allergens)
	if err != nil {
		return nil, models.StepResult{}, nil, err
	}

	note := ""
	if len(absent) > 0 {
		note = fmt.Sprintf("declared allergen(s) %v have no matching tag anywhere in the recipe corpus -- excluded 0 recipes because none carry this tag, not because the filter failed", absent)
	}

	return ids, models.StepResult{
		Step: 2, Name: "Allergy / intolerance / safety", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: len(ids), Note: note,
	}, absent, nil
}
