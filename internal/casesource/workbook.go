package casesource

import (
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed headers.json
var sourceHeaders []byte

type richText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (s richText) value() string {
	var b strings.Builder
	b.WriteString(s.Text)
	for _, r := range s.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

func parseWorkbook(data []byte, caseNumber int, remaining *uint64) (Workbook, error) {
	return parseWorkbookFormat(data, caseNumber, remaining, false)
}

func parseWorkbookFormat(data []byte, caseNumber int, remaining *uint64, locale bool) (Workbook, error) {
	files, err := packageFiles(data, 256)
	if err != nil {
		return Workbook{}, err
	}
	parts := make(map[string][]byte, len(files))
	for name, f := range files {
		if f.UncompressedSize64 > *remaining {
			return Workbook{}, fmt.Errorf("delivery expanded size exceeds limit")
		}
		*remaining -= f.UncompressedSize64
		b, err := readMember(f)
		if err != nil {
			return Workbook{}, err
		}
		parts[name] = b
	}
	var manifest struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(parts["xl/workbook.xml"], &manifest); err != nil {
		return Workbook{}, fmt.Errorf("read workbook manifest: %w", err)
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(parts["xl/_rels/workbook.xml.rels"], &rels); err != nil {
		return Workbook{}, fmt.Errorf("read workbook relationships: %w", err)
	}
	targets := map[string]string{}
	for _, r := range rels.Items {
		if r.ID == "" || targets[r.ID] != "" || r.Mode == "External" || strings.ContainsAny(r.Target, "\\:\x00") || strings.Contains(r.Target, "..") {
			return Workbook{}, fmt.Errorf("invalid workbook relationship %q", r.ID)
		}
		target := path.Join("xl", r.Target)
		if strings.HasPrefix(r.Target, "/") {
			target = strings.TrimPrefix(r.Target, "/")
		}
		targets[r.ID] = target
	}
	var shared struct {
		Items []richText `xml:"si"`
	}
	if b, ok := parts["xl/sharedStrings.xml"]; ok {
		if err := xml.Unmarshal(b, &shared); err != nil {
			return Workbook{}, fmt.Errorf("read shared strings: %w", err)
		}
	}
	stringsTable := make([]string, len(shared.Items))
	for i, s := range shared.Items {
		stringsTable[i] = s.value()
	}
	var headers map[string][]string
	if err := json.Unmarshal(sourceHeaders, &headers); err != nil {
		return Workbook{}, fmt.Errorf("read source headers: %w", err)
	}
	if locale {
		headers["Recipe_Master"] = append(headers["Recipe_Master"], "Nutrient_Gap")
		headers["For_translation"] = []string{"Recipe_ID", "Meal_Type", "Field", "English", "Quantity_g", "Bengali", "Note"}
		headers["Glossary"] = []string{"English", "Where it prints", "Bengali"}
	}
	if len(manifest.Sheets) != len(headers) {
		return Workbook{}, fmt.Errorf("expected exactly %d source sheets", len(headers))
	}
	w := Workbook{}
	seen := map[string]bool{}
	ids := map[string]map[string]bool{}
	for _, s := range manifest.Sheets {
		if headers[s.Name] == nil || seen[s.Name] {
			return Workbook{}, fmt.Errorf("unknown or repeated source sheet %q", s.Name)
		}
		seen[s.Name] = true
		part := targets[s.ID]
		if !strings.HasPrefix(part, "xl/worksheets/") {
			return Workbook{}, fmt.Errorf("missing worksheet relationship for %q", s.Name)
		}
		headerRow := 4
		if s.Name == "For_translation" || s.Name == "Glossary" {
			headerRow = 2
		}
		sheet, err := parseSheetFormat(parts[part], stringsTable, headers[s.Name], caseNumber, headerRow, s.Name != "Glossary")
		if err != nil {
			return Workbook{}, fmt.Errorf("sheet %s: %w", s.Name, err)
		}
		sheet.Name, sheet.Part = s.Name, part
		ids[s.Name] = map[string]bool{}
		for i := range sheet.Rows {
			row := &sheet.Rows[i]
			if row.RecipeID == "" {
				continue
			}
			if ids[s.Name][row.RecipeID] && s.Name != "For_translation" {
				return Workbook{}, fmt.Errorf("duplicate recipe ID %q in %s", row.RecipeID, s.Name)
			}
			ids[s.Name][row.RecipeID] = true
			if s.Name == "Recipe_Master" {
				row.Ingredients, err = ingredients(rowValues(*row))
				if err != nil {
					return Workbook{}, fmt.Errorf("recipe %s row %d: %w", row.RecipeID, row.Number, err)
				}
			}
		}
		w.Sheets = append(w.Sheets, sheet)
	}
	if len(ids["Recipe_Master"]) == 0 || len(ids["Recipe_Master"]) != len(ids["Uniqueness_Audit"]) {
		return Workbook{}, fmt.Errorf("recipe and audit ID sets must be nonempty and equal")
	}
	for id := range ids["Recipe_Master"] {
		if !ids["Uniqueness_Audit"][id] {
			return Workbook{}, fmt.Errorf("recipe %s has no audit row", id)
		}
	}
	return w, nil
}

var cellRef = regexp.MustCompile(`^([A-Z]{1,3})([1-9][0-9]*)$`)
var recipeID = regexp.MustCompile(`^MG-WB-C([0-9]{2})-[0-9]+$`)

func column(ref string, row int) (int, error) {
	m := cellRef.FindStringSubmatch(ref)
	if m == nil || m[2] != strconv.Itoa(row) {
		return 0, fmt.Errorf("invalid cell coordinate %q on row %d", ref, row)
	}
	n := 0
	for _, c := range m[1] {
		n = n*26 + int(c-'A'+1)
	}
	if n > 256 {
		return 0, fmt.Errorf("column limit exceeded: %s", ref)
	}
	return n, nil
}

func parseSheet(data []byte, shared, expected []string, caseNumber int) (Sheet, error) {
	return parseSheetFormat(data, shared, expected, caseNumber, 4, true)
}

func parseSheetFormat(data []byte, shared, expected []string, caseNumber, headerRow int, recipeRows bool) (Sheet, error) {
	var raw struct {
		Rows []struct {
			Number int `xml:"r,attr"`
			Cells  []struct {
				Ref     string   `xml:"r,attr"`
				Type    string   `xml:"t,attr"`
				Value   string   `xml:"v"`
				Inline  richText `xml:"is"`
				Formula *string  `xml:"f"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(data, &raw); err != nil {
		return Sheet{}, fmt.Errorf("read worksheet: %w", err)
	}
	if len(raw.Rows) > 10000 {
		return Sheet{}, fmt.Errorf("worksheet row limit exceeded")
	}
	sheet := Sheet{}
	lastRow, cells := 0, 0
	for _, r := range raw.Rows {
		if r.Number <= lastRow || r.Number > 10000 {
			return Sheet{}, fmt.Errorf("unordered or out-of-bounds row %d", r.Number)
		}
		lastRow = r.Number
		row := Row{Number: r.Number, Cells: make([]Cell, 0, len(r.Cells))}
		lastCol := 0
		for _, c := range r.Cells {
			col, err := column(c.Ref, r.Number)
			if err != nil {
				return Sheet{}, err
			}
			if col <= lastCol || c.Formula != nil {
				return Sheet{}, fmt.Errorf("duplicate/unordered cell or unsupported formula: %s", c.Ref)
			}
			lastCol = col
			v := c.Value
			switch c.Type {
			case "s":
				n, err := strconv.Atoi(c.Value)
				if err != nil || n < 0 || n >= len(shared) {
					return Sheet{}, fmt.Errorf("invalid shared string at %s", c.Ref)
				}
				v = shared[n]
			case "inlineStr":
				v = c.Inline.value()
			case "", "n", "str", "b", "e", "d":
			default:
				return Sheet{}, fmt.Errorf("unsupported cell type %q at %s", c.Type, c.Ref)
			}
			row.Cells = append(row.Cells, Cell{Ref: c.Ref, Type: c.Type, RawValue: c.Value, Value: v})
			cells++
			if cells > 200000 {
				return Sheet{}, fmt.Errorf("worksheet cell limit exceeded")
			}
		}
		values := rowValues(row)
		if r.Number == headerRow {
			if !slices.Equal(values, expected) {
				return Sheet{}, fmt.Errorf("source headers changed at row %d", headerRow)
			}
			sheet.Headers = values
		}
		if r.Number > headerRow && len(values) > 0 {
			if len(values) > len(expected) {
				return Sheet{}, fmt.Errorf("invalid recipe row %d for case %d", r.Number, caseNumber)
			}
			if recipeRows {
				m := recipeID.FindStringSubmatch(values[0])
				if m == nil || m[1] != fmt.Sprintf("%02d", caseNumber) {
					return Sheet{}, fmt.Errorf("invalid recipe row %d for case %d", r.Number, caseNumber)
				}
				row.RecipeID = values[0]
			}
		}
		sheet.Rows = append(sheet.Rows, row)
	}
	if sheet.Headers == nil {
		return Sheet{}, fmt.Errorf("missing header row %d", headerRow)
	}
	return sheet, nil
}

func rowValues(row Row) []string {
	var values []string
	for _, c := range row.Cells {
		n, _ := column(c.Ref, row.Number)
		for len(values) < n {
			values = append(values, "")
		}
		values[n-1] = c.Value
	}
	for len(values) > 0 && values[len(values)-1] == "" {
		values = values[:len(values)-1]
	}
	return values
}

var decimal = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$`)

func ingredients(values []string) ([]Ingredient, error) {
	if len(values) < 14 {
		return nil, fmt.Errorf("missing ingredient columns")
	}
	ids, names, grams := strings.Split(values[11], ";"), strings.Split(values[12], ";"), strings.Split(values[13], ";")
	if len(ids) != len(names) || len(ids) != len(grams) {
		return nil, fmt.Errorf("ingredient list lengths differ")
	}
	items := make([]Ingredient, len(ids))
	for i := range ids {
		g := strings.TrimSpace(grams[i])
		if strings.TrimSpace(ids[i]) == "" || strings.TrimSpace(names[i]) == "" || len(g) > 100 || !decimal.MatchString(g) || strings.Trim(g, "0.") == "" {
			return nil, fmt.Errorf("invalid ingredient occurrence %d", i+1)
		}
		items[i] = Ingredient{Ordinal: i + 1, RawID: ids[i], RawName: names[i], RawGrams: grams[i], Grams: g}
	}
	return items, nil
}
