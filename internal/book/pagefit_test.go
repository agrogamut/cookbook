package book

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/profile"
)

// Guards on the printed page itself: how full a sheet is, and what it opens with.
//
// This file exists because `rendered + reported == total` held perfectly while Book 1 printed
// nearly empty. Conservation accounting cannot see a sheet with four lines on it -- the four
// lines are rendered, nothing is missing, every count agrees. Eighteen of Book 1's forty-seven
// pages ended above 62% of the text block, one of them at 2%, and the suite was green.
//
// So these read the printed document. They need poppler's pdftotext and a Chromium, and skip
// without either -- the same contract the rest of this package has with TEST_DATABASE_URL. A
// skipped guard is not a passing guard; run them with both before calling layout work done.

// Page geometry in PostScript points, matching tokens.css and the print margins in pdf.go.
// A4 is 841.89pt tall; the 22mm top and 20mm bottom margins put the text block between these.
const (
	textBlockTopPt    = 62.36  // 22mm
	textBlockBottomPt = 785.20 // 297mm - 20mm
	pageWidthPt       = 595.28 // A4 portrait
)

// underfillThreshold is the fraction of the text block a page must reach to count as full.
//
// 0.62 is not a taste call. Below it a reader turning the page sees more paper than text, which
// is the state eighteen of Book 1's pages shipped in, and it is far enough below a normally set
// page (the recipe pages measure 0.86) that a page failing it is failing for a structural
// reason rather than because a paragraph happened to end early.
const underfillThreshold = 0.62

// maxUnderfilledPages is a budget, not a target, and it records the state the code is actually
// in. Each task that improves page filling lowers it; it is never set above what was measured.
//
// A budget rather than a per-page allowlist because page numbers move whenever a break moves,
// and an allowlist keyed on them would need rewriting after every change. A budget also keeps
// the legitimate cases -- covers, chapter openers, the imprint, a contents tail -- from needing
// to be enumerated and argued about one at a time.
//
// History, so a later reader can see whether a change helped:
//
//	21  both books, before any of this work
const maxUnderfilledPages = 21

// maxPagesOpeningOnAnOrphan is the same kind of budget for the other half of the problem: a
// sheet whose first line is a warning belonging to the previous page's topic, or a bare column
// header from a table named on the sheet before.
//
//	3  both books, before any of this work
const maxPagesOpeningOnAnOrphan = 3

// bboxDoc is the shape of `pdftotext -bbox`: one <word> per word, positioned in PostScript
// points with the origin at the top left.
type bboxDoc struct {
	Pages []bboxPage `xml:"body>doc>page"`
}

type bboxPage struct {
	Words []bboxWord `xml:"word"`
}

type bboxWord struct {
	XMin float64 `xml:"xMin,attr"`
	YMin float64 `xml:"yMin,attr"`
	XMax float64 `xml:"xMax,attr"`
	YMax float64 `xml:"yMax,attr"`
	Text string  `xml:",chardata"`
}

// inTextBlock reports whether a word is page content rather than the running head or the folio.
// Both of those are drawn by Chromium's own header and footer templates, outside the margins,
// and neither fills a page.
func (w bboxWord) inTextBlock() bool {
	return w.YMax > textBlockTopPt && w.YMax < textBlockBottomPt
}

// pageBoxes parses a printed PDF into one word list per page.
func pageBoxes(t *testing.T, pdf []byte) []bboxPage {
	t.Helper()
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext (poppler) is not installed; the printed-page guards cannot run")
	}
	path := filepath.Join(t.TempDir(), "measure.pdf")
	if err := os.WriteFile(path, pdf, 0o600); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	out, err := exec.Command("pdftotext", "-bbox", path, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext -bbox: %v", err)
	}
	var doc bboxDoc
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse pdftotext bbox output: %v", err)
	}
	return doc.Pages
}

