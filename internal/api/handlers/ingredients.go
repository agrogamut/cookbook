package handlers

import (
	"net/http"
	"strconv"
)

// Ingredients lists ingredient_nutrition_corrected: provider and IFCT-corrected values
// side by side, with value_source and verified so the UI never presents a placeholder
// group-level figure as a measured one.
func (h *Handlers) Ingredients(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT ingredient_id, english_name, bengali_name, food_group, ifct_food_code, ifct_food_name,
		       ifct_match_exactness, ifct_resolved_by, value_source, verified,
		       energy_kcal_100g, protein_g_100g, iron_mg_100g, calcium_mg_100g,
		       provider_energy_kcal_100g, provider_protein_g_100g, provider_iron_mg_100g, provider_calcium_mg_100g,
		       provider_review_status, provider_data_quality
		FROM ingredient_nutrition_corrected
		ORDER BY ingredient_id
		LIMIT $1`, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ingredient list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type ingredient struct {
		IngredientID            string   `json:"ingredient_id"`
		EnglishName             string   `json:"english_name"`
		BengaliName             *string  `json:"bengali_name"`
		FoodGroup               string   `json:"food_group"`
		IFCTFoodCode            *string  `json:"ifct_food_code"`
		IFCTFoodName            *string  `json:"ifct_food_name"`
		IFCTMatchExactness      *string  `json:"ifct_match_exactness"`
		IFCTResolvedBy          *string  `json:"ifct_resolved_by"`
		ValueSource             string   `json:"value_source"`
		Verified                bool     `json:"verified"`
		EnergyKcal100g          float64  `json:"energy_kcal_100g"`
		ProteinG100g            float64  `json:"protein_g_100g"`
		IronMg100g              float64  `json:"iron_mg_100g"`
		CalciumMg100g           float64  `json:"calcium_mg_100g"`
		ProviderEnergyKcal100g  float64  `json:"provider_energy_kcal_100g"`
		ProviderProteinG100g    float64  `json:"provider_protein_g_100g"`
		ProviderIronMg100g      float64  `json:"provider_iron_mg_100g"`
		ProviderCalciumMg100g   float64  `json:"provider_calcium_mg_100g"`
		ProviderReviewStatus    string   `json:"provider_review_status"`
		ProviderDataQuality     string   `json:"provider_data_quality"`
	}

	var out []ingredient
	for rows.Next() {
		var i ingredient
		if err := rows.Scan(&i.IngredientID, &i.EnglishName, &i.BengaliName, &i.FoodGroup,
			&i.IFCTFoodCode, &i.IFCTFoodName, &i.IFCTMatchExactness, &i.IFCTResolvedBy,
			&i.ValueSource, &i.Verified,
			&i.EnergyKcal100g, &i.ProteinG100g, &i.IronMg100g, &i.CalciumMg100g,
			&i.ProviderEnergyKcal100g, &i.ProviderProteinG100g, &i.ProviderIronMg100g, &i.ProviderCalciumMg100g,
			&i.ProviderReviewStatus, &i.ProviderDataQuality); err != nil {
			writeError(w, http.StatusInternalServerError, "ingredient scan failed: "+err.Error())
			return
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "ingredient rows failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, out)
}
