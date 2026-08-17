package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// clinicalMarkerToTarget maps the operator-facing marker keys the frontend's dropdown
// will offer to the real nutrition_target_master.target_code values. Checked live
// against trigger_input/trigger_logic per target_code: the underlying trigger is a mix
// of clinician entry (NT02, NT03, NT06, NT08, NT12), parent questionnaire (NT11), family
// diet declaration (NT09, NT10), and a WHO growth z-score (NT04, NT05) -- not uniformly
// "clinician-entered." ChildProfile has no z-score field yet, so letting the operator
// pick the marker directly is a deliberate simplification, not a data invention: the
// operator -- not the engine -- decides which marker applies. NT03/NT04/NT05 (thinness,
// overweight-under-5, overweight-5-19) all key on the same underlying concept
// (weight-for-age concern) but are age-gated by nutrition_target_master.age_from_months/
// age_to_months, so selectTarget re-checks the age band after the marker lookup rather
// than trusting the operator's marker key alone.
var clinicalMarkerToTarget = map[string]string{
	"growth_faltering":  "NT02",
	"thinness":          "NT03",
	"overweight_under5": "NT04",
	"overweight_5to19":  "NT05",
	"iron_deficiency":   "NT06",
	"calcium_bone":      "NT07",
	"high_protein":      "NT08",
	"vegetarian":        "NT09",
	"vegan":             "NT10",
	"picky_eating":      "NT11",
	"illness_recovery":  "NT12",
}

// selectTarget is engine step 5's target-selection half. NT01 auto-activates for ages
// 6-23 months per nutrition_target_master's own trigger_input ("Automatically active for
// complementary-feeding age"); an explicit operator marker is checked first because a
// clinician-entered condition should not be silently overridden by the age default.
// NT00 is the fallback, matching nt_engine_priority_logic priority 4's default.
func selectTarget(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (string, string, error) {
	if p.ClinicalMarker != "" {
		code, ok := clinicalMarkerToTarget[p.ClinicalMarker]
		if !ok {
			return "", "", fmt.Errorf("engine: unknown clinical marker %q: %w", p.ClinicalMarker, ErrInvalidProfile)
		}
		var ageFrom, ageTo int
		err := pool.QueryRow(ctx,
			`SELECT age_from_months, age_to_months FROM nutrition_target_master WHERE target_code = $1`,
			code).Scan(&ageFrom, &ageTo)
		if err != nil {
			return "", "", fmt.Errorf("engine: target age band for %s: %w", code, err)
		}
		if p.AgeMonths >= ageFrom && p.AgeMonths <= ageTo {
			return code, fmt.Sprintf("operator-selected marker %q, in target age band", p.ClinicalMarker), nil
		}
		// Marker doesn't apply at this age (e.g. NT08 high-protein starts at 24mo).
		// Fall through to the age default rather than erroring, since the marker
		// itself may still be clinically true; it just can't drive ranking yet.
	}
	if p.AgeMonths >= 6 && p.AgeMonths <= 23 {
		return "NT01", "age 6-23 months, complementary-feeding target auto-activated", nil
	}
	return "NT00", "no applicable clinical marker, routine age-appropriate default", nil
}

// rankByTarget is engine step 5's ranking half, reading the already-built recipe_ranked
// view (migration 0003). It never returns fewer rows than it receives -- a ranker only
// reorders.
func rankByTarget(ctx context.Context, pool *pgxpool.Pool, targetCode string, candidateIDs []string) ([]models.RankedRecipe, models.StepResult, error) {
	stepIn := len(candidateIDs)
	if stepIn == 0 {
		return nil, models.StepResult{Step: 5, Name: "Nutrition target", Kind: "ranker", CandidatesIn: 0, CandidatesOut: 0}, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT rr.recipe_id, rm.recipe_name, rr.region_culture, rm.meal_type, rm.clinical_tag,
		       rm.age_group, rr.nutrition_score, rr.ranked_score, rr.scored_axes, rr.value_kind
		FROM recipe_ranked rr
		JOIN recipe_master rm ON rm.recipe_id = rr.recipe_id
		WHERE rr.recipe_id = ANY($1) AND rr.target_code = $2
		ORDER BY rr.ranked_score DESC`,
		candidateIDs, targetCode)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: rank by target: %w", err)
	}
	defer rows.Close()

	var out []models.RankedRecipe
	for rows.Next() {
		var r models.RankedRecipe
		if err := rows.Scan(&r.RecipeID, &r.RecipeName, &r.RegionCulture, &r.MealType, &r.ClinicalTag,
			&r.AgeGroup, &r.NutritionScore, &r.RankedScore, &r.ScoredAxes, &r.ValueKind); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: rank by target scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: rank by target rows: %w", err)
	}

	return out, models.StepResult{
		Step: 5, Name: "Nutrition target", Kind: "ranker",
		CandidatesIn: stepIn, CandidatesOut: len(out),
	}, nil
}
