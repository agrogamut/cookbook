package bookdocx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNativeTallRows(t *testing.T) {
	if os.Getenv("BOOKDOCX_INTEGRATION") != "1" {
		t.Skip("set BOOKDOCX_INTEGRATION=1 for real local conversion")
	}
	dir := os.Getenv("BOOKDOCX_ARTIFACT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	c, err := NewConverter(ConverterOptions{TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "bn", "hi"} {
		t.Run(lang, func(t *testing.T) {
			r := request(t, 2, lang)
			var lines []string
			for i := range 96 {
				lines = append(lines, fmt.Sprintf("ROW-LINE-%03d 0.005 g", i))
			}
			rows := [][]string{{"MULTIROW HEADER", "Exact quantity"}}
			for i := range 48 {
				rows = append(rows, []string{fmt.Sprintf("SMALL-ROW-%03d", i), "12.50 g"})
			}
			r.Modules["B2-09"] = Module{Blocks: []Block{{Table: &Table{Header: true, Rows: [][]string{
				{"TALL ROW HEADER", "Exact quantity"},
				{"TALL-ROW-0001", strings.Join(lines, "\n")},
				{"FINAL-ROW-0002", "12.50 g"},
			}}}, {Table: &Table{Header: true, Rows: rows}}}}
			result, err := c.Render(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			stem := filepath.Join(dir, "book2-"+lang+"-tall-row")
			if err := os.WriteFile(stem+".docx", result.DOCX, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(stem+".pdf", result.PDF, 0600); err != nil {
				t.Fatal(err)
			}
			text := command(t, "pdftotext", "-layout", stem+".pdf", "-")
			for i := range 96 {
				id := fmt.Sprintf("ROW-LINE-%03d", i)
				if strings.Count(text, id) != 1 {
					t.Errorf("long row lost or duplicated %s", id)
				}
			}
			if strings.Count(text, "FINAL-ROW-0002") != 1 || strings.Count(text, "0.005 g") != 96 || !strings.Contains(text, "12.50 g") {
				t.Error("long row lost quantities or final row")
			}
			if strings.Count(text, "MULTIROW HEADER") < 2 {
				t.Error("useful continuation header missing")
			}
			for i := range 48 {
				id := fmt.Sprintf("SMALL-ROW-%03d", i)
				if strings.Count(text, id) != 1 {
					t.Errorf("multirow table lost or duplicated %s", id)
				}
			}
			if strings.Count(text, "12.50 g") != 49 {
				t.Error("multirow table lost or duplicated quantities")
			}
			if err := checkPageBounds(command(t, "pdftotext", "-bbox", stem+".pdf", "-")); err != nil {
				t.Fatal(err)
			}
			if err := validatePDFFonts(command(t, "pdffonts", stem+".pdf")); err != nil {
				t.Fatal(err)
			}
			pages := strings.Split(text, "\f")
			for i, page := range pages {
				if strings.Contains(page, "SMALL-ROW-") && !strings.Contains(page, "MULTIROW HEADER") {
					t.Error("multirow continuation has no header")
				}
				if strings.Contains(page, "ROW-LINE-095") {
					command(t, "pdftoppm", "-f", strconv.Itoa(i+1), "-l", strconv.Itoa(i+1), "-scale-to", "1500", "-singlefile", "-png", stem+".pdf", stem+"-last")
				}
			}
			t.Logf("%s: 96 exact tall-row lines, 48 exact short rows; multirow headers=%d; tall-row headers=%d", lang, strings.Count(text, "MULTIROW HEADER"), strings.Count(text, "TALL ROW HEADER"))
		})
	}
	assertClean(t, c)
}
