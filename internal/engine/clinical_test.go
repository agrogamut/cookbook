package engine

import (
	"context"
	"errors"
	"reflect"
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
		// Kidney Disease's mapped rules (CR-REN-001/002) also sit at the specialist tier,
		// so that case alone never isolates escalationOnlyDomains -- the whole suite would
		// still pass with `|| escalationOnlyDomains[...]` deleted from the escalates
		// expression. CR-GI-002 (Vomiting / Poor Intake) is the one loaded rule that is
		// map-only and not tier: hard_exclude_yn = 'Y' so the query loads it, but its
		// human_approval_level is 'Clinical approval'. This case is the one that actually
		// exercises the map half of the union.
		{"persistent vomiting (map-only, not tier)", map[string]string{"Persistent_Vomiting": "Yes"}},
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

// TestEscalationSourcesDisagreementIsPinned pins every rule where the provider's
// specialist tier and the hand-written domain map disagree, in both directions, against
// the live clinical_rule_master content. The engine escalates the union of the two, but
// only over rules the rule query in clinicalFilter actually loads -- see the long comment
// on escalationOnlyDomains for the rules that sit in a mapped domain but are never loaded
// at all, which is a different distinction from the one this test pins.
//
// A prior version of this test only t.Logf'd the two lists and asserted nothing, so
// deleting a domain from escalationOnlyDomains, or the provider retagging a rule's
// human_approval_level, passed silently -- go test without -v never prints Logf output
// from a passing test, so the "visible list" the original comment promised was invisible
// in CI. This version fails the build on either kind of drift instead.
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

	// Pinned against the live workbook as read on 2026-08-18. If this fails, read the
	// tierOnly/mapOnly output above (rerun with -v): either the provider moved a rule's
	// approval level, or someone edited escalationOnlyDomains. Either way a human has to
	// decide how the change affects the union, not this test.
	wantTierOnly := []string{
		"CR-ALL-002 (Food Allergy)",
		"CR-ALL-003 (Food Allergy)",
		"CR-DM-001 (Diabetes)",
		"CR-DM-002 (Diabetes)",
	}
	wantMapOnly := []string{
		"CR-CEL-001 (Coeliac Disease)",
		"CR-FEED-003 (Feeding/Swallowing)",
		"CR-GI-002 (Vomiting / Poor Intake)",
		"CR-GROW-001 (Growth)",
		"CR-GROW-003 (Growth)",
	}
	if !reflect.DeepEqual(tierOnly, wantTierOnly) {
		t.Fatalf("tier-only rules changed: got %v, want %v", tierOnly, wantTierOnly)
	}
	if !reflect.DeepEqual(mapOnly, wantMapOnly) {
		t.Fatalf("map-only rules changed: got %v, want %v", mapOnly, wantMapOnly)
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
