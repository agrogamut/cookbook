package book

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/madamgy/recipie/internal/bookdocx"
	"golang.org/x/text/unicode/norm"
)

func assertMixedScriptPDFFonts(t *testing.T, pdf []byte, latinFamily string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mixed-script.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("BOOK_READER_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "-")+".pdf"), pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "qpdf", "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("PDF structure: %v: %s", err, output)
	}
	fontList, err := exec.CommandContext(ctx, "pdffonts", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range strings.Split(string(fontList), "\n")[2:] {
		fields := strings.Fields(row)
		if strings.Contains(row, "FreeSans") {
			t.Errorf("unapproved fallback font: %s", row)
		}
		if len(fields) >= 6 && fields[len(fields)-5] != "yes" {
			t.Errorf("font is not embedded: %s", row)
		}
	}
	output, err := exec.CommandContext(ctx, "pdftohtml", "-xml", "-i", "-stdout", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Pages []struct {
			Fonts []struct {
				ID     string `xml:"id,attr"`
				Family string `xml:"family,attr"`
			} `xml:"fontspec"`
			Text []struct {
				Font string `xml:"font,attr"`
				Body string `xml:",innerxml"`
			} `xml:"text"`
		} `xml:"page"`
	}
	if err := xml.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	families := map[string]string{}
	seen := map[string]bool{}
	for _, page := range document.Pages {
		for _, font := range page.Fonts {
			families[font.ID] = font.Family
		}
		for _, run := range page.Text {
			decoder := xml.NewDecoder(strings.NewReader("<text>" + run.Body + "</text>"))
			var text strings.Builder
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if chars, ok := token.(xml.CharData); ok {
					text.Write(chars)
				}
			}
			s := text.String()
			family := families[run.Font]
			for _, script := range []struct {
				name  string
				runes *unicode.RangeTable
			}{{"Bengali", unicode.Bengali}, {"Devanagari", unicode.Devanagari}} {
				if strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(script.runes, r) }) {
					seen[script.name] = true
					if !strings.Contains(family, "Noto") || !strings.Contains(family, script.name) {
						t.Errorf("%s text %q uses %s", script.name, s, family)
					}
				}
			}
			if strings.Contains(s, "LatinRetention") {
				seen["Latin"] = true
				if !strings.Contains(family, latinFamily) {
					t.Errorf("Latin text changed font: %s", family)
				}
			}
		}
	}
	compact := func(s string) string { return strings.Join(strings.Fields(norm.NFC.String(s)), "") }
	all, err := exec.CommandContext(ctx, "pdftotext", "-raw", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"নব্য 72", "व्यक्तिगत", "LatinRetention", "0.005 g"} {
		if !strings.Contains(compact(string(all)), compact(text)) {
			t.Errorf("missing exact text %q", text)
		}
	}
	if !seen["Bengali"] || !seen["Devanagari"] || !seen["Latin"] {
		t.Fatalf("missing script controls: %v", seen)
	}
}

func TestReaderMixedScriptNativeFonts(t *testing.T) {
	if os.Getenv("BOOK_READER_NATIVE") != "1" {
		t.Skip("set BOOK_READER_NATIVE=1 for mixed-script native fonts")
	}
	defer ShutdownBrowser()
	for _, language := range []string{"en", "bn", "hi"} {
		for _, number := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/book%d", language, number), func(t *testing.T) {
				d := readerFixture(t, number, language)
				d.Nodes = []bookdocx.ReaderNode{{Paragraph: &bookdocx.ReaderParagraph{Paragraph: bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: "LatinRetention নব্য 72 व्यक्तिगत 0.005 g"}}}}}}
				child := ChildSummary{DisplayName: "নব্য 72 व्यक्तिगत"}
				opts := ReaderOptions{Book1: &Book1{Child: child}, Book2: &Book2{Child: child}}
				var html bytes.Buffer
				if err := RenderReaderHTML(&html, d, opts); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				meta := Metadata{Language: language}
				footer, err := readerFooter(meta)
				if err != nil {
					t.Fatal(err)
				}
				browser, err := sharedBrowser()
				if err != nil {
					t.Fatal(err)
				}
				pdf, err := printIn(ctx, browser, html.Bytes(), meta, footer)
				if err != nil {
					t.Fatal(err)
				}
				assertMixedScriptPDFFonts(t, pdf, "Poppins")
			})
		}
	}
}

