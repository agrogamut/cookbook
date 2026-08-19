package book

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
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

// allocatorFor builds the browser allocator, applying the container-only flags when the
// environment asks for them.
//
// Shared by the single and batch entry points so the two cannot disagree about how the
// browser is launched -- a sandbox flag that applied to one book and not to the other would
// mean a set where one book prints and the other does not.
func allocatorFor(ctx context.Context) (context.Context, context.CancelFunc) {
	// In a container the browser runs as root with user namespaces unavailable, where
	// Chrome's sandbox cannot initialise and the process exits before it prints anything.
	//
	// Off by default, so a developer's machine keeps the sandbox it can actually use. It is
	// safe where it is set and not in general: the browser here only ever loads a document
	// this service just rendered from its own database, never a remote page, so there is no
	// untrusted content for the sandbox to contain.
	if os.Getenv("CHROMIUM_NO_SANDBOX") == "" {
		return context.WithCancel(ctx)
	}
	return chromedp.NewExecAllocator(ctx,
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.NoSandbox,
			// Chrome sizes its shared memory from /dev/shm, which containers default to
			// 64 MB. A book of 22 pages exhausts it and the renderer crashes mid-print.
			chromedp.Flag("disable-dev-shm-usage", true),
		)...)
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

	allocCtx, allocCancel := allocatorFor(ctx)
	defer allocCancel()
	ctx = allocCtx

	ctx, cancel := chromedp.NewContext(ctx)
	defer cancel()

	return printIn(ctx, htmlDoc, meta)
}

// PrintPDFAll prints several documents in one browser.
//
// Launching Chromium dominates the cost of a print: on a small cloud instance a book takes
// about twenty seconds, nearly all of it startup, so printing a set as two separate calls
// paid that twice and overran the request budget. One browser, two documents, and the second
// print costs only its own layout.
//
// All or nothing: a set of books is one deliverable, so the first failure returns rather than
// handing back a partial set the caller has to reason about. Errors name the document that
// failed, since the caller knows which book that is and the browser does not.
func PrintPDFAll(ctx context.Context, docs []PrintJob) ([][]byte, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	if !browserOnPath() {
		return nil, fmt.Errorf("%w: no chromium build found on PATH", ErrChromiumUnavailable)
	}

	ctx, cancel := allocatorFor(ctx)
	defer cancel()
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	out := make([][]byte, 0, len(docs))
	for _, d := range docs {
		pdf, err := printIn(ctx, d.HTML, d.Metadata)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Name, err)
		}
		out = append(out, pdf)
	}
	return out, nil
}

// PrintJob is one document in a batch. Name identifies it in an error and is never printed
// into the document itself.
type PrintJob struct {
	Name     string
	HTML     []byte
	Metadata Metadata
}

// printIn prints one document in an existing browser context.
//
// Each document gets its own tab and its own timeout, so one slow book cannot consume the
// budget of the next and a crashed tab does not take the browser with it.
func printIn(parent context.Context, htmlDoc []byte, meta Metadata) ([]byte, error) {
	ctx, cancel := chromedp.NewContext(parent)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// A running head, not a poster. The disclosure is on every page and no template can
	// suppress it, which is the requirement; printing the whole three-line paragraph 42 times
	// was not. It cost 28mm of every sheet's head, left the top of each page empty and the
	// foot crowded, and by the fifth page a reader has stopped seeing it -- which is the one
	// thing a standing warning must not do. The full sentence appears once, in the document's
	// own .provisional block on the opening page; this line carries the same two facts on
	// every sheet in the space a running head occupies.
	header := fmt.Sprintf(`<div style="font-size:7pt;line-height:1.3;width:100%%;
		box-sizing:border-box;padding:0 20mm;margin:0;
		display:flex;justify-content:space-between;gap:6mm;
		font-family:'Noto Sans','Liberation Sans',sans-serif;color:#9b2226">
		<span><strong>Provisional - not clinically approved.</strong> Not a clinical prescription.</span>
		<span style="font-family:'Noto Sans Mono','DejaVu Sans Mono',ui-monospace,monospace;
		      white-space:nowrap;overflow:hidden;text-overflow:ellipsis">%s</span>
		</div>`,
		html.EscapeString(meta.ReviewStatus))

	// The release segment is omitted entirely when there is no release id, rather than
	// printing the label with nothing after it. There is no release layer yet, so on every
	// book printed today "release " followed by blank is a field a reader has to interpret;
	// end.html already draws a write-line in that case for the same reason.
	release := ""
	if meta.ReleaseID != "" {
		release = " | release " + html.EscapeString(meta.ReleaseID)
	}
	footer := fmt.Sprintf(`<div style="font-size:7pt;width:100%%;padding:0 20mm;
		display:flex;justify-content:space-between;align-items:baseline;
		font-family:'Noto Sans','Liberation Sans',sans-serif;color:#5a5a5a">
		<span>MadamGY &middot; %s%s &middot; generated %s</span>
		<span class="pageNumber" style="font-size:9pt;color:#1a1a1a"></span></div>`,
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
				// These four must match the @page margins in tokens.css, which is where
				// the page geometry is reasoned about and written down. Chromium's own
				// values win for the printed sheet, so a disagreement here does not error
				// -- it silently prints a different book from the one the stylesheet was
				// laid out for. 22mm head for the running line, 20mm elsewhere.
				WithMarginTop(0.866).
				WithMarginBottom(0.787).
				WithMarginLeft(0.787).
				WithMarginRight(0.787).
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
