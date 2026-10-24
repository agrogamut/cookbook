package bookdocx

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/bookmasters"
)

func TestNativeImages(t *testing.T) {
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
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewConverter(ConverterOptions{TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "bn", "hi"} {
		for _, book := range []int{1, 2} {
			t.Run(fmt.Sprintf("book%d-%s", book, lang), func(t *testing.T) {
				r := synthetic(t, book, lang)
				key := fmt.Sprintf("B%d-01", book)
				m := r.Modules[key]
				m.Blocks = append(m.Blocks, imageBlocks(t)...)
				r.Modules[key] = m
				result, err := c.Render(context.Background(), r)
				if err != nil {
					t.Fatal(err)
				}
				stem := filepath.Join(dir, fmt.Sprintf("book%d-%s-images", book, lang))
				if err := os.WriteFile(stem+".docx", result.DOCX, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(stem+".pdf", result.PDF, 0600); err != nil {
					t.Fatal(err)
				}
				text := command(t, "pdftotext", "-layout", stem+".pdf", "-")
				if err := checkFixtureHeaders(text); err != nil {
					t.Error(err)
				}
				if !strings.Contains(text, "SYN-RECIPE-0011") || !strings.Contains(text, "0.005 g") {
					t.Fatal("image insertion lost long content")
				}
				if err := checkPageBounds(command(t, "pdftotext", "-bbox", stem+".pdf", "-")); err != nil {
					t.Fatal(err)
				}
				if err := validatePDFFonts(command(t, "pdffonts", stem+".pdf")); err != nil {
					t.Fatal(err)
				}
				// Explicit output prefix confines extracted pictures to the test's temporary directory.
				layout := command(t, "pdftohtml", "-q", "-xml", "-hidden", "-zoom", "1", "-stdout", stem+".pdf", filepath.Join(t.TempDir(), "layout"))
				left, right := sourceImageBounds(t, book, lang)
				pages, err := checkPDFImages(layout, 3, left, right)
				if err != nil {
					t.Fatal(err)
				}
				for i, page := range pages {
					command(t, "pdftoppm", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-scale-to", "1500", "-singlefile", "-png", stem+".pdf", fmt.Sprintf("%s-%d", stem, i+1))
				}
				inventory := command(t, "pdfimages", "-list", stem+".pdf")
				count := 0
				for _, line := range strings.Split(inventory, "\n") {
					f := strings.Fields(line)
					if len(f) > 5 && f[2] == "image" {
						count++
						if f[3] != "60" || f[4] != "40" {
							t.Fatal("image pixel dimensions changed", line)
						}
					}
				}
				if count != 3 {
					t.Fatal("expected three embedded PDF images", inventory)
				}
				t.Logf("%s: DOCX=%d PDF=%d image-pages=%v image-count=%d", filepath.Base(stem), len(result.DOCX), len(result.PDF), pages, count)
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				if _, err := c.PDF(ctx, result.Document); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("image PDF cancellation", err)
				}
				assertClean(t, c)
			})
		}
	}
}

func sourceImageBounds(t *testing.T, book int, lang string) (float64, float64) {
	t.Helper()
	src, _, err := bookmasters.Read(book, lang, "v1")
	if err != nil {
		t.Fatal(err)
	}
	root, err := parseXML(parts(t, src)["word/document.xml"])
	if err != nil {
		t.Fatal(err)
	}
	body := root.direct("document")[0].direct("body")[0]
	margin, err := strconv.Atoi(body.direct("sectPr")[0].direct("pgMar")[0].attr("left"))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := Bindings(book, lang, "v1")
	table := body.direct("tbl")[m.Bindings[0].Table-1]
	cols := table.direct("tblGrid")[0].direct("gridCol")
	left := margin
	for _, col := range cols[:2] {
		width, err := strconv.Atoi(col.attr("w"))
		if err != nil {
			t.Fatal(err)
		}
		left += width
	}
	width, err := strconv.Atoi(cols[2].attr("w"))
	if err != nil {
		t.Fatal(err)
	}
	// Writer aligns the table's first text edge with the page margin, allowing half a cell margin.
	return float64(left-108) / 20, float64(left+width) / 20
}

func checkPDFImages(layout string, want int, left, right float64) ([]int, error) {
	var doc struct {
		Pages []struct {
			Number int     `xml:"number,attr"`
			Width  float64 `xml:"width,attr"`
			Height float64 `xml:"height,attr"`
			Images []struct {
				Left   float64 `xml:"left,attr"`
				Top    float64 `xml:"top,attr"`
				Width  float64 `xml:"width,attr"`
				Height float64 `xml:"height,attr"`
			} `xml:"image"`
		} `xml:"page"`
	}
	if err := xml.NewDecoder(bytes.NewBufferString(layout)).Decode(&doc); err != nil {
		return nil, err
	}
	var pages []int
	count := 0
	for _, page := range doc.Pages {
		if len(page.Images) > 0 {
			pages = append(pages, page.Number)
		}
		for _, img := range page.Images {
			count++
			if img.Left < left-1 || img.Top < 36 || img.Width <= 0 || img.Height <= 0 || img.Width > 109 || img.Height > 73 || img.Left+img.Width > right+1 || img.Left+img.Width > page.Width-36 || img.Top+img.Height > page.Height-36 {
				return nil, fmt.Errorf("image outside source cell or page bounds: %+v page %d", img, page.Number)
			}
		}
	}
	if count != want {
		return nil, fmt.Errorf("PDF image count %d, expected %d", count, want)
	}
	return pages, nil
}

func TestImagesPDFBoundsOracle(t *testing.T) {
	good := `<pdf2xml><page number="1" width="612" height="792"><image left="350" top="100" width="108" height="72"/></page></pdf2xml>`
	if _, err := checkPDFImages(good, 1, 300, 560); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.ReplaceAll(good, `left="350"`, `left="20"`), strings.ReplaceAll(good, `top="100"`, `top="780"`), strings.ReplaceAll(good, `width="108"`, `width="200"`)} {
		if _, err := checkPDFImages(bad, 1, 300, 560); err == nil {
			t.Fatal("bad image bounds accepted")
		}
	}
	if _, err := checkPDFImages(good, 2, 300, 560); err == nil {
		t.Fatal("lost image accepted")
	}
}
