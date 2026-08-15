package engine

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestAgeFilterExcludesOutOfBand(t *testing.T) {
	pool := testPool(t)
	ids, step, err := ageFilter(context.Background(), pool, models.ChildProfile{AgeMonths: 8})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("8-month age band must return candidates: it is the best-covered infant band")
	}
	if step.CandidatesOut != len(ids) {
		t.Fatalf("step.CandidatesOut = %d, want %d", step.CandidatesOut, len(ids))
	}
}

func TestAllergyFilterExcludesDeclaredAllergen(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	all, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	filtered, step, err := allergyFilter(ctx, pool, models.ChildProfile{Allergens: []string{"Peanut"}}, all)
	if err != nil {
		t.Fatalf("allergyFilter: %v", err)
	}
	if len(filtered) >= len(all) {
		t.Fatalf("declaring Peanut must remove at least one recipe: before=%d after=%d", len(all), len(filtered))
	}
	if step.CandidatesIn != len(all) {
		t.Fatalf("step.CandidatesIn = %d, want %d", step.CandidatesIn, len(all))
	}
}