// pageFills returns, for each page, the last text baseline as a fraction of the text block.
// A page with no text in the block yields 0.
func pageFills(t *testing.T, pdf []byte) []float64 {
	t.Helper()
	pages := pageBoxes(t, pdf)
	fills := make([]float64, 0, len(pages))
	for _, p := range pages {
		last := 0.0
		for _, w := range p.Words {
			if !w.inTextBlock() {
				continue
			}
			if w.YMax > last {
				last = w.YMax
			}
		}
		if last == 0 {
			fills = append(fills, 0)
			continue
		}
		fills = append(fills, (last-textBlockTopPt)/(textBlockBottomPt-textBlockTopPt))
	}
	return fills
}

// printedSet is both books of one run, printed once and shared by every guard in this file.
// Printing is the expensive part -- two Chromium renders of a forty-odd page document -- and
// every check here reads the same two documents.
type printedSet struct {
	Book1PDF []byte
	Book2PDF []byte
	Set      Set
}

var (
	printOnce sync.Once
	printed   printedSet
	printErr  error
	printSkip string
)

// printableSet assembles and prints both books for one fixture child.
//
// The child is a four-year-old West Bengal vegetarian with a confirmed peanut allergy: old
// enough that most of Book 1's blocks apply, and matching the region with the most recipes, so
// the printed books are the fullest the corpus can produce. A younger or narrower child prints
// a shorter book and would hide exactly the page-filling defects these guards look for.
func printableSet(t *testing.T) printedSet {
	t.Helper()
	printOnce.Do(func() {
		url := os.Getenv("TEST_DATABASE_URL")
		if url == "" {
			printSkip = "TEST_DATABASE_URL not set"
			return
		}
		if !browserOnPath() {
			printSkip = "no chromium on PATH"
			return
		}
		printed, printErr = buildPrintedSet(url)
	})
	if printSkip != "" {
		t.Skip(printSkip)
	}
	if printErr != nil {
		t.Fatalf("print fixture books: %v", printErr)
	}
	return printed
}

func buildPrintedSet(url string) (printedSet, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return printedSet{}, fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	s := profile.Stored{
		ChildID:       "PAGEFIT-001",
		DisplayName:   "Ananya Roy",
		DateOfBirth:   time.Date(2022, 3, 14, 0, 0, 0, 0, time.UTC),
		Sex:           "female",
		LanguageID:    "bn",
		RegionCulture: "West Bengal / East India",
		DietType:      "Vegetarian",
		BudgetBand:    "Moderate",
		Allergens: []profile.DeclaredAllergen{
			{Group: "Peanut", Status: "confirmed", Source: "clinician_documented"},
		},
	}
	asOf := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	set, err := AssembleSet(ctx, pool, s, asOf)
	if err != nil {
		return printedSet{}, fmt.Errorf("assemble: %w", err)
	}

	out := printedSet{Set: set}
	for _, b := range []struct {
		kind Kind
		meta Metadata
		data any
		into *[]byte
	}{
		{Kind1, set.Book1.Metadata, set.Book1, &out.Book1PDF},
		{Kind2, set.Book2.Metadata, set.Book2, &out.Book2PDF},
	} {
		var doc strings.Builder
		if err := RenderHTML(&doc, b.kind, b.meta, b.data); err != nil {
			return printedSet{}, fmt.Errorf("render %s: %w", b.kind, err)
		}
		pdf, err := PrintPDF(ctx, []byte(doc.String()), b.meta)
		if err != nil {
			return printedSet{}, fmt.Errorf("print %s: %w", b.kind, err)
		}
		*b.into = pdf
	}
	return out, nil
}

// bothBooks pairs each printed book with its name, for the range loops below.
func (p printedSet) bothBooks() []struct {
	Name string
	PDF  []byte
} {
	return []struct {
		Name string
		PDF  []byte
	}{{"book1", p.Book1PDF}, {"book2", p.Book2PDF}}
}

