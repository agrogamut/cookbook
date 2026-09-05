package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// Two things used to live here and are gone: specialistApprovalLevel, the provider's
// machine-readable escalation boundary, and escalationOnlyDomains, a hand-written list of
// ten clinical_domain values whose hard_exclude rules had no queryable recipe-side field.
// Between them they decided which flags stopped generation.
//
// Deleting them is not a loss of filtering, and escalationOnlyDomains' own comment recorded
// why: no renal-safe, gluten-free, dysphagia-texture or FODMAP tag exists on any table in
// this schema, so every rule they covered was guidance written for a human rather than a
// predicate anything could compile. The block was standing in for a filter that could never
// be written, and it was sized against a non-clinical operator. See
// docs/superpowers/specs/2026-09-05-direct-generation-design.md.

type clinicalRule struct {
	ruleID             string
	clinicalDomain     string
	triggerField       string
	triggerOperator    string
	triggerValue       string
	escalationReason   string
	specialistRequired string
}

// clinicalFilter is engine step 3. It is a record: it names every rule the child's flags
// fire, and removes nothing.
//
// Most clinical rules cannot be compiled into a recipe-side predicate at all -- see the note
// above where escalationOnlyDomains used to be -- so this step never had a filter to apply.
// What it had was a block, and the block is gone. The clinical signal still reaches the
// output twice over: SelectTarget chooses the nutrition target from the child's condition,
// and ActiveClinicalRuleActions feeds the per-recipe modification notes. A condition stopped
// being a wall and became a ranking and drafting input.
//
// The flag-key validation below stays, and the distinction matters: rejecting a typo is
// input validation, not a clinical gate. A misspelled key must not fail open into a full
// recipe list while the doctor believes they recorded something.
func clinicalFilter(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile, candidateIDs []string) ([]string, models.StepResult, error) {
	stepIn := len(candidateIDs)
	if len(p.ClinicalFlags) == 0 {
		return candidateIDs, models.StepResult{
			Step: 3, Name: "Clinical rules", Kind: "record",
			CandidatesIn: stepIn, CandidatesOut: stepIn,
			Note: "no clinical flags set, step is a no-op",
		}, nil
	}

	// Validate every declared flag key against clinical_rule_master.trigger_field before
	// evaluating anything. This is the same class of check as allergyFilter's unmatched-
	// allergen validation, but the stakes are higher here: an unrecognized key (a typo
	// like "CDK" for "CKD") must not fail open into a full, unescalated recipe list --
	// this is the boundary between a general recipe and a clinical-escalation pathway.
	validRows, err := pool.Query(ctx, `SELECT DISTINCT trigger_field FROM clinical_rule_master`)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: clinical trigger field lookup: %w", err)
	}
	validFields := make(map[string]bool)
	for validRows.Next() {
		var f string
		if err := validRows.Scan(&f); err != nil {
			validRows.Close()
			return nil, models.StepResult{}, fmt.Errorf("engine: clinical trigger field scan: %w", err)
		}
		validFields[f] = true
	}
	if err := validRows.Err(); err != nil {
		validRows.Close()
		return nil, models.StepResult{}, fmt.Errorf("engine: clinical trigger field rows: %w", err)
	}
	validRows.Close()

	var unmatched []string
	for key := range p.ClinicalFlags {
		if !validFields[key] {
			unmatched = append(unmatched, key)
		}
	}
	if len(unmatched) > 0 {
		return nil, models.StepResult{}, fmt.Errorf("engine: clinical filter: unrecognized clinical flag key(s) %v — must match clinical_rule_master.trigger_field exactly: %w", unmatched, ErrInvalidProfile)
	}

	// Every rule is loaded now, not only the ones that used to escalate
	// (hard_exclude_yn = 'Y' or the specialist tier). Once the step reports rather than
	// blocks, the subset that mattered for blocking is the wrong subset to report: a doctor
	// reading the step wants every rule their flags fired, not the handful that would once
	// have stopped the run.
	//
	// Two exclusions stay. Age/Feeding is enforced structurally by recipe_master's own age
	// bounds, and Data Quality is about the dataset rather than the child.
	//
	// ORDER BY still matters, now for the order rules appear in the note rather than for
	// which one won. Row order is not otherwise stable across re-imports, because an
	// upsert's UPDATE writes a new heap tuple. rule_priority is text ('Critical', 'High',
	// 'Low', 'Medium'), and a bare ORDER BY rule_priority would sort alphabetically -- High
	// before Low before Medium -- which is wrong for Low/Medium. The CASE maps it to the
	// real severity order explicitly. A priority word the provider has not used yet sorts
	// last (ELSE), not first: an unknown value must not silently outrank Critical. rule_id
	// is the final tie-break for rules sharing a priority, such as CR-REN-001 and
	// CR-REN-002.
	rows, err := pool.Query(ctx, `
		SELECT rule_id, clinical_domain, trigger_field, trigger_operator, trigger_value,
		       escalation_reason, coalesce(specialist_required, '')
		FROM clinical_rule_master
		WHERE clinical_domain NOT IN ('Age/Feeding', 'Data Quality')
		ORDER BY CASE rule_priority
		           WHEN 'Critical' THEN 0
		           WHEN 'High'     THEN 1
		           WHEN 'Medium'   THEN 2
		           WHEN 'Low'      THEN 3
		           ELSE 4
		         END, rule_id`)
	if err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: clinical rule lookup: %w", err)
	}
	defer rows.Close()

	var rules []clinicalRule
	for rows.Next() {
		var r clinicalRule
		if err := rows.Scan(&r.ruleID, &r.clinicalDomain, &r.triggerField, &r.triggerOperator,
			&r.triggerValue, &r.escalationReason, &r.specialistRequired); err != nil {
			return nil, models.StepResult{}, fmt.Errorf("engine: clinical rule scan: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, models.StepResult{}, fmt.Errorf("engine: clinical rule rows: %w", err)
	}

	// Every rule that fires is recorded by id and domain, in the deterministic priority
	// order the query set. Nothing returns early and nothing wins over anything else: the
	// complete list is the point, and an operator reading it can see exactly which of the
	// provider's rules this child's flags touched.
	var fired []string
	for i := range rules {
		r := rules[i]
		flagValue, set := p.ClinicalFlags[r.triggerField]
		if !set {
			continue
		}
		if !triggerFires(r.triggerOperator, r.triggerValue, flagValue) {
			continue
		}
		// The provider's own escalation_reason travels with the rule id, verbatim. It is
		// the sentence that says what the rule is actually about, and it is the reason this
		// note is worth reading rather than a list of identifiers. Same posture as the
		// special-care step: the text no longer stops anything, and it is still theirs.
		entry := fmt.Sprintf("%s (%s)", r.ruleID, r.clinicalDomain)
		if r.escalationReason != "" {
			entry += ": " + r.escalationReason
		}
		if r.specialistRequired != "" {
			entry += " Specialist: " + r.specialistRequired + "."
		}
		fired = append(fired, entry)
	}

	note := "clinical flags set, no rule fired"
	if len(fired) > 0 {
		note = "rules fired, recorded not filtered -- " + strings.Join(fired, " | ")
	}
	return candidateIDs, models.StepResult{
		Step: 3, Name: "Clinical rules", Kind: "record",
		CandidatesIn: stepIn, CandidatesOut: stepIn, Note: note,
	}, nil
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
