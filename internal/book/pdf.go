package book

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os/exec"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ErrChromiumUnavailable means no browser was found. Reported plainly rather than falling
// back to a lesser renderer: a fallback that cannot shape Bengali would produce a book whose
// ingredient names are subtly wrong, which is worse than producing none.
//
// It means exactly "no renderer is installed" and nothing else. Everything a present browser
// can still fail at is ErrPrintFailed.
var ErrChromiumUnavailable = errors.New("book: headless chromium unavailable")

// ErrPrintFailed means a browser was found and the print itself failed -- a timeout, a
// renderer crash, HTML the print engine choked on.
//
// Distinct from ErrChromiumUnavailable because the two need opposite responses. A missing
// browser is an install problem: retrying changes nothing and nothing about the child's data
// is implicated. A failed print may well be about this document, and telling an operator
// "nothing about this child changed" would have them retry a data problem forever.
var ErrPrintFailed = errors.New("book: pdf print failed")

// browserNames mirrors chromedp v0.16.0's own Unix search list (allocate.go, findExecPath).
// It must stay a superset of nothing and a copy of that: a name chromedp resolves but this
// list omits turns a working install into a hard "no browser found", which is exactly the
// regression a four-name version of this list produced -- the chromedp Docker image ships
// the browser as headless-shell, and snap installs it at /snap/bin/chromium.
//
// Keep it in sync when bumping chromedp. Drift in the other direction is harmless: a name
// here that chromedp does not resolve only means the run proceeds and fails, and that
// failure is classified correctly below.
var browserNames = []string{
	"headless_shell", "headless-shell", "chromium", "chromium-browser",
	"google-chrome", "google-chrome-stable", "google-chrome-beta", "google-chrome-unstable",
	"/usr/bin/google-chrome", "/usr/local/bin/chrome", "/snap/bin/chromium", "chrome",
}

// BrowserAvailable reports whether a browser is installed for PrintPDF to launch. Exported
// so tests in other packages skip rather than fail on a machine with no Chromium, without
// keeping their own copy of the name list -- one such copy had already gone stale.
func BrowserAvailable() bool { return browserOnPath() }

// browserOnPath reports whether chromedp will find a browser to launch.
//
// It lives here rather than in the test file so production and test cannot disagree about
// what counts as installed. It is only a fast path: a missing browser is also recognised
// from the run's own error, so getting this list wrong cannot on its own misreport a
// working install.
func browserOnPath() bool {
	for _, name := range browserNames {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

// PrintPDF renders one already-generated HTML document to PDF.
//
// A4 portrait with the contract's margins. Chrome's own header and footer templates carry
// the release fields and the page number, which is why they are set here rather than in CSS:
// only the print engine knows the total page count.
//
// The header template also carries the provisional-data disclosure banner, not just page
// furniture. Nothing in the dataset is approved, and that has to appear on every printed
// page with no template able to suppress it. An in-document position: fixed banner was
// tried first and rejected -- see the comment above the .provisional print rule in
// tokens.css -- because Chromium's fixed-element page-repeat tiles a negative offset onto
// the bottom of the page instead of the reserved band above the content. Chrome's own
// repeating header has no such coordinate math: it is drawn once per physical page by the
// print engine itself. marginTop is widened to match the --banner-reserve space tokens.css
// already carves out of @page for exactly this banner, so the header has room to render
// without clipping.
func PrintPDF(ctx context.Context, htmlDoc []byte, meta Metadata) ([]byte, error) {
	// Probed before the run, so the two failures stay distinguishable. Wrapping every
	// chromedp failure as "renderer unavailable" told an operator a 60-second timeout, a
	// crash and malformed HTML were all a missing install, which is a 503 they can only
	// retry.
	if !browserOnPath() {
		return nil, fmt.Errorf("%w: no chromium build found on PATH", ErrChromiumUnavailable)
	}

	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	header := fmt.Sprintf(`<div style="font-size:7.5pt;line-height:1.4;width:100%%;
		box-sizing:border-box;padding:5mm 12mm 3mm;margin:0;
		border:0.4mm solid #9b2226;background:#fdecea;color:#9b2226">
		<strong>Provisional - not clinically approved.</strong>
		This book was generated from provider data marked
		<span style="font-family:'Noto Sans Mono','DejaVu Sans Mono',ui-monospace,monospace">%s</span>.
		It has not completed culinary, nutrition or clinical review and must not be used as a
		clinical prescription.</div>`,
		html.EscapeString(meta.ReviewStatus))

	// The release segment is omitted entirely when there is no release id, rather than
	// printing the label with nothing after it. There is no release layer yet, so on every
	// book printed today "release " followed by blank is a field a reader has to interpret;
	// end.html already draws a write-line in that case for the same reason.
	release := ""
	if meta.ReleaseID != "" {
		release = " | release " + html.EscapeString(meta.ReleaseID)
	}
	footer := fmt.Sprintf(`<div style="font-size:7pt;width:100%%;padding:0 12mm;
		display:flex;justify-content:space-between;color:#52606d">
		<span>MadamGY | %s%s | generated %s</span>
		<span class="pageNumber"></span></div>`,
		html.EscapeString(meta.BookVersion),
		release,
		meta.GenerationDate.Format("2006-01-02"))

	var out []byte
	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frame, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(frame.Frame.ID, string(htmlDoc)).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			out, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPaperWidth(8.27). // A4 portrait, inches
				WithPaperHeight(11.69).
				WithMarginTop(1.97). // 50mm: safe-margin + header-height + banner-reserve, tokens.css @page
				WithMarginBottom(0.47).
				WithMarginLeft(0.47).
				WithMarginRight(0.47).
				WithDisplayHeaderFooter(true).
				WithHeaderTemplate(header).
				WithFooterTemplate(footer).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		// Classified from chromedp's own failure as well as from the probe above, because
		// the probe reads a copied list of names and chromedp reads its own. When the two
		// disagree the run is the authority: it is the thing that actually tried to launch
		// a browser, and exec.ErrNotFound from it means there was none to launch.
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: %v", ErrChromiumUnavailable, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrPrintFailed, err)
	}
	return out, nil
}
