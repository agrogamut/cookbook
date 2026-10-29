package bookdocx

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/madamgy/recipie/internal/bookmasters"
)

type ReaderDocument struct {
	Document
	Nodes []ReaderNode
}

type ReaderNode struct {
	Paragraph *ReaderParagraph
	Table     *ReaderTable
	Module    *ReaderModule
}

type ReaderParagraph struct {
	Paragraph
	Style string
}

type ReaderTable struct {
	Rows []ReaderRow
	// ContextParagraphs identifies the preceding source heading/intro for a repeating header.
	ContextParagraphs int
}
type ReaderRow struct{ Cells [][]ReaderParagraph }

type ReaderModule struct {
	Code    string
	Title   []ReaderParagraph
	Content Module
}

// ReadContent preserves master body order while exposing filled cells independently of their grid.
func ReadContent(ctx context.Context, req Request) (ReaderDocument, error) {
	doc, err := Fill(ctx, req)
	if err != nil {
		return ReaderDocument{}, err
	}
	master, _, err := bookmasters.Read(req.Book, req.Language, req.Version)
	if err != nil {
		return ReaderDocument{}, err
	}
	m, err := Bindings(req.Book, req.Language, req.Version)
	if err != nil {
		return ReaderDocument{}, err
	}
	z, err := zip.NewReader(bytes.NewReader(master), int64(len(master)))
	if err != nil {
		return ReaderDocument{}, err
	}
	f, err := z.Open("word/document.xml")
	if err != nil {
		return ReaderDocument{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, MaxDOCXBytes+1))
	closeErr := f.Close()
	if readErr != nil {
		return ReaderDocument{}, readErr
	}
	if closeErr != nil {
		return ReaderDocument{}, closeErr
	}
	if len(data) > MaxDOCXBytes {
		return ReaderDocument{}, fmt.Errorf("reader master body exceeds size limit")
	}
	if _, err := locate(data, m); err != nil {
		return ReaderDocument{}, err
	}
	root, err := parseXML(data)
	if err != nil {
		return ReaderDocument{}, err
	}
	bound := make(map[[2]int]Binding, len(m.Bindings))
	for _, b := range m.Bindings {
		bound[[2]int{b.Table, b.Row}] = b
	}
	out := ReaderDocument{Document: doc}
	tableIndex := 0
	for _, n := range root.direct("document")[0].direct("body")[0].children {
		if err := ctx.Err(); err != nil {
			return ReaderDocument{}, err
		}
		switch {
		case n.is("p"):
			p := readerParagraph(n)
			if len(p.Runs) > 0 {
				out.Nodes = append(out.Nodes, ReaderNode{Paragraph: &p})
			}
		case n.is("tbl"):
			tableIndex++
			var pending ReaderTable
			flush := func() error {
				if len(pending.Rows) > 0 {
					t := pending
					t.ContextParagraphs, err = readerFixedContext(req, tableIndex, t, out.Nodes)
					if err != nil {
						return err
					}
					out.Nodes = append(out.Nodes, ReaderNode{Table: &t})
					pending = ReaderTable{}
				}
				return nil
			}
			for i, row := range n.direct("tr") {
				cells := row.direct("tc")
				if b, ok := bound[[2]int{tableIndex, i + 1}]; ok {
					if err := flush(); err != nil {
						return ReaderDocument{}, err
					}
					out.Nodes = append(out.Nodes, ReaderNode{Module: &ReaderModule{
						Code: b.Code, Title: readerCell(cells[1]), Content: copyModule(req.Modules[b.Code]),
					}})
					continue
				}
				r := ReaderRow{}
				for _, c := range cells {
					r.Cells = append(r.Cells, readerCell(c))
				}
				pending.Rows = append(pending.Rows, r)
			}
			if err := flush(); err != nil {
				return ReaderDocument{}, err
			}
		}
	}
	return out, nil
}

func readerFixedContext(req Request, tableIndex int, table ReaderTable, preceding []ReaderNode) (int, error) {
	count, columns := 0, 0
	// These semantic locations belong to the v1 masters whose complete hashes were validated above.
	if req.Version == "v1" {
		if tableIndex == 1 {
			count, columns = 1, 3
		} else if req.Book == 1 && tableIndex == 15 || req.Book == 2 && tableIndex == 11 {
			count, columns = 2, 2
		}
	}
	if count == 0 {
		return 0, nil
	}
	if len(table.Rows) < 2 || len(preceding) < count {
		return 0, fmt.Errorf("reader fixed table %d lacks source context or data", tableIndex)
	}
	for _, row := range table.Rows {
		if len(row.Cells) != columns {
			return 0, fmt.Errorf("reader fixed table %d has unexpected columns", tableIndex)
		}
	}
	context := preceding[len(preceding)-count:]
	for i, node := range context {
		if node.Paragraph == nil || len(node.Paragraph.Runs) == 0 || i == 0 && node.Paragraph.Style != "Heading1" {
			return 0, fmt.Errorf("reader fixed table %d has unexpected source context", tableIndex)
		}
	}
	return count, nil
}

func readerCell(n *node) []ReaderParagraph {
	var out []ReaderParagraph
	for _, p := range n.direct("p") {
		out = append(out, readerParagraph(p))
	}
	return out
}

func readerParagraph(n *node) ReaderParagraph {
	p := ReaderParagraph{}
	if props := n.direct("pPr"); len(props) > 0 {
		if styles := props[0].direct("pStyle"); len(styles) > 0 {
			p.Style = styles[0].attr("val")
		}
	}
	n.walk(func(r *node) {
		if !r.is("r") {
			return
		}
		var text strings.Builder
		r.walk(func(c *node) {
			switch {
			case c.is("t"):
				text.WriteString(c.text)
			case c.is("tab"):
				text.WriteByte('\t')
			case c.is("br") && c.attr("type") != "page", c.is("cr"):
				text.WriteByte('\n')
			}
		})
		if text.Len() == 0 {
			return
		}
		run := Run{Text: text.String()}
		if props := r.direct("rPr"); len(props) > 0 {
			for _, prop := range props[0].children {
				enabled := prop.attr("val") != "0" && prop.attr("val") != "false"
				if prop.is("b") {
					run.Bold = enabled
				}
				if prop.is("i") {
					run.Italic = enabled
				}
			}
		}
		p.Runs = append(p.Runs, run)
	})
	return p
}

func copyModule(m Module) Module {
	copyBlocks := func(in []Block) []Block {
		out := make([]Block, len(in))
		for i, b := range in {
			if b.Paragraph != nil {
				out[i].Paragraph = &Paragraph{Runs: append([]Run(nil), b.Paragraph.Runs...)}
			}
			if b.Table != nil {
				t := &Table{Header: b.Table.Header}
				for _, row := range b.Table.Rows {
					t.Rows = append(t.Rows, append([]string(nil), row...))
				}
				out[i].Table = t
			}
			if b.Image != nil {
				img := *b.Image
				img.Data = append([]byte(nil), img.Data...)
				out[i].Image = &img
			}
		}
		return out
	}
	out := Module{Gap: m.Gap, Blocks: copyBlocks(m.Blocks)}
	for _, e := range m.Entries {
		out.Entries = append(out.Entries, Entry{ID: e.ID, Blocks: copyBlocks(e.Blocks)})
	}
	return out
}
