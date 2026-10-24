package bookdocx

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type budget struct{ text, blocks, entries, runs, cells int }

func (b *budget) string(s string) error {
	if len(s) > MaxStringBytes || !utf8.ValidString(s) {
		return fmt.Errorf("invalid or oversized text")
	}
	for _, r := range s {
		if !(r == 9 || r == 10 || r == 13 || r >= 32 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF) {
			return fmt.Errorf("text contains invalid XML character U+%04X", r)
		}
	}
	b.text += len(s)
	if b.text > MaxTextBytes {
		return fmt.Errorf("request exceeds %d text bytes", MaxTextBytes)
	}
	return nil
}

func (b *budget) validateBlocks(blocks []Block) error {
	b.blocks += len(blocks)
	if b.blocks > MaxBlocks {
		return fmt.Errorf("too many blocks")
	}
	for _, block := range blocks {
		if (block.Paragraph == nil) == (block.Table == nil) {
			return fmt.Errorf("block must contain exactly one paragraph or table")
		}
		if p := block.Paragraph; p != nil {
			b.runs += len(p.Runs)
			if b.runs > MaxRuns {
				return fmt.Errorf("too many text runs")
			}
			if len(p.Runs) == 0 || len(p.Runs) > 1000 {
				return fmt.Errorf("paragraph requires 1 to 1000 runs")
			}
			for _, r := range p.Runs {
				if err := b.string(r.Text); err != nil {
					return err
				}
			}
		}
		if t := block.Table; t != nil {
			if len(t.Rows) == 0 || len(t.Rows) > MaxTableRows {
				return fmt.Errorf("table requires 1 to %d rows", MaxTableRows)
			}
			cols := len(t.Rows[0])
			if cols < 1 || cols > 6 {
				return fmt.Errorf("table requires 1 to 6 columns")
			}
			for _, row := range t.Rows {
				b.cells += len(row)
				if b.cells > MaxTableCells {
					return fmt.Errorf("too many table cells")
				}
				if len(row) != cols {
					return fmt.Errorf("table must be rectangular")
				}
				for _, v := range row {
					if err := b.string(v); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (b *budget) module(m Module) error {
	filled := len(m.Blocks)+len(m.Entries) > 0
	if (strings.TrimSpace(m.Gap) != "") == filled {
		return fmt.Errorf("module requires content or an explicit gap, exclusively")
	}
	if m.Gap != "" && filled {
		return fmt.Errorf("gap cannot accompany content")
	}
	if err := b.string(m.Gap); err != nil {
		return err
	}
	if err := b.validateBlocks(m.Blocks); err != nil {
		return err
	}
	b.entries += len(m.Entries)
	if len(m.Entries) > MaxEntriesPerModule || b.entries > MaxEntries {
		return fmt.Errorf("too many repeated entries")
	}
	ids := map[string]bool{}
	for _, e := range m.Entries {
		if strings.TrimSpace(e.ID) == "" || ids[e.ID] || len(e.Blocks) == 0 {
			return fmt.Errorf("entry requires a unique nonempty ID and content")
		}
		ids[e.ID] = true
		if err := b.string(e.ID); err != nil {
			return err
		}
		if err := b.validateBlocks(e.Blocks); err != nil {
			return err
		}
	}
	return nil
}

func paragraph(p Paragraph, language, pPr string) string {
	var b strings.Builder
	b.WriteString("<w:p>" + pPr)
	for _, r := range p.Runs {
		b.WriteString("<w:r><w:rPr>" + runFonts(language))
		if r.Bold {
			b.WriteString("<w:b/><w:bCs/>")
		}
		if r.Italic {
			b.WriteString("<w:i/><w:iCs/>")
		}
		b.WriteString("</w:rPr>")
		// Tabs and line feeds are Word elements, not whitespace inside w:t.
		start := 0
		for i, c := range r.Text {
			if c != '\n' && c != '\t' && c != '\r' {
				continue
			}
			b.WriteString(`<w:t xml:space="preserve">` + escaped(r.Text[start:i]) + `</w:t>`)
			if c == '\t' {
				b.WriteString("<w:tab/>")
			} else if c == '\n' || i+1 == len(r.Text) || r.Text[i+1] != '\n' {
				b.WriteString("<w:br/>")
			}
			start = i + 1
		}
		b.WriteString(`<w:t xml:space="preserve">` + escaped(r.Text[start:]) + `</w:t></w:r>`)
	}
	b.WriteString("</w:p>")
	return b.String()
}

func renderBlocks(blocks []Block, language, pPr string, width int) string {
	var b strings.Builder
	for _, block := range blocks {
		if p := block.Paragraph; p != nil {
			b.WriteString(paragraph(*p, language, pPr))
			continue
		}
		t := block.Table
		cols := len(t.Rows[0])
		colWidth := width / cols
		// Relative width prevents column collapse when older Writer versions split the outer row.
		b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="5000" w:type="pct"/><w:jc w:val="left"/><w:tblLayout w:type="fixed"/><w:tblCellMar><w:left w:w="100" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tblCellMar></w:tblPr><w:tblGrid>`)
		for range cols {
			b.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, colWidth))
		}
		b.WriteString("</w:tblGrid>")
		for i, row := range t.Rows {
			b.WriteString("<w:tr>")
			if i == 0 && t.Header {
				b.WriteString("<w:trPr><w:tblHeader/></w:trPr>")
			}
			for _, v := range row {
				b.WriteString(fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/></w:tcPr>`, colWidth))
				b.WriteString(paragraph(Paragraph{Runs: []Run{{Text: v, Bold: i == 0 && t.Header}}}, language, pPr))
				b.WriteString("</w:tc>")
			}
			b.WriteString("</w:tr>")
		}
		b.WriteString("</w:tbl><w:p/>")
	}
	return b.String()
}

func moduleXML(m Module, language, pPr string, width int) []byte {
	if m.Gap != "" {
		return []byte(paragraph(Paragraph{Runs: []Run{{Text: m.Gap}}}, language, pPr))
	}
	var b strings.Builder
	b.WriteString(renderBlocks(m.Blocks, language, pPr, width))
	for _, e := range m.Entries {
		b.WriteString(paragraph(Paragraph{Runs: []Run{{Text: e.ID, Bold: true}}}, language, pPr))
		b.WriteString(renderBlocks(e.Blocks, language, pPr, width))
	}
	return []byte(b.String())
}
