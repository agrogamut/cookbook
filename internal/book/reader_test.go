package book

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/bookdocx"
	"golang.org/x/net/html"
)

func readerFixture(t *testing.T, book int, language string) bookdocx.ReaderDocument {
	t.Helper()
	m, err := bookdocx.Bindings(book, language, "v1")
	if err != nil {
		t.Fatal(err)
	}
	r := bookdocx.Request{Book: book, Language: language, Version: "v1", Modules: map[string]bookdocx.Module{}}
	for _, b := range m.Bindings {
		r.Modules[b.Code] = bookdocx.Module{Gap: "source gap " + b.Code}
	}
	r.Modules[fmt.Sprintf("B%d-01", book)] = bookdocx.Module{Blocks: []bookdocx.Block{{Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: "<source & exact> 0.005 g / 0007", Bold: true}}}}, {Table: &bookdocx.Table{Header: true, Rows: [][]string{{"source", "quantity"}, {"R-001", "12.50 g"}}}}}, Entries: []bookdocx.Entry{{ID: "R-001", Blocks: []bookdocx.Block{{Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: "recipe title"}}}}, {Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: "method & <source>"}}}}}}}}
	d, err := bookdocx.ReadContent(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReaderHTMLUsesOriginalThemeAndAllModules(t *testing.T) {
	for _, language := range []string{"en", "bn", "hi"} {
		for _, book := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d-%s", book, language), func(t *testing.T) {
				d := readerFixture(t, book, language)
				var out bytes.Buffer
				options := ReaderOptions{Book1: &Book1{}, Book2: &Book2{}}
				if err := RenderReaderHTML(&out, d, options); err != nil {
					t.Fatal(err)
				}
				s := out.String()
				css, _ := templateFS.ReadFile("templates/tokens.css")
				if !strings.Contains(s, string(css)) {
					t.Fatal("original CSS not preserved")
				}
				want := 63
				if book == 2 {
					want = 56
				}
				if got := strings.Count(s, `data-module="`); got != want {
					t.Fatalf("modules %d want %d", got, want)
				}
				for _, text := range []string{"&lt;source &amp; exact&gt; 0.005 g / 0007", "12.50 g", "method &amp; &lt;source&gt;", `data-layout="pink-flow-v1"`, `data-recipe="R-001"`} {
					if !strings.Contains(s, text) {
						t.Fatalf("missing %q", text)
					}
				}
				if _, err := html.Parse(strings.NewReader(s)); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(s, "ZgotmplZ") {
					t.Fatal("template rejected a trusted asset")
				}
				if book == 1 && (!strings.Contains(s, "Sections in this book") || strings.Contains(s, "Pages in this book")) {
					t.Fatal("cover count label does not describe sections")
				}
			})
		}
	}
}

func TestReaderFailureReturnsNoPartialDocument(t *testing.T) {
	for _, tc := range []struct {
		name     string
		canceled bool
	}{{"invalid-request", false}, {"canceled", true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			got, err := RenderReader(ctx, bookdocx.Request{Book: 1, Language: "en", Version: "v1"}, ReaderOptions{Book1: &Book1{}})
			if err == nil {
				t.Fatal("invalid request succeeded")
			}
			if len(got.DOCX) != 0 || len(got.PDF) != 0 || len(got.HTML) != 0 || got.LayoutVersion != "" {
				t.Fatal("failure returned a partial document")
			}
		})
	}
}

func TestReaderRequiresPresentationMetadata(t *testing.T) {
	var out bytes.Buffer
	if err := RenderReaderHTML(&out, readerFixture(t, 1, "en"), ReaderOptions{}); err == nil {
		t.Fatal("missing presentation metadata accepted")
	}
}
