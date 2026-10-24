// Package bookdocx fills the declared cells of versioned Word masters.
package bookdocx

import "context"

type Request struct {
	Book     int
	Language string
	Version  string
	Modules  map[string]Module
}
type Module struct {
	Blocks  []Block
	Entries []Entry
	Gap     string
}
type Entry struct {
	ID     string
	Blocks []Block
}

// Block contains exactly one kind of native Word content.
type Block struct {
	Paragraph *Paragraph
	Table     *Table
	Image     *Image
}
type Image struct {
	Data        []byte
	MIMEType    string
	Alt         string
	WidthTwips  int
	HeightTwips int
}
type Paragraph struct{ Runs []Run }
type Run struct {
	Text   string
	Bold   bool
	Italic bool
}
type Table struct {
	Rows   [][]string
	Header bool
}
type Omission struct{ Module, Reason string }
type Document struct {
	DOCX           []byte
	Book           int
	Language       string
	Version        string
	TemplateSHA256 string
	Omissions      []Omission
	digest         [32]byte
}
type Result struct {
	Document
	PDF []byte
}

func (c *Converter) Render(ctx context.Context, req Request) (Result, error) {
	doc, err := Fill(ctx, req)
	if err != nil {
		return Result{}, err
	}
	pdf, err := c.PDF(ctx, doc)
	return Result{Document: doc, PDF: pdf}, err
}

const (
	MaxTextBytes        = 4 << 20
	MaxStringBytes      = 64 << 10
	MaxEntriesPerModule = 500
	MaxEntries          = 2000
	MaxBlocks           = 20000
	MaxTableCells       = 20000
	MaxRuns             = 50000
	MaxTableRows        = 1000
	MaxDOCXBytes        = 16 << 20
	MaxPDFBytes         = 64 << 20
	MaxImageBytes       = 8 << 20
	MaxTotalImageBytes  = 12 << 20
	MaxImages           = 64
	MaxImageDimension   = 8192
	MaxImagePixels      = 16000000
	MaxTotalImagePixels = 32000000
	MaxImageTwips       = 7200
)
