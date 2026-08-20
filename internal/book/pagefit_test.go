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
//	24  after breaking on parts, protecting form tails, and binding warnings to their subject
//	    -- the last two cost fill and bought the two defects a count cannot see: a sheet
//	    holding three writing lines, and four sheets opening on a warning about nothing
const maxUnderfilledPages = 24

// maxPagesOpeningOnAnOrphan is the same kind of budget for the other half of the problem: a
// sheet whose first line is a warning belonging to the previous page's topic, or a bare column
// header from a table named on the sheet before.
//
//	3  both books, before any of this work
//	0  after binding a domain's red flag and referral to the reference table above them
const maxPagesOpeningOnAnOrphan = 0

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

	// Looking at the sheets is not optional for layout work -- every defect this file guards
	// against passed its own count and was found by printing a book and reading the pages. Set
	// BOOK_PAGE_DUMP to a directory to get both PDFs written there for exactly that.
	if dir := os.Getenv("BOOK_PAGE_DUMP"); dir != "" {
		for name, pdf := range map[string][]byte{"book1": out.Book1PDF, "book2": out.Book2PDF} {
			if err := os.WriteFile(filepath.Join(dir, name+".pdf"), pdf, 0o600); err != nil {
				return printedSet{}, fmt.Errorf("dump %s: %w", name, err)
			}
		}
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

// coverFixture is the printed Book 1 cover, with and without a portrait.
//
// A separate print from printableSet because the whole question is what the optional portrait
// does to the sheet below it, and the shared fixture carries no photograph.
func printBook1Cover(t *testing.T, photo bool) []byte {
	t.Helper()
	base := printableSet(t) // establishes the skips, and gives us an assembled book to vary
	b := base.Set.Book1
	if photo {
		p, err := ParsePhoto("data:image/png;base64,"+onePixelPNG, "Ananya, August 2026")
		if err != nil {
			t.Fatalf("parse fixture photo: %v", err)
		}
		b.Child.Photo = p
	} else {
		b.Child.Photo = nil
	}

	var doc strings.Builder
	if err := RenderHTML(&doc, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	pdf, err := PrintPDF(context.Background(), []byte(doc.String()), b.Metadata)
	if err != nil {
		t.Fatalf("print: %v", err)
	}
	return pdf
}

// TestTheCoverIsOnePageWithAndWithoutAPhotograph.
//
// It was not. The drop that carries the title down the sheet was a single 105mm constant while
// the band above it was 75mm taller whenever a portrait was present, so the cover with a
// photograph measured 262mm into a 255mm text block: the facts row printed alone on page 2
// above 250mm of white and the contents page moved to page 3. Without a photograph the same
// stack measured 201mm and left the foot floating 54mm above the bottom margin.
//
// Both halves matter. A cover that fits but stops two thirds of the way down is the other
// failure, and only checking for the spill would accept it.
func TestTheCoverIsOnePageWithAndWithoutAPhotograph(t *testing.T) {
	for _, tc := range []struct {
		name  string
		photo bool
	}{{"with a photograph", true}, {"without one", false}} {
		t.Run(tc.name, func(t *testing.T) {
			fills := pageFills(t, printBook1Cover(t, tc.photo))
			if len(fills) < 2 {
				t.Fatalf("book printed %d pages", len(fills))
			}
			// Page 2 is the contents page. If the cover spilled, it holds the three facts cells
			// and ends in the first fifth of the sheet.
			if fills[1] < 0.30 {
				t.Errorf("page 2 ends at %.0f%% of the text block: the cover spilled onto it", fills[1]*100)
			}
			// And the cover reaches the foot of its own sheet rather than stopping in the middle.
			if fills[0] < 0.80 {
				t.Errorf("the cover ends at %.0f%%: the facts row should sit near the foot", fills[0]*100)
			}
		})
	}
}

// TestTheCoverTitleIsCentredOnThePage.
//
// tokens.css set the auto side margins on one line and threw them away two rules later with a
// `margin` shorthand, so the title printed about 15mm left of the page centre and the standfirst
// about 27mm left, each with its text centred inside a box hard against the left edge. Centred
// text in a left-hung box is the specific thing that reads as "everything is left aligned".
func TestTheCoverTitleIsCentredOnThePage(t *testing.T) {
	pdf := printBook1Cover(t, false)
	pages := pageBoxes(t, pdf)
	if len(pages) == 0 {
		t.Fatal("no pages")
	}

	// The title is the widest run of words on the cover's own line. Locate it by the display
	// size: nothing else on the sheet is set at 34pt, so the tallest word is part of the title.
	var tallest bboxWord
	for _, w := range pages[0].Words {
		if !w.inTextBlock() {
			continue
		}
		if w.YMax-w.YMin > tallest.YMax-tallest.YMin {
			tallest = w
		}
	}
	if tallest.Text == "" {
		t.Fatal("no text found on the cover")
	}

	// Every word on the title's line, so the measurement is of the line and not of one word.
	lineMin, lineMax := tallest.XMin, tallest.XMax
	for _, w := range pages[0].Words {
		if w.YMin != tallest.YMin {
			continue
		}
		lineMin = min(lineMin, w.XMin)
		lineMax = max(lineMax, w.XMax)
	}

	centre := (lineMin + lineMax) / 2
	// 6pt is about 2mm: tighter than any misalignment a reader would notice, looser than the
	// sub-point drift of a centred line whose trailing space is measured differently.
	if off := centre - pageWidthPt/2; off > 6 || off < -6 {
		t.Errorf("the cover title %q is centred at %.1fpt, the page centre is %.1fpt (%.1fmm off)",
			tallest.Text, centre, pageWidthPt/2, off*25.4/72)
	}
}
