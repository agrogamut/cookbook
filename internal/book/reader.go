package book

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/madamgy/recipie/internal/bookdocx"
)

const ReaderLayoutVersion = "pink-flow-v1"

type ReaderOptions struct {
	Book1 *Book1
	Book2 *Book2
}

type ReaderResult struct {
	bookdocx.Result
	HTML          []byte
	LayoutVersion string
}

type readerView struct {
	Options ReaderOptions
	Content template.HTML
	Book    int
}

func RenderReader(ctx context.Context, request bookdocx.Request, options ReaderOptions) (ReaderResult, error) {
	content, err := bookdocx.ReadContent(ctx, request)
	if err != nil {
		return ReaderResult{}, fmt.Errorf("reader content: %w", err)
	}
	var out bytes.Buffer
	if err := RenderReaderHTML(&out, content, options); err != nil {
		return ReaderResult{}, err
	}
	meta, err := readerMetadata(content.Book, content.Language, options)
	if err != nil {
		return ReaderResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ReaderResult{}, err
	}
	browser, err := sharedBrowser()
	if err != nil {
		return ReaderResult{}, err
	}
	pdf, err := printIn(ctx, browser, out.Bytes(), meta, readerFooter(meta))
	if err != nil {
		return ReaderResult{}, err
	}
	if len(pdf) > bookdocx.MaxPDFBytes {
		return ReaderResult{}, fmt.Errorf("reader PDF exceeds size limit")
	}
	return ReaderResult{Result: bookdocx.Result{Document: content.Document, PDF: pdf}, HTML: out.Bytes(), LayoutVersion: ReaderLayoutVersion}, nil
}

func readerMetadata(book int, language string, options ReaderOptions) (Metadata, error) {
	var meta Metadata
	switch {
	case book == 1 && options.Book1 != nil:
		meta = options.Book1.Metadata
	case book == 2 && options.Book2 != nil:
		meta = options.Book2.Metadata
	default:
		return Metadata{}, fmt.Errorf("reader requires book %d presentation metadata", book)
	}
	meta.Language = language
	return meta, nil
}

func RenderReaderHTML(w io.Writer, content bookdocx.ReaderDocument, options ReaderOptions) error {
	if options.Book1 != nil {
		b := *options.Book1
		if b.Metadata.Logo == "" {
			b.Metadata.Logo = logoDataURI
		}
		options.Book1 = &b
	}
	if options.Book2 != nil {
		b := *options.Book2
		if b.Metadata.Logo == "" {
			b.Metadata.Logo = logoDataURI
		}
		options.Book2 = &b
	}
	meta, err := readerMetadata(content.Book, content.Language, options)
	if err != nil {
		return err
	}
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		return err
	}
	overlay, err := templateFS.ReadFile("templates/reader.css")
	if err != nil {
		return err
	}
	funcs := template.FuncMap{"readerImprint": func(c renderContext) renderContext { c.Data = c.Data.(readerView).Options.Book2; return c }}
	t, err := template.New("base.html").Funcs(templateFuncs).Funcs(funcs).ParseFS(templateFS, "templates/base.html", "templates/book1/end.html", "templates/book2/cover.html", "templates/book2/end.html")
	if err != nil {
		return err
	}
	cover, err := templateFS.ReadFile("templates/book1/cover.html")
	if err != nil {
		return err
	}
	// The original value counts sections, not printed sheets.
	if _, err = t.Parse(strings.Replace(string(cover), "Pages in this book", "Sections in this book", 1)); err != nil {
		return err
	}
	if _, err = t.ParseFS(templateFS, "templates/reader.html"); err != nil {
		return err
	}
	view := readerView{Options: options, Content: readerBody(content), Book: content.Book}
	render := renderContext{Metadata: meta, BookClass: fmt.Sprintf("book%d", content.Book), CSS: template.CSS(string(css) + "\n" + string(overlay)), FontFaces: fontFacesCSS, Watermark: watermarkDataURI, Data: view}
	if err = t.ExecuteTemplate(w, "base.html", render); err != nil {
		return fmt.Errorf("render reader: %w", err)
	}
	return nil
}

func readerFooter(meta Metadata) string {
	release := ""
	if meta.ReleaseID != "" {
		release = " | release " + html.EscapeString(meta.ReleaseID)
	}
	return fmt.Sprintf(`<div style="font-size:10pt;width:100%%;padding:0 20mm;display:flex;gap:4mm;justify-content:space-between;align-items:baseline;font-family:'Noto Sans',sans-serif;color:#5a5a5a"><span style="overflow-wrap:anywhere">MadamGY &middot; %s%s &middot; generated %s</span><span class="pageNumber" style="flex:none;color:#1a1a1a"></span></div>`, html.EscapeString(meta.BookVersion), release, meta.GenerationDate.Format("2006-01-02"))
}

