package bookdocx

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/bookmasters"
)

func command(t *testing.T, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v: %s", name, err, b)
	}
	return string(b)
}

func synthetic(t *testing.T, book int, lang string) Request {
	r := request(t, book, lang)
	for code := range r.Modules {
		r.Modules[code] = Module{Blocks: []Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "SYNTHETIC-" + code + " 00042 12.50 g"}}}}}}
	}
	gap := fmt.Sprintf("B%d-61", book)
	if book == 2 {
		gap = "B2-55"
	}
	r.Modules[gap] = Module{Gap: "SOURCE GAP: FAQ answers were not supplied."}
	src, _, err := bookmasters.Read(book, lang, "v1")
	if err != nil {
		t.Fatal(err)
	}
	root, err := parseXML(parts(t, src)["word/document.xml"])
	if err != nil {
		t.Fatal(err)
	}
	var probes []Run
	root.walk(func(n *node) {
		if len(probes) < 6 && n.is("tc") && strings.ContainsAny(n.words(), "্्") && len(n.words()) < 250 {
			probes = append(probes, Run{Text: n.words() + "\n", Bold: len(probes)%2 == 0})
		}
	})
	first := fmt.Sprintf("B%d-01", book)
	m := r.Modules[first]
	if len(probes) > 0 {
		m.Blocks = append(m.Blocks, Block{Paragraph: &Paragraph{Runs: probes}})
		r.Modules[first] = m
	}
	repeat := fmt.Sprintf("B%d-19", book)
	if book == 2 {
		repeat = "B2-09"
	}
	m = r.Modules[repeat]
	for i := range 12 {
		m.Entries = append(m.Entries, Entry{ID: fmt.Sprintf("SYN-RECIPE-%04d", i), Blocks: []Block{
			{Paragraph: &Paragraph{Runs: []Run{{Text: strings.Repeat("Synthetic layout probe. ", 18)}}}},
			{Table: &Table{Header: true, Rows: [][]string{{"Fixture ID", "Exact quantity"}, {fmt.Sprintf("SYN-ING-%04d", i), "12.50 g"}, {"00042", "0.005 g"}}}},
		}})
	}
	r.Modules[repeat] = m
	return r
}

