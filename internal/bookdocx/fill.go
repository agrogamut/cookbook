package bookdocx

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/madamgy/recipie/internal/bookmasters"
)

//go:embed bindings.json
var manifestJSON []byte

type Binding struct {
	Code         string `json:"code"`
	Part         string `json:"part"`
	Table        int    `json:"table"`
	Row          int    `json:"row"`
	Column       int    `json:"column"`
	XPath        string `json:"xpath"`
	CodeText     string `json:"code_text"`
	TitleSHA256  string `json:"title_sha256"`
	OutputSHA256 string `json:"output_sha256"`
}
type Manifest struct {
	Book     int       `json:"book"`
	Language string    `json:"language"`
	Version  string    `json:"version"`
	SHA256   string    `json:"sha256"`
	Bindings []Binding `json:"bindings"`
}

func Bindings(book int, language, version string) (Manifest, error) {
	var all []Manifest
	if err := json.Unmarshal(manifestJSON, &all); err != nil {
		return Manifest{}, fmt.Errorf("binding manifest: %w", err)
	}
	for _, m := range all {
		if m.Book == book && m.Language == language && m.Version == version {
			return m, nil
		}
	}
	return Manifest{}, fmt.Errorf("unknown template: book %d language %q version %q", book, language, version)
}

func hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func locate(data []byte, m Manifest) ([]*node, error) {
	root, err := parseXML(data)
	if err != nil {
		return nil, err
	}
	doc := root.direct("document")
	if len(doc) != 1 {
		return nil, fmt.Errorf("missing document root")
	}
	body := doc[0].direct("body")
	if len(body) != 1 {
		return nil, fmt.Errorf("missing document body")
	}
	tables := body[0].direct("tbl")
	var cells []*node
	seen := map[string]bool{}
	for _, b := range m.Bindings {
		if b.Part != "word/document.xml" || b.Column != 3 || b.Table < 1 || b.Table > len(tables) || seen[b.Code] {
			return nil, fmt.Errorf("invalid binding: %s", b.Code)
		}
		seen[b.Code] = true
		rows := tables[b.Table-1].direct("tr")
		if b.Row < 1 || b.Row > len(rows) {
			return nil, fmt.Errorf("binding row mismatch: %s", b.Code)
		}
		tc := rows[b.Row-1].direct("tc")
		path := fmt.Sprintf("/w:document/w:body/w:tbl[%d]/w:tr[%d]/w:tc[3]", b.Table, b.Row)
		if len(tc) != 3 || path != b.XPath || tc[0].words() != b.Code || b.CodeText != b.Code || hash([]byte(tc[1].words())) != b.TitleSHA256 || hash([]byte(tc[2].words())) != b.OutputSHA256 {
			return nil, fmt.Errorf("binding anchor mismatch: %s at %s", b.Code, b.XPath)
		}
		cells = append(cells, tc[2])
	}
	return cells, nil
}

func Fill(ctx context.Context, req Request) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	m, err := Bindings(req.Book, req.Language, req.Version)
	if err != nil {
		return Document{}, err
	}
	master, meta, err := bookmasters.Read(req.Book, req.Language, req.Version)
	if err != nil {
		return Document{}, err
	}
	if meta.SHA256 != m.SHA256 || hash(master) != m.SHA256 {
		return Document{}, fmt.Errorf("template manifest hash mismatch")
	}
	return fill(ctx, req, master, m)
}

func fill(ctx context.Context, req Request, master []byte, m Manifest) (Document, error) {
	if hash(master) != m.SHA256 {
		return Document{}, fmt.Errorf("template hash mismatch")
	}
	if len(req.Modules) != len(m.Bindings) {
		return Document{}, fmt.Errorf("template requires %d modules, got %d", len(m.Bindings), len(req.Modules))
	}
	budget := budget{}
	var omissions []Omission
	for _, b := range m.Bindings {
		mod, ok := req.Modules[b.Code]
		if !ok {
			return Document{}, fmt.Errorf("missing module %s (unknown keys are not accepted)", b.Code)
		}
		if err := budget.module(mod); err != nil {
			return Document{}, fmt.Errorf("module %s: %w", b.Code, err)
		}
		if mod.Gap != "" {
			omissions = append(omissions, Omission{Module: b.Code, Reason: mod.Gap})
		}
	}
	zr, err := zip.NewReader(bytes.NewReader(master), int64(len(master)))
	if err != nil {
		return Document{}, fmt.Errorf("open master: %w", err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return Document{}, err
		}
		modify := f.Name == "word/document.xml" || f.Name == "word/styles.xml" || f.Name == "word/stylesWithEffects.xml" || f.Name == "word/numbering.xml" || f.Name == "word/theme/theme1.xml" || strings.HasPrefix(f.Name, "word/header") || strings.HasPrefix(f.Name, "word/footer")
		if !modify {
			if err := zw.Copy(f); err != nil {
				return Document{}, fmt.Errorf("copy part %s: %w", f.Name, err)
			}
			continue
		}
		r, err := f.Open()
		if err != nil {
			return Document{}, err
		}
		data, err := io.ReadAll(io.LimitReader(r, MaxDOCXBytes+1))
		closeErr := r.Close()
		if err != nil {
			return Document{}, err
		}
		if closeErr != nil {
			return Document{}, closeErr
		}
		if len(data) > MaxDOCXBytes {
			return Document{}, fmt.Errorf("oversized master part %s", f.Name)
		}
		if f.Name == "word/document.xml" {
			cells, err := locate(data, m)
			if err != nil {
				return Document{}, err
			}
			var edits []edit
			for i, cell := range cells {
				start := cell.openEnd
				width := 4000
				if props := cell.direct("tcPr"); len(props) > 0 {
					start = props[0].end
					if sizes := props[0].direct("tcW"); len(sizes) > 0 {
						if w, e := strconv.Atoi(sizes[0].attr("w")); e == nil && w > 0 {
							width = w
						}
					}
				}
				pPr := ""
				if ps := cell.direct("p"); len(ps) > 0 {
					if props := ps[0].direct("pPr"); len(props) > 0 {
						pPr = string(data[props[0].start:props[0].end])
					}
				}
				edits = append(edits, edit{start, cell.closeStart, moduleXML(req.Modules[m.Bindings[i].Code], req.Language, pPr, width)})
			}
			data, err = splice(data, edits)
			if err != nil {
				return Document{}, err
			}
		}
		data, err = mapFonts(data, req.Language)
		if err != nil {
			return Document{}, err
		}
		if err := validateGlyphs(data); err != nil {
			return Document{}, fmt.Errorf("part %s: %w", f.Name, err)
		}
		if len(data) > MaxDOCXBytes {
			return Document{}, fmt.Errorf("filled XML exceeds %d bytes", MaxDOCXBytes)
		}
		h := f.FileHeader
		w, err := zw.CreateHeader(&h)
		if err != nil {
			return Document{}, err
		}
		if _, err = w.Write(data); err != nil {
			return Document{}, err
		}
	}
	if err := zw.Close(); err != nil {
		return Document{}, err
	}
	if out.Len() > MaxDOCXBytes {
		return Document{}, fmt.Errorf("filled DOCX exceeds %d bytes", MaxDOCXBytes)
	}
	data := out.Bytes()
	return Document{DOCX: data, Book: req.Book, Language: req.Language, Version: req.Version, TemplateSHA256: m.SHA256, Omissions: omissions, digest: sha256.Sum256(data)}, nil
}
