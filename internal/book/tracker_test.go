package book

import "testing"

// TestTrackerRowsFollowsTheDeclaredFrequency pins the one derived number on these pages.
//
// The cases are the provider's own frequency strings, copied from book1_monitoring_template
// rather than invented, so a change to that column's vocabulary shows up here as a failing
// case rather than as a silently defaulted grid.
func TestTrackerRowsFollowsTheDeclaredFrequency(t *testing.T) {
	for _, tc := range []struct {
		frequency string
		want      int
	}{
		{"Selected week", 7},
		{"Daily / visit", 8},
		{"Daily/selected days", 8},
		{"Weekly", 7},
		{"Weekly / as needed", 7},
		{"Monthly/visit", 6},
		{"At follow-up", 3},
		{"As advised / routine visit", 4},
		{"Routine follow-up", 4},
		{"Age checkpoint", 6},
		{"Term/follow-up", 4},
		{"During illness", 7},
		{"As needed", 6},
		{"Each vaccine", 8},
		{"", defaultTrackerRows},
		{"every third blue moon", defaultTrackerRows},
	} {
		t.Run(tc.frequency, func(t *testing.T) {
			if got := TrackerRows(tc.frequency); got != tc.want {
				t.Errorf("frequency %q: want %d rows, got %d", tc.frequency, tc.want, got)
			}
		})
	}
}

// TestSplitDeclaredKeepsCommasInsideSemicolonLists is the parsing trap. writable_fields uses
// semicolons and parent_facing_output uses commas, but several parent_facing_output values
// contain a comma inside one item -- "Bedtime/wake routine, naps when age-relevant" -- so a
// comma-first split would cut single topics in half.
func TestSplitDeclaredKeepsCommasInsideSemicolonLists(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "semicolon list wins over any comma inside it",
			in:   "Bedtime; sleep onset; waking, including night waking; parent note",
			want: []string{"Bedtime", "sleep onset", "waking, including night waking", "parent note"},
		},
		{
			name: "comma list when there is no semicolon",
			in:   "Screen during meals, screen before sleep, background media",
			want: []string{"Screen during meals", "screen before sleep", "background media"},
		},
		{name: "empty", in: "   ", want: nil},
		{name: "trailing separator produces no empty item", in: "Date; note;", want: []string{"Date", "note"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := splitDeclared(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("want %d items %v, got %d items %v", len(tc.want), tc.want, len(got), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("item %d: want %q, got %q", i, tc.want[i], got[i])
				}
			}
		})
	}
}

// TestSectionHasContentRejectsAnEmptyGrid: a tracker with no columns is a heading over an
// empty table, which is the defect this whole change exists to close. It must be reported as
// an omission, never appended as a rendered section.
func TestSectionHasContentRejectsAnEmptyGrid(t *testing.T) {
	empty := Section{TemplateID: "B1-TRACKER-01", Trackers: []TrackerSpec{{Columns: nil, Rows: 7}}}
	if SectionHasContent(empty) {
		t.Error("a tracker with no columns reports as content; it would print an empty grid")
	}

	filled := Section{TemplateID: "B1-TRACKER-01",
		Trackers: []TrackerSpec{{Columns: []string{"Date", "Actual"}, Rows: 7}}}
	if !SectionHasContent(filled) {
		t.Error("a tracker with columns reports as empty; the page would be dropped")
	}

	// A stage page whose current stage is nil is the over-eighteen case: the block matched on
	// age but age_feeding_stage_master stops at 216 months, so there is nothing to print.
	if SectionHasContent(Section{TemplateID: "B1-STAGE-01", Stage: &StagePage{}}) {
		t.Error("a stage page with no current stage reports as content")
	}
}
