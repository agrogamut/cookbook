package engine

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Step 1 hands the whole corpus downstream. It removes nothing, which is the change SP1
// made; applyAgeRank is what age actually does now. See
// docs/superpowers/specs/2026-09-05-direct-generation-design.md.
func TestAgeStepReturnsTheWholeCorpus(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// engine_candidate, not recipe_master: since migration 0039 the pool is both corpora, and
	// counting recipe_master here would have quietly passed while step 1 dropped every AI
	// recipe. Asserted as two counts rather than one so a union that silently contributes
	// nothing fails loudly instead of matching a smaller expectation.
	var total, provider, ai int
	if err := pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE source = 'provider'),
		       count(*) FILTER (WHERE source = 'ai')
		FROM engine_candidate`).Scan(&total, &provider, &ai); err != nil {
		t.Fatalf("count: %v", err)
	}
	if provider == 0 || ai == 0 {
		t.Fatalf("engine_candidate must carry both corpora: %d provider, %d ai", provider, ai)
	}

	ids, step, err := ageStep(ctx, pool)
	if err != nil {
		t.Fatalf("ageStep: %v", err)
	}
	if len(ids) != total {
		t.Fatalf("step 1 must remove nothing: got %d of %d recipes", len(ids), total)
	}
	if step.CandidatesIn != step.CandidatesOut {
		t.Fatalf("step 1 removed %d recipes", step.CandidatesIn-step.CandidatesOut)
	}
}

// The in-band lookup applyAgeRank uses to decide the partition. It is a real subset: if it
// ever returned everything, the partition would be a no-op and out-of-band recipes would
// mix freely into a child's list without anything failing.
func TestInBandIDsIsAStrictSubsetForAnInfant(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_master`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}

	ids, err := inBandIDs(ctx, pool, models.ChildProfile{AgeMonths: 8})
	if err != nil {
		t.Fatalf("inBandIDs: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("8-month age band must return candidates: it is the best-covered infant band")
	}
	if len(ids) >= total {
		t.Fatalf("an 8-month band cannot contain the whole corpus: %d of %d", len(ids), total)
	}
}

func TestAllergyFilterExcludesDeclaredAllergen(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, err := inBandIDs(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("inBandIDs: %v", err)
	}
	filtered, step, _, err := allergyFilter(ctx, pool, models.ChildProfile{Allergens: []string{"Peanut"}}, all)
	if err != nil {
		t.Fatalf("allergyFilter: %v", err)
	}
	if len(filtered) >= len(all) {
		t.Fatalf("declaring Peanut must remove at least one recipe: before=%d after=%d", len(all), len(filtered))
	}
	if step.CandidatesIn != len(all) {
		t.Fatalf("step.CandidatesIn = %d, want %d", step.CandidatesIn, len(all))
	}
}

func TestAllergyFilterErrorsOnUnmatchedAllergen(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, err := inBandIDs(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("inBandIDs: %v", err)
	}
	_, _, _, err = allergyFilter(ctx, pool, models.ChildProfile{Allergens: []string{"Peanut", "not-a-real-allergen"}}, all)
	if err == nil {
		t.Fatal("allergyFilter must error on unmatched allergen, got nil")
	}
	if errStr := err.Error(); errStr != "engine: allergy filter: unrecognized allergen(s) [not-a-real-allergen] — must match allergen_mapping.allergen_group exactly" {
		if !contains(errStr, "not-a-real-allergen") {
			t.Fatalf("error message must mention the unmatched allergen: %q", errStr)
		}
	}
}

// TestAllergyFilterExcludesGroundnutOilRecipeForPeanut pins the fix for a silent hard-filter
// gap: ING0063 (Groundnut oil) carries no allergen tag in ingredient_master ("None identified
// in starter tagging"), so a recipe built from it never propagated a Peanut tag anywhere the
// filter checks -- despite allergen_mapping's own ALG-PEANUT row naming "groundnut oil"
// directly under common_derivatives_or_hidden_sources. MG-R-00285 is a real corpus recipe
// that uses it. ingredient_allergen_override (migration 0024) is the correction.
func TestAllergyFilterExcludesGroundnutOilRecipeForPeanut(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	filtered, _, _, err := allergyFilter(ctx, pool, models.ChildProfile{Allergens: []string{"Peanut"}}, []string{"MG-R-00285"})
	if err != nil {
		t.Fatalf("allergyFilter: %v", err)
	}
	for _, id := range filtered {
		if id == "MG-R-00285" {
			t.Fatal("MG-R-00285 uses Groundnut oil, a documented peanut derivative -- it must be excluded for a declared Peanut allergy")
		}
	}
}

