package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// specialistApprovalLevel is the one clinical_rule_master.human_approval_level value that
// means "a specialist must set the targets before anything is generated". The column takes
// five values; the other four ('Clinical approval', 'Clinical/editorial approval',
// 'Clinical/nutrition approval', 'Editorial/clinical workflow approval') describe review of
// generated output rather than a precondition for generating it.
//
// This is the provider's own machine-readable escalation boundary and is preferred to any
// list written on this side. Note which column it is NOT: specialist_required is free text
// on all 31 rows ("Pediatric review if concern", "Not applicable"), so branching on its
// emptiness would escalate every rule in the master, including iron support.
const specialistApprovalLevel = "Specialist clinical approval"

// escalationOnlyDomains lists clinical_rule_master.clinical_domain values whose
// hard_exclude_yn='Y' rules have no queryable recipe-side field anywhere in the schema
// (no renal-safe, gluten-free, dysphagia-texture or FODMAP tag exists on any table).
// Verified by reading every hard_exclude_yn='Y' row's recipe_filter_action text: each of
// these says something like "filter only clinician-entered electrolyte/protein limits"
// or "only recipes permitted by clinical plan" -- guidance for a human, not a compiled
// predicate. When the operator sets the matching ClinicalFlags entry, the honest and
// safety-correct behaviour is to block automated generation and surface the rule's own
// escalation text, matching clinical_rule_priority_logic priority 1-3 ("stop recipe
// generation and route to clinical pathway" / "use only entered specialist constraints").
// Age/Feeding is excluded from this map because step 1's age bounds already enforce it
// structurally. Food Allergy is not in this map either, but not for the same reason: two
// of its three hard_exclude_yn='Y' rules (CR-ALL-002, CR-ALL-003) sit at the specialist
// tier and escalate through specialistApprovalLevel instead, and the third (CR-ALL-001)
// deliberately has no domain entry here -- it fails loudly rather than being silently
// mapped or silently passed through, because step 2 only covers the allergens the
// operator lists in Allergens, not this flag alone. See
// TestClinicalFilterRefusesAnUnclassifiedRule.
//
// Retained after specialistApprovalLevel was introduced, because the two sets are not
// identical and the map is broader in one place: CR-GI-002 (Vomiting / Poor Intake) sits
// at 'Clinical approval' yet holding a routine meal plan until hydration is assessed is
// the right behaviour.
//
// The engine escalates the union of the two, but only over rules the query in
// clinicalFilter actually loads (hard_exclude_yn = 'Y' OR at the specialist tier).
// Domain membership in this map does not by itself mean every rule in that domain
// escalates: CR-CEL-001 (Coeliac Disease), CR-FEED-003 (Feeding/Swallowing), CR-GROW-001
// and CR-GROW-003 (Growth) all carry hard_exclude_yn = 'N' and a below-tier approval
// level, so the query drops them regardless of their domain, and they can never fire.
// Each of those three domains still escalates through a sibling rule that IS loaded
// (CR-CEL-002, CR-FEED-002, CR-GROW-002 respectively), which is why the gap does not show
// up from a domain-level reading of this map -- it only shows up rule by rule.
// TestEscalationSourcesDisagreementIsPinned pins the tier/map disagreement against the
// live workbook so neither source drifts unnoticed; it does not cover this
// loaded/unloaded distinction, which is a property of the WHERE clause below, not of
// this map.
var escalationOnlyDomains = map[string]bool{
	"Coeliac Disease":          true,
	"Eating Disorder Risk":     true,
	"Feeding/Swallowing":       true,
	"Vomiting / Poor Intake":   true,
	"Growth":                   true, // severe wasting/oedema (CR-GROW-002) only; NT02/03/04/05 growth targets are unaffected
	"GI Chronic Disease":       true,
	"Liver Disease":            true,
	"Metabolic Disease":        true,
	"Prematurity/Complex Care": true,
	"Kidney Disease":           true,
}

type clinicalRule struct {
	ruleID             string
	clinicalDomain     string
	triggerField       string
	triggerOperator    string
	triggerValue       string
	escalationReason   string
	humanApprovalLevel string
	specialistRequired string
}

