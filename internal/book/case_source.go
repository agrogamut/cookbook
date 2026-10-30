package book

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
)

func loadSourceCards(ctx context.Context, pool *pgxpool.Pool, ids []string, category string, cp models.ChildProfile) (map[string]RecipeCard, []string, error) {
	allowed, gaps, err := engine.FilterSourceRecipes(ctx, pool, cp, ids)
	if err != nil {
		return nil, nil, err
	}
	recipes, err := casesource.Recipes(ctx, pool, allowed)
	if err != nil {
		return nil, nil, err
	}
	var omitted []string
	for _, gap := range gaps {
		omitted = append(omitted, fmt.Sprintf("%s [%s, row %d]: %s", gap.Source.RecipeID, gap.Field, gap.Source.Row, gap.Reason))
	}
	out := map[string]RecipeCard{}
	for _, r := range recipes {
		locale, err := casesource.Bengali(ctx, pool, r)
		if err != nil {
			return nil, nil, fmt.Errorf("source recipe locale: %w", err)
		}
		occ, err := casesource.Ingredients(ctx, pool, r.CandidateID)
		if err != nil {
			return nil, nil, err
		}
		f := r.Fields
		prep, err := strconv.Atoi(f["Prep_Time_Min"])
		if err != nil {
			return nil, nil, fmt.Errorf("source prep time: %w", err)
		}
		cook, err := strconv.Atoi(f["Cook_Time_Min"])
		if err != nil {
			return nil, nil, fmt.Errorf("source cook time: %w", err)
		}
		texture, band := f["Texture"], f["Budget_Band"]
		ingredients := make([]IngredientLine, 0, len(occ))
		for _, i := range occ {
			ingredients = append(ingredients, IngredientLine{Name: strings.TrimSpace(i.RawName), Quantity: i.Grams + " g", SourceOrdinal: i.Ordinal, SourceID: i.RawID, SourceMeaningID: i.MeaningID})
		}
		out[r.CandidateID] = RecipeCard{RecipeID: r.Citation.RecipeID, RecipeVersion: r.Citation.ArchiveSHA256, Title: f["Recipe_Name"], MealCategoryID: category,
			SelectionReasons: []string{"Source age, declared diet and confirmed allergens checked; no unmet mapped source prerequisites."},
			PrepTimeMinutes:  &prep, CookTimeMinutes: &cook, CostBand: &band, Ingredients: ingredients,
			MethodSteps: splitMethodSteps(r.PresentationText("Preparation_Method_FULL", "en", f["Preparation_Method_FULL"])), TextureServing: &texture, Serving: r.ServingGrams + " g",
			Safety: f["Safety_Rule"], ReviewStatus: f["Review_Status"], RegionCulture: f["Region_Culture"], Source: casesource.CandidateSource,
			SourceRevision: &r.Citation, SourceFields: f, SourceLocale: locale, SourcePolicy: r.Policy, StorageRule: f["Storage_Rule"], SourceCandidateID: r.CandidateID,
		}
	}
	return out, omitted, nil
}
