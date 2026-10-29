package book

import (
	"bytes"
	"context"
	"encoding/json"
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
				copy, err := readerLocale(language)
				if err != nil {
					t.Fatal(err)
				}
				if book == 1 && (!strings.Contains(s, copy["section_count_label"]) || strings.Contains(s, "Pages in this book")) {
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

func TestReaderCoverCountsFromPrintedContent(t *testing.T) {
	for _, language := range []string{"en", "bn", "hi"} {
		for _, number := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/book%d", language, number), func(t *testing.T) {
				d := readerFixture(t, number, language)
				request := bookdocx.Request{Book: number, Language: language, Version: "v1", Modules: map[string]bookdocx.Module{}}
				for _, node := range d.Nodes {
					if node.Module != nil {
						request.Modules[node.Module.Code] = node.Module.Content
					}
				}
				paragraph := func(text string) bookdocx.Block {
					return bookdocx.Block{Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: text}}}}
				}
				if number == 2 {
					request.Modules["B2-02"] = bookdocx.Module{
						Blocks:  []bookdocx.Block{paragraph("References R-001 R-001 R-002 INDEX-ONLY")},
						Entries: []bookdocx.Entry{{ID: "R-002", Blocks: []bookdocx.Block{paragraph("Second full card")}}},
					}
				}
				d, err := bookdocx.ReadContent(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
				want := "63 " + readerTestLocale(t, language)["section_count_unit"]
				if number == 2 {
					want = "2"
				}
				for _, legacy := range []string{"empty", "mismatched"} {
					t.Run(legacy, func(t *testing.T) {
						options := ReaderOptions{Book1: &Book1{}, Book2: &Book2{}}
						if legacy == "mismatched" {
							options.Book1.Sections = []Section{{Title: "Legacy section unrelated to reader Parts"}}
							options.Book2.MealSections = []MealSection{{Recipes: []RecipeCard{{RecipeID: "R-001"}, {RecipeID: "R-001"}, {}}}}
						}
						assertReaderCoverValue(t, d, options, want)
					})
				}
				if number != 2 {
					return
				}
				t.Run("unique-full-card-identity", func(t *testing.T) {
					request.Modules["B2-03"] = bookdocx.Module{Entries: []bookdocx.Entry{{ID: "R-001", Blocks: []bookdocx.Block{paragraph("Repeated full card identity")}}}}
					repeated, err := bookdocx.ReadContent(context.Background(), request)
					if err != nil {
						t.Fatal(err)
					}
					assertReaderCoverValue(t, repeated, ReaderOptions{Book2: &Book2{}}, "2")
				})
				t.Run("no-full-cards", func(t *testing.T) {
					for code := range request.Modules {
						request.Modules[code] = bookdocx.Module{Gap: "R-001 reference only"}
					}
					empty, err := bookdocx.ReadContent(context.Background(), request)
					if err != nil {
						t.Fatal(err)
					}
					options := ReaderOptions{Book2: &Book2{MealSections: []MealSection{{Recipes: []RecipeCard{{RecipeID: "LEGACY-ONLY"}}}}}}
					assertReaderCoverValue(t, empty, options, "0")
				})
			})
		}
	}
}

func assertReaderCoverValue(t *testing.T, content bookdocx.ReaderDocument, options ReaderOptions, value string) {
	t.Helper()
	snapshot := func() []byte {
		data, err := json.Marshal(struct {
			Content bookdocx.ReaderDocument
			Options ReaderOptions
		}{content, options})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := snapshot()
	var out bytes.Buffer
	if err := RenderReaderHTML(&out, content, options); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("caller content or metadata mutated")
	}
	cover := strings.SplitN(out.String(), `<main class="reader-flow"`, 2)[0]
	want := `<span class="value mono">` + value + `</span>`
	if !strings.Contains(cover, want) {
		t.Fatalf("cover does not contain printed-content count %q", want)
	}
}

func TestReaderFixedTablesRepeatSourceContext(t *testing.T) {
	for _, language := range []string{"en", "bn", "hi"} {
		for _, number := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/book%d", language, number), func(t *testing.T) {
				d := readerFixture(t, number, language)
				var out bytes.Buffer
				if err := RenderReaderHTML(&out, d, ReaderOptions{Book1: &Book1{}, Book2: &Book2{}}); err != nil {
					t.Fatal(err)
				}
				if got := strings.Count(out.String(), `<table class="reader-fixed reader-fixed-context">`); got != 2 {
					t.Fatalf("repeating source-context tables %d, want contents and FAQ", got)
				}
				root, err := html.Parse(strings.NewReader(out.String()))
				if err != nil {
					t.Fatal(err)
				}
				var tables []*html.Node
				var visit func(*html.Node)
				visit = func(n *html.Node) {
					for _, a := range n.Attr {
						if n.Data == "table" && a.Key == "class" && a.Val == "reader-fixed reader-fixed-context" {
							tables = append(tables, n)
						}
					}
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						visit(c)
					}
				}
				visit(root)
				var sourceTables []bookdocx.ReaderTable
				var contexts [][]bookdocx.ReaderNode
				for i, n := range d.Nodes {
					if n.Table == nil {
						continue
					}
					count := 0
					if len(sourceTables) == 0 {
						count = 1
					} else if len(n.Table.Rows) == 15 && len(n.Table.Rows[0].Cells) == 2 {
						count = 2
					}
					if count > 0 {
						sourceTables = append(sourceTables, *n.Table)
						contexts = append(contexts, d.Nodes[i-count:i])
					}
				}
				if len(sourceTables) != 2 || len(tables) != 2 {
					t.Fatal("expected two semantic fixed tables")
				}
				for i, table := range tables {
					var head, body *html.Node
					for n := table.FirstChild; n != nil; n = n.NextSibling {
						if n.Data == "thead" {
							head = n
						} else if n.Data == "tbody" {
							body = n
						}
					}
					if head == nil || body == nil {
						t.Fatal("fixed table needs a repeating header and data body")
					}
					text := func(node *html.Node) string {
						var b bytes.Buffer
						if err := html.Render(&b, node); err != nil {
							t.Fatal(err)
						}
						return readerVisibleText(t, b.String())
					}
					var wantHead, wantBody strings.Builder
					for _, n := range contexts[i] {
						wantHead.WriteString(readerRuns(n.Paragraph.Paragraph) + " ")
					}
					for r, row := range sourceTables[i].Rows {
						dest := &wantBody
						if r == 0 {
							dest = &wantHead
						}
						for _, cell := range row.Cells {
							for _, p := range cell {
								dest.WriteString(readerRuns(p.Paragraph) + " ")
							}
						}
					}
					if text(head) != readerVisibleText(t, wantHead.String()) || text(body) != readerVisibleText(t, wantBody.String()) {
						t.Fatal("fixed context/header/data words or order changed")
					}
				}
			})
		}
	}
}
