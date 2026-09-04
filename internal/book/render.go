package book

import (
	"embed"
	"fmt"
	"html/template"
	"io"
)

//go:embed templates
var templateFS embed.FS

// Kind selects a book's palette and template set. The provider ships two palettes and two
// template families; nothing else is renderable.
type Kind string

const (
	Kind1 Kind = "book1"
	Kind2 Kind = "book2"
)

// ErrUnknownKind is returned for a Kind that is neither book.
var ErrUnknownKind = fmt.Errorf("book: unknown kind")

// templateFuncs is deliberately one function long.
//
// html/template has no numeric range, and the tracker templates need to emit N blank rows.
// The alternative -- building a slice of N empty structs in Go and handing it to the template
// -- puts presentation padding into the render model, where a later reader has to work out
// whether those empty rows mean "no data" or "a form". Keeping it here says plainly that the
// blanks are a form.
//
// Nothing else belongs in this map. A template function that formatted, rounded or defaulted
// a value would put a data decision somewhere no test looks.
var templateFuncs = template.FuncMap{
	// rows returns a slice of length n purely so a template can iterate it. The values are
	// never read.
	"rows": func(n int) []struct{} {
		if n < 0 {
			return nil
		}
		return make([]struct{}, n)
	},
	// coverArt looks up one of the twelve embedded decorative illustrations by name (see
	// coverart.go). This is a template function rather than a lookup through the renderContext
	// field below, even though that field exists and carries the same map: html/template's `$`
	// is reset to the argument of every {{template}} invocation ("no dynamic scoping" -- see
	// text/template's exec.go, walkTemplate), and Book2's block templates are invoked with the
	// Book2 value itself as their dot (body.html's `{{ template "B2-COVER-01" $b }}`), not with
	// the render context that actually carries CoverArt. So `{{ index $.CoverArt "x" }}` inside
	// a Book2 block template resolves $ to Book2, not to renderContext, and fails at execution
	// time with "can't evaluate field CoverArt in type book.Book2" -- confirmed by hand before
	// writing this comment, not assumed. A closure over the same package-level map sidesteps
	// the scoping question: it needs no dot at all, so it works identically no matter which
	// value a template happens to be invoked with.
	"coverArt": func(key string) template.URL {
		v, ok := coverArt[key]
		if !ok {
			panic("book: no cover art named " + key)
		}
		return v
	},
}

type renderContext struct {
	Metadata  Metadata
	BookClass string
	CSS       template.CSS
	FontFaces template.CSS
	Watermark template.URL
	// CoverArt carries the same package-level map the coverArt template function reads. It is
	// threaded here too, for templates executed directly against the render context (base.html
	// and any future top-level block), even though the coverArt function is what a Book1/Book2
	// block template must use in practice -- see that function's own comment.
	CoverArt map[string]template.URL
	Data     any
}

// RenderHTML writes one book as a standalone HTML document. The output is both the reviewer
// preview and the source chromedp prints, so there is exactly one rendering of a book and no
// way for preview and print to disagree.
func RenderHTML(w io.Writer, kind Kind, meta Metadata, data any) error {
	if kind != Kind1 && kind != Kind2 {
		return fmt.Errorf("book: %q: %w", kind, ErrUnknownKind)
	}

	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		return fmt.Errorf("book: read tokens: %w", err)
	}

	t, err := template.New("base.html").Funcs(templateFuncs).ParseFS(templateFS,
		"templates/base.html",
		fmt.Sprintf("templates/%s/*.html", kind))
	if err != nil {
		return fmt.Errorf("book: parse %s templates: %w", kind, err)
	}

	// template.CSS marks the stylesheet as trusted so html/template does not escape it.
	// Safe because it is an embedded file in this repository, never anything from the
	// database or a request.
	ctx := renderContext{
		Metadata:  meta,
		BookClass: string(kind),
		CSS:       template.CSS(css),
		FontFaces: fontFacesCSS,
		Watermark: watermarkDataURI,
		CoverArt:  coverArt,
		Data:      data,
	}
	if err := t.ExecuteTemplate(w, "base.html", ctx); err != nil {
		return fmt.Errorf("book: render %s: %w", kind, err)
	}
	return nil
}
