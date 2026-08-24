package bulkintake

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func TestParseRowsReadsOneRowByHeaderName(t *testing.T) {
	csv := "Child Full Name,Date of Birth,Sex used for pediatric growth reference\n" +
		"Joyshree Debnath,03/05/2023,Female\n"
	rows, err := ParseRows(strings.NewReader(minimalHeaderCSV(csv)))
	if err != nil {
		t.Fatalf("ParseRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0]["Child Full Name"] != "Joyshree Debnath" {
		t.Errorf("Child Full Name = %q, want Joyshree Debnath", rows[0]["Child Full Name"])
	}
}

func TestParseRowsRejectsAFileMissingARequiredHeader(t *testing.T) {
	// Every required header except "Date of Birth".
	var cols []string
	for _, h := range RequiredHeaders {
		if h == "Date of Birth" {
			continue
		}
		cols = append(cols, h)
	}
	csv := strings.Join(cols, ",") + "\n"

	_, err := ParseRows(strings.NewReader(csv))
	if err == nil {
		t.Fatal("ParseRows: want error for missing required header, got nil")
	}
	if !strings.Contains(err.Error(), "Date of Birth") {
		t.Errorf("error %q does not name the missing header", err.Error())
	}
}

func TestParseRowsOnEmptyFile(t *testing.T) {
	_, err := ParseRows(strings.NewReader(""))
	if err == nil {
		t.Fatal("ParseRows: want error on an empty file, got nil")
	}
}

// minimalHeaderCSV pads a partial header+row pair with every other required header, empty,
// so tests can focus on the columns they care about without hand-writing all sixteen.
func minimalHeaderCSV(partial string) string {
	lines := strings.SplitN(partial, "\n", 2)
	givenHeader := strings.Split(lines[0], ",")
	given := map[string]bool{}
	for _, h := range givenHeader {
		given[h] = true
	}
	var extra []string
	for _, h := range RequiredHeaders {
		if !given[h] {
			extra = append(extra, h)
		}
	}
	fullHeader := append(givenHeader, extra...)

	// Properly encode the header row using csv.Writer to handle fields containing commas
	var headerBuf bytes.Buffer
	w := csv.NewWriter(&headerBuf)
	w.Write(fullHeader)
	w.Flush()
	headerRow := strings.TrimRight(headerBuf.String(), "\n")

	row := lines[1]
	if row != "" {
		for range extra {
			row = strings.TrimRight(row, "\n") + ","
		}
	}
	return headerRow + "\n" + row
}