// TestAllergyFilterWheatMatchesGlutenContainingCerealTag pins the fix for the final
// whole-branch review's Critical #1: allergen_mapping names this group "Wheat" but the
// corpus tags it "Gluten-containing cereal". Before allergen_tag_vocabulary existed,
// declaring a Wheat allergy matched zero recipes even though wheat-containing recipes
// are tagged in the corpus -- a silent safety gap, not an honest absence.
func TestAllergyFilterWheatMatchesGlutenContainingCerealTag(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, err := inBandIDs(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("inBandIDs: %v", err)
	}
	filtered, _, _, err := allergyFilter(ctx, pool, models.ChildProfile{Allergens: []string{"Wheat"}}, all)
	if err != nil {
		t.Fatalf("allergyFilter: %v", err)
	}
	if len(filtered) >= len(all) {
		t.Fatalf("declaring Wheat must remove at least one recipe via the Gluten-containing cereal corpus tag: before=%d after=%d", len(all), len(filtered))
	}
}

// TestAllergyFilterGenuinelyAbsentGroupNotesZeroExclusions covers the other half of the
// same fix: a declared allergen whose group has no corpus tag at all (e.g. Tree nuts)
// must still return the full candidate pool -- there is nothing to exclude -- but the
// step result must say so explicitly rather than reading like an ordinary no-op.
func TestAllergyFilterGenuinelyAbsentGroupNotesZeroExclusions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, err := inBandIDs(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("inBandIDs: %v", err)
	}
	filtered, step, _, err := allergyFilter(ctx, pool, models.ChildProfile{Allergens: []string{"Tree nuts"}}, all)
	if err != nil {
		t.Fatalf("allergyFilter: %v", err)
	}
	if len(filtered) != len(all) {
		t.Fatalf("Tree nuts has no corpus tag: expected zero exclusions, before=%d after=%d", len(all), len(filtered))
	}
	if !contains(step.Note, "Tree nuts") {
		t.Fatalf("step note must name the allergen with no corpus tag so the operator can tell absence from a silent bug, got %q", step.Note)
	}
}

