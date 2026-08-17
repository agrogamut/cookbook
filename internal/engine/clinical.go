package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

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
// Age/Feeding and Food Allergy domains are excluded from this list: they are already
// hard-enforced structurally by steps 1 and 2.
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
	ruleID           string
	clinicalDomain   string
	triggerField     string
	triggerOperator  string
	triggerValue     string
	escalationReason string
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

	rows, err := pool.Query(ctx, `
		SELECT rule_id, clinical_domain, trigger_field, trigger_operator, trigger_value, escalation_reason
		FROM clinical_rule_master
		WHERE hard_exclude_yn = 'Y'
		  AND clinical_domain != 'Age/Feeding'
		  AND clinical_domain != 'Food Allergy'
		  AND clinical_domain != 'Data Quality'`)
	if err != nil {
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule lookup: %w", err)
	}
	defer rows.Close()

	var rules []clinicalRule
	for rows.Next() {
		var r clinicalRule
		if err := rows.Scan(&r.ruleID, &r.clinicalDomain, &r.triggerField, &r.triggerOperator, &r.triggerValue, &r.escalationReason); err != nil {
			return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule scan: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, false, "", fmt.Errorf("engine: clinical rule rows: %w", err)
	}

	for _, r := range rules {
		flagValue, set := p.ClinicalFlags[r.triggerField]
		if !set {
			continue
		}
		if !triggerFires(r.triggerOperator, r.triggerValue, flagValue) {
			continue
		}
		if !escalationOnlyDomains[r.clinicalDomain] {
			// A hard_exclude rule fired outside the mapped escalation domains -- this
			// should be unreachable given the WHERE clause above, but fail loudly
			// rather than silently pass a clinically-flagged profile through.
			return nil, models.StepResult{}, false, "",
				fmt.Errorf("engine: clinical rule %s fired outside escalationOnlyDomains; add its domain to the map or handle it explicitly", r.ruleID)
		}
		reason := fmt.Sprintf("%s requires specialist review (rule %s, domain %s): %s",
			r.triggerField, r.ruleID, r.clinicalDomain, r.escalationReason)
		return nil, models.StepResult{
			Step: 3, Name: "Clinical rules", Kind: "escalation",
			CandidatesIn: stepIn, CandidatesOut: 0, Note: reason,
		}, true, reason, nil
	}

	return candidateIDs, models.StepResult{
		Step: 3, Name: "Clinical rules", Kind: "hard_filter",
		CandidatesIn: stepIn, CandidatesOut: stepIn,
		Note: "clinical flags set, none matched an escalation-only rule",
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