// TestNoMorePagesEndEarlyThanTheBudget is the guard the whole file exists for.
//
// A printed page that stops a third of the way down is the single loudest signal that a
// document was assembled rather than designed, and it is invisible to every count this package
// already keeps. Book 1 shipped with eighteen such pages.
func TestNoMorePagesEndEarlyThanTheBudget(t *testing.T) {
	set := printableSet(t)

	total, under := 0, 0
	var detail []string
	for _, b := range set.bothBooks() {
		for i, f := range pageFills(t, b.PDF) {
			total++
			if f < underfillThreshold {
				under++
				detail = append(detail, fmt.Sprintf("%s p%d ends at %.0f%%", b.Name, i+1, f*100))
			}
		}
	}

	t.Logf("page fill: %d of %d printed pages end above %.0f%% of the text block (budget %d)",
		under, total, underfillThreshold*100, maxUnderfilledPages)
	if under > maxUnderfilledPages {
		t.Errorf("%d of %d pages end early, budget is %d:\n  %s",
			under, total, maxUnderfilledPages, strings.Join(detail, "\n  "))
	}
}

// TestNoPrintedPageIsAlmostEntirelyBlank has no budget, because there is no page this is ever
// right for.
//
// Book 1 page 42 was three writing lines and a repeated column header on an otherwise blank
// sheet -- the tail of the food-diversity grid, split anywhere because Chromium ignores CSS
// orphans and widows on table rows.
func TestNoPrintedPageIsAlmostEntirelyBlank(t *testing.T) {
	set := printableSet(t)
	for _, b := range set.bothBooks() {
		for i, f := range pageFills(t, b.PDF) {
			if f > 0 && f < 0.15 {
				t.Errorf("%s p%d ends at %.0f%% of the text block: a sheet holding almost nothing",
					b.Name, i+1, f*100)
			}
		}
	}
}

// orphanOpeners are the first words of a page that mean the page was split away from what gives
// it meaning. Two kinds, both read off printed sheets:
//
//   - a callout heading: Book 1 pages 28 and 30 opened with "Warning: when to seek advice"
//     belonging to the domain on the previous sheet
//   - a bare column header: pages 17, 42, 44 and 47 opened with the columns of a table whose
//     name printed on the sheet before
//
// Matched on the opening words rather than on markup because this reads the printed document,
// which is the only place the defect exists -- the HTML is identical either way.
var orphanOpeners = []string{
	"Warning:",
	"Note:",
	"Who reviews this:",
	"Who to approach:",
}

// TestNoMorePagesOpenOnAnOrphanThanTheBudget checks what a sheet starts with.
func TestNoMorePagesOpenOnAnOrphanThanTheBudget(t *testing.T) {
	set := printableSet(t)

	total, orphans := 0, 0
	var detail []string
	for _, b := range set.bothBooks() {
		for i, opener := range pageOpeners(t, b.PDF) {
			total++
			for _, bad := range orphanOpeners {
				if strings.HasPrefix(opener, bad) {
					orphans++
					detail = append(detail, fmt.Sprintf("%s p%d opens with %q", b.Name, i+1, opener))
					break
				}
			}
		}
	}

	t.Logf("page openers: %d of %d printed pages open on an orphan (budget %d)",
		orphans, total, maxPagesOpeningOnAnOrphan)
	if orphans > maxPagesOpeningOnAnOrphan {
		t.Errorf("%d of %d pages open on an orphan, budget is %d:\n  %s",
			orphans, total, maxPagesOpeningOnAnOrphan, strings.Join(detail, "\n  "))
	}
}

// pageOpeners returns the first few words of each page's text block, joined, so a caller can
// match an opening phrase.
func pageOpeners(t *testing.T, pdf []byte) []string {
	t.Helper()
	pages := pageBoxes(t, pdf)
	out := make([]string, 0, len(pages))
	for _, p := range pages {
		var words []string
		for _, w := range p.Words {
			if !w.inTextBlock() {
				continue
			}
			words = append(words, w.Text)
			if len(words) == 4 {
				break
			}
		}
		out = append(out, strings.Join(words, " "))
	}
	return out
}
