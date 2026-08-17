package handlers

import (
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
)

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

// ReferenceAllergens returns allergen_tag_vocabulary: the eleven provider allergen groups
// with the literal corpus tag each maps to, and whether declaring it screens anything at
// all. Four groups have no corpus tag (migration 0011 verified this against the live
// corpus), so screens is false for them and the picker built from this endpoint must show
// that state rather than offering them as though they filter.
//
// screens is derived from corpus_tag rather than stored, so the two can never disagree.
func (h *Handlers) ReferenceAllergens(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT allergen_group, corpus_tag, note, corpus_tag IS NOT NULL AS screens
		FROM allergen_tag_vocabulary
		ORDER BY (corpus_tag IS NULL), allergen_group`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "allergen list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type allergen struct {
		AllergenGroup string  `json:"allergen_group"`
		CorpusTag     *string `json:"corpus_tag"`
		Note          string  `json:"note"`
		Screens       bool    `json:"screens"`
	}
	out := []allergen{}
	for rows.Next() {
		var a allergen
		if err := rows.Scan(&a.AllergenGroup, &a.CorpusTag, &a.Note, &a.Screens); err != nil {
			writeError(w, http.StatusInternalServerError, "allergen scan failed: "+err.Error())
			return
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "allergen rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ReferenceClinicalMarkers returns the 28 distinct clinical_rule_master.trigger_field
// values with the rules behind each, so a client can offer a clinical-flags control whose
// keys are guaranteed to resolve. ChildProfile.ClinicalFlags is validated against exactly
// this vocabulary and an unrecognized key returns 400, so a free-text input is a trap.
//
// escalates says whether setting this marker will hold generation rather than filter it.
// An operator deserves to know that before typing, not after an empty result page.
func (h *Handlers) ReferenceClinicalMarkers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT trigger_field,
		       string_agg(DISTINCT rule_id, ', ' ORDER BY rule_id)          AS rule_ids,
		       string_agg(DISTINCT clinical_domain, ', ')                   AS domains,
		       string_agg(DISTINCT engine_action, ' | ')                    AS engine_actions,
		       string_agg(DISTINCT coalesce(specialist_required, ''), ' | ') AS specialist_required,
		       bool_or(human_approval_level = 'Specialist clinical approval') AS escalates
		FROM clinical_rule_master
		GROUP BY trigger_field
		ORDER BY trigger_field`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "clinical marker list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type marker struct {
		TriggerField       string `json:"trigger_field"`
		RuleIDs            string `json:"rule_ids"`
		Domains            string `json:"domains"`
		EngineActions      string `json:"engine_actions"`
		SpecialistRequired string `json:"specialist_required"`
		Escalates          bool   `json:"escalates"`
	}
	out := []marker{}
	for rows.Next() {
		var m marker
		if err := rows.Scan(&m.TriggerField, &m.RuleIDs, &m.Domains, &m.EngineActions,
			&m.SpecialistRequired, &m.Escalates); err != nil {
			writeError(w, http.StatusInternalServerError, "clinical marker scan failed: "+err.Error())
			return
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "clinical marker rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// enumColumns are the recipe_master columns a client may offer as a filter or a ranker
// input. Each is returned with a live count so a control can show how much corpus sits
// behind each option, and so a zero-count value is visibly zero rather than absent.
//
// prep_time_min and cook_time_min are included as enums on purpose: the corpus holds four
// and six distinct values respectively, so a free minute entry would imply a precision the
// data has not got. A client renders them as stop selectors.
var enumColumns = []string{
	"diet_type", "meal_type", "budget_band", "season",
	"texture", "growth_target", "post_vaccine_context",
	"prep_time_min", "cook_time_min",
}

// ReferenceEnums returns every offerable recipe_master vocabulary with live counts.
//
// Unlike cuisine_option, which filters on COUNT(*) > 0 inside the view because a cuisine
// with no recipes is a broken option, a zero-count enum value is kept: season and texture
// are facts about the corpus an operator may want to see even at zero. The client decides
// whether to hide or disable.
func (h *Handlers) ReferenceEnums(w http.ResponseWriter, r *http.Request) {
	type value struct {
		Value string `json:"value"`
		Count int    `json:"count"`
	}
	out := map[string][]value{}

	for _, col := range enumColumns {
		// col is not user input: it comes from the package-level enumColumns slice, and
		// is sanitized anyway so a future edit cannot turn this into an injection.
		q := fmt.Sprintf(`
			SELECT %s::text AS value, count(*) AS n
			FROM recipe_master
			WHERE %s IS NOT NULL
			GROUP BY 1
			ORDER BY n DESC, 1`,
			pgx.Identifier{col}.Sanitize(), pgx.Identifier{col}.Sanitize())

		rows, err := h.pool.Query(r.Context(), q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "enum list failed for "+col+": "+err.Error())
			return
		}
		vals := []value{}
		for rows.Next() {
			var v value
			if err := rows.Scan(&v.Value, &v.Count); err != nil {
				rows.Close()
				writeError(w, http.StatusInternalServerError, "enum scan failed for "+col+": "+err.Error())
				return
			}
			vals = append(vals, v)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "enum rows failed for "+col+": "+err.Error())
			return
		}
		rows.Close()
		out[col] = vals
	}

	writeJSON(w, http.StatusOK, out)
}
