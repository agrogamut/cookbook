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

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_master`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
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
