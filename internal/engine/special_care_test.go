package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

// The gate no longer stops anything, but the provider's own text still has to survive it
// intact. That text is the reason an operator can act on the condition at all, and it is
// quoted rather than paraphrased for the same reason it always was: paraphrasing clinical
// instruction is how a summary becomes advice.
func TestSpecialCareConditionRecordsTheProvidersOwnText(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, c := range []struct {
		conditionID string
		name        string
	}{
		// Names exactly as the workbook spells them, so a reissue that renames a
		// condition fails here rather than passing on a loose match.
		{"SC-DS", "Down syndrome"},
		{"SC-CP", "Cerebral palsy"},
		{"SC-CHD", "Congenital heart disease"},
		{"SC-CLP", "Cleft lip/palate"},
		{"SC-ASD", "Autism spectrum disorder"},
		{"SC-ID", "Intellectual disability"},
	} {
		t.Run(c.conditionID, func(t *testing.T) {
			step, err := specialCareGate(ctx, pool,
				models.ChildProfile{AgeMonths: 36, SpecialCareCondition: c.conditionID})
			if err != nil {
				t.Fatalf("specialCareGate: %v", err)
			}
			// Read the expected reviewer from the row rather than matching keywords:
			// SC-CHD's is "Pediatric cardiology/pediatrician + dietitian", which contains
			// neither "clinician" nor "team", so any keyword guess would be testing the
			// guess.
			var wantReviewer string
			if err := pool.QueryRow(ctx,
				`SELECT mandatory_reviewer FROM special_care_condition_gate WHERE condition_id = $1`,
				c.conditionID).Scan(&wantReviewer); err != nil {
				t.Fatalf("reviewer lookup: %v", err)
			}
			if !strings.Contains(step.Note, wantReviewer) {
				t.Fatalf("the step note must quote the provider's reviewer %q, got %q",
					wantReviewer, step.Note)
			}
			if !strings.Contains(step.Note, c.name) {
				t.Fatalf("the step note must name the condition %q, got %q", c.name, step.Note)
			}
			if step.Kind != "record" {
				t.Fatalf("the gate records rather than filters or ranks, got kind %q", step.Kind)
			}
			if step.Step != 3 {
				t.Fatalf("the special-care row belongs to step 3, got %d", step.Step)
			}
		})
	}
}

func TestSpecialCareGateIsANoOpWhenNoConditionGiven(t *testing.T) {
	pool := testPool(t)
	step, err := specialCareGate(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("specialCareGate: %v", err)
	}
	if step.Note == "" {
		t.Fatal("a step that did nothing must say so rather than looking like it ran")
	}
}

// An unknown condition id is an error, not a silent pass. Accepting it would mean the
// operator believes they recorded a condition the engine never saw. This survives the gate
// removal unchanged: it is input validation, not a clinical stop.
func TestSpecialCareGateRejectsAnUnknownCondition(t *testing.T) {
	pool := testPool(t)
	_, err := specialCareGate(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, SpecialCareCondition: "SC-NOPE"})
	if err == nil {
		t.Fatal("an unrecognised special-care condition id must error, not pass silently")
	}
}

func TestSpecialCareConditionNoLongerStopsGeneration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	p := models.ChildProfile{AgeMonths: 36, SpecialCareCondition: "SC-CP"}
	res, err := Run(ctx, pool, p)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Recipes) == 0 {
		t.Fatal("a declared special-care condition must not empty the result list")
	}

	var note string
	for _, s := range res.Steps {
		if s.Name == "Special-care condition" {
			note = s.Note
		}
	}
	if note == "" {
		t.Fatal("the special-care step must still record the provider's own text")
	}
	if !strings.Contains(note, "SC-CP") {
		t.Fatalf("step note must name the condition, got %q", note)
	}
}

// The condition must not quietly change the result either. It is recorded and it feeds
// target selection and the drafted modification notes; it does not add or remove a recipe
// on its own.
func TestSpecialCareConditionDoesNotChangeTheRecipeList(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	base := models.ChildProfile{AgeMonths: 36, DietType: "Vegetarian"}
	without, err := Run(ctx, pool, base)
	if err != nil {
		t.Fatalf("Run baseline: %v", err)
	}
	if len(without.Recipes) == 0 {
		t.Fatal("baseline must return recipes for this test to mean anything")
	}

	base.SpecialCareCondition = "SC-DS"
	with, err := Run(ctx, pool, base)
	if err != nil {
		t.Fatalf("Run with condition: %v", err)
	}
	if len(with.Recipes) != len(without.Recipes) {
		t.Fatalf("the condition is recorded, not filtered: %d recipes without it, %d with",
			len(without.Recipes), len(with.Recipes))
	}
}
