package engine

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/importer"
	"github.com/madamgy/recipie/internal/models"
)

func casePool(t *testing.T) *pgxpool.Pool {
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
	name := fmt.Sprintf("case_engine_%d_%d", os.Getpid(), time.Now().UnixNano())
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

func TestCaseSourcePipeline(t *testing.T) {
	pool := casePool(t)
	ctx := context.Background()
	ids, _, err := ageStep(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	var original int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM engine_candidate_existing`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if len(ids) != original+6000 {
		t.Fatalf("additive candidates=%d original=%d", len(ids), original)
	}
	for _, test := range []struct {
		name           string
		p              models.ChildProfile
		sourceExpected bool
	}{
		{"routine infant", models.ChildProfile{AgeMonths: 8, Limit: 10000}, true},
		{"intersecting milk fish mustard", models.ChildProfile{AgeMonths: 8, Allergens: []string{"Milk", "Fish", "Mustard"}, Limit: 10000}, true},
		{"vegetarian", models.ChildProfile{AgeMonths: 36, DietType: "Vegetarian", Limit: 10000}, true},
		{"below source age", models.ChildProfile{AgeMonths: 5, Limit: 10000}, false},
		{"vegan source facts missing", models.ChildProfile{AgeMonths: 36, DietType: "Vegetarian", Vegan: true, Limit: 10000}, false},
		{"clinical crosswalk missing", models.ChildProfile{AgeMonths: 36, ClinicalFlags: map[string]string{"Coeliac_Status": "Confirmed"}, Limit: 10000}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			res, err := Run(ctx, pool, test.p)
			if err != nil {
				t.Fatal(err)
			}
			var sourceIDs []string
			for _, r := range res.Recipes {
				if r.Source == casesource.CandidateSource {
					sourceIDs = append(sourceIDs, r.RecipeID)
					if r.ValueKind != "unavailable" || r.ScoredAxes != "" {
						t.Fatal("source estimates treated as nutrition score")
					}
				}
			}
			if (len(sourceIDs) > 0) != test.sourceExpected {
				t.Fatalf("source candidates=%d expected presence=%t", len(sourceIDs), test.sourceExpected)
			}
			rows, err := casesource.Recipes(ctx, pool, sourceIDs)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				min, _ := strconv.Atoi(r.Fields["Min_Age_Months"])
				max, _ := strconv.Atoi(r.Fields["Max_Age_Months"])
				if test.p.AgeMonths < min || test.p.AgeMonths > max {
					t.Fatalf("source age bypass: %s", r.Citation.RecipeID)
				}
				if r.Citation.Case == 21 || r.Citation.Case == 26 || r.Citation.Case >= 40 {
					t.Fatalf("required source hold bypassed: %s", r.Citation.RecipeID)
				}
			}
			if test.name == "intersecting milk fish mustard" {
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM case_source_recipe_revision WHERE candidate_id=ANY($1) AND fields->>'Allergen_Tags' ~ 'ALG-(MILK|FISH|MUSTARD)'`, sourceIDs).Scan(&count); err != nil || count != 0 {
					t.Fatalf("intersection leak=%d %v", count, err)
				}
			}
			if test.p.DietType == "Vegetarian" {
				for _, r := range rows {
					if r.Fields["Diet_Type"] != "Vegetarian" {
						t.Fatal("diet bypass")
					}
				}
			}
			found := false
			for _, s := range res.Steps {
				if s.Name == "Case source eligibility" {
					found = true
					if len(s.Excluded) == 0 {
						t.Fatal("source gaps missing")
					}
				}
			}
			if !found {
				t.Fatal("eligibility step missing")
			}
		})
	}
	t.Logf("SOURCE_ENGINE_OK existing=%d source=6000", original)
}
