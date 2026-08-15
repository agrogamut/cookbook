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
