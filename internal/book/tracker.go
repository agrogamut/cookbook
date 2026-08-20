package book

import (
	"strings"
)

// defaultTrackerRows is what an unrecognised or absent frequency gets.
//
// Four, deliberately modest. The cost of the two errors is not symmetric: too few rows means a
// parent writes in the margin, too many means blank pages in a book someone printed.
const defaultTrackerRows = 4

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
	{"selected week", 4}, // fewer than the seven days the name suggests: four is what fits
	// beside a domain, and the grid splits rather than being cut short when a page runs long
	{"each vaccine", 5},
	{"daily", 5},
	{"weekly", 4},
	{"monthly", 4},
	{"routine follow-up", 3}, // a growth parameter recorded at each routine visit
	{"at follow-up", 3},
	{"routine visit", 3},
	{"age checkpoint", 4},
	{"term", 3},
	{"during illness", 4},
	{"as needed", 4},
}

// The counts above are also a page budget, not only a usefulness judgement.
//
// A domain page carries the page head, the block's purpose and what it covers, a reference
// table, a red-flag callout, a referral line and a scope note before a single row is drawn --
// about 175mm of a 255mm content box, leaving roughly 80mm. The tracker's own head (title,
// reference, threshold callout, reviewer, column headings) takes about 55mm of that, so four
// rows at 6.5mm is what actually fits beside a domain.
//
// The earlier numbers were nearly twice these and were measured against a book that was being
// printed at 79% scale without anyone knowing -- an over-wide table was making Chromium shrink
// the whole document, so every row was 5.2mm on paper rather than 6.5mm (see the note above
// td .write-line in tokens.css). Fixing the scale made every tracker overflow its page, which
// is how these were caught.
//
// A grid that still does not fit now splits and repeats its header rather than moving whole,
// so an over-long one degrades into a continuation rather than into two half-empty sheets.
// These numbers keep the common case on one page; the split is the safety net.
//
// So they are load-bearing in two directions, and changing one without re-printing a book and
// looking at it will silently reintroduce the problem. The row height is .tracker td in
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
		// No cells: every column is a writing line, so the headers are the whole demand. That
		// is exactly the case that was breaking words -- "HEAD CIRCUMFER / ENCE" is a header,
		// not data.
		Widths: ColumnWidths(cols, nil),
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
		Widths:    ColumnWidths(cols, nil),
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