func TestReaderFixedTableNativeContinuation(t *testing.T) {
	if os.Getenv("BOOK_READER_NATIVE") != "1" {
		t.Skip("set BOOK_READER_NATIVE=1 for actual fixed-table continuation")
	}
	defer ShutdownBrowser()
	compact := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, norm.NFC.String(s))
	}
	paragraph := func(p bookdocx.ReaderParagraph) string {
		var s strings.Builder
		for _, run := range p.Runs {
			s.WriteString(run.Text)
		}
		return compact(s.String())
	}
	for _, language := range []string{"en", "bn", "hi"} {
		for _, number := range []int{1, 2} {
			d := readerFixture(t, number, language)
			for index, node := range d.Nodes {
				if node.Table == nil || node.Table.ContextParagraphs == 0 {
					continue
				}
				kind := "contents"
				if node.Table.ContextParagraphs == 2 {
					kind = "faq"
				}
				t.Run(fmt.Sprintf("%s/book%d/%s", language, number, kind), func(t *testing.T) {
					sourceContext := d.Nodes[index-node.Table.ContextParagraphs : index]
					fragment := bookdocx.ReaderDocument{Nodes: d.Nodes[index-node.Table.ContextParagraphs : index+1]}
					body := `<div style="height:80mm">PRECEDING-CONTROL</div>` + string(readerBody(fragment, readerTestLocale(t, language)))
					pdf, _ := readerNativePDF(t, language, body)
					path := filepath.Join(t.TempDir(), "fixed.pdf")
					if err := os.WriteFile(path, pdf, 0600); err != nil {
						t.Fatal(err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					text, err := exec.CommandContext(ctx, "pdftotext", "-raw", path, "-").Output()
					if err != nil {
						t.Fatal(err)
					}
					bbox, err := exec.CommandContext(ctx, "pdftotext", "-bbox", path, "-").Output()
					if err != nil {
						t.Fatal(err)
					}
					var positioned struct {
						Pages []struct {
							Words []struct {
								Text string  `xml:",chardata"`
								X    float64 `xml:"xMin,attr"`
								Y    float64 `xml:"yMin,attr"`
							} `xml:"word"`
						} `xml:"body>doc>page"`
					}
					if err := xml.Unmarshal(bbox, &positioned); err != nil {
						t.Fatal(err)
					}
					firstLabel := paragraph(node.Table.Rows[0].Cells[0][0])
					measured := 0
					for _, page := range positioned.Pages {
						for i, word := range page.Words {
							if compact(word.Text) != firstLabel {
								continue
							}
							for _, next := range page.Words[i+1:] {
								if math.Abs(next.Y-word.Y) < 1 && next.X > word.X+5 {
									// Original first-cell measure is 26% of 170mm plus its 3mm right padding.
									if math.Abs(next.X-word.X-(170*.26+3)*72/25.4) > .75 {
										t.Fatalf("source table column measure changed: %.3fpt", next.X-word.X)
									}
									measured++
									break
								}
							}
						}
					}
					if measured == 0 {
						t.Fatal("no source header column geometry measured")
					}
					var identities []string
					for _, n := range sourceContext {
						identities = append(identities, paragraph(*n.Paragraph))
					}
					for _, cell := range node.Table.Rows[0].Cells {
						for _, p := range cell {
							identities = append(identities, paragraph(p))
						}
					}
					var rows []string
					for _, row := range node.Table.Rows[1:] {
						var s strings.Builder
						for _, p := range row.Cells[len(row.Cells)-1] {
							s.WriteString(paragraph(p))
						}
						rows = append(rows, s.String())
					}
					counts := make([]int, len(rows))
					dataPages := 0
					for pageIndex, page := range strings.Split(strings.TrimSuffix(string(text), "\f"), "\f") {
						page = compact(page)
						data := false
						for i, row := range rows {
							n := strings.Count(page, row)
							counts[i] += n
							data = data || n > 0
						}
						if !data {
							if strings.Contains(page, identities[0]) {
								t.Fatalf("context-only table page %d", pageIndex+1)
							}
							continue
						}
						dataPages++
						for _, identity := range identities {
							if !strings.Contains(page, identity) {
								t.Fatalf("page %d lacks source table context %q", pageIndex+1, identity)
							}
						}
					}
					if dataPages < 2 {
						t.Fatal("control did not exercise a table continuation")
					}
					for i, n := range counts {
						if n != 1 {
							t.Fatalf("source row %d occurs %d times, want once", i+1, n)
						}
					}
				})
			}
		}
	}
}

func readerNativePDF(t *testing.T, language, body string) ([]byte, []string) {
	t.Helper()
	if os.Getenv("BOOK_READER_NATIVE") != "1" {
		t.Skip("set BOOK_READER_NATIVE=1 for actual Chromium pagination")
	}
	if !BrowserAvailable() {
		t.Fatal("Chromium unavailable")
	}
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := templateFS.ReadFile("templates/reader.css")
	if err != nil {
		t.Fatal(err)
	}
	browser, err := sharedBrowser()
	if err != nil {
		t.Fatal(err)
	}
	doc := fmt.Sprintf(`<!doctype html><html lang="%s"><head><meta charset="utf-8"><style>%s%s%s .reader-flow{break-before:auto}</style></head><body class="book2"><main class="reader-flow">%s</main></body></html>`, language, fontFacesCSS, css, overlay, body)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pdf, err := printIn(ctx, browser, []byte(doc), Metadata{}, "<span></span>")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "reader.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("BOOK_READER_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		name := strings.ReplaceAll(t.Name(), "/", "-") + ".pdf"
		if err := os.WriteFile(filepath.Join(dir, name), pdf, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.CommandContext(ctx, "qpdf", "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("PDF structure: %v: %s", err, output)
	}
	out, err := exec.CommandContext(ctx, "pdftotext", "-layout", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	pages := strings.Split(strings.TrimSuffix(string(out), "\f"), "\f")
	for i, page := range pages {
		if strings.TrimSpace(page) == "" {
			t.Fatalf("empty body on page %d", i+1)
		}
	}
	return pdf, pages
}

func TestReaderRecipeHalfPageBoundary(t *testing.T) {
	if os.Getenv("BOOK_READER_NATIVE") != "1" {
		t.Skip("set BOOK_READER_NATIVE=1 for actual Chromium pagination")
	}
	if !BrowserAvailable() {
		t.Fatal("Chromium unavailable")
	}
	defer ShutdownBrowser()
	for _, language := range []string{"en", "bn", "hi"} {
		for _, tc := range []struct {
			name string
			used float64
			page int
		}{{"below", 128, 2}, {"equal", 127.5, 1}, {"above", 127, 1}} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				body := fmt.Sprintf(`<div style="height:%.4fmm">PRECEDING</div><div class="reader-recipe-start"></div><article class="recipe reader-recipe" style="margin:0"><table class="reader-context reader-recipe-context"><thead><tr><th><h3>THRESHOLD-RECIPE</h3></th></tr></thead><tbody><tr><td><p>Exact ingredient 0.005 g</p></td></tr></tbody></table></article>`, tc.used)
				_, pages := readerNativePDF(t, language, body)
				found := 0
				for i, p := range pages {
					if strings.Contains(p, "THRESHOLD-RECIPE") {
						found = i + 1
					}
				}
				if found != tc.page {
					t.Fatalf("recipe begins page %d, want %d; text=%q", found, tc.page, pages)
				}
			})
		}
	}
}