func readerRuns(p bookdocx.Paragraph) string {
	var out strings.Builder
	for _, r := range p.Runs {
		if r.Bold {
			out.WriteString("<strong>")
		}
		if r.Italic {
			out.WriteString("<em>")
		}
		out.WriteString(html.EscapeString(r.Text))
		if r.Italic {
			out.WriteString("</em>")
		}
		if r.Bold {
			out.WriteString("</strong>")
		}
	}
	return out.String()
}

func readerBody(d bookdocx.ReaderDocument) template.HTML {
	var out strings.Builder
	var moduleLabels []string
	lastHeading := -1
	for i, n := range d.Nodes {
		if n.Paragraph != nil && n.Paragraph.Style == "Heading1" {
			lastHeading = i
		}
	}
	formEnd := -1
	for i, n := range d.Nodes {
		if p := n.Paragraph; p != nil && p.Style == "Heading2" {
			end := i
			for end+1 < len(d.Nodes) && readerWritingLine(d.Nodes[end+1].Paragraph) {
				end++
			}
			if count := end - i; count > 0 && count <= 10 {
				out.WriteString(`<div class="reader-writing-form">`)
				formEnd = end
			}
		}
		if p := n.Paragraph; p != nil {
			tag, class := "p", ""
			switch p.Style {
			case "Title":
				tag, class = "h1", "reader-title"
			case "Heading1":
				tag = "h2"
				major := i == lastHeading
				for _, next := range d.Nodes[i+1:] {
					if next.Paragraph != nil && next.Paragraph.Style == "Heading1" {
						break
					}
					if next.Module != nil {
						major = true
						break
					}
				}
				if major {
					class = "reader-chapter"
				}
			case "Heading2":
				tag = "h3"
			case "ListBullet":
				class = "reader-bullet"
			}
			fmt.Fprintf(&out, `<%s class="%s">%s</%s>`, tag, class, readerRuns(p.Paragraph), tag)
		}
		if i == formEnd {
			out.WriteString("</div>")
			formEnd = -1
		}
		if t := n.Table; t != nil {
			if len(t.Rows) == 1 && len(t.Rows[0].Cells) == 3 && i+1 < len(d.Nodes) && d.Nodes[i+1].Module != nil {
				moduleLabels = nil
				for _, cell := range t.Rows[0].Cells {
					var label strings.Builder
					for _, p := range cell {
						label.WriteString(readerRuns(p.Paragraph))
					}
					moduleLabels = append(moduleLabels, label.String())
				}
				continue
			}
			out.WriteString(`<table class="reader-fixed"><tbody>`)
			for _, row := range t.Rows {
				out.WriteString("<tr>")
				for _, cell := range row.Cells {
					out.WriteString("<td>")
					for _, p := range cell {
						out.WriteString("<p>" + readerRuns(p.Paragraph) + "</p>")
					}
					out.WriteString("</td>")
				}
				out.WriteString("</tr>")
			}
			out.WriteString("</tbody></table>")
		}
		if m := n.Module; m != nil {
			heading := html.EscapeString(m.Code)
			if len(moduleLabels) == 3 {
				heading = `<span class="reader-label">` + moduleLabels[0] + `</span> ` + heading + ` <span class="reader-label">` + moduleLabels[1] + `</span>`
			}
			for _, p := range m.Title {
				heading += " " + readerRuns(p.Paragraph)
			}
			out.WriteString(`<section class="reader-module" data-module="` + html.EscapeString(m.Code) + `">`)
			readerContextStart(&out, heading, "reader-module-context")
			if len(moduleLabels) == 3 {
				readerRow(&out, `<p class="reader-label">`+moduleLabels[2]+`</p>`)
			}
			if m.Content.Gap != "" {
				readerRow(&out, "<p>"+html.EscapeString(m.Content.Gap)+"</p>")
			}
			readerBlocks(&out, m.Content.Blocks, m.Code)
			out.WriteString("</tbody></table>")
			for _, e := range m.Content.Entries {
				out.WriteString(`<div class="reader-recipe-start"></div><article class="recipe reader-recipe" data-recipe="` + html.EscapeString(e.ID) + `">`)
				head := `<p class="recipe-id"><span class="rid">` + html.EscapeString(e.ID) + `</span></p>`
				blocks := e.Blocks
				if len(blocks) > 0 && blocks[0].Paragraph != nil {
					head += `<div class="recipe-head"><h2>` + readerRuns(*blocks[0].Paragraph) + `</h2></div>`
					blocks = blocks[1:]
				}
				fmt.Fprintf(&out, `<table class="reader-context reader-recipe-context"><thead><tr><th>%s</th></tr></thead><tbody>`, head)
				if len(blocks) > 0 && blocks[0].Paragraph != nil {
					readerRow(&out, `<div class="metastrip"><div class="metacell"><span class="value">`+readerRuns(*blocks[0].Paragraph)+`</span></div></div>`)
					blocks = blocks[1:]
				}
				readerBlocks(&out, blocks, m.Code)
				out.WriteString("</tbody></table></article>")
			}
			out.WriteString("</section>")
		}
	}
	return template.HTML(out.String())
}

