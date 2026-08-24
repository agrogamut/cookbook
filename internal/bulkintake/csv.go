package bulkintake

import (
	"encoding/csv"
	"fmt"
	"io"
)

// RawRow is one submission, keyed by the form's own question text. Looking up by name rather
// than by column position is what makes this tolerant of the provider reordering columns
// (adding a question in the middle of the form) without a code change -- only a renamed or
// removed column, which changes the text itself, is a breaking change, and that is exactly
// the case ParseRows rejects loudly.
type RawRow map[string]string

// ParseRows reads a Google Form CSV export and returns one RawRow per submission.
//
// The header row is validated against RequiredHeaders before any row is read: a form edited
// since this system's header seed was written must fail the whole file, not silently produce
// rows with some fields missing. That mirrors the existing rule for the rest of this project's
// batch processing -- a bad file is reported, not partially trusted.
func ParseRows(r io.Reader) ([]RawRow, error) {
	cr := csv.NewReader(r)
	// The form's free-text answers can legitimately contain commas inside quoted fields;
	// encoding/csv already handles RFC 4180 quoting by default, so no FieldsPerRecord
	// override is needed here beyond leaving it at -1 while validating.
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("bulkintake: empty file, no header row")
	}
	if err != nil {
		return nil, fmt.Errorf("bulkintake: read header row: %w", err)
	}

	present := make(map[string]bool, len(header))
	for _, h := range header {
		present[h] = true
	}
	var missing []string
	for _, want := range RequiredHeaders {
		if !present[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"bulkintake: form header does not match the required seed -- missing column(s): %v. "+
				"The form was likely edited; update internal/bulkintake/headers.go to match the "+
				"current export and redeploy", missing)
	}

	var rows []RawRow
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bulkintake: read row %d: %w", len(rows)+2, err)
		}
		row := make(RawRow, len(header))
		for i, h := range header {
			if i < len(record) {
				row[h] = record[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
