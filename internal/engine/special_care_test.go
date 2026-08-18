package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

func TestSpecialCareConditionBlocksAndNamesTheReviewer(t *testing.T) {
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
			step, blocked, reason, err := specialCareGate(ctx, pool,
				models.ChildProfile{AgeMonths: 36, SpecialCareCondition: c.conditionID})
			if err != nil {
				t.Fatalf("specialCareGate: %v", err)
			}
			if !blocked {
				t.Fatalf("%s is STOP-REVIEW in the provider's master and must block", c.conditionID)
			}
			if reason == "" {
				t.Fatal("a block with no reason leaves the operator no next step")
			}
			// The reviewer is the operator's actual next action, so it has to be in the
			// text -- verbatim, not paraphrased. Read the expected value from the row
			// rather than matching keywords: SC-CHD's reviewer is "Pediatric
			// cardiology/pediatrician + dietitian", which contains neither "clinician"
			// nor "team", so any keyword guess would be testing the guess.
			var wantReviewer string
			if err := pool.QueryRow(ctx,
				`SELECT mandatory_reviewer FROM special_care_condition_gate WHERE condition_id = $1`,
				c.conditionID).Scan(&wantReviewer); err != nil {
				t.Fatalf("reviewer lookup: %v", err)
			}
			if !strings.Contains(reason, wantReviewer) {
				t.Fatalf("block reason must quote the provider's reviewer %q, got %q",
					wantReviewer, reason)
			}
			if !strings.Contains(reason, c.name) {
				t.Fatalf("block reason must name the condition %q, got %q", c.name, reason)
			}
			if step.Kind != "hard_filter" {
				t.Fatalf("the stop gate is a hard filter, got kind %q", step.Kind)
			}
			if step.Step != 3 {
				t.Fatalf("the stop gate belongs to step 3, got %d", step.Step)
			}
		})
	}
}

func TestSpecialCareGateIsANoOpWhenNoConditionGiven(t *testing.T) {
	pool := testPool(t)
	step, blocked, reason, err := specialCareGate(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("specialCareGate: %v", err)
	}
	if blocked || reason != "" {
		t.Fatalf("no condition declared must not block: blocked=%v reason=%q", blocked, reason)
	}
	if step.Note == "" {
		t.Fatal("a step that did nothing must say so rather than looking like it ran")
	}
}

// An unknown condition id is an error, not a silent pass. Accepting it would mean the
// operator believes they recorded a condition the engine never saw.
func TestSpecialCareGateRejectsAnUnknownCondition(t *testing.T) {
	pool := testPool(t)
	_, _, _, err := specialCareGate(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, SpecialCareCondition: "SC-NOPE"})
	if err == nil {
		t.Fatal("an unrecognised special-care condition id must error, not pass silently")
	}
}

// The whole pipeline must stop, not merely record a step. A ranked list alongside a block
// is exactly the false assurance the gate exists to prevent.
func TestRunReturnsNoRecipesForASpecialCareChild(t *testing.T) {
	pool := testPool(t)
	res, err := Run(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, SpecialCareCondition: "SC-CP"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Blocked {
		t.Fatal("a special-care condition must block the pipeline")
	}
	if len(res.Recipes) != 0 {
		t.Fatalf("a blocked result must carry no recipes, got %d", len(res.Recipes))
	}
	if res.BlockReason == "" {
		t.Fatal("a blocked result must carry a reason")
	}
}

// The stop must not depend on the child also matching a clinical rule, a diet or an age
// band that happens to have recipes. A profile that would otherwise return a full list
// must still return nothing.
func TestSpecialCareBlocksAProfileThatWouldOtherwiseSucceed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	base := models.ChildProfile{AgeMonths: 36, DietType: "Vegetarian"}
	ok, err := Run(ctx, pool, base)
	if err != nil {
		t.Fatalf("Run baseline: %v", err)
	}
	if ok.Blocked || len(ok.Recipes) == 0 {
		t.Fatalf("baseline must succeed for this test to mean anything: blocked=%v recipes=%d",
			ok.Blocked, len(ok.Recipes))
	}

	base.SpecialCareCondition = "SC-DS"
	got, err := Run(ctx, pool, base)
	if err != nil {
		t.Fatalf("Run with condition: %v", err)
	}
	if !got.Blocked || len(got.Recipes) != 0 {
		t.Fatalf("the same profile with a special-care condition must return nothing: "+
			"blocked=%v recipes=%d", got.Blocked, len(got.Recipes))
	}
}
