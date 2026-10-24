package casesource

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/madamgy/recipie/internal/importer"
	"github.com/xuri/excelize/v2"
)

func TestActivationDelivery(t *testing.T) {
	pool := isolatedPool(t)
	ctx := context.Background()
	if _, err := importer.Run(ctx, pool, "../../data/provider"); err != nil {
		t.Fatal(err)
	}
	before := digestTables(t, pool, false)
	b := delivery(t)
	staged, err := Import(ctx, pool, b)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Activate(ctx, pool, staged.ArchiveSHA256)
	if err != nil || first.RevisionsAdded != 6000 || first.CurrentChanged != 6000 {
		t.Fatalf("activation: %+v %v", first, err)
	}
	var count, cases int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT case_number) FROM case_source_active_recipe`).Scan(&count, &cases); err != nil || count != 6000 || cases != 60 {
		t.Fatalf("collection: %d %d %v", count, cases, err)
	}
	var old string
	if err := pool.QueryRow(ctx, `SELECT count(*)::text || ':' || md5(coalesce(string_agg(row_to_json(t)::text, E'\n' ORDER BY row_to_json(t)::text),'')) FROM engine_candidate_existing t`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if old != before["engine_candidate"] {
		t.Fatalf("old candidates changed: %s != %s", old, before["engine_candidate"])
	}
	archive, err := Parse(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range archive.Workbooks {
		for _, s := range w.Sheets {
			if s.Name != "Recipe_Master" {
				continue
			}
			ids := []string{}
			expected := map[string]map[string]string{}
			for _, r := range s.Rows {
				if r.RecipeID == "" {
					continue
				}
				id := CandidateID(archive.SHA256, r.RecipeID)
				ids = append(ids, id)
				expected[id] = Fields(s, r)
			}
			actual, err := Recipes(ctx, pool, ids)
			if err != nil || len(actual) != len(ids) {
				t.Fatalf("case %d: %d %v", w.Case, len(actual), err)
			}
			for _, r := range actual {
				if !reflect.DeepEqual(r.Fields, expected[r.CandidateID]) || r.ServingGrams != r.Fields["Serving_Size_g"] || r.Citation.Workbook != w.Name || r.Citation.MemberSHA256 != w.SHA256 {
					t.Fatalf("source fidelity: %+v", r.Citation)
				}
			}
		}
	}
	occ, err := Ingredients(ctx, pool, CandidateID(archive.SHA256, "MG-WB-C26-001"))
	if err != nil || len(occ) != 10 {
		t.Fatalf("occurrences: %d %v", len(occ), err)
	}
	if occ[7].Ordinal != 8 || occ[7].RawID != "WBING0080" || occ[7].RawName != "Cumin" || occ[7].Grams != "0.1" || occ[9].RawID != "WBING0080" || occ[9].RawName != "Potable water" || occ[9].Grams != "42" || occ[7].MeaningID == occ[9].MeaningID {
		t.Fatalf("case26 collision: %+v", occ)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM case_source_ingredient_meaning WHERE source_id='WBING0070'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("mustard/maize meanings: %d %v", count, err)
	}
	again, err := Activate(ctx, pool, archive.SHA256)
	if err != nil || again.RevisionsAdded != 0 || again.CurrentChanged != 0 {
		t.Fatalf("repeat activation: %+v %v", again, err)
	}
	files, err := packageFiles(b, 128)
	if err != nil {
		t.Fatal(err)
	}
	var correction []byte
	for _, w := range archive.Workbooks {
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
			setCell(t, f, sheet, "B5", "corrected title fixture")
		}
		buffer, err := f.WriteToBuffer()
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		correction = zipBytes(t, zipEntry{name: w.Name, data: buffer.Bytes()})
	}
	r, err := Import(ctx, pool, correction)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Activate(ctx, pool, r.ArchiveSHA256)
	if err != nil || updated.RevisionsAdded != 100 || updated.CurrentChanged != 100 {
		t.Fatalf("corrected activation: %+v %v", updated, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM case_source_active_recipe`).Scan(&count); err != nil || count != 6000 {
		t.Fatalf("correction deleted collection rows: %d %v", count, err)
	}
	revisions, err := Recipes(ctx, pool, []string{CandidateID(archive.SHA256, "MG-WB-C26-001"), CandidateID(r.ArchiveSHA256, "MG-WB-C26-001")})
	if err != nil || len(revisions) != 2 {
		t.Fatalf("history lost: %d %v", len(revisions), err)
	}
	snapshot := digestTables(t, pool, true)
	if _, err := importer.Run(ctx, pool, "../../data/provider"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, digestTables(t, pool, true)) {
		t.Fatal("provider importer changed case source rows")
	}
	current, err := Parse(ctx, correction)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("analyzed ingredient lookup", func(t *testing.T) {
		assertIngredientLookup(t, pool, current, archive.SHA256)
	})
	for _, sql := range []string{`UPDATE case_source_recipe_revision SET fields=fields`, `DELETE FROM case_source_ingredient_meaning`} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatal("immutable projection accepted mutation")
		}
	}
	t.Log("ACTIVATION_OK recipes=6000 cases=60 corrected_revisions=100 retained_current=6000 case26_ordinals=8,10")
}
