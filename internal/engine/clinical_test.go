package engine

import (
	"context"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

func TestClinicalFilterBlocksUnmappableCondition(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	p := models.ChildProfile{
		AgeMonths:     36,
		ClinicalFlags: map[string]string{"CKD": "Yes"},
	}
	_, step, blocked, reason, err := clinicalFilter(ctx, pool, p, []string{"MG-R-00001"})
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if !blocked {
		t.Fatal("CKD has no queryable recipe-side safety tag in the schema; the engine must block, not silently pass a recipe list through")
	}
	if reason == "" || step.CandidatesOut != 0 {
		t.Fatalf("blocked result must explain why and return zero candidates: reason=%q out=%d", reason, step.CandidatesOut)
	}
}

func TestClinicalFilterNoOpWithoutFlags(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	in := []string{"MG-R-00001", "MG-R-00002"}
	out, _, blocked, _, err := clinicalFilter(ctx, pool, models.ChildProfile{AgeMonths: 36}, in)
	if err != nil {
		t.Fatalf("clinicalFilter: %v", err)
	}
	if blocked || len(out) != len(in) {
		t.Fatalf("no clinical flags set: expected pass-through, got blocked=%v out=%d", blocked, len(out))
	}
}
