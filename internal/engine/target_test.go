package engine

import (
	"context"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

func TestSelectTargetAutoActivatesNT01ForComplementaryAge(t *testing.T) {
	pool := testPool(t)
	code, reason, err := selectTarget(context.Background(), pool, models.ChildProfile{AgeMonths: 8})
	if err != nil {
		t.Fatalf("selectTarget: %v", err)
	}
	if code != "NT01" {
		t.Fatalf("age 8mo must auto-activate NT01, got %q (%s)", code, reason)
	}
}

func TestSelectTargetFallsBackToNT00(t *testing.T) {
	pool := testPool(t)
	code, _, err := selectTarget(context.Background(), pool, models.ChildProfile{AgeMonths: 60})
	if err != nil {
		t.Fatalf("selectTarget: %v", err)
	}
	if code != "NT00" {
		t.Fatalf("60mo with no clinical marker must fall back to NT00, got %q", code)
	}
}

func TestRankByTargetOrdersDescending(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ids, _, err := ageFilter(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("ageFilter: %v", err)
	}
	ranked, step, err := rankByTarget(ctx, pool, "NT00", ids)
	if err != nil {
		t.Fatalf("rankByTarget: %v", err)
	}
	if len(ranked) != step.CandidatesOut || len(ranked) == 0 {
		t.Fatalf("ranker must never empty the pool: got %d recipes", len(ranked))
	}
	for i := 1; i < len(ranked); i++ {
		if ranked[i].RankedScore > ranked[i-1].RankedScore {
			t.Fatalf("recipes not in descending ranked_score order at index %d", i)
		}
	}
}
