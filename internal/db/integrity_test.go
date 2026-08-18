package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/importer"
)

// The suite runs against a real Postgres because the checks it encodes are constraints
// on the imported data, not on Go code. Point TEST_DATABASE_URL at a throwaway database
// (scripts/dev_db.fish starts one) and the suite migrates and imports into it itself.
//
// Without TEST_DATABASE_URL the suite skips, so `go test ./...` stays green on a
// machine with no database.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integrity suite")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	dir := os.Getenv("XLSX_DIR")
	if dir == "" {
		dir = "../../data/provider"
	}
	var loaded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_master`).Scan(&loaded); err != nil {
		t.Fatalf("probe recipe_master: %v", err)
	}
	if loaded == 0 {
		if _, err := importer.Run(ctx, pool, dir); err != nil {
			t.Fatalf("import: %v", err)
		}
	}
	return pool
}

// violation is a check that must return zero rows. The query selects the offending
// rows, so a failure prints what is actually wrong rather than just a count.
type violation struct {
	name  string
	why   string
	query string
}

// These encode the referential and clinical invariants the audit verified. They are
// regression tests: if a provider re-issue breaks one, the import is not trustworthy
// and the failure says exactly which row broke it.
var violations = []violation{
	{
		name:  "no orphan mapping recipe",
		why:   "every mapping row must point at a real recipe",
		query: `SELECT m.mapping_id FROM recipe_ingredient_mapping m LEFT JOIN recipe_master r USING (recipe_id) WHERE r.recipe_id IS NULL`,
	},
	{
		name:  "no orphan mapping ingredient",
		why:   "every mapping row must point at a real ingredient",
		query: `SELECT m.mapping_id FROM recipe_ingredient_mapping m LEFT JOIN ingredient_master i USING (ingredient_id) WHERE i.ingredient_id IS NULL`,
	},
	{
		name:  "no unmapped recipe",
		why:   "a recipe with no ingredients cannot be cooked or filtered",
		query: `SELECT r.recipe_id FROM recipe_master r WHERE NOT EXISTS (SELECT 1 FROM recipe_ingredient_mapping m WHERE m.recipe_id = r.recipe_id)`,
	},
	{
		name: "inline ingredient list matches mapping table",
		why:  "Recipe_Master.Ingredient_IDs is denormalised; it must agree with the mapping table or the two disagree about what is in the dish",
		query: `SELECT r.recipe_id
		        FROM recipe_master r
		        WHERE (SELECT array_agg(DISTINCT x ORDER BY x) FROM unnest(string_to_array(r.ingredient_ids, ';')) AS x)
		           IS DISTINCT FROM
		              (SELECT array_agg(DISTINCT m.ingredient_id ORDER BY m.ingredient_id)
		               FROM recipe_ingredient_mapping m WHERE m.recipe_id = r.recipe_id)`,
	},
	{
		name: "ingredient id, name and quantity lists align",
		why:  "a mismatch means a quantity is attached to the wrong ingredient",
		query: `SELECT recipe_id FROM recipe_master
		        WHERE array_length(string_to_array(ingredient_ids, ';'), 1)
		            <> array_length(string_to_array(ingredient_names, ';'), 1)
		           OR array_length(string_to_array(ingredient_ids, ';'), 1)
		            <> array_length(string_to_array(ingredient_quantities_g, ';'), 1)`,
	},
	{
		name:  "no recipe with min age above max age",
		why:   "engine step 1 is a hard filter on this range",
		query: `SELECT recipe_id FROM recipe_master WHERE min_age_months > max_age_months`,
	},
	{
		name: "no ingredient introduced earlier than it is allowed",
		why:  "an ingredient whose own minimum age exceeds the recipe's would be fed to a child too young for it",
		query: `SELECT m.mapping_id FROM recipe_ingredient_mapping m
		        JOIN ingredient_master i ON i.ingredient_id = m.ingredient_id
		        JOIN recipe_master r     ON r.recipe_id = m.recipe_id
		        WHERE i.min_age_months > r.min_age_months`,
	},
	{
		name: "no honey below twelve months",
		why:  "infant botulism risk. This is a named rule in the age master and is not negotiable",
		query: `SELECT m.mapping_id FROM recipe_ingredient_mapping m
		        JOIN recipe_master r ON r.recipe_id = m.recipe_id
		        WHERE m.ingredient_name_en ILIKE '%honey%' AND r.min_age_months < 12`,
	},
	{
		name: "no undeclared allergen",
		why:  "a recipe must declare every allergen its ingredients carry. The literal 'None identified in starter tagging' is an unscreened marker, not an allergen",
		query: `SELECT DISTINCT r.recipe_id
		        FROM recipe_master r
		        JOIN recipe_ingredient_mapping m ON m.recipe_id = r.recipe_id
		        JOIN ingredient_master i         ON i.ingredient_id = m.ingredient_id
		        WHERE i.allergen_tags NOT ILIKE '%starter tagging%'
		          AND lower(btrim(i.allergen_tags)) NOT IN ('', 'none', 'nil', '-', 'na')
		          AND position(lower(btrim(i.allergen_tags)) IN lower(r.allergen_tags)) = 0`,
	},
	{
		name: "no phantom allergen declaration",
		why:  "an allergen declared on a recipe but present on none of its ingredients would exclude the recipe for no reason",
		query: `SELECT r.recipe_id
		        FROM recipe_master r
		        WHERE r.allergen_tags NOT ILIKE '%starter tagging%'
		          AND lower(btrim(r.allergen_tags)) NOT IN ('', 'none', 'nil', '-', 'na')
		          AND NOT EXISTS (
		              SELECT 1 FROM recipe_ingredient_mapping m
		              JOIN ingredient_master i ON i.ingredient_id = m.ingredient_id
		              WHERE m.recipe_id = r.recipe_id
		                AND position(lower(btrim(i.allergen_tags)) IN lower(r.allergen_tags)) > 0
		                AND i.allergen_tags NOT ILIKE '%starter tagging%')`,
	},
	{
		name: "energy agrees with the macronutrients",
		why:  "kcal must sit within 25 percent of 4/4/9 over protein, carbohydrate and fat, or one of the two numbers is wrong",
		query: `SELECT recipe_id FROM recipe_master
		        WHERE energy_kcal > 0
		          AND abs((4*protein_g + 4*carb_g + 9*fat_g) - energy_kcal) / energy_kcal > 0.25`,
	},
	{
		name:  "no implausible ingredient energy density",
		why:   "nothing edible exceeds about 900 kcal per 100 g, which is pure fat",
		query: `SELECT ingredient_id FROM ingredient_master WHERE energy_kcal_100g < 0 OR energy_kcal_100g > 900`,
	},
	{
		name: "substitute ingredient ids resolve",
		why:  "a substitute pointing at a missing ingredient breaks the allergy substitution path",
		query: `SELECT i.ingredient_id
		        FROM ingredient_master i,
		             unnest(string_to_array(coalesce(i.substitute_ingredient_ids, ''), ';')) AS s(id)
		        WHERE btrim(s.id) <> ''
		          AND upper(btrim(s.id)) NOT IN ('NONE', 'NA', '-')
		          AND NOT EXISTS (SELECT 1 FROM ingredient_master x WHERE x.ingredient_id = btrim(s.id))`,
	},
	{
		name: "no vegetarian recipe containing a non-vegetarian ingredient",
		why:  "engine step 4 filters on declared food practice; a mislabelled recipe would be served to a family that excluded it",
		query: `SELECT DISTINCT r.recipe_id
		        FROM recipe_master r
		        JOIN recipe_ingredient_mapping m ON m.recipe_id = r.recipe_id
		        JOIN ingredient_master i         ON i.ingredient_id = m.ingredient_id
		        WHERE r.diet_type ILIKE 'vegetarian'
		          AND (i.diet_type ILIKE '%non-veg%' OR i.diet_type ILIKE '%egg%')`,
	},
	{
		name:  "review status is never overwritten as approved",
		why:   "nothing may be marked approved on our side. The provider owns sign-off",
		query: `SELECT recipe_id FROM recipe_master WHERE review_status !~* 'draft'`,
	},
	{
		name: "every culture region mapping resolves to a real culture code",
		why:  "the map is hand-written and carries no foreign key, so it is asserted here instead",
		query: `SELECT m.culture_code FROM culture_region_map m
		        LEFT JOIN culture_location_master c USING (culture_code) WHERE c.culture_code IS NULL`,
	},
	{
		name: "every recipe region resolves to at least one culture code",
		why:  "engine step 7 cannot execute for a region with no culture row",
		query: `SELECT DISTINCT r.region_culture FROM recipe_master r
		        WHERE NOT EXISTS (SELECT 1 FROM culture_region_map m WHERE m.region_culture = r.region_culture)`,
	},
	{
		name: "every recipe region has a focus tier",
		why:  "a region with no tier would drop out of recipe_ranked entirely",
		query: `SELECT DISTINCT r.region_culture FROM recipe_master r
		        LEFT JOIN region_focus f ON f.region_culture = r.region_culture WHERE f.region_culture IS NULL`,
	},
	{
		name: "every imported recipe is in project scope",
		why:  "the scope filter is what keeps out-of-region rows out of the product",
		query: `SELECT r.recipe_id FROM recipe_master r
		        LEFT JOIN region_focus f ON f.region_culture = r.region_culture
		        WHERE f.region_culture IS NULL`,
	},
	{
		name:  "no cuisine option without recipes",
		why:   "a dropdown entry that returns an empty page is the bug this view exists to prevent",
		query: `SELECT culture_code FROM cuisine_option WHERE recipe_count = 0`,
	},
	{
		name: "every recipe is scored against every nutrition target",
		why:  "a ranker that drops rows is a filter, and a filter can return zero",
		query: `SELECT r.recipe_id FROM recipe_master r
		        WHERE (SELECT count(*) FROM recipe_target_score s WHERE s.recipe_id = r.recipe_id)
		           <> (SELECT count(*) FROM nutrition_target_master)`,
	},
	{
		name:  "every score is a real number between zero and one",
		why:   "a null or out-of-range score means an axis went missing from the normalisation",
		query: `SELECT recipe_id FROM recipe_target_score WHERE score IS NULL OR score < 0 OR score > 1`,
	},
	{
		name:  "every measured gap has been counted",
		why:   "an unmeasured gap in the register is a gap nobody is watching",
		query: `SELECT gap_id FROM gap_register WHERE measured_by = 'importer' AND measured_at IS NULL`,
	},
	{
		name: "every allergen_mapping group has a vocabulary bridge row",
		why:  "allergyFilter joins through allergen_tag_vocabulary; a group missing from it silently excludes nothing for that allergen",
		query: `SELECT am.allergen_group FROM allergen_mapping am
		        LEFT JOIN allergen_tag_vocabulary v ON v.allergen_group = am.allergen_group
		        WHERE v.allergen_group IS NULL`,
	},
	{
		name: "every non-null allergen_tag_vocabulary corpus_tag actually appears in the corpus",
		why:  "a stale or mistyped corpus_tag would make allergyFilter silently exclude zero recipes for a real allergen, the exact bug this table fixes",
		query: `SELECT v.allergen_group FROM allergen_tag_vocabulary v
		        WHERE v.corpus_tag IS NOT NULL
		          AND NOT EXISTS (SELECT 1 FROM recipe_master r WHERE r.allergen_tags ILIKE '%' || v.corpus_tag || '%')
		          AND NOT EXISTS (SELECT 1 FROM recipe_ingredient_mapping m WHERE m.ingredient_allergen_tag ILIKE '%' || v.corpus_tag || '%')`,
	},
}

func TestDataIntegrity(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, v := range violations {
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

// TestExpectedRowCounts pins the shape of the provider release we audited. A changed
// count is not necessarily wrong, but it means the audit needs redoing before the data
// is trusted, so it should fail loudly rather than pass quietly.
func TestExpectedRowCounts(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Recipes and mappings are the in-scope counts, not the workbook counts: the
	// provider ships 1000 recipes and 3317 mapping rows, of which 940 and 3116 fall
	// inside the India + Bangladesh scope.
	expected := map[string]int{
		"recipe_master":             940,
		"ingredient_master":         406,
		"recipe_ingredient_mapping": 3116,
		"culture_location_master":   37,
		"nutrition_target_master":   13,
		"age_feeding_stage_master":  10,
		"clinical_rule_master":      31,
		"allergy_safety_master":     28,
		"recipe_selection_logic":    14,
		"culture_region_map":        27,
		"region_focus":              8,
	}
	for table, want := range expected {
		t.Run(table, func(t *testing.T) {
			var got int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
				t.Fatalf("count: %v", err)
			}
			if got != want {
				t.Errorf("got %d rows, want %d; the provider release changed and the audit needs redoing", got, want)
			}
		})
	}
}

// TestImportIsIdempotent re-runs the import and asserts the content hashes are
// unchanged. This is the Phase 1 exit criterion "re-running the importer twice produces
// an identical database".
func TestImportIsIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	dir := os.Getenv("XLSX_DIR")
	if dir == "" {
		dir = "../../data/provider"
	}

	first, err := importer.Run(ctx, pool, dir)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	second, err := importer.Run(ctx, pool, dir)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("import produced %d tables then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ContentHash != second[i].ContentHash {
			t.Errorf("%s: content hash changed between identical runs (%s then %s)",
				first[i].Table, first[i].ContentHash[:12], second[i].ContentHash[:12])
		}
		if second[i].RowsDeleted != 0 {
			t.Errorf("%s: second run deleted %d rows; the import is not idempotent",
				first[i].Table, second[i].RowsDeleted)
		}
	}
}

func TestGapRegisterCountMatchesTheDocumentedCount(t *testing.T) {
	pool := testPool(t)
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM gap_register`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	// Twenty-two after migration 0015. The build-up: twelve seeded in migration 0002, four
	// that internal/enrich/gaps.go upserts on every enrichment run, four added by 0012,
	// and two by 0015. The enrich four are easy to miss because no migration writes them,
	// which is exactly why this count is asserted rather than left in prose. If a gap is
	// added or retired, change this number and the documents that quote it in the same
	// commit.
	if n != 22 {
		t.Fatalf("gap_register holds %d rows, want 22", n)
	}
}

