package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/models"
)

func FilterSourceRecipes(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, ids []string) ([]string, []casesource.Gap, error) {
	retired, err := casesource.RetiredRecipes(ctx, pool, ids)
	if err != nil {
		return nil, nil, err
	}
	var retirementGaps []casesource.Gap
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if reason, ok := retired[id]; ok {
			retirementGaps = append(retirementGaps, casesource.Gap{Code: "whole_soy_reformulation_missing", Scope: "recipe", Field: "Ingredient_IDs", Reason: reason, Evidence: "Present tofu ingredient occurrence", Source: casesource.Citation{RecipeID: id}})
		} else {
			kept = append(kept, id)
		}
	}
	ids = kept
	var sourceIDs []string
	for _, id := range ids {
		if casesource.IsCandidate(id) {
			sourceIDs = append(sourceIDs, id)
		}
	}
	if len(sourceIDs) == 0 {
		return ids, retirementGaps, nil
	}
	currentRows, err := pool.Query(ctx, `SELECT candidate_id FROM case_source_recipe_current WHERE candidate_id=ANY($1)`, sourceIDs)
	if err != nil {
		return nil, nil, err
	}
	current := map[string]bool{}
	for currentRows.Next() {
		var id string
		if err := currentRows.Scan(&id); err != nil {
			currentRows.Close()
			return nil, nil, err
		}
		current[id] = true
	}
	err = currentRows.Err()
	currentRows.Close()
	if err != nil {
		return nil, nil, err
	}
	recipes, err := casesource.Recipes(ctx, pool, sourceIDs)
	if err != nil {
		return nil, nil, err
	}
	input := casesource.Input{AgeMonths: p.AgeMonths, Diet: p.DietType, Vegan: p.Vegan}
	if len(p.Allergens) > 0 {
		rows, err := pool.Query(ctx, `SELECT allergen_id FROM allergen_mapping WHERE allergen_group=ANY($1) ORDER BY allergen_id`, p.Allergens)
		if err != nil {
			return nil, nil, fmt.Errorf("source allergen mapping: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, nil, fmt.Errorf("source allergen scan: %w", err)
			}
			input.AllergenCodes = append(input.AllergenCodes, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("source allergen rows: %w", err)
		}
	}
	actions, err := ActiveClinicalRuleActions(ctx, pool, p)
	if err != nil {
		return nil, nil, err
	}
	for _, action := range actions {
		input.UnmappedClinical = append(input.UnmappedClinical, action.RuleID+" ("+action.ClinicalDomain+")")
	}
	if p.SpecialCareCondition != "" {
		input.UnmappedClinical = append(input.UnmappedClinical, p.SpecialCareCondition)
	}
	allowed := make(map[string]bool, len(recipes))
	gaps := retirementGaps
	for _, r := range recipes {
		missing := casesource.Assess(r, input)
		if !current[r.CandidateID] {
			missing = append(missing, casesource.Gap{Code: "source_revision_not_current", Scope: "recipe", Field: "archive_sha256", Reason: "The recipe revision is historical and cannot enter current selection.", Evidence: r.Citation.ArchiveSHA256, Source: r.Citation})
		}
		allowed[r.CandidateID] = len(missing) == 0
		gaps = append(gaps, missing...)
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !casesource.IsCandidate(id) || allowed[id] {
			out = append(out, id)
		}
	}
	return out, gaps, nil
}

func sourceStep(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, ids []string) ([]string, *models.StepResult, error) {
	out, gaps, err := FilterSourceRecipes(ctx, pool, p, ids)
	if err != nil {
		return nil, nil, err
	}
	if len(gaps) == 0 && len(out) == len(ids) {
		active := false
		for _, id := range ids {
			active = active || casesource.IsCandidate(id)
		}
		if !active {
			return ids, nil, nil
		}
	}
	step := &models.StepResult{Step: 1, Name: "Case source eligibility", Kind: "hard_filter", CandidatesIn: len(ids), CandidatesOut: len(out), Note: "Source age, diet, allergens and required source facts checked before ranking. Collection membership does not establish eligibility. Present-ingredient retirement applies to both corpora."}
	seen := map[string]bool{}
	for _, g := range gaps {
		key := fmt.Sprintf("%d:%s:%s", g.Source.Case, g.Code, g.Reason)
		if seen[key] {
			continue
		}
		seen[key] = true
		id := g.Source.RecipeID
		if g.Source.ArchiveSHA256 != "" {
			id = casesource.CandidateID(g.Source.ArchiveSHA256, g.Source.RecipeID)
		}
		step.Excluded = append(step.Excluded, models.ExclusionReason{RecipeID: id, RecipeName: g.Source.RecipeID, Reason: fmt.Sprintf("case %d %s row %d [%s]: %s", g.Source.Case, g.Source.Sheet, g.Source.Row, g.Field, g.Reason)})
	}
	return out, step, nil
}

func sourceRanked(ctx context.Context, pool *pgxpool.Pool, ids []string) ([]models.RankedRecipe, error) {
	recipes, err := casesource.Recipes(ctx, pool, ids)
	if err != nil {
		return nil, err
	}
	out := make([]models.RankedRecipe, 0, len(recipes))
	for _, r := range recipes {
		f := r.Fields
		diet := f["Diet_Type"]
		if strings.EqualFold(diet, "Non-vegetarian") {
			diet = "Non-vegetarian"
		}
		out = append(out, models.RankedRecipe{RecipeID: r.CandidateID, RecipeName: f["Recipe_Name"], RegionCulture: f["Region_Culture"], MealType: f["Meal_Type"], DietType: diet, ClinicalTag: f["Clinical_Tag"], AgeGroup: f["Age_Group"], Source: casesource.CandidateSource, ValueKind: "unavailable"})
	}
	return out, nil
}
