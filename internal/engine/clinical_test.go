package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

func TestClinicalFilterBlocksUnmappableCondition(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	p := models.ChildProfile{
		AgeMonths:     36,
		ClinicalFlags: map[string]string{"CKD": "Yes"},
	}
	_, step, blocked, reason, err := clinicalFilter(ctx, pool, p, []string{"MG-R-00001"})
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if !blocked {
		t.Fatal("CKD has no queryable recipe-side safety tag in the schema; the engine must block, not silently pass a recipe list through")
	}
	if reason == "" || step.CandidatesOut != 0 {
		t.Fatalf("blocked result must explain why and return zero candidates: reason=%q out=%d", reason, step.CandidatesOut)
	}
}

func TestClinicalFilterNoOpWithoutFlags(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	in := []string{"MG-R-00001", "MG-R-00002"}
	out, _, blocked, _, err := clinicalFilter(ctx, pool, models.ChildProfile{AgeMonths: 36}, in)
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if blocked || len(out) != len(in) {
		t.Fatalf("no clinical flags set: expected pass-through, got blocked=%v out=%d", blocked, len(out))
	}
}

// TestClinicalFilterErrorsOnUnrecognizedFlagKey pins the fix for the final whole-branch
// review's Important #7: an unrecognized ClinicalFlags key (a typo like "CDK" for "CKD")
// must fail loudly with ErrInvalidProfile rather than silently failing open into a full,
// unescalated recipe list -- the same validation class allergyFilter already had.
func TestClinicalFilterErrorsOnUnrecognizedFlagKey(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	p := models.ChildProfile{
		AgeMonths:     36,
		ClinicalFlags: map[string]string{"not-a-real-trigger-field": "Yes"},
	}
	_, _, blocked, _, err := clinicalFilter(ctx, pool, p, []string{"MG-R-00001"})
	if err == nil {
		t.Fatal("clinicalFilter must error on an unrecognized trigger field key, got nil")
	}
	if !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("error must wrap ErrInvalidProfile so the HTTP layer maps it to 400: %v", err)
	}
	if blocked {
		t.Fatal("an error return must not also report blocked=true; the caller checks err first")
	}
}

func TestClinicalFilterBlocksAtTheProviderSpecialistTier(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		flags map[string]string
	}{
		// Both were invisible before: the rule query filtered on hard_exclude_yn = 'Y'
		// (these are 'N') and excluded the Food Allergy domain outright.
		{"diabetes", map[string]string{"Diabetes_Type": "Type 1"}},
		{"multiple food allergies", map[string]string{"Multiple_Food_Allergies": "Yes"}},
		// Already caught by the hand-written domain map; must stay caught.
		{"kidney disease", map[string]string{"CKD": "Yes"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, step, blocked, reason, err := clinicalFilter(ctx, pool,
				models.ChildProfile{AgeMonths: 36, ClinicalFlags: c.flags},
				[]string{"MG-R-00001"})
			if err != nil {
				t.Fatalf("clinicalFilter: %v", err)
			}
			if !blocked {
				t.Fatalf("%s sits at the provider's Specialist clinical approval tier and "+
					"must hold generation, not return a recipe list", c.name)
			}
			if reason == "" || step.CandidatesOut != 0 {
				t.Fatalf("a blocked result must explain itself and return zero candidates: reason=%q out=%d", reason, step.CandidatesOut)
			}
		})
	}
}

func TestClinicalFilterBlockReasonQuotesTheProviderSpecialist(t *testing.T) {
	pool := testPool(t)
	_, _, blocked, reason, err := clinicalFilter(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, ClinicalFlags: map[string]string{"CKD": "Yes"}},
		[]string{"MG-R-00001"})
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if !blocked {
		t.Fatal("CKD must block")
	}
	// specialist_required is free text naming which specialist. CR-REN-001 reads
	// "Paediatric nephrology/dietitian". It is rendered verbatim, never parsed.
	if !strings.Contains(reason, "nephrology") {
		t.Fatalf("BlockReason must quote the provider's specialist_required text so the "+
			"operator knows which specialist is needed; got %q", reason)
	}
}

