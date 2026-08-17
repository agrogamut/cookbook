package handlers

import "net/http"

// ReferenceRegions returns region_focus: the 9 scoped regions with their tier and
// derived rank_weight, so the frontend's region picker matches what the engine's step 7
// actually uses instead of a hardcoded list drifting from it.
func (h *Handlers) ReferenceRegions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT region_culture, country, focus_tier, rank_weight, enrichment_scope, rationale
		FROM region_focus ORDER BY focus_tier, region_culture`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "region list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type region struct {
		RegionCulture    string  `json:"region_culture"`
		Country          string  `json:"country"`
		FocusTier        int     `json:"focus_tier"`
		RankWeight       float64 `json:"rank_weight"`
		EnrichmentScope  bool    `json:"enrichment_scope"`
		Rationale        string  `json:"rationale"`
	}
	// Nil-slice-marshals-to-null would violate the frontend's Region[] contract.
	out := []region{}
	for rows.Next() {
		var reg region
		if err := rows.Scan(&reg.RegionCulture, &reg.Country, &reg.FocusTier, &reg.RankWeight, &reg.EnrichmentScope, &reg.Rationale); err != nil {
			writeError(w, http.StatusInternalServerError, "region scan failed: "+err.Error())
			return
		}
		out = append(out, reg)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "region rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ReferenceCuisines returns cuisine_option, already built on a COUNT(*) > 0 join so it
// can never offer a cuisine that returns an empty result.
func (h *Handlers) ReferenceCuisines(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT culture_code, cuisine_cluster, country, state_province, region_culture,
		       focus_tier, rank_weight, recipe_count
		FROM cuisine_option ORDER BY focus_tier, recipe_count DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cuisine list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type cuisine struct {
		CultureCode     string  `json:"culture_code"`
		CuisineCluster  string  `json:"cuisine_cluster"`
		Country         string  `json:"country"`
		StateProvince   *string `json:"state_province"`
		RegionCulture   string  `json:"region_culture"`
		FocusTier       int     `json:"focus_tier"`
		RankWeight      float64 `json:"rank_weight"`
		RecipeCount     int     `json:"recipe_count"`
	}
	// Nil-slice-marshals-to-null would violate the frontend's Cuisine[] contract.
	out := []cuisine{}
	for rows.Next() {
		var c cuisine
		if err := rows.Scan(&c.CultureCode, &c.CuisineCluster, &c.Country, &c.StateProvince,
			&c.RegionCulture, &c.FocusTier, &c.RankWeight, &c.RecipeCount); err != nil {
			writeError(w, http.StatusInternalServerError, "cuisine scan failed: "+err.Error())
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "cuisine rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ReferenceNutritionTargets returns the 13 NT00-NT12 rows with their scored weights and
// the verbatim hard_exclusions/soft_penalties guidance text (see the plan's Architecture
// note on why that text is surfaced, not compiled into a filter).
func (h *Handlers) ReferenceNutritionTargets(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT target_code, target_name, target_category, age_from_months, age_to_months,
		       trigger_input, trigger_logic,
		       recipe_score_energy, recipe_score_protein, recipe_score_iron, recipe_score_calcium,
		       recipe_score_fruitveg, recipe_score_diversity, recipe_score_cost,
		       hard_exclusions, soft_penalties
		FROM nutrition_target_master ORDER BY target_code`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "nutrition target list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type target struct {
		TargetCode      string  `json:"target_code"`
		TargetName      string  `json:"target_name"`
		TargetCategory  *string `json:"target_category"`
		AgeFromMonths   int     `json:"age_from_months"`
		AgeToMonths     int     `json:"age_to_months"`
		TriggerInput    *string `json:"trigger_input"`
		TriggerLogic    *string `json:"trigger_logic"`
		ScoreEnergy     int     `json:"recipe_score_energy"`
		ScoreProtein    int     `json:"recipe_score_protein"`
		ScoreIron       int     `json:"recipe_score_iron"`
		ScoreCalcium    int     `json:"recipe_score_calcium"`
		ScoreFruitVeg   int     `json:"recipe_score_fruitveg"`
		ScoreDiversity  int     `json:"recipe_score_diversity"`
		ScoreCost       int     `json:"recipe_score_cost"`
		HardExclusions  *string `json:"hard_exclusions"`
		SoftPenalties   *string `json:"soft_penalties"`
	}
	// Nil-slice-marshals-to-null would violate the frontend's NutritionTarget[] contract.
	out := []target{}
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.TargetCode, &t.TargetName, &t.TargetCategory, &t.AgeFromMonths, &t.AgeToMonths,
			&t.TriggerInput, &t.TriggerLogic,
			&t.ScoreEnergy, &t.ScoreProtein, &t.ScoreIron, &t.ScoreCalcium,
			&t.ScoreFruitVeg, &t.ScoreDiversity, &t.ScoreCost,
			&t.HardExclusions, &t.SoftPenalties); err != nil {
			writeError(w, http.StatusInternalServerError, "nutrition target scan failed: "+err.Error())
			return
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "nutrition target rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
