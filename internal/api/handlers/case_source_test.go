package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/importer"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func sourceAPIPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CASE_SOURCE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CASE_SOURCE_TEST_DATABASE_URL requires disposable local PostgreSQL")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("source tests require loopback database URL")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	name := fmt.Sprintf("case_api_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	u.Path = "/" + name
	pool, err := db.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := importer.Run(ctx, pool, "../../../data/provider"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip")
	if err != nil {
		t.Fatal(err)
	}
	r, err := casesource.Import(ctx, pool, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := casesource.Activate(ctx, pool, r.ArchiveSHA256); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestCaseSourceAPI(t *testing.T) {
	pool := sourceAPIPool(t)
	h := New(pool, aidraft.Disabled)
	response := httptest.NewRecorder()
	h.Search(response, httptest.NewRequest("POST", "/api/search", strings.NewReader(`{"age_months":8,"limit":10000}`)))
	if response.Code != 200 {
		t.Fatalf("search: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		Recipes []struct {
			RecipeID  string               `json:"recipe_id"`
			Source    string               `json:"source"`
			Original  string               `json:"source_recipe_id"`
			Citation  *casesource.Citation `json:"source_revision"`
			Nutrition *float64             `json:"nutrition_score"`
			Ranked    *float64             `json:"ranked_score"`
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, row := range result.Recipes {
		if row.Source != casesource.CandidateSource {
			continue
		}
		found++
		if row.Nutrition != nil || row.Ranked != nil || row.Citation == nil || row.Original != row.Citation.RecipeID || row.Citation.Row < 5 || len(row.Citation.MemberSHA256) != 64 {
			t.Fatalf("source result provenance/scores: %+v", row)
		}
	}
	if found == 0 {
		t.Fatal("no source results")
	}
	router := chi.NewRouter()
	router.Get("/api/recipes/{recipeID}", h.RecipeDetail)
	detailRequest := func(id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/recipes/"+id, nil))
		return rec
	}
	for _, original := range []string{"MG-WB-C01-001", "MG-WB-C26-001"} {
		t.Run("detail_"+original, func(t *testing.T) {
			id := casesource.CandidateID("3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2", original)
			literal := detailRequest(id)
			if literal.Code != 200 {
				t.Fatalf("literal detail: %d %s", literal.Code, literal.Body.String())
			}
			for _, tc := range []struct{ name, path string }{
				{"encoded_upper", strings.ReplaceAll(id, ":", "%3A")},
				{"encoded_lower", strings.ReplaceAll(id, ":", "%3a")},
				{"mixed", strings.Replace(id, ":", "%3A", 1)},
				{"encoded_letter", "%63" + strings.ReplaceAll(id[1:], ":", "%3A")},
			} {
				t.Run(tc.name, func(t *testing.T) {
					encoded := detailRequest(tc.path)
					if encoded.Code != 200 || !bytes.Equal(encoded.Body.Bytes(), literal.Body.Bytes()) {
						t.Fatalf("encoded detail differs from literal: status=%d body=%s", encoded.Code, encoded.Body.String())
					}
				})
			}
			for _, tc := range []struct{ name, path string }{
				{"double_encoded", strings.ReplaceAll(id, ":", "%253A")},
				{"double_encoded_raw_path", "%63" + strings.ReplaceAll(id[1:], ":", "%253A")},
				{"unknown", strings.ReplaceAll(id+"-missing", ":", "%3A")},
			} {
				t.Run(tc.name, func(t *testing.T) {
					missing := detailRequest(tc.path)
					if missing.Code != 404 {
						t.Fatalf("different identity resolved: status=%d body=%s", missing.Code, missing.Body.String())
					}
				})
			}
		})
	}
	id := casesource.CandidateID("3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2", "MG-WB-C26-001")
	response = detailRequest(strings.ReplaceAll(id, ":", "%3A"))
	var detail struct {
		Nutrition any               `json:"nutrition"`
		Gap       string            `json:"nutrition_gap"`
		Source    casesource.Recipe `json:"case_source"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Nutrition != nil || detail.Gap == "" || detail.Source.Citation.RecipeID != "MG-WB-C26-001" || len(detail.Source.Fields) != 37 || len(detail.Source.Ingredients) != 10 {
		t.Fatalf("detail fidelity: %+v", detail)
	}
	first, last := detail.Source.Ingredients[7], detail.Source.Ingredients[9]
	if first.RawID != "WBING0080" || last.RawID != "WBING0080" || first.Ordinal != 8 || last.Ordinal != 10 || first.MeaningID == last.MeaningID || first.Grams != "0.1" || last.Grams != "42" {
		t.Fatalf("API collision: %+v %+v", first, last)
	}
	t.Logf("SOURCE_API_OK search_source_rows=%d unknown_scores=null case26_ordinals=8,10", found)
}

func TestFinalSourceAPI(t *testing.T) {
	pool := sourceAPIPool(t)
	ctx := context.Background()
	baseline, err := os.ReadFile("../../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../../data/case-source/" + casesource.FinalArchiveSHA256 + ".zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := casesource.ImportIntake(ctx, pool, data, baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := casesource.Activate(ctx, pool, casesource.FinalArchiveSHA256); err != nil {
		t.Fatal(err)
	}
	h := New(pool, aidraft.Disabled)
	router := chi.NewRouter()
	router.Get("/api/recipes/{recipeID}", h.RecipeDetail)
	for _, recipe := range []string{"MG-WB-C01-001", "MG-WB-C26-001", "MG-WB-C13-012", "MG-WB-C39-027"} {
		id := casesource.CandidateID(casesource.FinalArchiveSHA256, recipe)
		for _, language := range []string{"en", "bn", "hi"} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/recipes/"+url.PathEscape(id)+"?language="+language, nil))
			if rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			var out struct {
				Method  map[string]string  `json:"method"`
				Source  casesource.Recipe  `json:"case_source"`
				Locale  *casesource.Locale `json:"source_locale"`
				Numbers map[string]*string `json:"source_nutrition"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Source.Fields) != 38 || out.Source.Policy == nil || out.Locale == nil {
				t.Fatal("final provenance missing")
			}
			if err := out.Locale.Validate(out.Source); err != nil {
				t.Fatal(err)
			}
			if language == "bn" {
				for _, v := range out.Locale.Texts {
					if v.Field == "title" && out.Method["recipe_name"] != v.Text || v.Field == "method" && out.Method["provider_method"] != v.Presented() {
						t.Fatal("supplied Bengali not preferred")
					}
				}
			} else if out.Method["recipe_name"] != out.Source.Fields["Recipe_Name"] || out.Method["provider_method"] != out.Source.PresentationText("Preparation_Method_FULL", "en", out.Source.Fields["Preparation_Method_FULL"]) {
				t.Fatal("English/Hindi fallback source changed")
			}
			for field, value := range out.Numbers {
				raw := out.Source.Fields[field]
				if raw == "" {
					if value != nil {
						t.Fatal("null nutrient became a number")
					}
				} else if value == nil || *value != raw {
					t.Fatal("source numeric token changed", field)
				}
			}
			if recipe == "MG-WB-C26-001" {
				occ := out.Source.Ingredients
				if len(occ) != 10 || occ[7].Ordinal != 8 || occ[7].RawID != "ING0093" || occ[7].Grams != "0.1" || occ[9].Ordinal != 10 || occ[9].RawID != "WBING0080" || occ[9].RawGrams != " 42" {
					t.Fatal("C26 occurrence fidelity", occ)
				}
			}
		}
	}
	t.Log("FINAL_API_OK encoded_identity=true fields=38 supplied_Bengali=true Hindi_fallback=English nullable_source_values=true")
}