// clinicalFilter is engine step 3. It is demoted from a pure hard filter to
// "hard/conditional" per CLAUDE.md's "Deviation from the spec": most clinical rules
// cannot be safely compiled into a recipe-side predicate, so instead of guessing, the
// engine either passes candidates through untouched (no matching flag set) or blocks
// generation entirely (a flag matching an escalation-only domain is set), never a
// half-applied filter.
func clinicalFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, bool, string, error) {
	stepIn := len(candidateIDs)
	if len(p.ClinicalFlags) == 0 {
		return candidateIDs, models.StepResult{
			Step: 3, Name: "Clinical rules", Kind: "hard_filter",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no clinical flags set, step is a no-op",
		}, false, "", nil
	}

	// Validate every declared flag key against clinical_rule_master.trigger_field before
	// evaluating anything. This is the same class of check as allergyFilter's unmatched-
	// allergen validation, but the stakes are higher here: an unrecognized key (a typo
	// like "CDK" for "CKD") must not fail open into a full, unescalated recipe list --
	// this is the boundary between a general recipe and a clinical-escalation pathway.
	validRows, err := pool.Query(ctx, `SELECT DISTINCT trigger_field FROM clinical_rule_master`)
	if err != nil {
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical trigger field lookup: %w", err)
	}
	validFields := make(map[string]bool)
	for validRows.Next() {
		var f string
		if err := validRows.Scan(&f); err != nil {
			validRows.Close()
			return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical trigger field scan: %w", err)
		}
		validFields[f] = true
	}
	if err := validRows.Err(); err != nil {
		validRows.Close()
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical trigger field rows: %w", err)
	}
	validRows.Close()

	var unmatched []string
	for key := range p.ClinicalFlags {
		if !validFields[key] {
			unmatched = append(unmatched, key)
		}
	}
	if len(unmatched) > 0 {
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical filter: unrecognized clinical flag key(s) %v — must match clinical_rule_master.trigger_field exactly: %w", unmatched, ErrInvalidProfile)
	}

	// ORDER BY makes the outcome deterministic when more than one loaded rule fires for the
	// same profile (CKD fires both CR-REN-001 and CR-REN-002, for instance): the reason text
	// always comes from the same rule rather than whichever the heap scan reaches first, and
	// row order is not otherwise stable across re-imports because an upsert's UPDATE writes a
	// new heap tuple. rule_priority is text ('Critical', 'High', 'Low', 'Medium'), and a bare
	// ORDER BY rule_priority would sort alphabetically -- High before Low before Medium --
	// which is wrong for Low/Medium. The CASE maps it to the real severity order explicitly.
	// A priority word the provider has not used yet sorts last (ELSE), not first: an unknown
	// value must not silently outrank Critical. rule_id is the final tie-break for rules that
	// share both a loaded status and a priority, such as CR-REN-001 and CR-REN-002.
	rows, err := pool.Query(ctx, `
		SELECT rule_id, clinical_domain, trigger_field, trigger_operator, trigger_value,
		       escalation_reason, human_approval_level, coalesce(specialist_required, '')
		FROM clinical_rule_master
		WHERE (human_approval_level = $1 OR hard_exclude_yn = 'Y')
		  AND clinical_domain NOT IN ('Age/Feeding', 'Data Quality')
		ORDER BY CASE rule_priority
		           WHEN 'Critical' THEN 0
		           WHEN 'High'     THEN 1
		           WHEN 'Medium'   THEN 2
		           WHEN 'Low'      THEN 3
		           ELSE 4
		         END, rule_id`,
		specialistApprovalLevel)
	if err != nil {
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule lookup: %w", err)
	}
	defer rows.Close()

	var rules []clinicalRule
	for rows.Next() {
		var r clinicalRule
		if err := rows.Scan(&r.ruleID, &r.clinicalDomain, &r.triggerField, &r.triggerOperator,
			&r.triggerValue, &r.escalationReason, &r.humanApprovalLevel, &r.specialistRequired); err != nil {
			return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule scan: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule rows: %w", err)
	}

	// Scan every loaded rule rather than returning on the first match. An unclassified rule
	// (neither at the specialist tier nor in escalationOnlyDomains) must always win over a
	// block, never depend on which rule the scan reaches first: a profile that sets both an
	// unclassified flag and a classified, escalating one must always error, regardless of
	// which rule's row comes first in ORDER BY. So the unclassified check returns
	// immediately on sight, and an escalating match is only recorded, not returned, until the
	// whole rule set has been checked for an unclassified one.
	var firstEscalating *clinicalRule
	for i := range rules {
		r := rules[i]
		flagValue, set := p.ClinicalFlags[r.triggerField]
		if !set {
			continue
		}
		if !triggerFires(r.triggerOperator, r.triggerValue, flagValue) {
			continue
		}
		escalates := r.humanApprovalLevel == specialistApprovalLevel || escalationOnlyDomains[r.clinicalDomain]
		if !escalates {
			// A hard_exclude rule fired that neither sits at the specialist tier nor has
			// a mapped domain. Its recipe_filter_action needs a per-recipe clinical
			// safety tag that does not exist on any table, so passing it through would
			// half-apply a clinical filter. Fail loudly instead of choosing silently,
			// even if a different rule already qualified as an escalation candidate.
			return nil, models.StepResult{}, false, "",
				fmt.Errorf("engine: clinical rule %s (domain %s, approval %q) fired but is neither at the specialist tier nor in escalationOnlyDomains; classify it explicitly", r.ruleID, r.clinicalDomain, r.humanApprovalLevel)
		}
		if firstEscalating == nil {
			firstEscalating = &r
		}
	}

	if firstEscalating != nil {
		r := firstEscalating
		reason := fmt.Sprintf("%s requires specialist review (rule %s, domain %s, %s): %s",
			r.triggerField, r.ruleID, r.clinicalDomain, r.humanApprovalLevel, r.escalationReason)
		if r.specialistRequired != "" {
			// Verbatim provider guidance naming which specialist. Never parsed.
			reason += " Specialist: " + r.specialistRequired + "."
		}
		return nil, models.StepResult{
			Step: 3, Name: "Clinical rules", Kind: "escalation",
			CandidatesIn: stepIn, CandidatesOut: 0, Note: reason,
		}, true, reason, nil
	}

	return candidateIDs, models.StepResult{
		Step: 3, Name: "Clinical rules", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: "clinical flags set, none matched the specialist tier or an escalation-only domain",
	}, false, "", nil
}

// triggerFires evaluates clinical_rule_master's five real trigger_operator values
// against the operator-entered flag value. "incompatible_with" is not handled here: it
// only appears on CR-AGE-002, which compares two recipe-side columns (texture skill vs
// recipe texture) and is enforced structurally by the age/texture bounds on
// recipe_master and age_feeding_stage_master, never reached from clinicalFilter because
// clinical_domain = 'Age/Feeding' is excluded above.
func triggerFires(operator, ruleValue, actualValue string) bool {
	switch operator {
	case "equals":
		return strings.EqualFold(actualValue, ruleValue)
	case "contains":
		return strings.Contains(strings.ToLower(actualValue), strings.ToLower(ruleValue))
	case "in_list":
		for _, v := range strings.Split(ruleValue, ";") {
			if strings.EqualFold(actualValue, strings.TrimSpace(v)) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
