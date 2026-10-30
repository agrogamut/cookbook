package casesource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/importer"
	"github.com/xuri/excelize/v2"
)

var localeTables = []string{"case_source_locale_validation", "case_source_locale_sheet", "case_source_locale_raw_row", "case_source_locale_text", "case_source_locale_glossary"}

func localeDigest(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range localeTables {
		var digest string
		if err := pool.QueryRow(context.Background(), `SELECT count(*)::text || ':' || md5(coalesce(string_agg(h,'' ORDER BY h),'')) FROM (SELECT md5(row_to_json(t)::text) h FROM `+pgx.Identifier{name}.Sanitize()+` t) x`).Scan(&digest); err != nil {
			t.Fatal(err)
		}
		out[name] = digest
	}
	return out
}

func TestIntakeImportDelivery(t *testing.T) {
	data, baseline := bengaliDelivery(t), delivery(t)
	pool := isolatedPool(t)
	ctx := context.Background()
	if _, err := importer.Run(ctx, pool, "../../data/provider"); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, pool, baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := Activate(ctx, pool, deliveryHash); err != nil {
		t.Fatal(err)
	}
	before := digestTables(t, pool, false)
	var pointers string
	if err := pool.QueryRow(ctx, `SELECT md5(string_agg(candidate_id,'' ORDER BY candidate_id)) FROM case_source_recipe_current`).Scan(&pointers); err != nil {
		t.Fatal(err)
	}
	var tofu, dairy int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE r.allergen_tags ILIKE '%Milk%') FROM recipe_master r WHERE EXISTS (SELECT 1 FROM recipe_ingredient_mapping m WHERE m.recipe_id=r.recipe_id AND m.ingredient_id='ING0075')`).Scan(&tofu, &dairy); err != nil || tofu != 54 || dairy != 11 {
		t.Fatalf("provider tofu/dairy evidence: %d/%d %v", tofu, dairy, err)
	}
	down, err := os.ReadFile("../db/migrations/0048_case_source_locale_staging.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../db/migrations/0048_case_source_locale_staging.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := ImportIntake(ctx, pool, data, baseline)
	if err != nil || !r.Inserted || !r.Report.ActivationBlocked {
		t.Fatalf("locale import: %+v %v", r.Report, err)
	}
	i, err := ParseIntake(ctx, data, baseline)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT archive_bytes FROM case_source_archive WHERE archive_sha256=$1`, i.Archive.SHA256).Scan(&stored); err != nil || !bytes.Equal(stored, data) {
		t.Fatal("original v2 ZIP bytes changed", err)
	}
	verifyStored(t, pool, i.Baseline, baseline)
	expected := map[string][]Cell{}
	for _, w := range i.Archive.Workbooks {
		var member string
		if err := pool.QueryRow(ctx, `SELECT member_sha256 FROM case_source_workbook WHERE archive_sha256=$1 AND case_number=$2`, i.Archive.SHA256, w.Case).Scan(&member); err != nil || member != w.SHA256 {
			t.Fatal("member provenance changed", err)
		}
		for _, s := range w.Sheets {
			for _, row := range s.Rows {
				expected[fmt.Sprintf("%d/%s/%d", w.Case, s.Name, row.Number)] = row.Cells
			}
		}
	}
	rows, err := pool.Query(ctx, `SELECT case_number,sheet_name,row_number,raw_cells,row_sha256 FROM case_source_locale_raw_row WHERE archive_sha256=$1`, i.Archive.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	var rawCount int
	for rows.Next() {
		var c, row int
		var sheet, digest string
		var raw []byte
		if err := rows.Scan(&c, &sheet, &row, &raw, &digest); err != nil {
			t.Fatal(err)
		}
		var cells []Cell
		if err := json.Unmarshal(raw, &cells); err != nil {
			t.Fatal(err)
		}
		want := expected[fmt.Sprintf("%d/%s/%d", c, sheet, row)]
		encoded, _ := json.Marshal(want)
		if !reflect.DeepEqual(cells, want) || digest != hash(encoded) {
			t.Fatalf("raw values/flags/empties changed at %d/%s/%d", c, sheet, row)
		}
		rawCount++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if rawCount != len(expected) {
		t.Fatal("raw source rows missing")
	}
	for table, want := range map[string]int{"case_source_locale_text": 78106, "case_source_locale_glossary": 2420, "case_source_locale_sheet": 240} {
		var got int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: %d want %d: %v", table, got, want, err)
		}
	}
	localeRows := map[string]LocaleRow{}
	for _, row := range i.Locales {
		localeRows[fmt.Sprintf("%d/%d", row.Case, row.Row)] = row
	}
	rows, err = pool.Query(ctx, `SELECT case_number,row_number,master_row,recipe_id,field,ingredient_ordinal,
	    english_text,english_sha256,bengali_text,raw_quantity,source_note,
	    raw_ingredient_id,raw_ingredient_name,raw_ingredient_grams,grams::text FROM case_source_locale_text`)
	if err != nil {
		t.Fatal(err)
	}
	var paired int
	for rows.Next() {
		var row LocaleRow
		var id, name, rawGrams, grams *string
		if err := rows.Scan(&row.Case, &row.Row, &row.MasterRow, &row.RecipeID, &row.Field, &row.Ordinal,
			&row.English, &row.EnglishSHA256, &row.Bengali, &row.RawQuantity, &row.Note, &id, &name, &rawGrams, &grams); err != nil {
			t.Fatal(err)
		}
		want := localeRows[fmt.Sprintf("%d/%d", row.Case, row.Row)]
		if want.Ingredient != nil {
			if id == nil || name == nil || rawGrams == nil || grams == nil || *id != want.Ingredient.RawID || *name != want.Ingredient.RawName || *rawGrams != want.Ingredient.RawGrams {
				t.Fatal("raw ingredient source association changed", row.RecipeID, row.Ordinal)
			}
			actual, ok := new(big.Rat).SetString(*grams)
			expected, valid := new(big.Rat).SetString(want.Ingredient.Grams)
			if !ok || !valid || actual.Cmp(expected) != 0 {
				t.Fatal("stored decimal value differs", row.RecipeID)
			}
			row.Ingredient = want.Ingredient
		} else if id != nil || name != nil || rawGrams != nil || grams != nil {
			t.Fatal("absent ingredient data was filled", row.RecipeID)
		}
		if !reflect.DeepEqual(row, want) {
			t.Fatal("locale source/value/hash/provenance mismatch", row.RecipeID, row.Field, row.Ordinal)
		}
		paired++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if paired != len(i.Locales) {
		t.Fatal("missing paired locale rows")
	}
	var rawID, rawName, grams, englishHash string
	if err := pool.QueryRow(ctx, `SELECT raw_ingredient_id,raw_ingredient_name,raw_ingredient_grams,english_sha256 FROM case_source_locale_text WHERE recipe_id='MG-WB-C26-001' AND ingredient_ordinal=10`).Scan(&rawID, &rawName, &grams, &englishHash); err != nil || rawID != "WBING0080" || rawName != "Potable water" || grams != " 42" || englishHash != hash([]byte("Potable water")) {
		t.Fatal("C26 source association changed", err)
	}
	snapshot := localeDigest(t, pool)
	again, err := ImportIntake(ctx, pool, data, baseline)
	if err != nil || again.Inserted || !reflect.DeepEqual(snapshot, localeDigest(t, pool)) {
		t.Fatal("repeat import modified immutable staging", err)
	}
	var projected int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM case_source_recipe_revision WHERE archive_sha256=$1`, i.Archive.SHA256).Scan(&projected); err != nil || projected != 0 {
		t.Fatal("staging implicitly activated final source", err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(down)); err == nil || !strings.Contains(err.Error(), "populated immutable") {
		t.Fatal("destructive rollback was not refused", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range localeTables {
		for _, sql := range []string{"UPDATE " + table + " SET archive_sha256=archive_sha256", "DELETE FROM " + table, "TRUNCATE " + table + " CASCADE"} {
			if _, err := pool.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "immutable") {
				t.Fatal("mutable locale staging", table, err)
			}
		}
	}
	var afterPointers string
	if err := pool.QueryRow(ctx, `SELECT md5(string_agg(candidate_id,'' ORDER BY candidate_id)) FROM case_source_recipe_current`).Scan(&afterPointers); err != nil || afterPointers != pointers || !reflect.DeepEqual(before, digestTables(t, pool, false)) {
		t.Fatal("staging changed canonical pointers, provider data or candidate results", err)
	}
	if !reflect.DeepEqual(snapshot, localeDigest(t, pool)) {
		t.Fatal("rejected mutations changed source history")
	}
	t.Logf("INTAKE_STAGING_OK raw_rows=%d locales=78106 glossary=2420 provider_tofu=%d actual_dairy=%d unchanged_current=6000", rawCount, tofu, dairy)
}

func TestIntakeImportConcurrentRevision(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	data, baseline := intakeFixture(t, nil)
	var results [2]IntakeResult
	var errs [2]error
	var wg sync.WaitGroup
	for n := range results {
		wg.Go(func() { results[n], errs[n] = ImportIntake(ctx, pool, data, baseline) })
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || results[0].Inserted == results[1].Inserted {
		t.Fatalf("concurrent import: %v %v", results, errs)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO case_source_locale_text
	    (archive_sha256,case_number,recipe_id,field,ingredient_ordinal,row_number,master_row,
	     english_text,english_sha256,bengali_text,raw_quantity,source_note)
	    SELECT archive_sha256,case_number,'MG-WB-C26-999',field,ingredient_ordinal,row_number,master_row,
	           english_text,english_sha256,bengali_text,raw_quantity,source_note
	    FROM case_source_locale_text WHERE field='title'`); err == nil {
		t.Fatal("locale association with another logical recipe was accepted by the schema")
	}
	corrected, _ := intakeFixture(t, func(f *excelize.File) { setCell(t, f, "For_translation", "F3", "বাংলা সংশোধন") })
	r, err := ImportIntake(ctx, pool, corrected, baseline)
	if err != nil || !r.Inserted || r.Report.ArchiveSHA256 == hash(data) {
		t.Fatal("immutable revision not added", err)
	}
	beforeFailure := localeDigest(t, pool)
	if _, err := pool.Exec(ctx, `ALTER TABLE case_source_locale_text ADD CONSTRAINT intake_failure_fixture CHECK (bengali_text <> 'পরীক্ষা') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	failing, _ := intakeFixture(t, func(f *excelize.File) { setCell(t, f, "For_translation", "F3", "পরীক্ষা") })
	if _, err := ImportIntake(ctx, pool, failing, baseline); err == nil {
		t.Fatal("database failure fixture accepted")
	}
	if !reflect.DeepEqual(beforeFailure, localeDigest(t, pool)) {
		t.Fatal("failed import left partial locale history")
	}
	var partial int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM case_source_archive WHERE archive_sha256=$1`, hash(failing)).Scan(&partial); err != nil || partial != 0 {
		t.Fatal("failed v2 import retained a partial archive", err)
	}
	var histories, active int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT archive_sha256) FROM case_source_locale_text`).Scan(&histories); err != nil || histories != 2 {
		t.Fatal("locale history lost", histories, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM case_source_recipe_current`).Scan(&active); err != nil || active != 0 {
		t.Fatal("staging activated recipes", active, err)
	}
	t.Log("INTAKE_STAGING_OK concurrent duplicate no-op and corrected immutable revision")
}