func TestNewGapsAreMeasuredNotGuessed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cases := []struct {
		gapID string
		want  int
	}{
		// Four allergen groups with no corpus tag, counted from the vocabulary table.
		{"GAP-017", 4},
		// Clinical rules at the provider's specialist tier that clinicalFilter's rule
		// query structurally drops before any escalation check can see them, because the
		// query excludes the Age/Feeding and Data Quality domains outright. Zero today:
		// no specialist-tier rule is filed under either domain. A non-zero count means
		// the provider added one, and the engine would silently never load it regardless
		// of the escalation logic -- so someone must widen the query or classify it.
		{"GAP-020", 0},
	}
	for _, c := range cases {
		t.Run(c.gapID, func(t *testing.T) {
			var got *int
			err := pool.QueryRow(ctx,
				`SELECT affected_rows FROM gap_register WHERE gap_id = $1`, c.gapID).Scan(&got)
			if err != nil {
				t.Fatalf("lookup %s: %v", c.gapID, err)
			}
			if got == nil {
				t.Fatalf("%s has a NULL count; it is declared measured_by = importer and "+
					"must carry a number after an import", c.gapID)
			}
			if *got != c.want {
				t.Fatalf("%s affected_rows = %d, want %d", c.gapID, *got, c.want)
			}
		})
	}
}
