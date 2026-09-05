package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// specialCareGate records the provider's special-care row for a declared condition. It no
// longer stops generation.
//
// The workbook's OR-001 reads "STOP_SPECIAL_CARE_GENERATION", and this used to honour it
// literally: six conditions returned zero recipes and named a mandatory reviewer instead.
// That was sized against a non-clinical operator who could not evaluate a recipe put in
// front of them, so blocking was the conservative direction and needed no sign-off. The
// input is now a verified doctor, and a 409 telling a doctor to route to a mandatory
// reviewer hands them nothing while telling the reviewer to go find themselves. See
// docs/superpowers/specs/2026-09-05-direct-generation-design.md.
//
// The provider's own automatic_action, mandatory_reviewer and stop_if still travel,
// verbatim and unparaphrased, in the step note. Paraphrasing clinical instruction is how a
// summary becomes advice, and that has not changed just because the text no longer halts
// anything.
//
// The condition is not discarded from the pipeline either. SelectTarget reads it when
// choosing the nutrition target, and ActiveClinicalRuleActions feeds it to the per-recipe
// modification notes. It stopped being a wall and became a ranking and drafting input.
func specialCareGate(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (models.StepResult, error) {
	if p.SpecialCareCondition == "" {
		return models.StepResult{
			Step: 3, Name: "Special-care condition", Kind: "record",
			CandidatesIn: -1, CandidatesOut: -1,
			Note: "no special-care condition declared",
		}, nil
	}

	var condition, gateLevel, reviewer, automaticAction, stopIf string
	err := pool.QueryRow(ctx, `
		SELECT condition, coalesce(gate_level, ''), coalesce(mandatory_reviewer, ''),
		       coalesce(automatic_action, ''), coalesce(stop_if, '')
		FROM special_care_condition_gate
		WHERE condition_id = $1`, p.SpecialCareCondition).
		Scan(&condition, &gateLevel, &reviewer, &automaticAction, &stopIf)
	// An unrecognised condition id is still an error, and deliberately so: it is input
	// validation rather than a clinical stop. Accepting it would mean the doctor believes
	// they recorded a condition the engine never saw.
	if errors.Is(err, pgx.ErrNoRows) {
		return models.StepResult{}, fmt.Errorf(
			"engine: unknown special-care condition %q: %w", p.SpecialCareCondition, ErrInvalidProfile)
	}
	if err != nil {
		return models.StepResult{}, fmt.Errorf("engine: special-care gate lookup: %w", err)
	}

	// The gate_level != "STOP-REVIEW" refusal that used to sit here is gone with the block
	// it guarded. Nothing branches on the level any more, so refusing an unexpected value
	// would reject a valid provider row to no effect.
	return models.StepResult{
		Step: 3, Name: "Special-care condition", Kind: "record",
		CandidatesIn: -1, CandidatesOut: -1,
		Note: fmt.Sprintf(
			"%s (%s), gate level %s in the provider's Special-Care master. "+
				"Provider's stated action: %s Named reviewer: %s. Provider's stop condition: %s",
			condition, p.SpecialCareCondition, gateLevel, automaticAction, reviewer, stopIf),
	}, nil
}