func readerWritingLine(p *bookdocx.ReaderParagraph) bool {
	if p == nil || p.Style != "" {
		return false
	}
	underscore := false
	for _, run := range p.Runs {
		for _, r := range run.Text {
			if r == '_' {
				underscore = true
				continue
			}
			if !unicode.IsSpace(r) && !unicode.IsDigit(r) && r != '.' {
				return false
			}
		}
	}
	return underscore
}

func readerContextStart(out *strings.Builder, heading, class string) {
	fmt.Fprintf(out, `<table class="reader-context %s"><thead><tr><th><h3>%s</h3></th></tr></thead><tbody>`, class, heading)
}

func readerRow(out *strings.Builder, body string) { out.WriteString("<tr><td>" + body + "</td></tr>") }

func readerBlocks(out *strings.Builder, blocks []bookdocx.Block, code string) {
	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		if code == "B1-50" && b.Paragraph != nil && i+1 < len(blocks) && blocks[i+1].Table != nil {
			t := blocks[i+1].Table
			if len(t.Rows) == 1 && len(t.Rows[0]) == 4 {
				var situation strings.Builder
				readerContextStart(&situation, readerRuns(*b.Paragraph), "illness reader-situation")
				row := t.Rows[0]
				for j, label := range []string{"What to do", "Watch for"} {
					readerRow(&situation, `<p class="reader-label">`+label+`</p><p>`+html.EscapeString(row[j])+`</p>`)
				}
				readerRow(&situation, `<div class="callout warning"><h3>Warning: seek medical care</h3><p>`+html.EscapeString(row[2])+`</p></div>`)
				readerRow(&situation, `<p class="domain-limit">`+html.EscapeString(row[3])+`</p>`)
				situation.WriteString("</tbody></table>")
				readerRow(out, situation.String())
				i++
				continue
			}
		}
		if b.Paragraph != nil {
			readerRow(out, "<p>"+readerRuns(*b.Paragraph)+"</p>")
		}
		if b.Image != nil {
			img := b.Image
			// Fill has already validated these bytes and dimensions.
			mime := img.MIMEType
			if mime == "" {
				mime = http.DetectContentType(img.Data)
			}
			uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
			limits := ""
			if img.WidthTwips > 0 {
				limits += fmt.Sprintf("max-width:min(100%%,%.2fpt);", float64(img.WidthTwips)/20)
			}
			if img.HeightTwips > 0 {
				limits += fmt.Sprintf("max-height:min(180mm,%.2fpt);", float64(img.HeightTwips)/20)
			}
			readerRow(out, fmt.Sprintf(`<figure class="reader-upload"><img src="%s" alt="%s" style="%s"></figure>`, html.EscapeString(uri), html.EscapeString(img.Alt), limits))
		}
		if b.Table != nil {
			readerRow(out, readerTable(*b.Table))
		}
	}
}

func readerTable(t bookdocx.Table) string {
	var out strings.Builder
	stacked := false
	for _, row := range t.Rows {
		if len(row) > 2 {
			if len(row) > 3 {
				stacked = true
			}
			for _, cell := range row {
				if utf8.RuneCountInString(cell) > 64 {
					stacked = true
				}
			}
		}
	}
	if stacked {
		out.WriteString(`<div class="reader-records">`)
		for i, row := range t.Rows {
			class := "reader-record"
			characters := 0
			for _, cell := range row {
				characters += utf8.RuneCountInString(cell)
			}
			if len(row) <= 6 && characters <= 300 {
				class += " reader-short-record"
			}
			fmt.Fprintf(&out, `<div class="%s">`, class)
			for j, cell := range row {
				if i > 0 && t.Header {
					out.WriteString(`<p class="reader-label">` + html.EscapeString(t.Rows[0][j]) + `</p>`)
				}
				out.WriteString("<p>" + html.EscapeString(cell) + "</p>")
			}
			out.WriteString("</div>")
		}
		out.WriteString("</div>")
	} else {
		out.WriteString(`<table class="reader-data">`)
		for i, row := range t.Rows {
			if i == 0 {
				if t.Header {
					out.WriteString("<thead>")
				} else {
					out.WriteString("<tbody>")
				}
			}
			out.WriteString("<tr>")
			tag := "td"
			if i == 0 && t.Header {
				tag = "th"
			}
			for _, cell := range row {
				fmt.Fprintf(&out, "<%s>%s</%s>", tag, html.EscapeString(cell), tag)
			}
			out.WriteString("</tr>")
			if i == 0 && t.Header {
				out.WriteString("</thead><tbody>")
			}
		}
		out.WriteString("</tbody></table>")
	}
	return out.String()
}
