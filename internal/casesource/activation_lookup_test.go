package casesource

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ingredientLookupRow struct {
	recipe, meaning, grams string
}

type ingredientQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func ingredientLookupRows(t *testing.T, q ingredientQuerier, sql string, args ...any) map[ingredientLookupRow]int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[ingredientLookupRow]int{}
	for rows.Next() {
		var row ingredientLookupRow
		if err := rows.Scan(&row.recipe, &row.meaning, &row.grams); err != nil {
			t.Fatal(err)
		}
		out[row]++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertIngredientLookup(t *testing.T, pool *pgxpool.Pool, current *Archive, previousSHA string) {
	t.Helper()
	ctx := context.Background()
	// The fresh import's estimates hide the slow join seen after statistics refresh.
	if _, err := pool.Exec(ctx, `ANALYZE case_source_ingredient, case_source_ingredient_meaning,
	    case_source_recipe_revision, case_source_recipe_current, recipe_ingredient_mapping`); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT recipe_id FROM engine_candidate_existing ORDER BY recipe_id`)
	if err != nil {
		t.Fatal(err)
	}
	var legacyIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		legacyIDs = append(legacyIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	legacy := ingredientLookupRows(t, pool, `SELECT recipe_id,ingredient_id,coalesce(quantity::text,'')
	    FROM engine_candidate_ingredient_existing WHERE recipe_id=ANY($1)`, legacyIDs)
	if len(legacyIDs) < 900 || len(legacy) == 0 {
		t.Fatal("full legacy ingredient control is missing")
	}
	mixed := make(map[ingredientLookupRow]int, len(legacy))
	for row, count := range legacy {
		mixed[row] = count
	}
	mixedIDs := append([]string(nil), legacyIDs...)
	for _, workbook := range current.Workbooks {
		for _, sheet := range workbook.Sheets {
			if sheet.Name != "Recipe_Master" {
				continue
			}
			for _, recipe := range sheet.Rows {
				if recipe.RecipeID == "" {
					continue
				}
				id := CandidateID(current.SHA256, recipe.RecipeID)
				mixedIDs = append(mixedIDs, id, CandidateID(previousSHA, recipe.RecipeID))
				for _, ingredient := range recipe.Ingredients {
					mixed[ingredientLookupRow{id, MeaningID(ingredient.RawID, ingredient.RawName), ingredient.Grams}]++
				}
			}
		}
	}
	const query = `SELECT recipe_id,ingredient_id,coalesce(quantity::text,'')
	    FROM engine_candidate_ingredient WHERE recipe_id=ANY($1)`
	for _, test := range []struct {
		name string
		ids  []string
		want map[ingredientLookupRow]int
	}{
		{"legacy only", legacyIDs, legacy},
		{"current and retired revisions", mixedIDs, mixed},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := time.Now()
			got := ingredientLookupRows(t, pool, query, test.ids)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ingredient rows changed: distinct rows got=%d want=%d", len(got), len(test.want))
			}
			t.Logf("INGREDIENT_LOOKUP_OK candidates=%d distinct_rows=%d elapsed=%s", len(test.ids), len(got), time.Since(started))
		})
	}
	t.Run("migration preserves every occurrence", func(t *testing.T) {
		assertIngredientLookupMigration(t, pool)
	})
}

func assertIngredientLookupMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	digest := func() string {
		t.Helper()
		var value string
		if err := tx.QueryRow(ctx, `SELECT count(*)::text || ':' ||
		    md5(string_agg(row_to_json(t)::text, E'\n' ORDER BY row_to_json(t)::text))
		    FROM case_source_recipe_ingredient t`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	want := digest()
	var corrected string
	if err := tx.QueryRow(ctx, `SELECT pg_get_viewdef('case_source_recipe_ingredient')`).Scan(&corrected); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"down", "up", "up"} {
		migration, err := os.ReadFile("../db/migrations/0047_case_source_revision_lookup." + direction + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
		if got := digest(); got != want {
			t.Fatalf("migration %s changed occurrence rows: got=%s want=%s", direction, got, want)
		}
		var actual string
		if err := tx.QueryRow(ctx, `SELECT pg_get_viewdef('case_source_recipe_ingredient')`).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if (direction == "up") != (actual == corrected) {
			t.Fatalf("migration %s did not restore the expected view definition", direction)
		}
	}
	t.Logf("MIGRATION_LOOKUP_OK down_up_repeat occurrence_digest=%s", want)
}
