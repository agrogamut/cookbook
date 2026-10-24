package handlers

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/madamgy/recipie/internal/casesource"
)

// RecipeDetail returns the provider's method beside the external suggestion, and both
// the provider and IFCT-corrected nutrition figures, everything already carrying its
// own provenance columns from recipe_method_card and recipe_nutrition_recomputed.
func (h *Handlers) RecipeDetail(w http.ResponseWriter, r *http.Request) {
	recipeID := chi.URLParam(r, "recipeID")
	// Chi matches RawPath when present; otherwise URL.Path is already decoded.
	if r.URL.RawPath != "" {
		var err error
		recipeID, err = url.PathUnescape(recipeID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recipe identifier")
			return
		}
	}
	if casesource.IsCandidate(recipeID) {
		h.caseRecipeDetail(w, r, recipeID)
		return
	}

	var method struct {
		RecipeID                   string   `json:"recipe_id"`
		RecipeName                 string   `json:"recipe_name"`
		RegionCulture              string   `json:"region_culture"`
		ProviderMethod             string   `json:"provider_method"`
		ProviderReviewStatus       string   `json:"provider_review_status"`
		SuggestedMethodExternal    *string  `json:"suggested_method_external"`
		SuggestedMethodSource      *string  `json:"suggested_method_source"`
		SuggestedMethodURL         *string  `json:"suggested_method_url"`
		SuggestedMethodConfidence  *float64 `json:"suggested_method_confidence"`
		SuggestedMethodRegionMatch *string  `json:"suggested_method_region_match"`
		SuggestionDisclosure       string   `json:"suggestion_disclosure"`
	}
	err := h.pool.QueryRow(r.Context(), `
		SELECT recipe_id, recipe_name, region_culture, provider_method, provider_review_status,
		       suggested_method_external, suggested_method_source, suggested_method_url,
		       suggested_method_confidence, suggested_method_region_match, suggestion_disclosure
		FROM recipe_method_card WHERE recipe_id = $1`, recipeID).Scan(
		&method.RecipeID, &method.RecipeName, &method.RegionCulture, &method.ProviderMethod, &method.ProviderReviewStatus,
		&method.SuggestedMethodExternal, &method.SuggestedMethodSource, &method.SuggestedMethodURL,
		&method.SuggestedMethodConfidence, &method.SuggestedMethodRegionMatch, &method.SuggestionDisclosure)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recipe not found: "+recipeID)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "recipe lookup failed: "+err.Error())
		return
	}

	var nutrition struct {
		EnergyKcal         float64  `json:"energy_kcal"`
		ProteinG           float64  `json:"protein_g"`
		IronMg             float64  `json:"iron_mg"`
		CalciumMg          float64  `json:"calcium_mg"`
		IngredientCoverage float64  `json:"ingredient_coverage"`
		FullyVerified      bool     `json:"fully_verified"`
		ProviderEnergyKcal float64  `json:"provider_energy_kcal"`
		ProviderProteinG   float64  `json:"provider_protein_g"`
		ProviderIronMg     float64  `json:"provider_iron_mg"`
		ProviderCalciumMg  float64  `json:"provider_calcium_mg"`
		EnergyPctDiff      *float64 `json:"energy_pct_diff"`
		IronPctDiff        *float64 `json:"iron_pct_diff"`
		ValueKind          string   `json:"value_kind"`
		Formula            string   `json:"formula"`
	}
	err = h.pool.QueryRow(r.Context(), `
		SELECT energy_kcal, protein_g, iron_mg, calcium_mg, ingredient_coverage, fully_verified,
		       provider_energy_kcal, provider_protein_g, provider_iron_mg, provider_calcium_mg,
		       energy_pct_diff, iron_pct_diff, value_kind, formula
		FROM recipe_nutrition_recomputed WHERE recipe_id = $1`, recipeID).Scan(
		&nutrition.EnergyKcal, &nutrition.ProteinG, &nutrition.IronMg, &nutrition.CalciumMg,
		&nutrition.IngredientCoverage, &nutrition.FullyVerified,
		&nutrition.ProviderEnergyKcal, &nutrition.ProviderProteinG, &nutrition.ProviderIronMg, &nutrition.ProviderCalciumMg,
		&nutrition.EnergyPctDiff, &nutrition.IronPctDiff, &nutrition.ValueKind, &nutrition.Formula)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "nutrition lookup failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"method":    method,
		"nutrition": nutrition,
	})
}

func (h *Handlers) caseRecipeDetail(w http.ResponseWriter, r *http.Request, id string) {
	recipes, err := casesource.Recipes(r.Context(), h.pool, []string{id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "source recipe lookup failed: "+err.Error())
		return
	}
	if len(recipes) != 1 {
		writeError(w, http.StatusNotFound, "recipe not found: "+id)
		return
	}
	recipe := recipes[0]
	recipe.Ingredients, err = casesource.Ingredients(r.Context(), h.pool, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "source ingredients lookup failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method":        map[string]any{"recipe_id": recipe.Citation.RecipeID, "recipe_name": recipe.Fields["Recipe_Name"], "region_culture": recipe.Fields["Region_Culture"], "provider_method": recipe.Fields["Preparation_Method_FULL"], "provider_review_status": recipe.Fields["Review_Status"]},
		"nutrition":     nil,
		"nutrition_gap": "Source estimates have no verified ingredient nutrient crosswalk or measured cooked yield.",
		"case_source":   recipe,
	})
}