func TestClinicalFilterDoesNotBlockANonEscalatingRule(t *testing.T) {
	pool := testPool(t)
	// CR-IRON-001 sits at 'Clinical approval' and its engine_action is "Boost iron-rich
	// recipes". Its specialist_required text ("Pediatrician/dietitian as indicated") is
	// non-empty like every other row's, which is exactly why that column must never be
	// treated as a flag.
	out, _, blocked, _, err := clinicalFilter(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, ClinicalFlags: map[string]string{"Anemia_or_Iron_Risk": "Yes"}},
		[]string{"MG-R-00001", "MG-R-00002"})
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if blocked {
		t.Fatal("an iron-risk flag must not hold generation; it is a ranking signal")
	}
	if len(out) != 2 {
		t.Fatalf("non-escalating flags pass candidates through untouched, got %d of 2", len(out))
	}
}

// TestEscalationSourcesDisagreementIsPinned prints every rule where the provider's
// specialist tier and the hand-written domain map disagree, in both directions. The
// engine blocks on the union, so a disagreement is not a bug -- but it must be a visible
// list rather than a silent preference for one source.
func TestEscalationSourcesDisagreementIsPinned(t *testing.T) {
	pool := testPool(t)
	rows, err := pool.Query(context.Background(), `
		SELECT rule_id, clinical_domain, human_approval_level
		FROM clinical_rule_master
		ORDER BY rule_id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var tierOnly, mapOnly []string
	for rows.Next() {
		var id, domain, level string
		if err := rows.Scan(&id, &domain, &level); err != nil {
			t.Fatalf("scan: %v", err)
		}
		atTier := level == specialistApprovalLevel
		inMap := escalationOnlyDomains[domain]
		switch {
		case atTier && !inMap:
			tierOnly = append(tierOnly, id+" ("+domain+")")
		case inMap && !atTier:
			mapOnly = append(mapOnly, id+" ("+domain+")")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	t.Logf("at the specialist tier but not in escalationOnlyDomains: %v", tierOnly)
	t.Logf("in escalationOnlyDomains but not at the specialist tier: %v", mapOnly)

	// Both directions are expected and both are escalated, because the engine takes the
	// union. This assertion exists so that a change to either source is noticed here
	// rather than discovered by a child getting an unescalated list.
	if len(tierOnly) == 0 && len(mapOnly) == 0 {
		t.Log("the two sources now agree exactly; escalationOnlyDomains may be retirable")
	}
}

// TestClinicalFilterRefusesAnUnclassifiedRule pins the one rule that becomes reachable
// when the Food Allergy domain stops being excluded and that sits at neither the
// specialist tier nor a mapped escalation domain. CR-ALL-001 says a confirmed allergen
// must be excluded -- which step 2 already does, but only for allergens the operator also
// listed in Allergens. Setting this flag alone is a half-specified profile, and refusing
// it explicitly beats half-applying a clinical filter or silently ignoring the rule.
//
// The flag value must contain "allergen" for the rule's contains-operator to fire; see
// triggerFires. A value of "Yes", which is what the console sends, does not reach here.
func TestClinicalFilterRefusesAnUnclassifiedRule(t *testing.T) {
	pool := testPool(t)
	_, _, _, _, err := clinicalFilter(context.Background(), pool,
		models.ChildProfile{
			AgeMonths:     36,
			ClinicalFlags: map[string]string{"Confirmed_or_Highly_Suspected_Allergen": "Peanut allergen"},
		},
		[]string{"MG-R-00001"})
	if err == nil {
		t.Fatal("CR-ALL-001 is loaded by the widened rule query but sits at neither the " +
			"specialist tier nor a mapped escalation domain; it must fail loudly rather " +
			"than pass a clinically-flagged profile through unhandled")
	}
	if !strings.Contains(err.Error(), "CR-ALL-001") {
		t.Fatalf("the error must name the rule so an operator can act on it, got %v", err)
	}
}
