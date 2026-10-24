package casesource

import (
	"context"
	"strconv"
	"testing"
)

func TestSourceEligibility(t *testing.T) {
	a, err := Parse(context.Background(), delivery(t))
	if err != nil {
		t.Fatal(err)
	}
	var eligible, held int
	byCase := map[int]Recipe{}
	for _, w := range a.Workbooks {
		for _, s := range w.Sheets {
			if s.Name != "Recipe_Master" {
				continue
			}
			for _, row := range s.Rows {
				if row.RecipeID == "" {
					continue
				}
				r := Recipe{CandidateID: CandidateID(a.SHA256, row.RecipeID), Fields: Fields(s, row), Citation: Citation{ArchiveSHA256: a.SHA256, Case: w.Case, Row: row.Number, RecipeID: row.RecipeID}}
				if knownRules[row.RecipeID] != RuleFingerprint(r.Fields) {
					t.Fatalf("unmapped delivered source %s", row.RecipeID)
				}
				age, _ := strconv.Atoi(r.Fields["Min_Age_Months"])
				gaps := Assess(r, Input{AgeMonths: age})
				if len(gaps) == 0 {
					eligible++
				} else {
					held++
				}
				if row.Number == 5 {
					byCase[w.Case] = r
				}
			}
		}
	}
	if eligible == 0 || held == 0 || eligible+held != 6000 {
		t.Fatalf("source classification: %d %d", eligible, held)
	}
	r := byCase[12]
	age, _ := strconv.Atoi(r.Fields["Min_Age_Months"])
	if gaps := Assess(r, Input{AgeMonths: age}); len(gaps) != 0 {
		t.Fatalf("ordinary provisional row blocked: %+v", gaps)
	}
	for _, test := range []struct {
		name   string
		recipe Recipe
		input  Input
		code   string
	}{
		{"age", r, Input{AgeMonths: age - 1}, "age_excluded"},
		{"unknown clinical", r, Input{AgeMonths: age, UnmappedClinical: []string{"CKD=Yes"}}, "clinical_unmapped"},
		{"vegan unknown", r, Input{AgeMonths: age, Vegan: true}, "vegan_unmapped"},
		{"metabolic", byCase[48], Input{AgeMonths: 6}, "source_prerequisite"},
		{"oral route", byCase[55], Input{AgeMonths: 6}, "source_prerequisite"},
		{"post-vaccine familiar foods", byCase[26], Input{AgeMonths: 6}, "source_prerequisite"},
		{"feeding readiness", byCase[56], Input{AgeMonths: 6}, "source_prerequisite"},
	} {
		t.Run(test.name, func(t *testing.T) {
			gaps := Assess(test.recipe, test.input)
			for _, g := range gaps {
				if g.Code == test.code && g.Evidence != "" && g.Source.RecipeID != "" {
					return
				}
			}
			t.Fatalf("missing sourced gap %s: %+v", test.code, gaps)
		})
	}
	r.Fields["Safety_Rule"] += " corrected source condition"
	gaps := Assess(r, Input{AgeMonths: age})
	found := false
	for _, g := range gaps {
		found = found || g.Code == "rule_unmapped"
	}
	if !found {
		t.Fatal("changed safety content passed stale mapping")
	}
	t.Logf("SOURCE_POLICY_OK recipes=6000 eligible_at_source_min_age=%d held_for_recorded_prerequisites=%d", eligible, held)
}
