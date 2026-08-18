package importer

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// gapMeasure re-counts one seeded gap against the freshly imported data, so the gap
// register reports what is true now rather than what was true when the audit was run.
// The query defines exactly what "affected" means for that gap; nothing is estimated.
type gapMeasure struct {
	gapID string
	query string
}

var gapMeasures = []gapMeasure{
	{"GAP-001", `SELECT count(*) FROM recipe_master r
	             WHERE (SELECT count(*) FROM recipe_master x
	                    WHERE x.preparation_method_full = r.preparation_method_full) > 1`},

	{"GAP-002", `SELECT count(*) FROM recipe_master r
	             WHERE (SELECT count(*) FROM recipe_master x
	                    WHERE x.safety_rule = r.safety_rule) > 1`},

	// Population left without an iron-support option: every recipe in an age band that
	// ends at or before 11 months, none of which carries an iron/anaemia clinical tag.
	{"GAP-003", `SELECT count(*) FROM recipe_master
	             WHERE max_age_months <= 11
	               AND clinical_tag !~* '(iron|anaem|anem)'`},

	{"GAP-004", `SELECT (SELECT count(*) FROM recipe_master             WHERE review_status !~* 'approved')
	                  + (SELECT count(*) FROM ingredient_master         WHERE review_status !~* 'approved')
	                  + (SELECT count(*) FROM recipe_ingredient_mapping WHERE mapping_status !~* 'approved')`},

	// Single-valued tag columns: a recipe carrying no separator in any tag column can
	// only ever satisfy one value of that filter, which is what collapses the results.
	{"GAP-005", `SELECT count(*) FROM recipe_master
	             WHERE clinical_tag         !~ '[;,|]'
	               AND growth_target        !~ '[;,|]'
	               AND season               !~ '[;,|]'
	               AND meal_type            !~ '[;,|]'
	               AND texture              !~ '[;,|]'
	               AND post_vaccine_context !~ '[;,|]'`},

	{"GAP-006", `SELECT count(*) FROM ingredient_master
	             WHERE allergen_tags ILIKE '%starter tagging%'`},

	{"GAP-007", `SELECT count(*) FROM ingredient_master i
	             WHERE NOT EXISTS (SELECT 1 FROM recipe_ingredient_mapping m
	                               WHERE m.ingredient_id = i.ingredient_id)`},

	// Workbook rows read but not loaded because their region is out of scope. Measured
	// from the current run's own stats, so it reflects this import rather than a
	// historical one.
	{"GAP-011", `SELECT coalesce(sum(rows_skipped), 0) FROM import_table_stat
	             WHERE table_name = 'recipe_master'
	               AND run_id = (SELECT max(run_id) FROM import_table_stat)`},

	{"GAP-012", `SELECT count(*) FROM culture_location_master c
	             WHERE NOT EXISTS (
	                 SELECT 1 FROM culture_region_map m
	                 JOIN recipe_master r ON r.region_culture = m.region_culture
	                 WHERE m.culture_code = c.culture_code)`},

	// Allergen groups the provider's vocabulary names but the corpus never tags. Counted
	// from the vocabulary table rather than from a hardcoded list, so it drops to zero by
	// itself when the provider tags the corpus.
	{"GAP-017", `SELECT count(*) FROM allergen_tag_vocabulary WHERE corpus_tag IS NULL`},

	// Clinical rules at the provider's own 'Specialist clinical approval' tier that the
	// engine's rule query drops before any escalation check can see them. The query
	// excludes the Age/Feeding and Data Quality domains -- Age/Feeding because step 1's
	// min_age_months/max_age_months bounds already enforce it structurally, Data Quality
	// because it is a pipeline concern rather than a child's condition. A specialist-tier
	// rule filed under either would be invisible no matter what the escalation logic says,
	// which is why this is the honest measure of the gap rather than a count of domains.
	//
	// Reads zero today. Non-zero means the provider filed a new specialist-tier rule under
	// a domain the engine structurally drops, and someone must classify it.
	{"GAP-020", `SELECT count(*) FROM clinical_rule_master
	             WHERE human_approval_level = 'Specialist clinical approval'
	               AND clinical_domain IN ('Age/Feeding', 'Data Quality')`},

	// Special-care recipe candidates with no counterpart in the validated Recipe Master.
	// Measured by name, which is the only join the provider left available: the sheet's
	// Meal_Category reads "Assign in main Recipe Master" on every row, so the mapping is
	// explicitly outstanding work rather than a missing column.
	//
	// Reads 108 today, the whole table. It falls as the provider maps candidates onto
	// real recipes, and reaching zero is what would make the table servable -- which is
	// also why TestSpecialCareCandidatesAreNeverApproved fails on the first mapping
	// rather than letting it happen quietly.
	{"GAP-021", `SELECT count(*) FROM special_care_recipe_candidate c
	             WHERE NOT EXISTS (
	                 SELECT 1 FROM recipe_master r WHERE r.recipe_name = c.recipe_name)`},

	// Special-care output rules with no engine implementation. Only OR-001, the
	// condition-detected stop, is implemented; the other 13 need feeding route, prescribed
	// IDDSI level, cardiology fluid orders, post-operative status, pica or sensory profile,
	// none of which this project collects.
	//
	// Counted from the table rather than hardcoded at 13, so reissuing the workbook with
	// more rules raises the number instead of leaving it stale.
	{"GAP-022", `SELECT count(*) FROM special_care_output_rule WHERE rule_id <> 'OR-001'`},

	// Recipes that can reach no Book 2 chapter. Counted through the mapping table rather
	// than by comparing vocabularies, so a provider ruling that adds a mapping drops this
	// number without anyone editing the query.
	//
	// Reads 354 today: Snack 182, School Tiffin 99, Recovery Meal 73.
	{"GAP-023", `SELECT count(*) FROM recipe_master r
	             WHERE NOT EXISTS (
	                 SELECT 1 FROM meal_category_recipe_map m
	                 WHERE m.meal_type = r.meal_type)`},
}

// measureGaps refreshes the counted gaps inside the import transaction.
func measureGaps(ctx context.Context, tx pgx.Tx) error {
	for _, g := range gapMeasures {
		var n int
		if err := tx.QueryRow(ctx, g.query).Scan(&n); err != nil {
			return fmt.Errorf("importer: measure %s: %w", g.gapID, err)
		}
		tag, err := tx.Exec(ctx,
			`UPDATE gap_register SET affected_rows = $2, measured_at = now() WHERE gap_id = $1`,
			g.gapID, n)
		if err != nil {
			return fmt.Errorf("importer: record %s: %w", g.gapID, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("importer: gap %s is measured but not registered; add it to the gap register seed", g.gapID)
		}
	}
	return nil
}
