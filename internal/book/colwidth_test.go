package book

import "testing"

func TestColumnWidthsSumTo100AndRespectTheFloor(t *testing.T) {
	for _, c := range []struct {
		name    string
		headers []string
		cells   [][]string
	}{
		{"a long header beside short ones",
			[]string{"Date", "Head circumference", "Note"},
			[][]string{{"2026-08-01", "", ""}}},
		{"an unbreakable identifier",
			[]string{"Source", "Topic"},
			[][]string{{"IAP-STG-CONSTIPATION", "Constipation"}}},
		{"equal content",
			[]string{"A", "B", "C", "D"},
			[][]string{{"x", "x", "x", "x"}}},
		{"no cells at all",
			[]string{"Tried on", "Amount accepted", "Response", "Parent note"},
			nil},
		{"a ragged row shorter than the header",
			[]string{"Area", "Reference", "Actual", "Date", "Note"},
			[][]string{{"Growth: Weight", "Age/sex reference interpretation"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ColumnWidths(c.headers, c.cells)
			if len(got) != len(c.headers) {
				t.Fatalf("%d widths for %d columns", len(got), len(c.headers))
			}
			if sum := sumOf(got); sum != 100 {
				t.Errorf("widths %v sum to %d, want 100", got, sum)
			}
			for i, w := range got {
				if w < minColumnPct {
					t.Errorf("column %d is %d%%, below the %d%% floor: %v", i, w, minColumnPct, got)
				}
			}
		})
	}
}

// The point of the whole file: a column must be wide enough for the longest word it holds, or
// fixed table layout breaks that word. Measured as a share -- a header of eighteen characters
// beside one of four gets more room than an equal split would give it.
func TestALongHeaderGetsMoreRoomThanAnEqualSplit(t *testing.T) {
	got := ColumnWidths([]string{"Date", "Head circumference", "Note"},
		[][]string{{"2026-08-01", "", ""}})
	if got[1] <= 34 {
		t.Errorf("the long column got %d%%, no more than an equal split: %v", got[1], got)
	}
	if got[1] <= got[0] || got[1] <= got[2] {
		t.Errorf("the long column is not the widest: %v", got)
	}
}

// Slashes and hyphens are not break opportunities. Breaking inside "Language/cognitive" or
// "IAP-STG-CONSTIPATION" produces a string that reads as a different value, and both did on
// printed pages.
func TestLongestTokenDoesNotBreakOnPunctuation(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"Date", 4},
		{"Head circumference", 13},
		{"Language/cognitive", 18},
		{"IAP-STG-CONSTIPATION", 20},
		{"Reference z-score/interpretation", 22},
		{"  spaced   out  ", 6},
	} {
		if got := longestToken(c.in); got != c.want {
			t.Errorf("longestToken(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// An empty table must not divide by zero or return nothing renderable.
func TestColumnWidthsHandlesDegenerateInput(t *testing.T) {
	if got := ColumnWidths(nil, nil); got != nil {
		t.Errorf("no headers should yield no widths, got %v", got)
	}
	got := ColumnWidths([]string{"", "", ""}, nil)
	if sum := sumOf(got); sum != 100 {
		t.Errorf("three empty headers gave %v summing to %d, want 100", got, sum)
	}
}
