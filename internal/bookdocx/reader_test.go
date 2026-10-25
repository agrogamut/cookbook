package bookdocx

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func readerWords(d ReaderDocument) string {
	var out strings.Builder
	p := func(p Paragraph) {
		for _, r := range p.Runs {
			out.WriteString(r.Text)
		}
	}
	blocks := func(blocks []Block) {
		for _, b := range blocks {
			if b.Paragraph != nil {
				p(*b.Paragraph)
			}
			if b.Table != nil {
				for _, row := range b.Table.Rows {
					for _, cell := range row {
						out.WriteString(cell)
					}
				}
			}
		}
	}
	for _, n := range d.Nodes {
		if n.Paragraph != nil {
			p(n.Paragraph.Paragraph)
		}
		if n.Table != nil {
			for _, row := range n.Table.Rows {
				for _, cell := range row.Cells {
					for _, para := range cell {
						p(para.Paragraph)
					}
				}
			}
		}
		if n.Module != nil {
			out.WriteString(n.Module.Code)
			for _, title := range n.Module.Title {
				p(title.Paragraph)
			}
			m := n.Module.Content
			out.WriteString(m.Gap)
			blocks(m.Blocks)
			for _, e := range m.Entries {
				out.WriteString(e.ID)
				blocks(e.Blocks)
			}
		}
	}
	return out.String()
}

func TestReaderPreservesAllMasterTextAndBindingOrder(t *testing.T) {
	for _, lang := range []string{"en", "bn", "hi"} {
		for _, book := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d-%s", book, lang), func(t *testing.T) {
				r := request(t, book, lang)
				code := fmt.Sprintf("B%d-01", book)
				r.Modules[code] = Module{Blocks: []Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "<exact & "}, {Text: "quantity>", Bold: true}, {Text: " 0007 / 12.50 g"}}}}, {Table: &Table{Header: true, Rows: [][]string{{"ID", "Quantity"}, {"R/001", "0.005 g"}}}}}, Entries: []Entry{{ID: "CARD-001", Blocks: []Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "source wording"}}}}}}}}
				d, err := ReadContent(context.Background(), r)
				if err != nil {
					t.Fatal(err)
				}
				x, err := parseXML(parts(t, d.DOCX)["word/document.xml"])
				if err != nil {
					t.Fatal(err)
				}
				if got, want := readerWords(d), x.words(); got != want {
					t.Fatalf("reader text/order differs: lengths %d/%d", len(got), len(want))
				}
				count := 0
				for _, n := range d.Nodes {
					if n.Module != nil {
						count++
					}
				}
				want := 63
				if book == 2 {
					want = 56
				}
				if count != want {
					t.Fatalf("module count %d, want %d", count, want)
				}
			})
		}
	}
}

func TestReaderRejectsInvalidRequestsAndCancellation(t *testing.T) {
	for _, change := range []func(*Request){func(r *Request) { r.Book = 3 }, func(r *Request) { r.Language = "xx" }, func(r *Request) { r.Version = "v99" }, func(r *Request) { delete(r.Modules, "B1-01") }, func(r *Request) { r.Modules["unknown"] = Module{Gap: "unknown"} }} {
		r := request(t, 1, "en")
		change(&r)
		if _, err := ReadContent(context.Background(), r); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadContent(ctx, request(t, 1, "en")); err == nil {
		t.Fatal("cancelled request accepted")
	}
}

func TestReaderCopiesCallerContent(t *testing.T) {
	r := request(t, 1, "en")
	r.Modules["B1-01"] = Module{Blocks: []Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "original"}}}}, {Table: &Table{Rows: [][]string{{"exact"}}}}}}
	d, err := ReadContent(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	r.Modules["B1-01"].Blocks[0].Paragraph.Runs[0].Text = "changed"
	r.Modules["B1-01"].Blocks[1].Table.Rows[0][0] = "changed"
	if strings.Contains(readerWords(d), "changed") {
		t.Fatal("reader retains caller-owned content")
	}
}
