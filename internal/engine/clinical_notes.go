package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// ClinicalRuleAction is one clinical_rule_master row whose trigger condition this child's
// profile actually meets, carrying the provider's own book2_action/required_modification
// text. Neither column is read anywhere else in the engine today (they exist on the table
// but nothing selects them) -- this is the first reader, and it is a read, not a filter:
// selection/blocking decisions still belong to clinicalFilter (step 3) alone.
type ClinicalRuleAction struct {
	RuleID               string
	ClinicalDomain       string
	Book2Action          string
	RequiredModification string
}

// ActiveClinicalRuleActions returns every clinical_rule_master row whose trigger condition
// is met by p.ClinicalFlags and that carries non-empty book2_action or required_modification
// text -- the provider's own source for a per-recipe feeding note, grounding
// internal/aidraft's modification-note drafting. Nothing here writes a word of guidance;
// it only surfaces which provider rows apply and what they already say.
//
// A row for a rule that would have escalated generation entirely (clinicalFilter's
// hard-exclude/specialist tier) never has a caller in practice, since a blocked child's book
// is never assembled -- but this function does not itself re-check escalation, it only reads
// two columns nothing else in the engine reads, using the same trigger evaluation
// clinicalFilter already applies so the two never disagree about which rules fire.
func ActiveClinicalRuleActions(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) ([]ClinicalRuleAction, error) {
	if len(p.ClinicalFlags) == 0 {
		return nil, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT rule_id, clinical_domain, trigger_field, trigger_operator, trigger_value,
		       coalesce(book2_action, ''), coalesce(required_modification, '')
		FROM clinical_rule_master
		ORDER BY rule_id`)
	if err != nil {
		return nil, fmt.Errorf("engine: clinical rule action lookup: %w", err)
	}
	defer rows.Close()

	var out []ClinicalRuleAction
	for rows.Next() {
		var ruleID, domain, triggerField, triggerOperator, triggerValue, book2Action, requiredMod string
		if err := rows.Scan(&ruleID, &domain, &triggerField, &triggerOperator, &triggerValue,
			&book2Action, &requiredMod); err != nil {
			return nil, fmt.Errorf("engine: clinical rule action scan: %w", err)
		}

		flagValue, set := p.ClinicalFlags[triggerField]
		if !set || !triggerFires(triggerOperator, triggerValue, flagValue) {
			continue
		}
		if book2Action == "" && requiredMod == "" {
			continue
		}
		out = append(out, ClinicalRuleAction{
			RuleID:               ruleID,
			ClinicalDomain:       domain,
			Book2Action:          book2Action,
			RequiredModification: requiredMod,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engine: clinical rule action rows: %w", err)
	}
	return out, nil
}
