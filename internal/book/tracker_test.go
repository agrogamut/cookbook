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
		// The counts halved when the type stopped being printed at 79% scale -- an
		// over-wide table had been making Chromium shrink every page of Book 1, so a
		// 6.5mm row was really 5.2mm on paper and nearly twice as many fitted. See the
		// page-budget note in tracker.go.
		{"Selected week", 4},
		{"Daily / visit", 5},
		{"Daily/selected days", 5},
		{"Weekly", 4},
		{"Weekly / as needed", 4},
		{"Monthly/visit", 4},
		{"At follow-up", 3},
		{"As advised / routine visit", 3},
		{"Routine follow-up", 3},
		{"Age checkpoint", 4},
		{"Term/follow-up", 3},
		{"During illness", 4},
		{"As needed", 4},
		{"Each vaccine", 5},
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

// TestABlankFormProtectsOnlyItsTail.
//
// The regression: Book 1 page 42 was three writing lines and a repeated column header on an
// otherwise blank sheet, the tail of the food-diversity grid. Chromium ignores CSS orphans and
// widows on table rows, so the tbody is the only unit a form can be held together by -- and
// grouping every row was measured worse than protecting the tail alone. See FlowRows.
func TestABlankFormProtectsOnlyItsTail(t *testing.T) {
	for _, c := range []struct {
		rows               int
		wantFlow, wantTail int
	}{
		{rows: 0, wantFlow: 0, wantTail: 0},
		{rows: 1, wantFlow: 0, wantTail: 1}, // a form shorter than the tail is all tail
		{rows: 4, wantFlow: 0, wantTail: 4},
		{rows: 5, wantFlow: 1, wantTail: 4},
		{rows: 10, wantFlow: 6, wantTail: 4},
	} {
		spec := TrackerSpec{Columns: []string{"a"}, Rows: c.rows}
		if got := len(spec.FlowRows()); got != c.wantFlow {
			t.Errorf("%d rows: %d flow rows, want %d", c.rows, got, c.wantFlow)
		}
		if got := len(spec.TailRows()); got != c.wantTail {
			t.Errorf("%d rows: %d tail rows, want %d", c.rows, got, c.wantTail)
		}
		if got := len(spec.FlowRows()) + len(spec.TailRows()); got != c.rows {
			t.Errorf("%d rows: the two bodies hold %d", c.rows, got)
		}
	}
}

// A prefilled grid is not split: every row carries its own label, so a break leaves both halves
// readable and holding a tail together would only add a place the fragmenter cannot break.
func TestAPrefilledGridIsNotGrouped(t *testing.T) {
	spec := TrackerSpec{Columns: []string{"a", "b"}, Rows: 8, Prefilled: [][]string{{"x"}, {"y"}}}
	if got := spec.FlowRows(); got != nil {
		t.Errorf("a prefilled grid returned %d flow rows, want none", len(got))
	}
	if got := spec.TailRows(); got != nil {
		t.Errorf("a prefilled grid returned %d tail rows, want none", len(got))
	}
}
