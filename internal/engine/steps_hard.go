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

// ageStep is engine step 1. It returns the whole corpus and removes nothing.
//
// It was a hard filter, and the filter was the right shape for a non-clinical operator: a
// recipe outside a child's age band is not for that child, and nobody downstream could be
// relied on to notice. The input is now a verified doctor, and an empty page is worse than
// a short one that opens with the closest fits. Age still decides the ordering -- see
// applyAgeRank, which partitions rather than removes -- so an out-of-band recipe surfaces
// only once the in-band pool has run out. See
// docs/superpowers/specs/2026-09-05-direct-generation-design.md.
//
// It has no "candidate IDs in" parameter because it is always the first step in the
// pipeline, and no profile parameter because it no longer looks at the child at all.
func ageStep(ctx context.Context, pool *pgxpool.Pool) ([]string, models.StepResult, error) {
	rows, err := pool.Query(ctx, `SELECT recipe_id FROM engine_candidate`)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age step: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: age step scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: age step rows: %w", err)
	}

	return ids, models.StepResult{
		Step: 1, Name: "Age / feeding stage", Kind: "record",
		CandidatesIn: len(ids), CandidatesOut: len(ids),
		Note: "age orders the list rather than filtering it; see step 1's ranker half",
	}, nil
}

// inBandIDs returns the recipes whose [min_age_months, max_age_months] contains the child's
// age. This is the query ageFilter used to be, kept because applyAgeRank needs exactly it to
// decide which half of the partition a recipe belongs in.
func inBandIDs(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT recipe_id FROM engine_candidate WHERE min_age_months <= $1 AND max_age_months >= $1`,
		p.AgeMonths)
	if err != nil {
		return nil, fmt.Errorf("engine: in-band recipes: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("engine: in-band recipes scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engine: in-band recipes rows: %w", err)
	}
	return ids, nil
}

// allergyFilter is engine step 2, a hard filter that is never relaxed and never
// overridable. A recipe is excluded if its own allergen_tags names a declared allergen,
// or any mapped ingredient's ingredient_allergen_tag does -- both columns are verified
// clean per CLAUDE.md ("Verified clean": zero allergen-propagation omissions), so this
// is a straight substring match against real data, not a fuzzy join. It is also excluded
// if a mapped ingredient appears in ingredient_allergen_override, which covers the small
// set of ingredients ingredient_master itself left untagged despite allergen_mapping
// documenting them as a derivative or example of a declared group (Groundnut oil, Mustard
// oil, Mustard seeds -- see migration 0024).
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
	//
	// The second OR EXISTS catches what corpus_tag matching cannot: ingredient_master left
	// Groundnut oil and Mustard oil/seeds untagged even though allergen_mapping's own text
	// names them as Peanut/Mustard. ingredient_allergen_override (migration 0024) records
	// that correction without touching ingredient_master itself.
	rows, err := pool.Query(ctx, `
		SELECT r.recipe_id
		FROM engine_candidate r
		WHERE r.recipe_id = ANY($1)
		  AND NOT EXISTS (
		      SELECT 1 FROM allergen_tag_vocabulary v
		      WHERE v.allergen_group = ANY($2)
		        AND v.corpus_tag IS NOT NULL
		        AND (r.allergen_tags ILIKE '%' || v.corpus_tag || '%'
		             OR EXISTS (
		                 SELECT 1 FROM engine_candidate_ingredient m
		                 WHERE m.recipe_id = r.recipe_id
		                   AND m.ingredient_allergen_tag ILIKE '%' || v.corpus_tag || '%'))
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM engine_candidate_ingredient m
		      JOIN ingredient_allergen_override o ON o.ingredient_id = m.ingredient_id
		      WHERE m.recipe_id = r.recipe_id
		        AND o.allergen_group = ANY($2)
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

	// A declared allergen whose allergen_group has no corpus_tag and no override row
	// (Crustacean/Mollusc, Sulphites, Tree nuts as verified live) correctly excludes zero
	// recipes -- there's genuinely nothing tagged. That is indistinguishable from an
	// ordinary no-op exclusion unless it's called out explicitly, so name it here. Mustard
	// used to be in this list; it no longer is, because ingredient_allergen_override now
	// covers it (see unscreenedGroups).
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
