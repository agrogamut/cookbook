package book

import (
	"context"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/profile"
)

// The Food-Safety SOP table (allergy_safety_master, 8 rows) is imported and, per CLAUDE.md,
// "already has real, citable content sitting unused, imported but never rendered." This pins
// that AssembleBook2 actually reads it: 8 real rows, each traceable to a sop_id.
//
// None of the 8 currently resolve a citation -- GAP-028, found while writing this test:
// food_safety_sop.evidence_id names EV-CDC-FOODSAFETY and EV-WHO-FIVEKEYS, and neither exists
// in evidence_reference_master (9 real rows, all for other domains). The rule text is still
// real, provider-authored content; only the citation is an open gap, and the honest behaviour
// is an unresolved row printing without one, never a fabricated title.
func TestBook2CarriesTheFoodSafetySOP(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	s := profile.Stored{
		ChildID:     "BOOK-TEST-SAFETY-01",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		DietType:    "Vegetarian",
	}
	b, _, err := AssembleBook2(ctx, pool, s,
		time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AssembleBook2: %v", err)
	}

	if got := len(b.SafetySOP); got != 8 {
		t.Fatalf("SafetySOP has %d rows, want all 8 food_safety_sop rows", got)
	}

	for _, g := range b.SafetySOP {
		if g.SOPID == "" {
			t.Errorf("guideline with empty SOPID: %+v", g)
		}
		if g.Rule == "" {
			t.Errorf("guideline %s has empty Rule text", g.SOPID)
		}
		if g.Area == "" {
			t.Errorf("guideline %s has empty Area", g.SOPID)
		}
	}
}

// Static reference content: two different children must see byte-identical guidance, the same
// way Book 1's blank tracker forms do not vary by child. A join keyed off anything in the
// child's own profile would be a bug here, not a feature.
func TestFoodSafetySOPIsIdenticalAcrossChildren(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	asOf := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)

	a, _, err := AssembleBook2(ctx, pool, profile.Stored{
		ChildID: "BOOK-TEST-SAFETY-A", DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		DietType: "Vegetarian",
	}, asOf)
	if err != nil {
		t.Fatalf("AssembleBook2 (a): %v", err)
	}
	c, _, err := AssembleBook2(ctx, pool, profile.Stored{
		ChildID: "BOOK-TEST-SAFETY-B", DateOfBirth: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		DietType: "Non-vegetarian",
	}, asOf)
	if err != nil {
		t.Fatalf("AssembleBook2 (b): %v", err)
	}

	if len(a.SafetySOP) != len(c.SafetySOP) {
		t.Fatalf("SafetySOP length differs across children: %d vs %d", len(a.SafetySOP), len(c.SafetySOP))
	}
	for i := range a.SafetySOP {
		if a.SafetySOP[i] != c.SafetySOP[i] {
			t.Fatalf("SafetySOP[%d] differs across children: %+v vs %+v", i, a.SafetySOP[i], c.SafetySOP[i])
		}
	}
}
