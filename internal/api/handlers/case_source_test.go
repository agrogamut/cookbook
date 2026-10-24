package handlers

import (
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
	id := casesource.CandidateID("3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2", "MG-WB-C26-001")
	request := httptest.NewRequest("GET", "/api/recipes/"+id, nil)
	params := chi.NewRouteContext()
	params.URLParams.Add("recipeID", id)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, params))
	response = httptest.NewRecorder()
	h.RecipeDetail(response, request)
	if response.Code != 200 {
		t.Fatalf("detail: %d %s", response.Code, response.Body.String())
	}
	var detail struct {
		Nutrition any               `json:"nutrition"`
		Gap       string            `json:"nutrition_gap"`
		Source    casesource.Recipe `json:"case_source"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Nutrition != nil || detail.Gap == "" || len(detail.Source.Fields) != 37 || len(detail.Source.Ingredients) != 10 {
		t.Fatalf("detail fidelity: %+v", detail)
	}
	first, last := detail.Source.Ingredients[7], detail.Source.Ingredients[9]
	if first.RawID != "WBING0080" || last.RawID != "WBING0080" || first.Ordinal != 8 || last.Ordinal != 10 || first.MeaningID == last.MeaningID || first.Grams != "0.1" || last.Grams != "42" {
		t.Fatalf("API collision: %+v %+v", first, last)
	}
	t.Logf("SOURCE_API_OK search_source_rows=%d unknown_scores=null case26_ordinals=8,10", found)
}
