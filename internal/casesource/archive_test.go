package casesource

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

const deliveryHash = "3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2"

func delivery(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../data/case-source/" + deliveryHash + ".zip")
	if err != nil {
		t.Fatal(err)
	}
	if hash(b) != deliveryHash {
		t.Fatal("delivery bytes changed")
	}
	return b
}

func TestDelivery(t *testing.T) {
	b := delivery(t)
	a, err := Parse(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*zip.File{}
	for _, f := range z.File {
		byName[f.Name] = f
	}
	ids := map[string]bool{}
	var sheets, recipes, audits, occurrences, nonempty, rawCells, rows, fractions int
	conflicts := map[string]bool{}
	for _, w := range a.Workbooks {
		member, err := readMember(byName[w.Name])
		if err != nil || hash(member) != w.SHA256 {
			t.Fatalf("member hash %s: %v", w.Name, err)
		}
		f, err := excelize.OpenReader(bytes.NewReader(member), excelize.Options{RawCellValue: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range w.Sheets {
			sheets++
			oracle, err := f.GetRows(s.Name, excelize.Options{RawCellValue: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range s.Rows {
				rows++
				for _, c := range r.Cells {
					rawCells++
					col, row, err := excelize.CellNameToCoordinates(c.Ref)
					if err != nil {
						t.Fatal(err)
					}
					var expected string
					if row <= len(oracle) && col <= len(oracle[row-1]) {
						expected = oracle[row-1][col-1]
					}
					if c.Value != expected {
						t.Fatalf("%s/%s/%s: value %q differs from spreadsheet reader %q", w.Name, s.Name, c.Ref, c.Value, expected)
					}
					if c.Value != "" {
						nonempty++
					}
				}
				if r.RecipeID == "" {
					continue
				}
				if s.Name == "Uniqueness_Audit" {
					audits++
					continue
				}
				recipes++
				if ids[r.RecipeID] {
					t.Fatal("duplicate recipe ID", r.RecipeID)
				}
				ids[r.RecipeID] = true
				v := rowValues(r)
				if strings.Contains(v[17], ".") {
					fractions++
				}
				for _, i := range r.Ingredients {
					occurrences++
					if strings.TrimSpace(i.RawID) == "WBING0070" {
						conflicts[strings.TrimSpace(i.RawName)] = true
					}
				}
				if r.RecipeID == "MG-WB-C26-001" {
					if r.Number != 5 {
						t.Fatal("case 26 row moved")
					}
					var repeated []Ingredient
					for _, i := range r.Ingredients {
						if strings.TrimSpace(i.RawID) == "WBING0080" {
							repeated = append(repeated, i)
						}
					}
					if len(repeated) != 2 || repeated[0].Ordinal == repeated[1].Ordinal || strings.TrimSpace(repeated[0].RawName) != "Cumin" || strings.TrimSpace(repeated[1].RawName) != "Potable water" {
						t.Fatalf("case 26 collision lost: %+v", repeated)
					}
					t.Logf("case26 row5 occurrences: %+v", repeated)
				}
			}
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.Workbooks) != 60 || sheets != 120 || recipes != 6000 || audits != 6000 || len(ids) != 6000 || occurrences != 60106 || nonempty != 285060 || fractions != 1038 {
		t.Fatalf("delivery totals: workbooks=%d sheets=%d recipes=%d audits=%d IDs=%d occurrences=%d nonempty=%d fractional servings=%d", len(a.Workbooks), sheets, recipes, audits, len(ids), occurrences, nonempty, fractions)
	}
	if len(conflicts) != 2 || !conflicts["Mustard oil"] || !conflicts["Maize flour, certified gluten-free"] {
		t.Fatalf("WBING0070 meanings: %v", conflicts)
	}
	for c := 1; c <= 60; c++ {
		for r := 1; r <= 100; r++ {
			if !ids[fmt.Sprintf("MG-WB-C%02d-%03d", c, r)] {
				t.Fatalf("missing case %d recipe %d", c, r)
			}
		}
	}
	t.Logf("SOURCE_FIDELITY_OK workbooks=%d sheets=%d recipes=%d audits=%d occurrences=%d rows=%d cells=%d nonempty=%d fractional_servings=%d", len(a.Workbooks), sheets, recipes, audits, occurrences, rows, rawCells, nonempty, fractions)
}

func fixtureWorkbook(t *testing.T, edit func(*excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", "Recipe_Master"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.NewSheet("Uniqueness_Audit"); err != nil {
		t.Fatal(err)
	}
	var headers map[string][]string
	if err := json.Unmarshal(sourceHeaders, &headers); err != nil {
		t.Fatal(err)
	}
	for name, values := range headers {
		for i, v := range values {
			ref, _ := excelize.CoordinatesToCellName(i+1, 4)
			setCell(t, f, name, ref, v)
		}
		setCell(t, f, name, "A5", "MG-WB-C26-001")
	}
	setCell(t, f, "Recipe_Master", "L5", "WBING0080; WBING0080")
	setCell(t, f, "Recipe_Master", "M5", "Cumin; Potable water")
	setCell(t, f, "Recipe_Master", "N5", "0.125; 12.50")
	setCell(t, f, "Recipe_Master", "AK5", "source review text")
	if edit != nil {
		edit(f)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func setCell(t *testing.T, f *excelize.File, sheet, ref string, value any) {
	t.Helper()
	if err := f.SetCellValue(sheet, ref, value); err != nil {
		t.Fatal(err)
	}
}

type zipEntry struct {
	name string
	data []byte
	mode os.FileMode
}

func zipBytes(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const fixtureMember = "MadamGY_WB_Case_26_fixture.xlsx"

func fixtureArchive(t *testing.T, edit func(*excelize.File)) []byte {
	t.Helper()
	return zipBytes(t, zipEntry{name: fixtureMember, data: fixtureWorkbook(t, edit)})
}

func TestParseRejection(t *testing.T) {
	valid := fixtureArchive(t, nil)
	if _, err := Parse(context.Background(), valid); err != nil {
		t.Fatalf("positive control: %v", err)
	}
	workbook := fixtureWorkbook(t, nil)
	for _, name := range []string{"../escape.xlsx", "/absolute.xlsx", "a/../escape.xlsx", `a\escape.xlsx`, "C:drive.xlsx", "unexpected.txt"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(context.Background(), zipBytes(t, zipEntry{name: name, data: workbook})); err == nil {
				t.Fatal("unsafe member accepted")
			}
		})
	}
	for name, input := range map[string][]byte{
		"invalid ZIP":         []byte("invalid"),
		"empty ZIP":           zipBytes(t),
		"duplicate member":    zipBytes(t, zipEntry{name: fixtureMember, data: workbook}, zipEntry{name: fixtureMember, data: workbook}),
		"duplicate case":      zipBytes(t, zipEntry{name: fixtureMember, data: workbook}, zipEntry{name: "MadamGY_WB_Case_26_other.xlsx", data: workbook}),
		"symlink":             zipBytes(t, zipEntry{name: fixtureMember, data: workbook, mode: os.ModeSymlink | 0777}),
		"corrupt workbook":    zipBytes(t, zipEntry{name: fixtureMember, data: []byte("invalid")}),
		"oversized expansion": zipBytes(t, zipEntry{name: fixtureMember, data: bytes.Repeat([]byte{0}, maxMemberBytes+1)}),
		"nested traversal":    zipBytes(t, zipEntry{name: fixtureMember, data: zipBytes(t, zipEntry{name: "../escape", data: []byte("x")})}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(context.Background(), input); err == nil {
				t.Fatal("malformed source accepted")
			}
		})
	}
	for name, edit := range map[string]func(*excelize.File){
		"header":             func(f *excelize.File) { setCell(t, f, "Recipe_Master", "B4", "Changed") },
		"audit ID":           func(f *excelize.File) { setCell(t, f, "Uniqueness_Audit", "A5", "MG-WB-C26-002") },
		"wrong case":         func(f *excelize.File) { setCell(t, f, "Recipe_Master", "A5", "MG-WB-C25-001") },
		"ingredient lengths": func(f *excelize.File) { setCell(t, f, "Recipe_Master", "L5", "WBING0080") },
		"negative grams":     func(f *excelize.File) { setCell(t, f, "Recipe_Master", "N5", "1; -2") },
		"duplicate ID":       func(f *excelize.File) { setCell(t, f, "Recipe_Master", "A6", "MG-WB-C26-001") },
		"extra sheet": func(f *excelize.File) {
			if _, err := f.NewSheet("Other"); err != nil {
				t.Fatal(err)
			}
		},
		"missing sheet": func(f *excelize.File) {
			if err := f.DeleteSheet("Uniqueness_Audit"); err != nil {
				t.Fatal(err)
			}
		},
		"formula": func(f *excelize.File) {
			if err := f.SetCellFormula("Recipe_Master", "R5", "1+2"); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(context.Background(), fixtureArchive(t, edit)); err == nil {
				t.Fatal("malformed workbook accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Parse(ctx, valid); err == nil {
		t.Fatal("cancelled parse accepted")
	}
	if _, err := Read(io.LimitReader(zeroReader{}, MaxArchiveBytes+1)); err == nil {
		t.Fatal("oversized archive accepted")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestIngredients(t *testing.T) {
	values := make([]string, 14)
	values[11], values[12], values[13] = " ID; ID", " Name, with comma; Other ", " .125; 12.50000000000000000001 "
	got, err := ingredients(values)
	want := []Ingredient{{1, " ID", " Name, with comma", " .125", ".125"}, {2, " ID", " Other ", " 12.50000000000000000001 ", "12.50000000000000000001"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("exact occurrences: %+v %v", got, err)
	}
	for _, g := range []string{"NaN", "Inf", "0", "-1", "1e3", "1,000", "", "1/2"} {
		values[13] = g + "; 1"
		if _, err := ingredients(values); err == nil {
			t.Fatalf("unsupported quantity %q accepted", g)
		}
	}
}

func TestParseZIPGuards(t *testing.T) {
	valid := fixtureArchive(t, nil)
	for _, tc := range []struct {
		name   string
		offset int
		change func([]byte)
		want   string
	}{
		{"encrypted", 8, func(b []byte) { binary.LittleEndian.PutUint16(b, binary.LittleEndian.Uint16(b)|1) }, "unsupported ZIP"},
		{"compression", 10, func(b []byte) { binary.LittleEndian.PutUint16(b, 99) }, "unsupported ZIP"},
		{"checksum", 16, func(b []byte) { b[0] ^= 1 }, "checksum"},
		{"size", 24, func(b []byte) { binary.LittleEndian.PutUint32(b, maxMemberBytes+1) }, "size exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := bytes.Clone(valid)
			central := bytes.Index(b, []byte{'P', 'K', 1, 2})
			if central < 0 {
				t.Fatal("fixture missing central directory")
			}
			tc.change(b[central+tc.offset:])
			if _, err := Parse(context.Background(), b); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q rejection, got %v", tc.want, err)
			}
		})
	}
	var entries []zipEntry
	for i := 0; i < 129; i++ {
		entries = append(entries, zipEntry{name: fmt.Sprintf("%d.xlsx", i)})
	}
	if _, err := Parse(context.Background(), zipBytes(t, entries...)); err == nil || !strings.Contains(err.Error(), "member count") {
		t.Fatalf("member count limit: %v", err)
	}
	remaining := uint64(1)
	if _, err := parseWorkbook(fixtureWorkbook(t, nil), 26, &remaining); err == nil || !strings.Contains(err.Error(), "delivery expanded size") {
		t.Fatalf("cumulative expansion limit: %v", err)
	}
}
