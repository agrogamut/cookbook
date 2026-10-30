package casesource

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/importer"
)

func TestFinalSourceActivation(t *testing.T) {
	ctx := context.Background()
	pool := isolatedPool(t)
	if _, err := importer.Run(ctx, pool, "../../data/provider"); err != nil {
		t.Fatal(err)
	}
	baseline, data := delivery(t), bengaliDelivery(t)
	if _, err := Import(ctx, pool, baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := Activate(ctx, pool, deliveryHash); err != nil {
		t.Fatal(err)
	}
	var providerBefore, originalBefore string
	fingerprint := func(query string) string {
		t.Helper()
		var s string
		if err := pool.QueryRow(ctx, query).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	providerSQL := `SELECT md5(string_agg(row_to_json(r)::text,'' ORDER BY recipe_id)) FROM recipe_master r`
	originalSQL := `SELECT md5(string_agg(row_to_json(r)::text,'' ORDER BY candidate_id)) FROM case_source_recipe_revision r WHERE archive_sha256='` + deliveryHash + `'`
	providerBefore, originalBefore = fingerprint(providerSQL), fingerprint(originalSQL)
	down, err := os.ReadFile("../db/migrations/0049_final_case_source.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../db/migrations/0049_final_case_source.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	i := finalDelivery(t)
	staged, err := ImportIntake(ctx, pool, data, baseline)
	if err != nil || !staged.Inserted {
		t.Fatal("stage", err)
	}
	before := localeDigest(t, pool)
	result, err := Activate(ctx, pool, FinalArchiveSHA256)
	if err != nil || result.RevisionsAdded != 6000 || result.CurrentChanged != 6000 {
		t.Fatalf("activation %+v %v", result, err)
	}
	var counts [4]int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM case_source_recipe_current),(SELECT count(*) FROM case_source_recipe_revision),(SELECT count(*) FROM case_source_recipe_policy),(SELECT count(*) FROM case_source_recipe_ingredient WHERE archive_sha256=$1)`, FinalArchiveSHA256).Scan(&counts[0], &counts[1], &counts[2], &counts[3]); err != nil || counts != [4]int{6000, 12000, 6000, 60106} {
		t.Fatalf("projection %v %v", counts, err)
	}
	expectedLocales := map[string][]LocaleRow{}
	for _, l := range i.Locales {
		expectedLocales[l.RecipeID] = append(expectedLocales[l.RecipeID], l)
	}
	var fieldsCount, localeCount, occurrenceCount int
	for _, w := range i.Archive.Workbooks {
		s := sheetByName(w, "Recipe_Master")
		ids := []string{}
		expected := map[string]Row{}
		for _, row := range s.Rows {
			if row.RecipeID != "" {
				id := CandidateID(FinalArchiveSHA256, row.RecipeID)
				ids = append(ids, id)
				expected[id] = row
			}
		}
		recipes, err := Recipes(ctx, pool, ids)
		if err != nil || len(recipes) != 100 {
			t.Fatal("recipes", w.Case, len(recipes), err)
		}
		for _, r := range recipes {
			row := expected[r.CandidateID]
			if !reflect.DeepEqual(r.Fields, Fields(s, row)) || len(r.Fields) != 38 || r.Policy == nil || r.Citation.MemberSHA256 != w.SHA256 {
				t.Fatal("final fields or policy differ", r.Citation)
			}
			fieldsCount++
			locale, err := Bengali(ctx, pool, r)
			if err != nil || locale == nil {
				t.Fatal("locale", r.Citation, err)
			}
			if err := locale.Validate(r); err != nil {
				t.Fatal("derived locale validation", r.Citation, err)
			}
			want := expectedLocales[r.Citation.RecipeID]
			if len(locale.Texts) != len(want) {
				t.Fatal("locale count")
			}
			for j, v := range locale.Texts {
				x := want[j]
				if v.Field != x.Field || v.Ordinal != x.Ordinal || v.English != x.English || v.EnglishSHA256 != x.EnglishSHA256 || v.Text != x.Bengali || v.Row != x.Row || v.Quantity != x.RawQuantity {
					t.Fatal("locale pair differs", r.Citation.RecipeID, j)
				}
				localeCount++
			}
			occ, err := Ingredients(ctx, pool, r.CandidateID)
			if err != nil || len(occ) != len(row.Ingredients) {
				t.Fatal("occurrences", err)
			}
			for j, v := range occ {
				x := row.Ingredients[j]
				if v.Ordinal != x.Ordinal || v.RawID != x.RawID || v.RawName != x.RawName || v.RawGrams != x.RawGrams || v.Grams != x.Grams || v.MeaningID != MeaningID(x.RawID, x.RawName) {
					t.Fatal("raw occurrence changed", r.Citation.RecipeID, j)
				}
				occurrenceCount++
			}
		}
	}
	if fieldsCount != 6000 || localeCount != 78106 || occurrenceCount != 60106 {
		t.Fatal(fieldsCount, localeCount, occurrenceCount)
	}
	if !reflect.DeepEqual(before, localeDigest(t, pool)) || providerBefore != fingerprint(providerSQL) || originalBefore != fingerprint(originalSQL) {
		t.Fatal("activation overwrote source history/provider rows")
	}
	if result, err := ImportIntake(ctx, pool, data, baseline); err != nil || result.Inserted {
		t.Fatal("repeat import", err)
	}
	if result, err := Activate(ctx, pool, FinalArchiveSHA256); err != nil || result.RevisionsAdded != 0 || result.CurrentChanged != 0 {
		t.Fatal("repeat activation", result, err)
	}
	for _, sql := range []string{`UPDATE case_source_recipe_policy SET policy=policy`, `DELETE FROM case_source_recipe_policy`, `TRUNCATE case_source_recipe_policy`, string(down)} {
		if _, err := pool.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatal("policy history mutation accepted", err)
		}
	}
	pointerSQL := `SELECT md5(string_agg(row_to_json(r)::text,'' ORDER BY case_number,recipe_id)) FROM case_source_recipe_current r`
	policySQL := `SELECT md5(string_agg(row_to_json(r)::text,'' ORDER BY candidate_id)) FROM case_source_recipe_policy r`
	pointers, policies := fingerprint(pointerSQL), fingerprint(policySQL)
	nonAuthoritative, err := Import(ctx, pool, fixtureArchive(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, sha := range []string{deliveryHash, nonAuthoritative.ArchiveSHA256} {
		if result, err := Activate(ctx, pool, sha); err == nil || result.RevisionsAdded != 0 || result.CurrentChanged != 0 {
			t.Fatal("historical activation replaced authoritative source", result, err)
		}
	}
	type activationResult struct {
		sha   string
		value Activation
		err   error
	}
	start := make(chan struct{})
	results := make(chan activationResult, 2)
	for _, sha := range []string{deliveryHash, FinalArchiveSHA256} {
		go func(sha string) {
			<-start
			value, err := Activate(ctx, pool, sha)
			results <- activationResult{sha, value, err}
		}(sha)
	}
	close(start)
	for range 2 {
		x := <-results
		if (x.err == nil) != (x.sha == FinalArchiveSHA256) || x.value.RevisionsAdded != 0 || x.value.CurrentChanged != 0 {
			t.Errorf("competing activation bypassed precedence: %+v %v", x.value, x.err)
		}
	}
	if fingerprint(pointerSQL) != pointers || fingerprint(policySQL) != policies || !reflect.DeepEqual(before, localeDigest(t, pool)) {
		t.Fatal("refused activation changed current pointers, policies or locale history")
	}
	for _, sha := range []string{deliveryHash, FinalArchiveSHA256} {
		recipes, err := Recipes(ctx, pool, []string{CandidateID(sha, "MG-WB-C26-001")})
		if err != nil || len(recipes) != 1 {
			t.Fatal("source history unreadable", err)
		}
		locale, err := Bengali(ctx, pool, recipes[0])
		if err != nil || (locale != nil) != (sha == FinalArchiveSHA256) {
			t.Fatal("locale active revision differs", err)
		}
	}
	unknown, base := intakeFixture(t, nil)
	other, err := ImportIntake(ctx, pool, unknown, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Activate(ctx, pool, other.Report.ArchiveSHA256); !errors.Is(err, ErrLocaleActivation) {
		t.Fatal("unknown v2 activated", err)
	}
	if providerBefore != fingerprint(providerSQL) || originalBefore != fingerprint(originalSQL) {
		t.Fatal("history changed after refused activation")
	}
	t.Logf("FINAL_ACTIVATION_OK fields=%d locale=%d occurrences=%d revisions=12000 current=6000 policies=6000 provider_digest=%s original_digest=%s", fieldsCount, localeCount, occurrenceCount, providerBefore, originalBefore)
}
