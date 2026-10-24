package bookdocx

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

const wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

type node struct {
	name                            xml.Name
	attrs                           []xml.Attr
	start, openEnd, closeStart, end int
	children                        []*node
	text                            string
}

func parseXML(data []byte) (*node, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	root := &node{}
	stack := []*node{root}
	for {
		start := int(d.InputOffset())
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse document XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name, attrs: t.Attr, start: start, openEnd: int(d.InputOffset())}
			p := stack[len(stack)-1]
			p.children = append(p.children, n)
			stack = append(stack, n)
		case xml.EndElement:
			n := stack[len(stack)-1]
			n.closeStart, n.end = start, int(d.InputOffset())
			stack = stack[:len(stack)-1]
		case xml.CharData:
			stack[len(stack)-1].text += string(t)
		}
	}
	return root, nil
}

func (n *node) is(local string) bool { return n.name.Space == wordNS && n.name.Local == local }
func (n *node) direct(local string) []*node {
	var out []*node
	for _, c := range n.children {
		if c.is(local) {
			out = append(out, c)
		}
	}
	return out
}
func (n *node) walk(fn func(*node)) {
	fn(n)
	for _, c := range n.children {
		c.walk(fn)
	}
}
func (n *node) words() string {
	var b strings.Builder
	n.walk(func(c *node) {
		if c.is("t") {
			b.WriteString(c.text)
		}
	})
	return b.String()
}
func (n *node) attr(local string) string {
	for _, a := range n.attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

type edit struct {
	start, end int
	value      []byte
}

func splice(data []byte, edits []edit) ([]byte, error) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var b bytes.Buffer
	pos := 0
	for _, e := range edits {
		if e.start < pos || e.end < e.start || e.end > len(data) {
			return nil, fmt.Errorf("overlapping or invalid XML edit")
		}
		b.Write(data[pos:e.start])
		b.Write(e.value)
		pos = e.end
	}
	b.Write(data[pos:])
	return b.Bytes(), nil
}

func escaped(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func complexFont(language string) string {
	switch language {
	case "bn":
		return "Noto Sans Bengali"
	case "hi":
		return "Noto Sans Devanagari"
	}
	return "Noto Sans"
}

func runFonts(language string) string {
	return `<w:rFonts w:ascii="Noto Sans" w:hAnsi="Noto Sans" w:eastAsia="Noto Sans" w:cs="` + complexFont(language) + `"/>`
}

// Only font declarations change; text, metrics, colors and field instructions stay byte-identical.
func mapFonts(data []byte, language string) ([]byte, error) {
	root, err := parseXML(data)
	if err != nil {
		return nil, err
	}
	var edits []edit
	root.walk(func(n *node) {
		if n.is("rFonts") {
			edits = append(edits, edit{n.start, n.end, []byte(runFonts(language))})
		}
		if n.name.Space == "http://schemas.openxmlformats.org/drawingml/2006/main" && (n.name.Local == "latin" || n.name.Local == "ea" || n.name.Local == "cs" || n.name.Local == "font") {
			font := "Noto Sans"
			switch n.attr("script") {
			case "Beng":
				font = "Noto Sans Bengali"
			case "Deva":
				font = "Noto Sans Devanagari"
			}
			if n.name.Local == "cs" {
				font = complexFont(language)
			}
			var b strings.Builder
			b.WriteString("<a:" + n.name.Local)
			for _, a := range n.attrs {
				if a.Name.Local == "typeface" {
					a.Value = font
				}
				b.WriteString(" " + a.Name.Local + `="` + escaped(a.Value) + `"`)
			}
			b.WriteString("/>")
			edits = append(edits, edit{n.start, n.end, []byte(b.String())})
		}
	})
	return splice(data, edits)
}
