package bookdocx

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed page_starts.json
var pageStartsJSON []byte

type pageStart struct {
	Book            int    `json:"book"`
	Language        string `json:"language"`
	Version         string `json:"version"`
	SHA256          string `json:"sha256"`
	Boundary        string `json:"boundary"`
	BodyChild       int    `json:"body_child"`
	HeadingSHA256   string `json:"heading_sha256"`
	PrecedingModule string `json:"preceding_module"`
}

// Only these verified boundaries avoid empty sheets without clipping long tables.
func pageStartEdits(data []byte, m Manifest) ([]edit, error) {
	var all []pageStart
	if err := json.Unmarshal(pageStartsJSON, &all); err != nil {
		return nil, fmt.Errorf("page start manifest: %w", err)
	}
	root, err := parseXML(data)
	if err != nil {
		return nil, err
	}
	docs := root.direct("document")
	if len(docs) != 1 || len(docs[0].direct("body")) != 1 {
		return nil, fmt.Errorf("missing page start document body")
	}
	body := docs[0].direct("body")[0].children
	codes := map[string]bool{}
	for _, b := range m.Bindings {
		codes[b.Code] = true
	}
	var edits []edit
	seen := map[string]bool{}
	for _, p := range all {
		if p.Book != m.Book || p.Language != m.Language || p.Version != m.Version {
			continue
		}
		if p.SHA256 != m.SHA256 || seen[p.Boundary] || p.BodyChild < 0 || p.BodyChild+1 >= len(body) {
			return nil, fmt.Errorf("page start identity mismatch: %s", p.Boundary)
		}
		seen[p.Boundary] = true
		carrier, heading := body[p.BodyChild], body[p.BodyChild+1]
		preceding := ""
		for _, n := range body[:p.BodyChild] {
			if !n.is("tbl") {
				continue
			}
			for _, row := range n.direct("tr") {
				cells := row.direct("tc")
				if len(cells) > 0 && codes[cells[0].words()] {
					preceding = cells[0].words()
				}
			}
		}
		pair := pageStartPair(data, carrier, heading)
		if hash(data[heading.start:heading.end]) != p.HeadingSHA256 || preceding != p.PrecedingModule || len(pair) != 2 {
			return nil, fmt.Errorf("page start anchor mismatch: %s after %s", p.Boundary, p.PrecedingModule)
		}
		edits = append(edits, pair...)
	}
	if !seen["faq-to-review"] || len(seen) != m.Book || (m.Book == 2 && !seen["section8-to-section9"]) {
		return nil, fmt.Errorf("missing page start manifest: book %d language %q version %q", m.Book, m.Language, m.Version)
	}
	return edits, nil
}

func pageStartPair(data []byte, carrier, heading *node) []edit {
	if !bytes.Equal(data[carrier.start:carrier.end], []byte(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)) ||
		!heading.is("p") || string(data[heading.start:heading.openEnd]) != "<w:p>" || len(heading.children) < 2 || strings.TrimSpace(heading.words()) == "" {
		return nil
	}
	props := heading.children[0]
	if !bytes.Equal(data[props.start:props.end], []byte(`<w:pPr><w:pStyle w:val="Heading1"/></w:pPr>`)) {
		return nil
	}
	for _, run := range heading.children[1:] {
		if !run.is("r") {
			return nil
		}
		for _, child := range run.children {
			if !child.is("rPr") && !child.is("t") {
				return nil
			}
		}
	}
	return []edit{{carrier.start, carrier.end, nil}, {props.closeStart, props.closeStart, []byte(`<w:pageBreakBefore/>`)}}
}
