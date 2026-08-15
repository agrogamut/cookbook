package enrich

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// refreshGaps updates the gap register with what enrichment actually achieved, and
// registers the gaps enrichment itself creates. Coverage that falls short is recorded as
// a gap rather than reported as a success with a footnote.
func refreshGaps(ctx context.Context, tx pgx.Tx) error {
	// GAP-001 no longer means "1000 recipes share 6 texts". It now means "this many
	// recipes still have nothing but the boilerplate".
	if _, err := tx.Exec(ctx, `
		UPDATE gap_register SET
		    description = 'The provider ships 6 unique preparation texts across all recipes. Recipes with no external match above the confidence threshold have only that boilerplate.',
		    affected_rows = (SELECT count(*) FROM recipe_master r
		                     WHERE NOT EXISTS (SELECT 1 FROM recipe_method_external m WHERE m.recipe_id = r.recipe_id)),
		    ui_behaviour = 'Provider text shown verbatim. Where a suggestion exists it appears beside it, labelled with its source, confidence and whether the match came from the same region.',
		    measured_at = now()
		WHERE gap_id = 'GAP-001'`); err != nil {
		return fmt.Errorf("enrich: update GAP-001: %w", err)
	}

	rows := []struct {
		id, severity, area, table, column, desc, ui, path, query string
	}{
		{
			"GAP-013", "major", "External coverage", "recipe_method_external", "suggested_method",
			"Recipes with no external method suggestion: no candidate from their region cleared the confidence threshold.",
			"Provider text only. No suggestion panel is shown, and no lower-quality match is substituted to fill the space.",
			"More regional recipes in the external corpus, or a better ingredient synonym table. Lowering the threshold is not a fix.",
			`SELECT count(*) FROM recipe_master r
			 WHERE NOT EXISTS (SELECT 1 FROM recipe_method_external m WHERE m.recipe_id = r.recipe_id)`,
		},
		{
			"GAP-014", "minor", "External coverage", "recipe_method_external", "region_match",
			"Recipes whose suggested method came from a pan-Indian row rather than one labelled with their own region.",
			"Shown with the match labelled pan-India, so a reader can see it is not region-specific.",
			"More region-labelled rows in the external corpus.",
			`SELECT count(*) FROM recipe_method_external WHERE region_match = 'pan-india'`,
		},
		{
			"GAP-015", "major", "Nutrition audit", "ingredient_nutrition_audit", "verdict",
			"Ingredients used in at least one recipe whose provider nutrition differs from IFCT 2017 by more than 20 percent on energy, protein, iron or calcium.",
			"Provider values are displayed. The audit is a review artefact, not a correction: nothing is silently replaced.",
			"Provider review. The discrepancy list is the deliverable to send them.",
			`SELECT count(*) FROM ingredient_nutrition_audit WHERE verdict = 'discrepancy' AND used_in_recipes > 0`,
		},
		{
			"GAP-016", "major", "Nutrition audit", "ingredient_nutrition_audit", "food_code",
			"Ingredients used in at least one recipe that could not be matched to any IFCT 2017 food with enough confidence, so their nutrition is unverified.",
			"Provider values displayed and marked unverified. No approximate food is substituted.",
			"Hand-map the remaining names, or accept them as unverifiable against IFCT and say so in the limitations section.",
			`SELECT count(*) FROM ingredient_nutrition_audit WHERE verdict = 'unmatched' AND used_in_recipes > 0`,
		},
	}

	for _, r := range rows {
		var n int
		if err := tx.QueryRow(ctx, r.query).Scan(&n); err != nil {
			return fmt.Errorf("enrich: measure %s: %w", r.id, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO gap_register
			    (gap_id, severity, area, source_table, source_column, description,
			     affected_rows, measured_by, ui_behaviour, resolution_path, measured_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'importer',$8,$9, now())
			ON CONFLICT (gap_id) DO UPDATE SET
			    severity = EXCLUDED.severity,
			    description = EXCLUDED.description,
			    affected_rows = EXCLUDED.affected_rows,
			    ui_behaviour = EXCLUDED.ui_behaviour,
			    resolution_path = EXCLUDED.resolution_path,
			    measured_at = now()`,
			r.id, r.severity, r.area, r.table, r.column, r.desc, n, r.ui, r.path); err != nil {
			return fmt.Errorf("enrich: record %s: %w", r.id, err)
		}
	}
	return nil
}