func TestReaderCopyFooterNativeFonts(t *testing.T) {
	if os.Getenv("BOOK_READER_NATIVE") != "1" {
		t.Skip("set BOOK_READER_NATIVE=1 for actual footer fonts")
	}
	if !BrowserAvailable() {
		t.Fatal("Chromium unavailable")
	}
	defer ShutdownBrowser()
	for _, language := range []string{"en", "bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			meta := Metadata{Language: language, BookVersion: "V-0007", ReleaseID: "R-0008", GenerationDate: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}
			footer, err := readerFooter(meta)
			if err != nil {
				t.Fatal(err)
			}
			browser, err := sharedBrowser()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			pdf, err := printIn(ctx, browser, []byte(`<html><body style="font:12pt 'Noto Sans'">FOOTER-CONTROL</body></html>`), meta, footer)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "footer.pdf")
			if err := os.WriteFile(path, pdf, 0600); err != nil {
				t.Fatal(err)
			}
			fonts, err := exec.CommandContext(ctx, "pdffonts", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(fonts), "FreeSans") {
				t.Fatalf("unapproved footer font: %s", fonts)
			}
			if language != "en" {
				family := "NotoSansBengali"
				if language == "hi" {
					family = "NotoSansDevanagari"
				}
				if !strings.Contains(string(fonts), family) {
					t.Fatalf("missing %s: %s", family, fonts)
				}
			}
			text, err := exec.CommandContext(ctx, "pdftotext", "-raw", path, "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(strings.Fields(string(text)), "")
			for _, value := range []string{readerTestLocale(t, language)["footer_generated"], readerTestLocale(t, language)["footer_release"], "V-0007", "R-0008", "2026-10-24"} {
				if !strings.Contains(got, strings.Join(strings.Fields(value), "")) {
					t.Fatalf("footer missing %q: %s", value, text)
				}
			}
		})
	}
}

