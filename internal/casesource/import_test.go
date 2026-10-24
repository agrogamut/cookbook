package casesource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/importer"
	"github.com/xuri/excelize/v2"
)

func isolatedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CASE_SOURCE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CASE_SOURCE_TEST_DATABASE_URL not set; requires disposable local Postgres")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" {
		t.Fatal("case intake tests require loopback Postgres")
	}
	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	name := fmt.Sprintf("case_intake_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	testURL, err := url.Parse(dsn)
	if err != nil || (testURL.Scheme != "postgres" && testURL.Scheme != "postgresql") {
		t.Fatal("case intake tests require a postgres URL")
	}
	// ConnString retains the original string even when ConnConfig.Database changes.
	testURL.Path = "/" + name
	query := testURL.Query()
	query.Del("dbname")
	testURL.RawQuery = query.Encode()
	pool, err := db.Connect(ctx, testURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var actual string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatalf("isolated database mismatch: %q %v", actual, err)
	}
	return pool
}

func digestTables(t *testing.T, pool *pgxpool.Pool, staging bool) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(name, "case_source_") == staging {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !staging {
		tables = append(tables, "engine_candidate", "engine_candidate_ingredient")
	}
	result := map[string]string{}
	for _, name := range tables {
		var value string
		if err := pool.QueryRow(ctx, `SELECT count(*)::text || ':' || md5(coalesce(string_agg(row_to_json(t)::text, E'\n' ORDER BY row_to_json(t)::text), '')) FROM `+pgx.Identifier{name}.Sanitize()+` t`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		result[name] = value
	}
	return result
}

func TestImport(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	if _, err := importer.Run(ctx, pool, "../../data/provider"); err != nil {
		t.Fatal(err)
	}
	before := digestTables(t, pool, false)
	if strings.HasPrefix(before["engine_candidate"], "0:") {
		t.Fatal("candidate isolation check needs populated positive control")
	}
	b := delivery(t)
	r, err := Import(ctx, pool, b)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Inserted || r.Workbooks != 60 || r.Recipes != 6000 || r.Audits != 6000 || r.Occurrences != 60106 {
		t.Fatalf("unexpected intake: %+v", r)
	}
	a, err := Parse(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	verifyStored(t, pool, a, b)
	stored := digestTables(t, pool, true)
	again, err := Import(ctx, pool, b)
	if err != nil || again.Inserted || again.ArchiveSHA256 != r.ArchiveSHA256 {
		t.Fatalf("repeat intake: %+v %v", again, err)
	}
	if !reflect.DeepEqual(stored, digestTables(t, pool, true)) {
		t.Fatal("unchanged import changed staging")
	}

	files, err := packageFiles(b, 128)
	if err != nil {
		t.Fatal(err)
	}
	var corrected []byte
	for _, w := range a.Workbooks {
		if w.Case != 26 {
			continue
		}
		member, err := readMember(files[w.Name])
		if err != nil {
			t.Fatal(err)
		}
		f, err := excelize.OpenReader(bytes.NewReader(member))
		if err != nil {
			t.Fatal(err)
		}
		for _, sheet := range []string{"Recipe_Master", "Uniqueness_Audit"} {
			setCell(t, f, sheet, "B5", "correction fixture")
		}
		buf, err := f.WriteToBuffer()
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		corrected = zipBytes(t, zipEntry{name: w.Name, data: buf.Bytes()})
	}
	correction, err := Import(ctx, pool, corrected)
	if err != nil || !correction.Inserted || correction.Recipes != 100 || correction.ArchiveSHA256 == r.ArchiveSHA256 {
		t.Fatalf("correction: %+v %v", correction, err)
	}
	verifyStored(t, pool, a, b)
	var revisions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM case_source_row WHERE case_number=26 AND sheet_name='Recipe_Master' AND recipe_id='MG-WB-C26-001'`).Scan(&revisions); err != nil || revisions != 2 {
		t.Fatalf("revision history: %d %v", revisions, err)
	}
	if !reflect.DeepEqual(before, digestTables(t, pool, false)) {
		t.Fatal("intake changed existing corpus or candidates")
	}
	for _, table := range []string{"case_source_archive", "case_source_workbook", "case_source_sheet", "case_source_row", "case_source_ingredient"} {
		for _, sql := range []string{"UPDATE " + table + " SET archive_sha256=archive_sha256", "DELETE FROM " + table, "TRUNCATE " + table + " CASCADE"} {
			if _, err := pool.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "immutable") {
				t.Fatalf("mutation was not rejected by immutability guard: %s: %v", sql, err)
			}
		}
	}
	fixture := fixtureArchive(t, nil)
	beforeFailure := digestTables(t, pool, true)
	if _, err := pool.Exec(ctx, `ALTER TABLE case_source_ingredient ADD CONSTRAINT failure_fixture CHECK (grams <> 0.125) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, pool, fixture); err == nil {
		t.Fatal("database failure fixture did not reject intake")
	}
	if !reflect.DeepEqual(beforeFailure, digestTables(t, pool, true)) {
		t.Fatal("failed intake left a partial snapshot")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE case_source_ingredient DROP CONSTRAINT failure_fixture`); err != nil {
		t.Fatal(err)
	}
	var results [2]Result
	var errors [2]error
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() { results[i], errors[i] = Import(ctx, pool, fixture) })
	}
	wg.Wait()
	if errors[0] != nil || errors[1] != nil || results[0].Inserted == results[1].Inserted {
		t.Fatalf("concurrent duplicate intake: %+v %v", results, errors)
	}
	if !reflect.DeepEqual(before, digestTables(t, pool, false)) {
		t.Fatal("fixture intake changed old tables")
	}
	staged := digestTables(t, pool, true)
	if _, err := importer.Run(ctx, pool, "../../data/provider"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(staged, digestTables(t, pool, true)) {
		t.Fatal("provider importer changed case source staging")
	}
	beforeRollback := digestTables(t, pool, false)
	for _, migration := range []string{"0046_case_source_activation.down", "0045_case_source_staging.down", "0045_case_source_staging.up", "0046_case_source_activation.up"} {
		sql, err := os.ReadFile("../db/migrations/" + migration + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("migration %s: %v", migration, err)
		}
	}
	if !reflect.DeepEqual(beforeRollback, digestTables(t, pool, false)) {
		t.Fatal("staging migration rollback changed old tables")
	}
	t.Logf("STAGING_DATABASE_OK original=%+v unchanged_tables_and_views=%d candidates=%s revisions=%d", r, len(before), before["engine_candidate"], revisions)
}

func verifyStored(t *testing.T, pool *pgxpool.Pool, a *Archive, original []byte) {
	t.Helper()
	ctx := context.Background()
	var stored []byte
	var source, parser string
	if err := pool.QueryRow(ctx, `SELECT archive_bytes, source_key, parser_version FROM case_source_archive WHERE archive_sha256=$1`, a.SHA256).Scan(&stored, &source, &parser); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, original) || source != SourceKey || parser != ParserVersion {
		t.Fatal("stored archive or namespace differs")
	}
	for _, w := range a.Workbooks {
		var name, memberHash string
		if err := pool.QueryRow(ctx, `SELECT member_name, member_sha256 FROM case_source_workbook WHERE archive_sha256=$1 AND case_number=$2`, a.SHA256, w.Case).Scan(&name, &memberHash); err != nil || name != w.Name || memberHash != w.SHA256 {
			t.Fatalf("stored member: %s %s %v", name, memberHash, err)
		}
		for _, s := range w.Sheets {
			var headers []byte
			var part string
			var headerRow int
			if err := pool.QueryRow(ctx, `SELECT raw_headers, xml_part, header_row FROM case_source_sheet WHERE archive_sha256=$1 AND case_number=$2 AND sheet_name=$3`, a.SHA256, w.Case, s.Name).Scan(&headers, &part, &headerRow); err != nil {
				t.Fatal(err)
			}
			var h []string
			if err := json.Unmarshal(headers, &h); err != nil || !reflect.DeepEqual(h, s.Headers) || part != s.Part || headerRow != 4 {
				t.Fatal("stored sheet metadata differs", err)
			}
			rows, err := pool.Query(ctx, `SELECT row_number, coalesce(recipe_id,''), raw_cells, row_sha256 FROM case_source_row WHERE archive_sha256=$1 AND case_number=$2 AND sheet_name=$3 ORDER BY row_number`, a.SHA256, w.Case, s.Name)
			if err != nil {
				t.Fatal(err)
			}
			i := 0
			for rows.Next() {
				var n int
				var id, digest string
				var raw []byte
				if err := rows.Scan(&n, &id, &raw, &digest); err != nil {
					t.Fatal(err)
				}
				var cells []Cell
				if err := json.Unmarshal(raw, &cells); err != nil {
					t.Fatal(err)
				}
				if i >= len(s.Rows) {
					t.Fatal("extra stored row")
				}
				expected := s.Rows[i]
				encoded, err := json.Marshal(expected.Cells)
				if err != nil {
					t.Fatal(err)
				}
				if n != expected.Number || id != expected.RecipeID || !reflect.DeepEqual(cells, expected.Cells) || digest != hash(encoded) {
					t.Fatalf("stored cells differ at case %d sheet %s row %d", w.Case, s.Name, n)
				}
				i++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if i != len(s.Rows) {
				t.Fatal("missing stored rows")
			}
		}
		rows, err := pool.Query(ctx, `SELECT row_number, ordinal, raw_id, raw_name, raw_grams, grams::text FROM case_source_ingredient WHERE archive_sha256=$1 AND case_number=$2 ORDER BY row_number, ordinal`, a.SHA256, w.Case)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range w.Sheets {
			for _, r := range s.Rows {
				for _, i := range r.Ingredients {
					if !rows.Next() {
						t.Fatal("missing ingredient occurrence", rows.Err())
					}
					var n, ordinal int
					var id, name, raw, grams string
					if err := rows.Scan(&n, &ordinal, &id, &name, &raw, &grams); err != nil {
						t.Fatal(err)
					}
					g, ok := new(big.Rat).SetString(grams)
					want, valid := new(big.Rat).SetString(i.Grams)
					if n != r.Number || ordinal != i.Ordinal || id != i.RawID || name != i.RawName || raw != i.RawGrams || !ok || !valid || g.Cmp(want) != 0 {
						t.Fatalf("stored ingredient differs: case %d row %d ordinal %d", w.Case, n, ordinal)
					}
				}
			}
		}
		if rows.Next() || rows.Err() != nil {
			t.Fatal("extra ingredient occurrence or query failure", rows.Err())
		}
		rows.Close()
	}
}
