package book

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
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
// Called once per browser launch rather than once per print, since sharedBrowser holds the
// browser for the process lifetime. It stays a function so there is exactly one description
// of how this service launches a browser -- a sandbox flag that applied to one path and not
// another would mean a set where one book prints and the other does not.
func allocatorFor(ctx context.Context) (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		// No network, at all. Every document this browser prints is self-contained by
		// construction -- the stylesheet is inlined by RenderHTML and the only image a book
		// can carry is the cover photograph, embedded as a data: URI -- so a subresource
		// fetch is never something a correct book does. Resolving every host to nothing
		// makes that invariant something the browser enforces rather than something the
		// templates are trusted to honour.
		//
		// It matters because a book is built partly from request input: the child's name and
		// that cover photograph arrive in the POST body. html/template escapes the text and
		// ParsePhoto restricts the image to a base64 png/jpeg/webp, so there is no route from
		// input to a fetch today -- this is the second lock, for the day a template grows an
		// attribute someone forgets to think about. A print browser that cannot reach the
		// network cannot be turned into an SSRF pivot from inside the document.
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-component-update", true),
	)

	// In a container the browser runs as root with user namespaces unavailable, where
	// Chrome's sandbox cannot initialise and the process exits before it prints anything.
	//
	// Off by default, so a developer's machine keeps the sandbox it can actually use.
	if os.Getenv("CHROMIUM_NO_SANDBOX") != "" {
		opts = append(opts,
			chromedp.NoSandbox,
			// Chrome sizes its shared memory from /dev/shm, which containers default to
			// 64 MB. A book of 22 pages exhausts it and the renderer crashes mid-print.
			chromedp.Flag("disable-dev-shm-usage", true),
		)
	}
	return chromedp.NewExecAllocator(ctx, opts...)
}

// maxConcurrentPrints bounds how many tabs can be laying out a book at once.
//
// The browser is now process-wide, so without a bound one client can hold open as many tabs
// as it can open connections -- and a print tab laying out a 47-page book is the most
// expensive thing this service does. On the deployed free instance, which has 512 MB for
// Chromium and everything else, a handful of concurrent prints is an out-of-memory kill
// rather than a slow response.
//
// Two rather than one: a single slot makes a second operator wait out a whole print before
// their own starts, and the zip route prints two books back to back. Two is also strictly
// better than what the per-request browser did, which was to allow unbounded concurrent
// *browsers* -- the bound is new, and at any value it is an improvement.
const maxConcurrentPrints = 2

var printSlots = make(chan struct{}, maxConcurrentPrints)

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
	browser, err := sharedBrowser()
	if err != nil {
		return nil, err
	}
	return printIn(ctx, browser, htmlDoc, meta)
}

// browser holds the one Chromium this process launches, and the cancel that stops it.
//
// Guarded by mu rather than sync.Once because a browser can die -- it is a child process on a
// memory-capped instance -- and a Once cannot restart. ctx is nil until the first print and
// after a death; its Err() being non-nil is what "died" looks like from here.
var browserState struct {
	mu   sync.Mutex
	ctx  context.Context
	stop context.CancelFunc
}

// sharedBrowser returns a context whose Chromium is already running, launching one if there
// is none.
//
// Launching the browser is the single largest cost in a print and it was being paid per
// request. Measured against the deployed free instance, printing Book 1 alone took 24.4s and
// Book 2 alone 20.5s, while printing both in one browser took 31.2s -- so the launch is
// about 13.7s of every request and the layout is the remaining 10.8s and 6.8s. On a
// developer's machine the same two prints are under half a second each, which is why this
// only ever looked like a production problem.
//
// One browser for the process lifetime, a fresh tab per print (printIn), and the tab closed
// on the way out. The alternative -- a browser per request -- pays a 14-second fork on a
// 0.1-CPU instance to render a document that takes seven.
//
// It is rooted at context.Background() deliberately. Deriving it from a request context would
// tie the browser's life to whichever request happened to start it and kill it the moment
// that response was written, which is the same launch-per-request cost with extra steps.
func sharedBrowser() (context.Context, error) {
	browserState.mu.Lock()
	defer browserState.mu.Unlock()

	if browserState.ctx != nil && browserState.ctx.Err() == nil {
		return browserState.ctx, nil
	}
	// Dead or never started. Release whatever is left before replacing it, so a crashed
	// browser does not leak its allocator.
	if browserState.stop != nil {
		browserState.stop()
		browserState.ctx, browserState.stop = nil, nil
	}

	// Probed here rather than per print, so a missing install stays distinguishable from a
	// print that failed with a browser present -- the two need opposite operator responses.
	if !browserOnPath() {
		return nil, fmt.Errorf("%w: no chromium build found on PATH", ErrChromiumUnavailable)
	}

	allocCtx, allocCancel := allocatorFor(context.Background())
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)

	// chromedp starts the browser lazily, on the first Run. Starting it here means the launch
	// cost is paid by whoever calls this first -- WarmUp at boot, when nobody is waiting --
	// rather than by a request. A failure here is a launch failure, so it is reported as an
	// unavailable renderer rather than as a failed print.
	stop := func() { browserCancel(); allocCancel() }
	if err := chromedp.Run(browserCtx); err != nil {
		stop()
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: %v", ErrChromiumUnavailable, err)
		}
		return nil, fmt.Errorf("%w: launching browser: %v", ErrChromiumUnavailable, err)
	}

	browserState.ctx, browserState.stop = browserCtx, stop
	return browserCtx, nil
}