func TestNative(t *testing.T) {
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
	c, err := NewConverter(ConverterOptions{TempDir: t.TempDir(), MaxConcurrent: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(strings.TrimSpace(command(t, "libreoffice", "--version")))
	var wg sync.WaitGroup
	for _, lang := range []string{"en", "bn", "hi"} {
		for _, book := range []int{1, 2} {
			t.Run(fmt.Sprintf("book%d-%s", book, lang), func(t *testing.T) {
				r := synthetic(t, book, lang)
				start := time.Now()
				result, err := c.Render(context.Background(), r)
				if err != nil {
					t.Fatal(err)
				}
				stem := filepath.Join(dir, fmt.Sprintf("book%d-%s", book, lang))
				if err := os.WriteFile(stem+".docx", result.DOCX, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(stem+".pdf", result.PDF, 0600); err != nil {
					t.Fatal(err)
				}
				text := command(t, "pdftotext", "-layout", stem+".pdf", "-")
				for _, marker := range []string{fmt.Sprintf("SYNTHETIC-B%d-01", book), "SYN-RECIPE-0000", "SYN-RECIPE-0011", "12.50 g", "0.005 g", "SOURCE GAP"} {
					if !strings.Contains(text, marker) {
						t.Errorf("PDF lost marker %q", marker)
					}
				}
				if strings.ContainsRune(text, '\uFFFD') {
					t.Error("replacement glyph in PDF extraction")
				}
				for code, mod := range r.Modules {
					if mod.Gap == "" && !strings.Contains(text, "SYNTHETIC-"+code) {
						t.Errorf("module missing in PDF: %s", code)
					}
				}
				info := command(t, "pdfinfo", stem+".pdf")
				if !strings.Contains(info, "612 x 792") {
					t.Error("source Letter page size changed", info)
				}
				fontInfo := command(t, "pdffonts", stem+".pdf")
				if err := validatePDFFonts(fontInfo); err != nil {
					t.Fatal(err)
				}
				if lang == "bn" && !strings.Contains(fontInfo, "NotoSansBengali") || lang == "hi" && !strings.Contains(fontInfo, "NotoSansDevanagari") {
					t.Fatal("expected script font absent", fontInfo)
				}
				bbox := command(t, "pdftotext", "-bbox", stem+".pdf", "-")
				if err := checkPageBounds(bbox); err != nil {
					t.Fatal(err)
				}
				if err := checkQuantityColumns(bbox, 12); err != nil {
					t.Fatal(err)
				}
				pages := strings.Split(strings.TrimSuffix(text, "\f"), "\f")
				repeatPage := 1
				for i, page := range pages {
					if strings.Contains(page, "SYN-RECIPE-0011") {
						repeatPage = i + 1
					}
				}
				probePage := 1
				for i, page := range pages {
					if strings.Contains(page, fmt.Sprintf("SYNTHETIC-B%d-01", book)) {
						probePage = i + 1
					}
				}
				for label, page := range map[string]int{"first": 1, "probe": probePage, "repeat": repeatPage, "last": len(pages)} {
					command(t, "pdftoppm", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-scale-to", "1500", "-singlefile", "-png", stem+".pdf", stem+"-"+label)
				}
				if book == 2 && lang != "en" {
					again, err := c.Render(context.Background(), r)
					if err != nil {
						t.Fatal(err)
					}
					repeat := filepath.Join(t.TempDir(), "repeat")
					if err := os.WriteFile(repeat+".pdf", again.PDF, 0600); err != nil {
						t.Fatal(err)
					}
					command(t, "pdftoppm", "-f", strconv.Itoa(probePage), "-l", strconv.Itoa(probePage), "-scale-to", "1500", "-singlefile", "-png", repeat+".pdf", repeat)
					a, err := os.ReadFile(stem + "-probe.png")
					if err != nil {
						t.Fatal(err)
					}
					b, err := os.ReadFile(repeat + ".png")
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(a, b) {
						t.Error("repeated source-derived probe page changed pixels")
					}
				}
				t.Logf("%s: pages=%d DOCX=%d PDF=%d duration=%s repeat-page=%d fonts=%s", filepath.Base(stem), len(pages), len(result.DOCX), len(result.PDF), time.Since(start).Round(time.Millisecond), repeatPage, strings.Join(strings.Fields(fontInfo), " "))
			})
		}
	}
	// Two actual native processes must complete with separate profiles.
	for _, lang := range []string{"bn", "hi"} {
		r := request(t, 1, lang)
		wg.Go(func() {
			if _, err := c.Render(context.Background(), r); err != nil {
				t.Error("concurrent native conversion", err)
			}
		})
	}
	wg.Wait()
	doc, err := Fill(context.Background(), request(t, 2, "bn"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.PDF(ctx, doc); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("native subprocess cancellation", err)
	}
	assertClean(t, c)
}

func checkPageBounds(bbox string) error {
	d := xml.NewDecoder(strings.NewReader(bbox))
	width, height := 0.0, 0.0
	words := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		n, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := map[string]float64{}
		for _, a := range n.Attr {
			v, _ := strconv.ParseFloat(a.Value, 64)
			attrs[a.Name.Local] = v
		}
		if n.Name.Local == "page" {
			width, height = attrs["width"], attrs["height"]
		}
		if n.Name.Local == "word" {
			words++
			if width <= 0 || height <= 0 || attrs["xMin"] < 0 || attrs["yMin"] < 0 || attrs["xMax"] > width+0.5 || attrs["yMax"] > height+0.5 {
				return fmt.Errorf("text outside PDF page: %+v", attrs)
			}
		}
	}
	if words == 0 {
		return fmt.Errorf("PDF contains no positioned text")
	}
	return nil
}

func TestContentBoundsOracle(t *testing.T) {
	good := `<doc><page width="100" height="100"><word xMin="1" yMin="2" xMax="20" yMax="30">test</word></page></doc>`
	if err := checkPageBounds(good); err != nil {
		t.Fatal(err)
	}
	if err := checkPageBounds(strings.ReplaceAll(good, `xMax="20"`, `xMax="120"`)); err == nil {
		t.Fatal("out-of-page text accepted")
	}
}

type positionedWord struct {
	Text string  `xml:",chardata"`
	XMin float64 `xml:"xMin,attr"`
	XMax float64 `xml:"xMax,attr"`
	YMin float64 `xml:"yMin,attr"`
}

func checkQuantityColumns(bbox string, want int) error {
	d := xml.NewDecoder(strings.NewReader(bbox))
	var page []positionedWord
	ids, quantities := 0, 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch n := token.(type) {
		case xml.StartElement:
			if n.Name.Local == "page" {
				page = nil
			}
			if n.Name.Local == "word" {
				var word positionedWord
				if err := d.DecodeElement(&word, &n); err != nil {
					return err
				}
				page = append(page, word)
			}
		case xml.EndElement:
			if n.Name.Local != "page" {
				continue
			}
			for _, word := range page {
				if strings.HasPrefix(word.Text, "SYN-ING-") {
					ids++
					if len(word.Text) != 12 {
						return fmt.Errorf("fixture ID visually joined to adjacent column: %q", word.Text)
					}
				}
				if word.Text != "0.005" {
					continue
				}
				quantities++
				separate := false
				for _, other := range page {
					if other.Text == "00042" && math.Abs(other.YMin-word.YMin) < 1 && word.XMin-other.XMin >= 80 && word.XMin-other.XMax >= 8 {
						separate = true
					}
				}
				if !separate {
					return fmt.Errorf("quantity column collapsed at x=%.2f y=%.2f", word.XMin, word.YMin)
				}
			}
		}
	}
	if ids != want || quantities != want {
		return fmt.Errorf("lost or joined table text: IDs=%d quantities=%d expected=%d", ids, quantities, want)
	}
	return nil
}

func TestContentColumnOracle(t *testing.T) {
	good := `<doc><page><word xMin="10" xMax="50" yMin="10">SYN-ING-0000</word><word xMin="10" xMax="40" yMin="20">00042</word><word xMin="110" xMax="140" yMin="20">0.005</word></page></doc>`
	if err := checkQuantityColumns(good, 1); err != nil {
		t.Fatal(err)
	}
	if err := checkQuantityColumns(strings.ReplaceAll(good, `xMin="110"`, `xMin="40"`), 1); err == nil {
		t.Fatal("collapsed columns accepted")
	}
}
