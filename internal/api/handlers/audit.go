package handlers

import (
	"net/http"
	"time"
)

// NutritionAudit returns nutrition_discrepancy_report: the exact-name-match findings to
// hand the provider, never the broader probable-match worklist mixed in.
func (h *Handlers) NutritionAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT ingredient_id, english_name, matched_ifct_food, used_in_recipes,
		       provider_energy, external_energy, energy_pct_diff,
		       provider_protein, external_protein, protein_pct_diff,
		       provider_iron, external_iron, iron_pct_diff,
		       provider_calcium, external_calcium, calcium_pct_diff
		FROM nutrition_discrepancy_report`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "nutrition audit failed: "+err.Error())
		return
	}
	defer rows.Close()

	type discrepancy struct {
		IngredientID     string   `json:"ingredient_id"`
		EnglishName      string   `json:"english_name"`
		MatchedIFCTFood  *string  `json:"matched_ifct_food"`
		UsedInRecipes    int      `json:"used_in_recipes"`
		ProviderEnergy   *float64 `json:"provider_energy"`
		ExternalEnergy   *float64 `json:"external_energy"`
		EnergyPctDiff    *float64 `json:"energy_pct_diff"`
		ProviderProtein  *float64 `json:"provider_protein"`
		ExternalProtein  *float64 `json:"external_protein"`
		ProteinPctDiff   *float64 `json:"protein_pct_diff"`
		ProviderIron     *float64 `json:"provider_iron"`
		ExternalIron     *float64 `json:"external_iron"`
		IronPctDiff      *float64 `json:"iron_pct_diff"`
		ProviderCalcium  *float64 `json:"provider_calcium"`
		ExternalCalcium  *float64 `json:"external_calcium"`
		CalciumPctDiff   *float64 `json:"calcium_pct_diff"`
	}

	// Nil-slice-marshals-to-null would violate the frontend's NutritionDiscrepancy[]
	// contract on a zero-row result.
	out := []discrepancy{}
	for rows.Next() {
		var d discrepancy
		if err := rows.Scan(&d.IngredientID, &d.EnglishName, &d.MatchedIFCTFood, &d.UsedInRecipes,
			&d.ProviderEnergy, &d.ExternalEnergy, &d.EnergyPctDiff,
			&d.ProviderProtein, &d.ExternalProtein, &d.ProteinPctDiff,
			&d.ProviderIron, &d.ExternalIron, &d.IronPctDiff,
			&d.ProviderCalcium, &d.ExternalCalcium, &d.CalciumPctDiff); err != nil {
			writeError(w, http.StatusInternalServerError, "nutrition audit scan failed: "+err.Error())
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "nutrition audit rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// Gaps returns the full gap register, all 16 rows, so /audit/gaps never needs its own
// invented severity scale -- it renders exactly what gap_register already carries.
func (h *Handlers) Gaps(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT gap_id, severity, area, source_table, source_column, description,
		       affected_rows, measured_by, ui_behaviour, resolution_path, measured_at
		FROM gap_register ORDER BY
		  CASE severity WHEN 'blocker' THEN 1 WHEN 'major' THEN 2 WHEN 'minor' THEN 3 ELSE 4 END,
		  gap_id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gap register failed: "+err.Error())
		return
	}
	defer rows.Close()

	type gap struct {
		GapID          string     `json:"gap_id"`
		Severity       string     `json:"severity"`
		Area           string     `json:"area"`
		SourceTable    *string    `json:"source_table"`
		SourceColumn   *string    `json:"source_column"`
		Description    string     `json:"description"`
		AffectedRows   *int       `json:"affected_rows"`
		MeasuredBy     string     `json:"measured_by"`
		UIBehaviour    string     `json:"ui_behaviour"`
		ResolutionPath string     `json:"resolution_path"`
		MeasuredAt     *time.Time `json:"measured_at"`
	}

	// Same nil-vs-empty-array concern as NutritionAudit above.
	out := []gap{}
	for rows.Next() {
		var g gap
		if err := rows.Scan(&g.GapID, &g.Severity, &g.Area, &g.SourceTable, &g.SourceColumn,
			&g.Description, &g.AffectedRows, &g.MeasuredBy, &g.UIBehaviour, &g.ResolutionPath, &g.MeasuredAt); err != nil {
			writeError(w, http.StatusInternalServerError, "gap register scan failed: "+err.Error())
			return
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "gap register rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
