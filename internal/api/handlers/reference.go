package handlers

import (
	"encoding/json"
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
		RegionCulture   string  `json:"region_culture"`
		Country         string  `json:"country"`
		FocusTier       int     `json:"focus_tier"`
		RankWeight      float64 `json:"rank_weight"`
		EnrichmentScope bool    `json:"enrichment_scope"`
		Rationale       string  `json:"rationale"`
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
		CultureCode    string  `json:"culture_code"`
		CuisineCluster string  `json:"cuisine_cluster"`
		Country        string  `json:"country"`
		StateProvince  *string `json:"state_province"`
		RegionCulture  string  `json:"region_culture"`
		FocusTier      int     `json:"focus_tier"`
		RankWeight     float64 `json:"rank_weight"`
		RecipeCount    int     `json:"recipe_count"`
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
		TargetCode     string  `json:"target_code"`
		TargetName     string  `json:"target_name"`
		TargetCategory *string `json:"target_category"`
		AgeFromMonths  int     `json:"age_from_months"`
		AgeToMonths    int     `json:"age_to_months"`
		TriggerInput   *string `json:"trigger_input"`
		TriggerLogic   *string `json:"trigger_logic"`
		ScoreEnergy    int     `json:"recipe_score_energy"`
		ScoreProtein   int     `json:"recipe_score_protein"`
		ScoreIron      int     `json:"recipe_score_iron"`
		ScoreCalcium   int     `json:"recipe_score_calcium"`
		ScoreFruitVeg  int     `json:"recipe_score_fruitveg"`
		ScoreDiversity int     `json:"recipe_score_diversity"`
		ScoreCost      int     `json:"recipe_score_cost"`
		HardExclusions *string `json:"hard_exclusions"`
		SoftPenalties  *string `json:"soft_penalties"`
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
// Escalation is a fact about a VALUE, not a field: Coeliac_Status fires CR-CEL-002
// (specialist tier) on "Confirmed" but CR-CEL-001 (below tier, and not even loaded by the
// engine's query) on "Suspected_Not_Confirmed". A field-level bool_or over that would tell
// the operator every Coeliac_Status choice holds generation, which is false for one of the
// two values. So every distinct trigger_value carries its own loadable and escalates,
// computed with the same predicates internal/engine/clinical.go's clinicalFilter uses:
//
//	loadable  := (human_approval_level = 'Specialist clinical approval' OR hard_exclude_yn = 'Y')
//	             AND clinical_domain NOT IN ('Age/Feeding', 'Data Quality')
//	escalates := loadable AND (human_approval_level = 'Specialist clinical approval'
//	             OR clinical_domain IN <the ten escalationOnlyDomains names>)
//
// The ten domain names are inlined below rather than importing engine.escalationOnlyDomains:
// handlers must not depend on engine internals, so the list is duplicated on purpose.
//
// Be precise about what guards that duplication, because it is less than it looks.
// TestEscalationSourcesDisagreementIsPinned is engine-side and pins engine data only; it
// never compares these two lists. This handler's own tests pin specific rows -- Coeliac_Status
// and the fourteen inert markers -- so a divergence is caught only where it changes one of
// those rows. Dropping a domain from this list that no pinned row touches would pass. Any
// edit to escalationOnlyDomains must be mirrored here by hand, and the safe direction is to
// treat that as a manual invariant rather than a tested one.
//
// A trigger_value with operator in_list (only BMI_for_Age_Classification today) is split on
// ';' into one value per option, since "Overweight;Obesity" is two selectable values sharing
// one rule, not one value. Rows that reduce to the same (trigger_field, value) pair after
// that split -- CKD fires both CR-REN-001 and CR-REN-002 on the literal "Yes" -- are merged
// into a single value entry rather than shown twice.
func (h *Handlers) ReferenceClinicalMarkers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		WITH loaded AS (
			SELECT
				r.trigger_field,
				r.rule_id,
				r.clinical_domain,
				r.trigger_operator,
				trim(both from val.value)                                       AS value,
				(r.human_approval_level = 'Specialist clinical approval' OR r.hard_exclude_yn = 'Y')
					AND r.clinical_domain NOT IN ('Age/Feeding', 'Data Quality') AS loadable,
				r.human_approval_level,
				-- Mirrors internal/engine/clinical.go's escalationOnlyDomains. Duplicated
				-- deliberately: handlers must not import engine internals, and the two
				-- lists are each asserted against the live workbook on their own side.
				r.clinical_domain IN (
					'Coeliac Disease', 'Eating Disorder Risk', 'Feeding/Swallowing',
					'Vomiting / Poor Intake', 'Growth', 'GI Chronic Disease', 'Liver Disease',
					'Metabolic Disease', 'Prematurity/Complex Care', 'Kidney Disease'
				)                                                                AS in_escalation_domain,
				coalesce(r.engine_action, '')                                    AS engine_action,
				coalesce(r.specialist_required, '')                              AS specialist_required
			FROM clinical_rule_master r
			CROSS JOIN LATERAL unnest(
				CASE WHEN r.trigger_operator = 'in_list'
				     THEN string_to_array(r.trigger_value, ';')
				     ELSE ARRAY[r.trigger_value]
				END
			) AS val(value)
			WHERE r.trigger_field IS NOT NULL
			  AND r.trigger_value IS NOT NULL
			  AND trim(both from val.value) <> ''
		),
		scored AS (
			SELECT *,
			       (loadable AND (human_approval_level = 'Specialist clinical approval' OR in_escalation_domain)) AS escalates
			FROM loaded
		),
		per_value AS (
			SELECT trigger_field, value,
			       string_agg(DISTINCT rule_id, ', ' ORDER BY rule_id) AS rule_id,
			       bool_or(loadable)  AS loadable,
			       bool_or(escalates) AS escalates
			FROM scored
			GROUP BY trigger_field, value
		),
		field_agg AS (
			SELECT trigger_field,
			       string_agg(DISTINCT rule_id, ', ' ORDER BY rule_id)      AS rule_ids,
			       string_agg(DISTINCT clinical_domain, ', ' ORDER BY clinical_domain) AS domains,
			       string_agg(DISTINCT engine_action, ' | ')                AS engine_actions,
			       string_agg(DISTINCT specialist_required, ' | ')          AS specialist_required,
			       array_agg(DISTINCT trigger_operator)                     AS operators,
			       bool_or(escalates)                                      AS any_escalates
			FROM scored
			GROUP BY trigger_field
		),
		values_agg AS (
			SELECT trigger_field,
			       json_agg(
			           jsonb_build_object(
			               'value', value, 'rule_id', rule_id,
			               'loadable', loadable, 'escalates', escalates
			           ) ORDER BY escalates DESC, value
			       ) AS values_json
			FROM per_value
			GROUP BY trigger_field
		)
		SELECT f.trigger_field, f.rule_ids, f.domains, f.engine_actions, f.specialist_required,
		       f.operators, v.values_json
		FROM field_agg f
		JOIN values_agg v USING (trigger_field)
		ORDER BY f.any_escalates DESC, f.trigger_field`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "clinical marker list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type markerValue struct {
		Value     string `json:"value"`
		RuleID    string `json:"rule_id"`
		Loadable  bool   `json:"loadable"`
		Escalates bool   `json:"escalates"`
	}
	type marker struct {
		TriggerField       string        `json:"trigger_field"`
		RuleIDs            string        `json:"rule_ids"`
		Domains            string        `json:"domains"`
		EngineActions      string        `json:"engine_actions"`
		SpecialistRequired string        `json:"specialist_required"`
		TriggerOperator    string        `json:"trigger_operator"`
		Values             []markerValue `json:"values"`
	}
	out := []marker{}
	for rows.Next() {
		var m marker
		var operators []string
		var valuesJSON []byte
		if err := rows.Scan(&m.TriggerField, &m.RuleIDs, &m.Domains, &m.EngineActions,
			&m.SpecialistRequired, &operators, &valuesJSON); err != nil {
			writeError(w, http.StatusInternalServerError, "clinical marker scan failed: "+err.Error())
			return
		}
		// Every trigger_field is expected to carry exactly one trigger_operator (asserted
		// by TestReferenceClinicalMarkersCoversEveryTriggerField). A field with two would
		// mean a client control cannot be a pure function of the field, and that is a data
		// condition nobody has designed a control for -- fail loudly rather than silently
		// picking one or joining them into a string the client would mis-parse.
		if len(operators) != 1 {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf(
				"clinical marker %s carries %d distinct trigger_operator values %v, expected exactly one",
				m.TriggerField, len(operators), operators))
			return
		}
		m.TriggerOperator = operators[0]
		if err := json.Unmarshal(valuesJSON, &m.Values); err != nil {
			writeError(w, http.StatusInternalServerError, "clinical marker value decode failed: "+err.Error())
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

// enumColumns are recipe_master vocabularies returned with live counts, so a control can
// show how much corpus sits behind each option and a zero-count value is visibly zero
// rather than absent.
//
// Only five of the nine are accepted as engine inputs -- diet_type, meal_type, budget_band,
// prep_time_min and cook_time_min have a matching field on models.ChildProfile. The other
// four (season, texture, growth_target, post_vaccine_context) are populated on every recipe
// but the engine takes no input for them; docs/engine-inputs.md lists them under columns the
// engine does not accept. They are returned anyway because they are real, measurable facts
// about the corpus that an operator may want to see, and because a client building an
// exploratory view should not have to query the database directly to get them. A client must
// not render them as filters: the API would silently ignore the value.
//
// prep_time_min and cook_time_min are enums on purpose: the corpus holds four and six
// distinct values respectively, so a free minute entry would imply a precision the data has
// not got. A client renders them as stop selectors.
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

// ReferenceBook1Blocks returns the 32 Book 1 content blocks in render order.
//
// Ordered by book_order, never by block_id: the daily-life blocks B1-023..B1-032 occupy
// positions 15-24, so id order produces a different book. Returning them pre-sorted means
// no client can get that wrong.
//
// The link columns are returned verbatim. They are guidance text for a human, not
// references, and a client renders them as text.
func (h *Handlers) ReferenceBook1Blocks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT block_id, book_order, part, section, subsection,
		       age_from_mo, age_to_mo, trigger_or_eligibility,
		       personalization_inputs, table_or_format,
		       nutrition_target_link, clinical_rule_link, safety_link,
		       ai_can_draft, human_approval, status
		FROM book1_content_block
		ORDER BY book_order`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "book1 block list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type block struct {
		BlockID              string  `json:"block_id"`
		BookOrder            int     `json:"book_order"`
		Part                 *string `json:"part"`
		Section              *string `json:"section"`
		Subsection           *string `json:"subsection"`
		AgeFromMo            *int    `json:"age_from_mo"`
		AgeToMo              *int    `json:"age_to_mo"`
		TriggerOrEligibility *string `json:"trigger_or_eligibility"`
		PersonalizationInput *string `json:"personalization_inputs"`
		TableOrFormat        *string `json:"table_or_format"`
		NutritionTargetLink  *string `json:"nutrition_target_link"`
		ClinicalRuleLink     *string `json:"clinical_rule_link"`
		SafetyLink           *string `json:"safety_link"`
		AICanDraft           string  `json:"ai_can_draft"`
		HumanApproval        *string `json:"human_approval"`
		Status               *string `json:"status"`
	}
	out := []block{}
	for rows.Next() {
		var b block
		if err := rows.Scan(&b.BlockID, &b.BookOrder, &b.Part, &b.Section, &b.Subsection,
			&b.AgeFromMo, &b.AgeToMo, &b.TriggerOrEligibility,
			&b.PersonalizationInput, &b.TableOrFormat,
			&b.NutritionTargetLink, &b.ClinicalRuleLink, &b.SafetyLink,
			&b.AICanDraft, &b.HumanApproval, &b.Status); err != nil {
			writeError(w, http.StatusInternalServerError, "book1 block scan failed: "+err.Error())
			return
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "book1 block rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ReferenceSpecialCareConditions returns the six STOP-REVIEW conditions, so the console's
// picker is built from the provider's master rather than a hardcoded list that could drift
// from what the engine actually blocks on.
//
// mandatory_reviewer is included because it is the operator's next action once the engine
// stops, and a picker that hides it makes the block look like a dead end.
func (h *Handlers) ReferenceSpecialCareConditions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT condition_id, condition, coalesce(gate_level, ''),
		       coalesce(mandatory_reviewer, ''), coalesce(automatic_action, '')
		FROM special_care_condition_gate ORDER BY condition_id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "special-care condition list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type condition struct {
		ConditionID       string `json:"condition_id"`
		Condition         string `json:"condition"`
		GateLevel         string `json:"gate_level"`
		MandatoryReviewer string `json:"mandatory_reviewer"`
		AutomaticAction   string `json:"automatic_action"`
	}
	// Nil-slice-marshals-to-null would violate the frontend's SpecialCareCondition[] contract.
	out := []condition{}
	for rows.Next() {
		var c condition
		if err := rows.Scan(&c.ConditionID, &c.Condition, &c.GateLevel,
			&c.MandatoryReviewer, &c.AutomaticAction); err != nil {
			writeError(w, http.StatusInternalServerError, "special-care condition scan failed: "+err.Error())
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "special-care condition rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
