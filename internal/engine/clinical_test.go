package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

// A clinical flag records the rules it fires and removes nothing.
//
// This asserted the opposite before SP1: CKD held generation because no renal-safe tag
// exists on any recipe-side table, so the honest options were "block" or "pass through",
// and blocking was the conservative one for a non-clinical operator. The input is now a
// verified doctor. See docs/superpowers/specs/2026-09-05-direct-generation-design.md.
func TestClinicalFilterRecordsAnUnmappableConditionWithoutRemovingAnything(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	in := []string{"MG-R-00001", "MG-R-00002"}
	p := models.ChildProfile{
		AgeMonths:     36,
		ClinicalFlags: map[string]string{"CKD": "Yes"},
	}
	out, step, err := clinicalFilter(ctx, pool, p, in)
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("a clinical flag must not remove a candidate: %d in, %d out", len(in), len(out))
	}
	if step.Kind != "record" {
		t.Fatalf("the step records rather than filters, got kind %q", step.Kind)
	}
	// CKD fires CR-REN-001 and CR-REN-002. Naming them is what makes the step useful to
	// the doctor reading it, now that it no longer changes the result.
	if !strings.Contains(step.Note, "CR-REN") {
		t.Fatalf("the note must name the rules that fired, got %q", step.Note)
	}
}

func TestClinicalFilterNoOpWithoutFlags(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	in := []string{"MG-R-00001", "MG-R-00002"}
	out, step, err := clinicalFilter(ctx, pool, models.ChildProfile{AgeMonths: 36}, in)
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("no clinical flags set: expected pass-through, got %d of %d", len(out), len(in))
	}
	if step.Note == "" {
		t.Fatal("a step that did nothing must say so rather than looking like it ran")
	}
}

// An unrecognized ClinicalFlags key (a typo like "CDK" for "CKD") must still fail loudly
// with ErrInvalidProfile.
//
// This survives SP1 deliberately, and the distinction is the point: rejecting a typo is
// input validation, not a clinical gate. Without it a misspelled key silently records
// nothing while the doctor believes they entered a condition.
func TestClinicalFilterErrorsOnUnrecognizedFlagKey(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	p := models.ChildProfile{
		AgeMonths:     36,
		ClinicalFlags: map[string]string{"not-a-real-trigger-field": "Yes"},
	}
	_, _, err := clinicalFilter(ctx, pool, p, []string{"MG-R-00001"})
	if err == nil {
		t.Fatal("clinicalFilter must error on an unrecognized trigger field key, got nil")
	}
	if !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("error must wrap ErrInvalidProfile so the HTTP layer maps it to 400: %v", err)
	}
}

// Every flag that used to escalate now records instead. Kept as a table over the same four
// cases the block test used, because the interesting property is still "all of these behave
// the same way", and these four span the two sources the old code unioned: the provider's
// specialist tier (diabetes, multiple food allergies), the hand-written domain map
// (persistent vomiting, which sat at 'Clinical approval' and was map-only), and one in both
// (CKD).
func TestFlagsThatUsedToEscalateNowRecord(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		flags map[string]string
	}{
		{"diabetes", map[string]string{"Diabetes_Type": "Type 1"}},
		{"multiple food allergies", map[string]string{"Multiple_Food_Allergies": "Yes"}},
		{"kidney disease", map[string]string{"CKD": "Yes"}},
		{"persistent vomiting", map[string]string{"Persistent_Vomiting": "Yes"}},
	}

	in := []string{"MG-R-00001", "MG-R-00002"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, step, err := clinicalFilter(ctx, pool,
				models.ChildProfile{AgeMonths: 36, ClinicalFlags: c.flags}, in)
			if err != nil {
				t.Fatalf("clinicalFilter: %v", err)
			}
			if len(out) != len(in) {
				t.Fatalf("%s must not remove a candidate: %d in, %d out", c.name, len(in), len(out))
			}
			if step.CandidatesOut != step.CandidatesIn {
				t.Fatalf("%s: in %d out %d", c.name, step.CandidatesIn, step.CandidatesOut)
			}
			if !strings.Contains(step.Note, "recorded not filtered") {
				t.Fatalf("%s: the note must say the rules were recorded, got %q", c.name, step.Note)
			}
		})
	}
}

// CR-ALL-001 used to be refused outright: it says a confirmed allergen must be excluded,
// which step 2 does, but only for allergens also listed in Allergens, so the flag alone was
// a half-specified profile and failing loudly beat half-applying a clinical filter.
//
// With no block left, refusing is not an option the engine has. The flag is recorded like
// any other and the honest position is that it changes nothing on its own: the confirmed
// allergen hard filter at step 2 is what actually excludes, and it reads Allergens. That
// filter is untouched by SP1, and this test pins the pass-through so the two are not
// confused for each other later.
//
// The flag value must contain "allergen" for the rule's contains-operator to fire; see
// triggerFires. A value of "Yes", which is what the console sends, does not reach it.
func TestAnAllergenFlagAloneRecordsAndExcludesNothing(t *testing.T) {
	pool := testPool(t)
	in := []string{"MG-R-00001"}
	out, step, err := clinicalFilter(context.Background(), pool,
		models.ChildProfile{
			AgeMonths:     36,
			ClinicalFlags: map[string]string{"Confirmed_or_Highly_Suspected_Allergen": "Peanut allergen"},
		},
		in)
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("the flag alone must not exclude: %d in, %d out", len(in), len(out))
	}
	if !strings.Contains(step.Note, "CR-ALL-001") {
		t.Fatalf("the note must still name the rule that fired, got %q", step.Note)
	}
}

// The rule query loads every domain except Age/Feeding and Data Quality. Those two
// exclusions are the only ones left, and both are structural rather than clinical:
// recipe_master's own age bounds enforce Age/Feeding, and Data Quality describes the
// dataset rather than the child. Pinned because widening the query to "all rules" was part
// of SP1, and a future narrowing would silently shrink what the step reports.
func TestTheRuleQueryExcludesOnlyTheTwoStructuralDomains(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var total, loaded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM clinical_rule_master`).Scan(&total); err != nil {
		t.Fatalf("count all: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM clinical_rule_master
		WHERE clinical_domain NOT IN ('Age/Feeding', 'Data Quality')`).Scan(&loaded); err != nil {
		t.Fatalf("count loaded: %v", err)
	}
	if loaded == 0 || loaded == total {
		t.Fatalf("expected the two structural domains to exclude some but not all rules: "+
			"%d of %d loaded", loaded, total)
	}
}