func TestAllergyFilterReportsUnscreenedGroups(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, err := inBandIDs(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("inBandIDs: %v", err)
	}

	cases := []struct {
		name           string
		allergens      []string
		wantUnscreened []string
	}{
		{"tree nuts have no corpus tag", []string{"Tree nuts"}, []string{"Tree nuts"}},
		{"peanut has one", []string{"Peanut"}, nil},
		// Mustard has no corpus_tag but is screened via ingredient_allergen_override
		// (Mustard oil, Mustard seeds -- migration 0024), so it is no longer unscreened.
		{"mixed reports only the unscreened half", []string{"Tree nuts", "Mustard"}, []string{"Tree nuts"}},
		{"none declared", nil, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, unscreened, err := allergyFilter(ctx, pool,
				models.ChildProfile{AgeMonths: 36, Allergens: c.allergens}, all)
			if err != nil {
				t.Fatalf("allergyFilter: %v", err)
			}
			if len(unscreened) != len(c.wantUnscreened) {
				t.Fatalf("unscreened = %v, want %v", unscreened, c.wantUnscreened)
			}
			for i := range c.wantUnscreened {
				if unscreened[i] != c.wantUnscreened[i] {
					t.Fatalf("unscreened = %v, want %v", unscreened, c.wantUnscreened)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// The safety property the union rests on: an AI recipe is screened by the same step 2 that
// screens a provider one, not by a second implementation beside it.
//
// Asserted against AI rows specifically rather than inferred from the real corpus passing,
// because the failure mode is invisible from the provider side. Step 2 is a keep-list
// (`FROM engine_candidate r WHERE r.recipe_id = ANY($1) AND NOT EXISTS ...`), so a query left
// pointing at recipe_master returns every provider recipe correctly and simply omits every AI
// id handed to it. Every provider-only assertion still passes while the AI corpus vanishes.
//
// Two allergens, because they exercise different arms of the filter:
//
//   - Milk travels on the corpus allergen tag, the ordinary path.
//   - Peanut on an AI recipe travels only through ingredient_allergen_override (migration
//     0024), which records that ingredient_master left Groundnut oil untagged. 17 AI recipes
//     reach it that way and no tag would catch them.
//
// The exact-survivor count is the assertion that matters, and it replaced a "removed at least
// one" check that was worthless: pointing the keep-list back at recipe_master makes zero AI
// recipes survive, and zero survivors also carry no allergen, so the weak form passed the very
// mutation it existed to catch. Verified by running both mutations against the exact form.
//
// One known redundancy, recorded so it is not mistaken for a gap: reverting the ingredient
// subquery to recipe_ingredient_mapping does NOT fail this test, and should not.
// ai_recipe_derived.allergen_tags is computed from ai_recipe_ingredient rather than shipped
// alongside it, so an AI recipe's own tag cannot disagree with its ingredients -- measured at
// zero rows where the ingredient tag exceeds the recipe tag. That arm exists for the provider
// corpus, whose denormalised copy can drift.
func TestAllergyFilterScreensAIRecipesLikeProviderOnes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	all, _, err := ageStep(ctx, pool)
	if err != nil {
		t.Fatalf("ageStep: %v", err)
	}

	countAI := func(ids []string) int {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM engine_candidate WHERE recipe_id = ANY($1) AND source = 'ai'`,
			ids).Scan(&n); err != nil {
			t.Fatalf("count ai: %v", err)
		}
		return n
	}

	// Baseline: with nothing declared, AI recipes survive. Without this the assertions below
	// would pass against a filter that discarded the AI corpus wholesale.
	kept, _, _, err := allergyFilter(ctx, pool, models.ChildProfile{AgeMonths: 36}, all)
	if err != nil {
		t.Fatalf("allergyFilter with no allergens: %v", err)
	}
	if countAI(kept) == 0 {
		t.Fatal("no AI recipe survives a profile declaring no allergens; the union is not reaching step 2")
	}

	for _, group := range []string{"Milk", "Peanut"} {
		t.Run(group, func(t *testing.T) {
			// AI recipes that genuinely carry this allergen, by either route.
			var carriers int
			if err := pool.QueryRow(ctx, `
				SELECT count(DISTINCT c.recipe_id) FROM engine_candidate c
				WHERE c.source = 'ai' AND (
				    EXISTS (SELECT 1 FROM engine_candidate_ingredient m
				            JOIN allergen_tag_vocabulary v
				              ON v.allergen_group = $1 AND v.corpus_tag IS NOT NULL
				            WHERE m.recipe_id = c.recipe_id
				              AND (m.ingredient_allergen_tag ILIKE '%' || v.corpus_tag || '%'
				                   OR c.allergen_tags ILIKE '%' || v.corpus_tag || '%'))
				 OR EXISTS (SELECT 1 FROM engine_candidate_ingredient m
				            JOIN ingredient_allergen_override o ON o.ingredient_id = m.ingredient_id
				            WHERE m.recipe_id = c.recipe_id AND o.allergen_group = $1))`,
				group).Scan(&carriers); err != nil {
				t.Fatalf("count carriers: %v", err)
			}
			if carriers == 0 {
				t.Skipf("no AI recipe carries %s; nothing for this case to screen", group)
			}

			kept, _, _, err := allergyFilter(ctx, pool,
				models.ChildProfile{AgeMonths: 36, Allergens: []string{group}}, all)
			if err != nil {
				t.Fatalf("allergyFilter: %v", err)
			}

			var wantAI int
			if err := pool.QueryRow(ctx, `
				SELECT count(*) FROM engine_candidate c
				WHERE c.source = 'ai'
				  AND NOT EXISTS (SELECT 1 FROM engine_candidate_ingredient m
				                  JOIN allergen_tag_vocabulary v
				                    ON v.allergen_group = $1 AND v.corpus_tag IS NOT NULL
				                  WHERE m.recipe_id = c.recipe_id
				                    AND (m.ingredient_allergen_tag ILIKE '%' || v.corpus_tag || '%'
				                         OR c.allergen_tags ILIKE '%' || v.corpus_tag || '%'))
				  AND NOT EXISTS (SELECT 1 FROM engine_candidate_ingredient m
				                  JOIN ingredient_allergen_override o ON o.ingredient_id = m.ingredient_id
				                  WHERE m.recipe_id = c.recipe_id AND o.allergen_group = $1)`,
				group).Scan(&wantAI); err != nil {
				t.Fatalf("count expected survivors: %v", err)
			}

			if got := countAI(kept); got != wantAI {
				t.Fatalf("declaring %s left %d AI recipes; exactly %d do not carry it. Zero here "+
					"means step 2 is still reading recipe_master and the AI corpus never reaches "+
					"the filter at all", group, got, wantAI)
			}
		})
	}
}
