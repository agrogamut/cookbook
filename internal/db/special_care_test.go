package db_test

import (
	"context"
	"testing"
)

// TestSpecialCareRowCounts pins what the workbook ships. A count that moves means the
// provider reissued the file, which is a thing to notice rather than absorb silently.
func TestSpecialCareRowCounts(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, c := range []struct {
		table string
		want  int
	}{
		{"special_care_condition_gate", 6},
		{"special_care_parameter", 30},
		{"special_care_food_type", 10},
		{"special_care_feeding_style", 13},
		{"special_care_recipe_candidate", 108},
		{"special_care_output_rule", 14},
		{"special_care_evidence_source", 14},
		{"special_care_qa_check", 12},
		{"special_care_coverage_metric", 9},
	} {
		t.Run(c.table, func(t *testing.T) {
			var got int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+c.table).Scan(&got); err != nil {
				t.Fatalf("count %s: %v", c.table, err)
			}
			if got != c.want {
				t.Fatalf("%s: expected %d rows, got %d", c.table, c.want, got)
			}
		})
	}
}

// TestEverySpecialCareConditionIsAStopGate is the invariant the engine's stop gate depends
// on. The workbook's README states the rule as "Condition is a STOP GATE, not a simple
// recipe filter", and all six rows carry Gate_Level = STOP-REVIEW. If the provider ever
// ships a condition at a lower gate level, the engine must not keep blocking it as though
// the provider still required that -- so this fails and forces the decision.
func TestEverySpecialCareConditionIsAStopGate(t *testing.T) {
	pool := testPool(t)
	var notStop int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM special_care_condition_gate WHERE gate_level <> 'STOP-REVIEW'`).
		Scan(&notStop)
	if err != nil {
		t.Fatalf("gate level query: %v", err)
	}
	if notStop != 0 {
		t.Fatalf("%d special-care conditions are not STOP-REVIEW; internal/engine/special_care.go "+
			"blocks every one of them, so a non-stop condition needs an explicit decision "+
			"rather than an inherited block", notStop)
	}
}

// TestEverySpecialCareConditionNamesAReviewer pins the other half of the block: the engine
// quotes mandatory_reviewer into its block reason, and a block that cannot say who to
// escalate to is a dead end for the operator.
func TestEverySpecialCareConditionNamesAReviewer(t *testing.T) {
	pool := testPool(t)
	var missing int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM special_care_condition_gate
		 WHERE mandatory_reviewer IS NULL OR btrim(mandatory_reviewer) = ''`).Scan(&missing)
	if err != nil {
		t.Fatalf("reviewer query: %v", err)
	}
	if missing != 0 {
		t.Fatalf("%d special-care conditions name no mandatory reviewer", missing)
	}
}

// TestSpecialCareCandidatesAreNeverApproved is the safety pin. All 108 candidates ship as
// CANDIDATE-REVIEW and none of their names exist in recipe_master. If a future import
// carried an approved-looking status, an engine path might reasonably start serving them,
// and this test is what stops that happening without a deliberate decision.
func TestSpecialCareCandidatesAreNeverApproved(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var notCandidate int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM special_care_recipe_candidate WHERE status <> 'CANDIDATE-REVIEW'`).
		Scan(&notCandidate); err != nil {
		t.Fatalf("status query: %v", err)
	}
	if notCandidate != 0 {
		t.Fatalf("%d special-care candidates are no longer CANDIDATE-REVIEW; nothing may be "+
			"served from this table without a recorded clinical decision", notCandidate)
	}

	var mapped int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM special_care_recipe_candidate c
		 JOIN recipe_master r ON r.recipe_name = c.recipe_name`).Scan(&mapped); err != nil {
		t.Fatalf("mapping query: %v", err)
	}
	if mapped != 0 {
		t.Fatalf("%d candidate names now match a Recipe Master row. That mapping is the "+
			"provider's job and changes what the engine could serve, so it needs a "+
			"decision rather than an inherited assumption", mapped)
	}
}

// The candidate and feeding-style tables carry real foreign keys, so a broken reference
// would fail the import rather than reach this test. What this checks is the other
// direction: that every condition the provider defines is actually used, so a condition
// silently losing all its rows is visible.
func TestEverySpecialCareConditionHasCandidatesAndAProtocol(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, c := range []struct{ name, query string }{
		{"conditions with no recipe candidate", `
			SELECT count(*) FROM special_care_condition_gate g
			WHERE NOT EXISTS (SELECT 1 FROM special_care_recipe_candidate c
			                  WHERE c.condition_id = g.condition_id)`},
		{"conditions with no feeding-style protocol", `
			SELECT count(*) FROM special_care_condition_gate g
			WHERE NOT EXISTS (SELECT 1 FROM special_care_feeding_style f
			                  WHERE f.condition_id = g.condition_id)`},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got int
			if err := pool.QueryRow(ctx, c.query).Scan(&got); err != nil {
				t.Fatalf("query: %v", err)
			}
			if got != 0 {
				t.Fatalf("%d %s", got, c.name)
			}
		})
	}
}

// The gap register must account for what this workbook does and does not deliver.
func TestSpecialCareGapsAreRegistered(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gap_register`).Scan(&total); err != nil {
		t.Fatalf("gap count: %v", err)
	}
	// The count lives in TestGapRegisterIsComplete in integrity_test.go, with the build-up
	// written out. Asserting it a second time here meant every migration that adds a gap
	// broke a special-care test that has nothing to do with the change -- which is noise
	// that trains a reader to update numbers without reading them. What this test is
	// actually for is the two rows below.
	if total < 2 {
		t.Fatalf("gap register holds %d rows; the special-care gaps cannot be among them", total)
	}

	for _, id := range []string{"GAP-021", "GAP-022"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM gap_register WHERE gap_id = $1`, id).Scan(&n); err != nil {
			t.Fatalf("gap %s: %v", id, err)
		}
		if n != 1 {
			t.Fatalf("%s missing from the gap register", id)
		}
	}

	// GAP-019 is rewritten by 0015, not closed: the conditions now have a source and
	// still have no clinical_rule_master row and no validated recipe.
	var desc string
	if err := pool.QueryRow(ctx,
		`SELECT description FROM gap_register WHERE gap_id = 'GAP-019'`).Scan(&desc); err != nil {
		t.Fatalf("GAP-019: %v", err)
	}
	if desc == "" {
		t.Fatal("GAP-019 must remain in the register after migration 0015")
	}
}