// WarmUp launches the shared browser before any request needs it.
//
// Called at startup. Without it the first operator to ask for a PDF after a deploy pays the
// whole launch on top of their own print, which on the free instance is the difference
// between a slow request and one that reads as broken.
//
// A failure is returned rather than fatal: a service that cannot print is still a service
// that can serve the console, the audit pages and the HTML previews, and it already reports
// a missing renderer as a 503 per request.
func WarmUp() error {
	_, err := sharedBrowser()
	return err
}

// ShutdownBrowser stops the shared browser. For a clean process exit and for tests that need
// to prove a restart; printing after it simply launches a new one.
func ShutdownBrowser() {
	browserState.mu.Lock()
	defer browserState.mu.Unlock()
	if browserState.stop != nil {
		browserState.stop()
		browserState.ctx, browserState.stop = nil, nil
	}
}

// PrintPDFAll prints several documents.
//
// Since the browser became process-wide (see sharedBrowser) this no longer saves a launch --
// every print shares one browser now. It stays because the set is one deliverable: it prints
// the documents in order and returns on the first failure rather than handing back a partial
// set the caller has to reason about.
//
// All or nothing: a set of books is one deliverable, so the first failure returns rather than
// handing back a partial set the caller has to reason about. Errors name the document that
// failed, since the caller knows which book that is and the browser does not.
func PrintPDFAll(ctx context.Context, docs []PrintJob) ([][]byte, error) {
	if len(docs) == 0 {
		return nil, nil
	}
	browser, err := sharedBrowser()
	if err != nil {
		return nil, err
	}

	out := make([][]byte, 0, len(docs))
	for _, d := range docs {
		pdf, err := printIn(ctx, browser, d.HTML, d.Metadata)
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

// printIn prints one document in the shared browser.
//
// Each document gets its own tab and its own timeout, so one slow book cannot consume the
// budget of the next and a crashed tab does not take the browser with it. The tab is closed
// on the way out, which matters more now that the browser outlives the request: a leaked tab
// per print would grow the process until the instance's memory cap killed it.
func printIn(caller, browser context.Context, htmlDoc []byte, meta Metadata) ([]byte, error) {
	// Wait for a slot, or give up if the caller has gone. A queued request whose client
	// disconnected must not go on to occupy a tab.
	select {
	case printSlots <- struct{}{}:
		defer func() { <-printSlots }()
	case <-caller.Done():
		return nil, fmt.Errorf("%w: %v", ErrPrintFailed, caller.Err())
	}

	// The tab must descend from the browser, because that is what makes it a tab in the
	// shared browser rather than a new one. But it also has to die when the caller does, and
	// a context has one parent -- so the caller's cancellation is wired across by hand.
	//
	// Without this the ctx argument was decorative: when the browser became process-wide the
	// tab stopped descending from the request, so a disconnected client left a print running
	// to its own 60-second timeout and the route's 180-second budget could not stop it
	// either. Every abandoned request held a tab for a full print.
	//
	ctx, cancel := chromedp.NewContext(browser)
	defer cancel()
	stopPropagating := context.AfterFunc(caller, cancel)
	defer stopPropagating()

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
		// No scripting in the print tab.
		//
		// The browser outlives the request now, so two children's books are two families'
		// data passing through one process. The per-request browser they used to get made
		// isolation free; this buys it back by removing the capability rather than
		// partitioning it. Storage only carries between prints if a document writes storage,
		// and writing storage takes script -- so a tab that cannot execute script cannot
		// leave anything behind for the next child's book to find. It closes fetch,
		// service workers, localStorage and cookies in one move.
		//
		// It costs nothing: no template in this package emits a <script>, an event handler or
		// a javascript: URL, and the running head and folio are drawn by Chromium's own print
		// engine rather than by the page. A book that needed script to render would be a book
		// whose content was computed in the browser, which this project forbids for its own
		// reasons -- the figures on the page have to come from the database.
		//
		// Preferred over chromedp.WithNewBrowserContext, which the CDP suggestion names and
		// which is the textbook answer: against this Chromium it fails the print outright
		// with "Failed to open new tab - no browser is open (-32000)". Disabling script is a
		// stronger guarantee anyway -- a storage partition still lets a document act, it just
		// gives it a private place to act in.
		emulation.SetScriptExecutionDisabled(true),
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
