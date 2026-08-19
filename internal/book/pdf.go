package book

import (
	"context"
	"errors"
	"fmt"
	"html"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ErrChromiumUnavailable means no browser was found. Reported plainly rather than falling
// back to a lesser renderer: a fallback that cannot shape Bengali would produce a book whose
// ingredient names are subtly wrong, which is worse than producing none.
var ErrChromiumUnavailable = errors.New("book: headless chromium unavailable")

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

	footer := fmt.Sprintf(`<div style="font-size:7pt;width:100%%;padding:0 12mm;
		display:flex;justify-content:space-between;color:#52606d">
		<span>MadamGY | %s | release %s | generated %s</span>
		<span class="pageNumber"></span></div>`,
		html.EscapeString(meta.BookVersion),
		html.EscapeString(meta.ReleaseID),
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
		return nil, fmt.Errorf("%w: %v", ErrChromiumUnavailable, err)
	}
	return out, nil
}
