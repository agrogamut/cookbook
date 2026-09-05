package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// Run executes engine steps 1-13 against the given child profile. Step 8 (likes /
// dislikes / sensory) has no data source anywhere in the schema -- CLAUDE.md never
// claims a questionnaire-preference table exists -- so it is skipped, not faked; step 14
// (human audit / release gate) is an editorial process, not a query, and does not belong
// in a request-scoped function. Step 4 is recorded twice -- once as its hard filter,
// once as the preference ranker that runs after step 5 scores the pool -- so the
// returned step count is 14 entries for steps 1-13, not 15 for the full spec.
func Run(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (models.EngineResult, error) {
	var steps []models.StepResult

	ids, step1, err := ageStep(ctx, pool)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step1)

	ids, step2, unscreened, err := allergyFilter(ctx, pool, p, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step2)

	// The special-care row is recorded, not enforced. See specialCareGate. It is still
	// reported as step 3 alongside the clinical rule filter, so the why-panel shows both
	// halves of the clinical step rather than inventing a step number the spec does not
	// have.
	scStep, err := specialCareGate(ctx, pool, p)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, scStep)

	ids, step3, err := clinicalFilter(ctx, pool, p, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step3)

	ids, step4, err := dietFilter(ctx, pool, p, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step4)

	targetCode, targetReason, err := SelectTarget(ctx, pool, p)
	if err != nil {
		return models.EngineResult{}, err
	}
	ranked, step5, err := rankByTarget(ctx, pool, targetCode, ids)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step5)

	// Age's ranker half runs first among the post-scoring rankers: it is the coarsest
	// relevance signal, and every later ranker sorts within the two halves it sets rather
	// than across them.
	ranked, step1rank, err := applyAgeRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step1rank)

	// Step 2's ranking half runs before step 4's, because a suspected allergen is a safety
	// signal and step 2 outranks step 4 in the spec's priority order. Like step 4's ranker,
	// it cannot run beside its own hard filter: it adjusts a RankedScore that does not
	// exist until step 5 has scored the pool.
	ranked, step2rank, err := applySuspectedAllergenRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step2rank)

	// Step 4's ranking half runs here rather than beside its hard filter, for the same
	// reason. Both halves are recorded as step 4 so the why-panel shows the filter and the
	// preference as one concept with two effects.
	ranked, step4rank, err := applyDietRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step4rank)

	ranked, step6, err := applyMealFilter(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step6)

	ranked, step7, err := applyCultureRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step7)

	// Step 8 (likes / dislikes / sensory) has no data source anywhere in the schema --
	// no questionnaire-preference table exists on any master -- so it is recorded as an
	// explicit no-op rather than silently omitted from the step list. This is distinct
	// from step 14 (human audit / release gate), which is an editorial process outside
	// any request-scoped function and never appears in Steps at all.
	steps = append(steps, models.StepResult{
		Step: 8, Name: "Likes / dislikes / sensory", Kind: "ranker",
		CandidatesIn: len(ranked), CandidatesOut: len(ranked),
		Note: "the preference table child_preference exists as of migration 0014 but is not yet wired into the engine's query input, so this step still cannot run and is recorded as a no-op",
	})

	ranked, step9, err := applyAvailabilityRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step9)

	ranked, step10, err := applyBudgetRank(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step10)

	ranked, step11, err := applyTimeFilter(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step11)

	ranked, step12, err := dedupeNearDuplicates(ctx, pool, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step12)

	ranked, step13, err := capToTarget(ctx, pool, p, ranked)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, step13)

	// A hard filter collapsing the pool to zero (steps 1, 2 or 4; see CLAUDE.md's "Filter
	// collapse") leaves ranked as a nil slice all the way through -- rankByTarget returns
	// nil on zero input, and every ranker after it passes an empty slice through
	// unchanged. A nil slice marshals to JSON null, not []; the frontend's EngineResult
	// type promises recipes is always an array, and the zero-result case is exactly the
	// "why is this empty" scenario the UI most needs to render correctly, not crash on.
	if ranked == nil {
		ranked = []models.RankedRecipe{}
	}

	return models.EngineResult{
		Recipes:             ranked,
		Steps:               steps,
		ActiveTarget:        targetCode,
		TargetReason:        targetReason,
		UnscreenedAllergens: unscreened,
	}, nil
}
