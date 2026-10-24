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
	var sourceIDs []string
	for _, id := range ids {
		if casesource.IsCandidate(id) {
			sourceIDs = append(sourceIDs, id)
		}
	}
	if len(sourceIDs) == 0 {
		return ids, nil, nil
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
	var gaps []casesource.Gap
	for _, r := range recipes {
		missing := casesource.Assess(r, input)
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
	active := false
	for _, id := range ids {
		active = active || casesource.IsCandidate(id)
	}
	if !active {
		return ids, nil, nil
	}
	out, gaps, err := FilterSourceRecipes(ctx, pool, p, ids)
	if err != nil {
		return nil, nil, err
	}
	step := &models.StepResult{Step: 1, Name: "Case source eligibility", Kind: "hard_filter", CandidatesIn: len(ids), CandidatesOut: len(out), Note: "Source age, diet, allergens and required source facts checked before ranking. Collection membership does not establish eligibility. Existing corpus rules are unchanged."}
	seen := map[string]bool{}
	for _, g := range gaps {
		key := fmt.Sprintf("%d:%s:%s", g.Source.Case, g.Code, g.Reason)
		if seen[key] {
			continue
		}
		seen[key] = true
		step.Excluded = append(step.Excluded, models.ExclusionReason{RecipeID: casesource.CandidateID(g.Source.ArchiveSHA256, g.Source.RecipeID), RecipeName: g.Source.RecipeID, Reason: fmt.Sprintf("case %d %s row %d [%s]: %s", g.Source.Case, g.Source.Sheet, g.Source.Row, g.Field, g.Reason)})
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
