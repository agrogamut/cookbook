// Package xlsx reads the provider workbooks.
//
// Every sheet carries a title banner in row 1 and a purpose note in row 2, so the real
// header is on row 3 or row 4 depending on the sheet. The header row is never guessed:
// it is declared per sheet in the import spec and verified against the expected first
// column name, so a provider re-layout fails loudly instead of importing garbage.
package xlsx

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Sheet is one loaded worksheet: snake_cased column names plus raw string cell values.
type Sheet struct {
	File      string
	Name      string
	HeaderRow int      // 1-indexed row holding the real column names
	Columns   []string // snake_cased, in sheet order
	Rows      [][]string
}

var nonWord = regexp.MustCompile(`[^0-9A-Za-z]+`)

// Snake normalises a provider header into a Postgres column name. It lowercases and
// collapses runs of non-alphanumeric characters into a single underscore. It does not
// split camel case, so "VitA_ug_RAE_100g" becomes "vita_ug_rae_100g" and the DDL
// matches the workbook one-for-one.
func Snake(header string) string {
	s := nonWord.ReplaceAllString(strings.TrimSpace(header), "_")
	s = strings.Trim(s, "_")
	return strings.ToLower(s)
}

// Load reads one sheet. Cell values are read raw (unformatted) so a numeric cell keeps
// its stored precision instead of whatever display rounding the workbook applies.
// Rows that are entirely empty are dropped; a row that is short is padded, because
// openpyxl-authored sheets frequently omit trailing empty cells.
func Load(path, sheetName string, headerRow int) (*Sheet, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	raw, err := f.GetRows(sheetName, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("read sheet %q in %s: %w", sheetName, path, err)
	}
	if len(raw) < headerRow {
		return nil, fmt.Errorf("sheet %q in %s has %d rows, header row %d is out of range",
			sheetName, path, len(raw), headerRow)
	}

	hdr := raw[headerRow-1]
	cols := make([]string, 0, len(hdr))
	width := 0
	for i, h := range hdr {
		c := Snake(h)
		if c == "" {
			// Trailing unnamed columns are padding, not data.
			continue
		}
		cols = append(cols, c)
		width = i + 1
	}
	if len(cols) < 2 {
		return nil, fmt.Errorf("sheet %q in %s: row %d does not look like a header (%d named columns); "+
			"the real header is on row 3 or row 4 depending on the sheet",
			sheetName, path, headerRow, len(cols))
	}

	out := &Sheet{File: path, Name: sheetName, HeaderRow: headerRow, Columns: cols}
	for _, r := range raw[headerRow:] {
		if allBlank(r) {
			continue
		}
		row := make([]string, len(cols))
		for i := 0; i < len(cols) && i < len(r) && i < width; i++ {
			row[i] = strings.TrimSpace(r[i])
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// Index returns the position of a column, or -1.
func (s *Sheet) Index(column string) int {
	for i, c := range s.Columns {
		if c == column {
			return i
		}
	}
	return -1
}

func allBlank(row []string) bool {
	for _, v := range row {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}
