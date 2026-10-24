package book

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/importer"
	"github.com/madamgy/recipie/internal/models"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func sourceBookPool(t *testing.T) *pgxpool.Pool {
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
	name := fmt.Sprintf("case_book_%d_%d", os.Getpid(), time.Now().UnixNano())
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
	if _, err := importer.Run(ctx, pool, "../../data/provider"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip")
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

func TestSourceBookSelection(t *testing.T) {
	pool := sourceBookPool(t)
	ctx := context.Background()
	var id string
	if err := pool.QueryRow(ctx, `SELECT candidate_id FROM case_source_active_recipe WHERE case_number=30
	    AND (fields->>'Min_Age_Months')::int<=8 AND (fields->>'Max_Age_Months')::int>=8
	    AND serving_grams<>trunc(serving_grams) ORDER BY recipe_id LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	blocked := casesource.CandidateID("3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2", "MG-WB-C26-001")
	cards, gaps, err := loadSourceCards(ctx, pool, []string{id, blocked}, "MC-01", models.ChildProfile{AgeMonths: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || len(gaps) == 0 {
		t.Fatalf("direct source loading: cards=%d gaps=%v", len(cards), gaps)
	}
	card := cards[id]
	if card.SourceRevision == nil || card.SourceRevision.RecipeID != card.RecipeID || card.Nutrition != nil || !strings.Contains(card.Serving, ".") || len(card.SourceFields) != 37 {
		t.Fatalf("source card fidelity: %+v", card)
	}
	for i, ingredient := range card.Ingredients {
		if ingredient.SourceOrdinal != i+1 || ingredient.SourceID == "" || ingredient.SourceMeaningID == "" {
			t.Fatalf("occurrence identity lost: %+v", ingredient)
		}
	}
	if _, ok := cards[blocked]; ok {
		t.Fatal("required familiarity gap bypassed")
	}
	asOf := time.Now()
	set, err := AssembleSet(ctx, pool, storedProfileAged(8), asOf)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	sourceCount := 0
	for _, section := range set.Book2.MealSections {
		if len(section.Recipes) > maxRecipesPerSection {
			t.Fatal("category count exceeded")
		}
		for _, recipe := range section.Recipes {
			seen[recipe.RecipeID] = true
			if recipe.Source == casesource.CandidateSource {
				sourceCount++
				if recipe.SourceRevision.Case == 26 {
					t.Fatal("held recipe reintroduced")
				}
			}
		}
	}
	if sourceCount == 0 || len(set.Book2.MealSections) <= 3 {
		t.Fatalf("source meal expansion missing: sections=%d source_cards=%d", len(set.Book2.MealSections), sourceCount)
	}
	for _, section := range set.Book1.Sections {
		for _, index := range section.RecipeLinkIndex {
			for _, recipeID := range index.RecipeIDs {
				if !seen[recipeID] {
					t.Fatalf("Book1 link absent from Book2: %s", recipeID)
				}
			}
		}
	}
	got, note := topUpInvented(ctx, pool, aidraft.Disabled, models.ChildProfile{AgeMonths: 36}, mealCategory{ID: "CS:Snack", Name: "Snack", Target: 10, SourceOnly: true}, "fixture", models.EngineResult{}, nil)
	if len(got) != 0 || !strings.Contains(note, "cannot be bypassed") {
		t.Fatalf("source fallback: %d %s", len(got), note)
	}
	t.Logf("SOURCE_BOOK_OK sections=%d source_cards=%d original_id=%s serving=%s", len(set.Book2.MealSections), sourceCount, card.RecipeID, card.Serving)
}
