package db_test

import (
	"context"
	"testing"
)

// These hold the external annotation layer to the same rule as the provider layer: every
// value traces to a named source, and nothing overwrites what the provider shipped.
var externalViolations = []violation{
	{
		name: "every suggested method names its source",
		why:  "an external value without provenance cannot be traced or attributed",
		query: `SELECT recipe_id FROM recipe_method_external
		        WHERE source_key IS NULL OR match_confidence IS NULL OR suggested_method = ''`,
	},
	{
		name: "every suggested method points at a real external row",
		why:  "the row id is what makes a wrong suggestion traceable rather than mysterious",
		query: `SELECT m.recipe_id FROM recipe_method_external m
		        LEFT JOIN external_recipe e ON e.external_recipe_id = m.external_recipe_id
		        WHERE e.external_recipe_id IS NULL`,
	},
	{
		name: "no external recipe from an out-of-scope cuisine",
		why:  "a Continental or out-of-region recipe is not a source of South Asian pediatric method text",
		query: `SELECT DISTINCT e.cuisine FROM external_recipe e
		        LEFT JOIN external_cuisine_region_map c ON c.external_cuisine = e.cuisine
		        WHERE c.region_culture IS NULL`,
	},
	{
		name: "no suggested method from a deep-fried dish",
		why:  "deep frying is not an appropriate preparation for pediatric food",
		query: `SELECT m.recipe_id FROM recipe_method_external m
		        JOIN external_recipe e ON e.external_recipe_id = m.external_recipe_id
		        JOIN external_method_exclusion x ON lower(e.recipe_name) ~ ('\y' || x.keyword || '\y')`,
	},
	{
		name:  "no suggested method below the stored threshold",
		why:   "a coverage number is only meaningful if every row in it cleared the bar",
		query: `SELECT recipe_id FROM recipe_method_external WHERE match_confidence < 0.10`,
	},
	{
		name: "provider preparation text is never overwritten",
		why:  "the provider column is the reference; external text is annotation and lives in its own table",
		query: `SELECT recipe_id FROM recipe_master
		        WHERE preparation_method_full IS NULL OR preparation_method_full = ''`,
	},
	{
		name: "every external dataset records its provenance",
		why:  "a dataset without a URL, licence and checksum cannot be attributed or re-fetched",
		query: `SELECT source_key FROM external_source
		        WHERE url = '' OR licence = '' OR sha256 = '' OR retrieved_on IS NULL`,
	},
	{
		name: "every audited ingredient with a match records how sure that match is",
		why:  "a discrepancy is only a finding once the two rows are known to be the same food",
		query: `SELECT ingredient_id FROM ingredient_nutrition_audit
		        WHERE food_code IS NOT NULL AND match_certainty IS NULL`,
	},
	{
		name: "provider nutrition is never replaced by external nutrition",
		why:  "the audit compares and reports; it does not correct",
		query: `SELECT a.ingredient_id FROM ingredient_nutrition_audit a
		        JOIN ingredient_master i ON i.ingredient_id = a.ingredient_id
		        WHERE a.provider_energy IS DISTINCT FROM i.energy_kcal_100g`,
	},
	{
		name:  "every audited ingredient has a verdict",
		why:   "an ingredient with no verdict is one nobody checked",
		query: `SELECT ingredient_id FROM ingredient_nutrition_audit WHERE verdict IS NULL`,
	},
}

func TestExternalDataIntegrity(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var loaded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM external_recipe`).Scan(&loaded); err != nil {
		t.Fatalf("probe external_recipe: %v", err)
	}
	if loaded == 0 {
		t.Skip("no external data loaded; run cmd/enrich first")
	}

	for _, v := range externalViolations {
		t.Run(v.name, func(t *testing.T) {
			rows, err := pool.Query(ctx, v.query)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			defer rows.Close()

			var offenders []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					t.Fatalf("scan: %v", err)
				}
				offenders = append(offenders, id)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate: %v", err)
			}
			if len(offenders) > 0 {
				shown := offenders
				if len(shown) > 10 {
					shown = shown[:10]
				}
				t.Errorf("%d rows violate this invariant (%s); first offenders: %v",
					len(offenders), v.why, shown)
			}
		})
	}
}

// TestScopeAccountingIsRecorded holds the importer to reporting what it left out. A
// silently dropped row is exactly the failure the gap register exists to prevent, and
// the count is what explains a 940-row table built from a 1000-row workbook.
func TestScopeAccountingIsRecorded(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var read, written, skipped int
	err := pool.QueryRow(ctx, `
		SELECT rows_read, rows_written, rows_skipped
		FROM import_table_stat
		WHERE table_name = 'recipe_master'
		ORDER BY run_id DESC LIMIT 1`).Scan(&read, &written, &skipped)
	if err != nil {
		t.Fatalf("read import stats: %v", err)
	}
	if read != written+skipped {
		t.Errorf("rows_read = %d but rows_written + rows_skipped = %d; some rows are unaccounted for",
			read, written+skipped)
	}
	if skipped == 0 {
		t.Errorf("no rows were recorded as out of scope, but the workbook holds regions the project does not serve")
	}

	var gap int
	if err := pool.QueryRow(ctx,
		`SELECT affected_rows FROM gap_register WHERE gap_id = 'GAP-011'`).Scan(&gap); err != nil {
		t.Fatalf("read GAP-011: %v", err)
	}
	if gap != skipped {
		t.Errorf("GAP-011 reports %d out-of-scope rows but the import skipped %d", gap, skipped)
	}
}
