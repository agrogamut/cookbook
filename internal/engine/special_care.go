package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// SpecialCareBlock reports whether the special-care stop gate halts generation for this
// child, and, when it does, the provider's own stop text -- automatic_action,
// mandatory_reviewer and stop_if, quoted verbatim by specialCareGate.
//
// It exists for callers that produce an artifact for a child without ranking recipes.
// internal/book's Book 1 is the one today: it carries no recipe, so it never reaches Run,
// and without this it would print general-population milestone tables under the name of a
// child whose clinician has stopped generation. The workbook's rule is "Condition is a STOP
// GATE, not a simple recipe filter", which is broader than recipes, so the same gate has to
// be reachable from outside the pipeline. It is the same function Run calls, deliberately,
// rather than a second implementation that could drift from it.
func SpecialCareBlock(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (bool, string, error) {
	_, blocked, reason, err := specialCareGate(ctx, pool, p)
	if err != nil {
		return false, "", fmt.Errorf("engine: special-care block check: %w", err)
	}
	return blocked, reason, nil
}

// specialCareGate implements OR-001 from the provider's Special-Care Output Rule Matrix:
// "special_care_condition != null ... -> STOP_SPECIAL_CARE_GENERATION", error code
// SC-HOLD-001, classified HARD.
//
// The workbook's README states the principle it enforces: "Condition is a STOP GATE, not a
// simple recipe filter." A positive diagnosis pauses generation, because the feeding
// decision for these children turns on swallowing assessment, feeding route, prescribed
// texture and sometimes a fluid order -- none of which this project collects, and none of
// which may be guessed.
//
// This blocks rather than ranks on purpose. Blocking is the conservative direction and
// needs no clinical sign-off: a block never puts an unsafe recipe in front of an operator.
// Before this gate existed the engine scored a child with cerebral palsy exactly like any
// other child, which is what GAP-019 named as its failure.
//
// Only OR-001 is implemented. OR-002 through OR-014 depend on inputs this project does not
// collect, and the stop is what makes their absence safe: no ranked list is produced for
// these children at all. GAP-022 records that, counted from the rule table itself.
func specialCareGate(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (models.StepResult, bool, string, error) {
	if p.SpecialCareCondition == "" {
		return models.StepResult{
			Step: 3, Name: "Special-care condition stop gate", Kind: "hard_filter",
			CandidatesIn: -1, CandidatesOut: -1,
			Note: "no special-care condition declared, gate is a no-op",
		}, false, "", nil
	}

	var condition, gateLevel, reviewer, automaticAction, stopIf string
	err := pool.QueryRow(ctx, `
		SELECT condition, coalesce(gate_level, ''), coalesce(mandatory_reviewer, ''),
		       coalesce(automatic_action, ''), coalesce(stop_if, '')
		FROM special_care_condition_gate
		WHERE condition_id = $1`, p.SpecialCareCondition).
		Scan(&condition, &gateLevel, &reviewer, &automaticAction, &stopIf)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.StepResult{}, false, "", fmt.Errorf(
			"engine: unknown special-care condition %q: %w", p.SpecialCareCondition, ErrInvalidProfile)
	}
	if err != nil {
		return models.StepResult{}, false, "", fmt.Errorf("engine: special-care gate lookup: %w", err)
	}

	// Every row the provider ships is STOP-REVIEW, pinned by
	// TestEverySpecialCareConditionIsAStopGate. If a reissued workbook ever carries a
	// lower gate level, refuse rather than inheriting a block the provider no longer asks
	// for -- and refuse rather than silently proceeding, because either choice is a
	// clinical decision this code cannot make on its own.
	if gateLevel != "STOP-REVIEW" {
		return models.StepResult{}, false, "", fmt.Errorf(
			"engine: special-care condition %s carries gate_level %q rather than STOP-REVIEW; "+
				"classify it explicitly before the engine runs against it",
			p.SpecialCareCondition, gateLevel)
	}

	// The reason quotes the provider's own text rather than paraphrasing it. Paraphrasing
	// clinical instruction is how a summary becomes advice.
	reason := fmt.Sprintf(
		"%s (%s) is a STOP-REVIEW condition in the provider's Special-Care master. "+
			"Required action: %s Required reviewer: %s. "+
			"The provider's stop condition reads: %s",
		condition, p.SpecialCareCondition, automaticAction, reviewer, stopIf)

	return models.StepResult{
		Step: 3, Name: "Special-care condition stop gate", Kind: "hard_filter",
		CandidatesIn: -1, CandidatesOut: 0,
		Note: fmt.Sprintf("blocked by %s (OR-001, error code SC-HOLD-001)", p.SpecialCareCondition),
	}, true, reason, nil
}