func TestReaderLongLocalizedContinuation(t *testing.T) {
	defer ShutdownBrowser()
	for _, tc := range []struct{ language, text string }{
		{"en", "Exact source paragraph"},
		{"bn", "ব্যক্তিগত সংস্করণে প্রয়োগ"},
		{"hi", "व्यक्तिगत संस्करण में उपयोग"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			var tall strings.Builder
			for i := 0; i < 90; i++ {
				fmt.Fprintf(&tall, "%s TALL-%03d 0.005 g\n", tc.text, i)
			}
			blocks := []bookdocx.Block{{Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: "RECIPE-CONTEXT"}}}}, {Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: tall.String()}}}}}
			for i := 0; i < 25; i++ {
				blocks = append(blocks, bookdocx.Block{Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: fmt.Sprintf("%s ROW-%03d 12.50 g", tc.text, i)}}}})
			}
			d := bookdocx.ReaderDocument{Nodes: []bookdocx.ReaderNode{{Module: &bookdocx.ReaderModule{Code: "B2-10", Content: bookdocx.Module{Entries: []bookdocx.Entry{{ID: "R-EXACT", Blocks: blocks}}}}}, {Paragraph: &bookdocx.ReaderParagraph{Paragraph: bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: "ORDINARY-FOLLOWING"}}}}}}}
			_, pages := readerNativePDF(t, tc.language, string(readerBody(d, readerTestLocale(t, tc.language))))
			all := strings.Join(pages, "\n")
			for prefix, count := range map[string]int{"TALL": 90, "ROW": 25} {
				for i := 0; i < count; i++ {
					marker := fmt.Sprintf("%s-%03d", prefix, i)
					if strings.Count(all, marker) != 1 {
						t.Fatalf("lost or repeated %s", marker)
					}
				}
			}
			for i, p := range pages {
				if !strings.Contains(p, "TALL-") && !strings.Contains(p, "ROW-") && !strings.Contains(p, "ORDINARY-FOLLOWING") {
					t.Fatalf("heading-only page %d: %q", i+1, p)
				}
				if strings.Contains(p, "TALL-") || strings.Contains(p, "ROW-") {
					if !strings.Contains(p, "RECIPE-CONTEXT") {
						t.Fatalf("continuation identity absent page %d", i+1)
					}
				}
			}
			last := pages[len(pages)-1]
			if !strings.Contains(last, "ROW-024") || !strings.Contains(last, "ORDINARY-FOLLOWING") {
				t.Fatalf("ordinary section did not continue after final row: %q", last)
			}
		})
	}
}

func TestReaderShortWritingFormStaysTogether(t *testing.T) {
	defer ShutdownBrowser()
	for _, language := range []string{"en", "bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			d := bookdocx.ReaderDocument{Nodes: []bookdocx.ReaderNode{{Paragraph: &bookdocx.ReaderParagraph{Style: "Heading2", Paragraph: bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: "WRITING-FORM"}}}}}}}
			for i := 1; i <= 5; i++ {
				d.Nodes = append(d.Nodes, bookdocx.ReaderNode{Paragraph: &bookdocx.ReaderParagraph{Paragraph: bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: fmt.Sprintf("%d.  %s", i, strings.Repeat("_", 74))}}}}})
			}
			_, pages := readerNativePDF(t, language, `<div style="height:205mm">PRECEDING-CONTENT</div>`+string(readerBody(d, readerTestLocale(t, language))))
			if len(pages) != 2 || strings.Contains(pages[0], "WRITING-FORM") {
				t.Fatalf("form did not move intact: %q", pages)
			}
			for _, marker := range []string{"WRITING-FORM", "1.", "2.", "3.", "4.", "5."} {
				if !strings.Contains(pages[1], marker) {
					t.Fatalf("form lost %q", marker)
				}
			}
		})
	}
}

func TestReaderShortRecordStaysTogether(t *testing.T) {
	defer ShutdownBrowser()
	for _, language := range []string{"en", "bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			body := `<div style="height:240mm">PRECEDING-CONTENT</div>` + readerTable(bookdocx.Table{Rows: [][]string{{"SOURCE-RECORD", "EXACT-CONDITION", "EXACT-ACTION", "EXACT-GROUP"}}})
			_, pages := readerNativePDF(t, language, body)
			if len(pages) != 2 || strings.Contains(pages[0], "SOURCE-RECORD") {
				t.Fatalf("record did not move intact: %q", pages)
			}
			for _, marker := range []string{"SOURCE-RECORD", "EXACT-CONDITION", "EXACT-ACTION", "EXACT-GROUP"} {
				if !strings.Contains(pages[1], marker) {
					t.Fatalf("record lost %q", marker)
				}
			}
		})
	}
}
