package book

import (
	"strings"
)

// defaultTrackerRows is what an unrecognised or absent frequency gets.
//
// Six, deliberately modest. The cost of the two errors is not symmetric: too few rows means a
// parent writes in the margin, too many means blank pages in a book someone printed. Six fits
// a page beside its reference text either way.
const defaultTrackerRows = 6

// trackerRowsByFrequency maps the provider's own frequency vocabulary -- the eighteen distinct
// values in book1_monitoring_template -- to how many blank rows the grid gets.
//
// Matched on a lowercased substring rather than on equality, because the column mixes shapes
// ("Daily / visit", "Weekly / as needed", "As advised / routine visit") and an exact-match
// table would silently fall to the default for most of them. Order matters: the slice is
// scanned in order and the first hit wins, so "selected week" is tested before "week".
var trackerRowsByFrequency = []struct {
	match string
	rows  int
}{
	{"selected week", 7}, // one row per day of the week the parent picks
	{"each vaccine", 8},
	{"daily", 8},
	{"weekly", 7},
	{"monthly", 6},
	{"routine follow-up", 4}, // a growth parameter recorded at each routine visit
	{"at follow-up", 3},
	{"routine visit", 4},
	{"age checkpoint", 6},
	{"term", 4},
	{"during illness", 7},
	{"as needed", 6},
}

// The counts above are also a page budget, not only a usefulness judgement.
//
// A domain page carries a section band, the block's purpose, a reference table, a red-flag
// callout, a referral line and then the grid, in about 130mm before a single row is drawn.
// The content box is 225mm, so the grid has roughly 90mm. At 6.5mm a row, eight rows and a
// header fit; at ten the tracker no longer fits beside its domain, and since .tracker is
// break-inside: avoid it moved whole -- printing every daily-life block as two half-empty
// pages instead of one full one. At the original fourteen it split mid-grid and stranded two
// writing lines on a sheet of their own.
//
// Eight rows is therefore a page-fit number as much as a useful-history one, which is why the
// two are documented together.
//
// So these numbers are load-bearing in two directions, and changing one without re-printing a
// book and looking at it will silently reintroduce that. The row height is .tracker td in
// tokens.css and the two must be considered together.

// TrackerRows is how many blank rows a tracker grid gets for a declared frequency.
//
// Derived, and the only derived number on these pages. The provider declares the frequency
// and not the row count, and a weekly tracker with one row is unusable while a daily one with
// thirty is three wasted pages. The frequency string itself is printed on the page, so a
// reader can see what the grid is for rather than inferring it from the number of lines.
func TrackerRows(frequency string) int {
	f := strings.ToLower(strings.TrimSpace(frequency))
	if f == "" {
		return defaultTrackerRows
	}
	for _, r := range trackerRowsByFrequency {
		if strings.Contains(f, r.match) {
			return r.rows
		}
	}
	return defaultTrackerRows
}

// splitDeclared splits one of the provider's semicolon-separated declaration columns --
// writable_fields, parent_facing_output, parent_notes -- into its items.
//
// Semicolon first, then comma: writable_fields uses semicolons throughout, while
// parent_facing_output uses commas, and several values contain a comma inside a single item
// ("Bedtime/wake routine, naps when age-relevant"). Splitting on comma first would cut those
// in half, so a string containing any semicolon is treated as semicolon-separated and left
// alone by the comma pass.
func splitDeclared(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	sep := ","
	if strings.Contains(s, ";") {
		sep = ";"
	}
	var out []string
	for _, part := range strings.Split(s, sep) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// trackerFromMonitoring builds a grid from a monitoring template's declared columns.
//
// The reference and actual columns are the ideal-versus-actual pair the workbook is built
// around, and the date and note columns follow them. Alarm and review do not become columns:
// they are thresholds and they print under the grid, where a parent comparing a filled row
// against them can read them.
func trackerFromMonitoring(m MonitoringTemplate) TrackerSpec {
	cols := []string{}
	if m.DateTime != "" {
		cols = append(cols, m.DateTime)
	}
	if m.Reference != "" {
		cols = append(cols, m.Reference)
	}
	if m.Actual != "" {
		cols = append(cols, m.Actual)
	}
	cols = append(cols, splitDeclared(m.Notes)...)

	return TrackerSpec{
		Title:     strings.TrimSpace(m.Section + " - " + m.Parameter),
		Parameter: m.Parameter,
		Reference: m.Reference,
		Frequency: m.Frequency,
		Columns:   cols,
		Rows:      TrackerRows(m.Frequency),
		Alarm:     m.Alarm,
		Review:    m.Review,
	}
}

// trackerFromWritable builds a grid from a block's own writable_fields, for the blocks that
// declare a form and have no monitoring template behind them.
func trackerFromWritable(title, writableFields, frequency string) *TrackerSpec {
	cols := splitDeclared(writableFields)
	if len(cols) == 0 {
		return nil
	}
	return &TrackerSpec{
		Title:     title,
		Frequency: frequency,
		Columns:   cols,
		Rows:      TrackerRows(frequency),
	}
}

// SectionHasContent reports whether a section would print anything under its heading.
//
// The guard exists because conservation accounting cannot see this: a section counted as
// rendered and a section that actually carries content are different claims, and the first
// stayed true while Book 1 shipped headings over empty areas. A section that fails this is
// reported as an omission instead of being appended.
func SectionHasContent(s Section) bool {
	if len(s.Rows) > 0 || len(s.Growth) > 0 || len(s.Domains) > 0 ||
		len(s.Illness) > 0 || len(s.Refs) > 0 || s.Callout != nil {
		return true
	}
	if s.Stage != nil && s.Stage.Current != nil {
		return true
	}
	if s.Safety != nil {
		return true
	}
	for _, t := range s.Trackers {
		// Columns, not rows: a grid with headings and blank lines is a form and counts as
		// content, while a grid with no headings is a heading over nothing.
		if len(t.Columns) > 0 {
			return true
		}
	}
	return false
}
