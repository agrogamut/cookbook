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
}

type renderContext struct {
	Metadata  Metadata
	BookClass string
	CSS       template.CSS
	Data      any
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
		Data:      data,
	}
	if err := t.ExecuteTemplate(w, "base.html", ctx); err != nil {
		return fmt.Errorf("book: render %s: %w", kind, err)
	}
	return nil
}
