package bookdocx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/bookmasters"
)

func TestPageStartRegression(t *testing.T) {
	for _, lang := range []string{"en", "bn", "hi"} {
		for _, book := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d-%s", book, lang), func(t *testing.T) {
				doc, err := Fill(context.Background(), request(t, book, lang))
				if err != nil {
					t.Fatal(err)
				}
				xml := parts(t, doc.DOCX)["word/document.xml"]
				want, untouched := 1, 7
				if book == 2 {
					want, untouched = 2, 5
				}
				if count := bytes.Count(xml, []byte(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)); count != untouched {
					t.Fatalf("untouched carriers: got %d, want %d", count, untouched)
				}
				if got := bytes.Count(xml, []byte(`<w:pageBreakBefore/>`)); got != want {
					t.Fatalf("page starts: got %d, want %d", got, want)
				}
			})
		}
	}
}

func TestPageStartRecognition(t *testing.T) {
	carrier := `<w:p><w:r><w:br w:type="page"/></w:r></w:p>`
	heading := `<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Fixed &amp; heading</w:t></w:r></w:p>`
	for _, tc := range []struct {
		name, body string
		change     bool
	}{
		{"canonical", carrier + heading, true},
		{"inline", strings.Replace(carrier, `<w:br`, `<w:t>Keep me</w:t><w:br`, 1) + heading, false},
		{"trailing text", strings.Replace(carrier, `</w:r>`, `<w:t>Keep me</w:t></w:r>`, 1) + heading, false},
		{"carrier properties", strings.Replace(carrier, `<w:p>`, `<w:p><w:pPr/>`, 1) + heading, false},
		{"carrier attributes", strings.Replace(carrier, `<w:p>`, `<w:p w:rsidR="0001">`, 1) + heading, false},
		{"drawing", strings.Replace(carrier, `</w:r>`, `<w:drawing/></w:r>`, 1) + heading, false},
		{"column", strings.Replace(carrier, `type="page"`, `type="column"`, 1) + heading, false},
		{"other style", carrier + strings.Replace(heading, "Heading1", "Heading2", 1), false},
		{"empty heading", carrier + strings.Replace(heading, "Fixed &amp; heading", "", 1), false},
		{"heading break", carrier + strings.Replace(heading, `</w:r>`, `<w:br w:type="page"/></w:r>`, 1), false},
		{"heading section", carrier + strings.Replace(heading, `</w:pPr>`, `<w:sectPr/></w:pPr>`, 1), false},
		{"existing start", carrier + strings.Replace(heading, `</w:pPr>`, `<w:pageBreakBefore/></w:pPr>`, 1), false},
		{"intervening paragraph", carrier + `<w:p/>` + heading, false},
		{"inside cell", `<w:tbl><w:tr><w:tc>` + carrier + heading + `</w:tc></w:tr></w:tbl>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(`<w:document xmlns:w="` + wordNS + `"><w:body>` + tc.body + `</w:body></w:document>`)
			root, err := parseXML(data)
			if err != nil {
				t.Fatal(err)
			}
			body := root.direct("document")[0].direct("body")[0].children
			var edits []edit
			if len(body) >= 2 {
				edits = pageStartPair(data, body[0], body[1])
			}
			got, err := splice(data, edits)
			if err != nil {
				t.Fatal(err)
			}
			want := data
			if tc.change {
				want = bytes.Replace(data, []byte(carrier), nil, 1)
				want = bytes.Replace(want, []byte(`</w:pPr>`), []byte(`<w:pageBreakBefore/></w:pPr>`), 1)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("unexpected fixed XML edit: %s", got)
			}
		})
	}
	if _, err := pageStartEdits([]byte(`<w:document>`), Manifest{}); err == nil {
		t.Fatal("invalid XML accepted")
	}
}

func TestPageStartManifestRejectsDrift(t *testing.T) {
	m, err := Bindings(2, "en", "v1")
	if err != nil {
		t.Fatal(err)
	}
	master, _, err := bookmasters.Read(2, "en", "v1")
	if err != nil {
		t.Fatal(err)
	}
	data := parts(t, master)["word/document.xml"]
	for _, tc := range []struct {
		name   string
		mutate func(*Manifest, []byte) []byte
	}{
		{"book", func(m *Manifest, b []byte) []byte { m.Book = 3; return b }},
		{"language", func(m *Manifest, b []byte) []byte { m.Language = "xx"; return b }},
		{"version", func(m *Manifest, b []byte) []byte { m.Version = "v2"; return b }},
		{"hash", func(m *Manifest, b []byte) []byte { m.SHA256 = "changed"; return b }},
		{"heading", func(_ *Manifest, b []byte) []byte {
			return bytes.Replace(b, []byte("Release and review controls"), []byte("Changed review controls"), 1)
		}},
		{"module", func(_ *Manifest, b []byte) []byte { return bytes.ReplaceAll(b, []byte("B2-49"), []byte("B2-99")) }},
		{"carrier", func(_ *Manifest, b []byte) []byte {
			return bytes.ReplaceAll(b, []byte(`<w:br w:type="page"/>`), []byte(`<w:br w:type="column"/>`))
		}},
		{"location", func(_ *Manifest, b []byte) []byte {
			return bytes.Replace(b, []byte(`<w:body>`), []byte(`<w:body><w:p/>`), 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := m
			if _, err := pageStartEdits(tc.mutate(&changed, data), changed); err == nil {
				t.Fatal("changed template accepted")
			}
		})
	}
}

// The oracle reconstructs only the known original heading/carrier pairs.
func restorePageStarts(t *testing.T, data []byte, m Manifest) []byte {
	t.Helper()
	master, _, err := bookmasters.Read(m.Book, m.Language, m.Version)
	if err != nil {
		t.Fatal(err)
	}
	source := parts(t, master)["word/document.xml"]
	root, err := parseXML(source)
	if err != nil {
		t.Fatal(err)
	}
	original := root.direct("document")[0].direct("body")[0].children
	root, err = parseXML(data)
	if err != nil {
		t.Fatal(err)
	}
	got := root.direct("document")[0].direct("body")[0].children
	var boundaries []pageStart
	if err := json.Unmarshal(pageStartsJSON, &boundaries); err != nil {
		t.Fatal(err)
	}
	targets := map[int]bool{}
	for _, p := range boundaries {
		if p.Book == m.Book && p.Language == m.Language && p.Version == m.Version {
			targets[p.BodyChild] = true
		}
	}
	var edits []edit
	carrier := []byte(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
	for i, n := range original {
		if !bytes.Equal(source[n.start:n.end], carrier) {
			continue
		}
		h := original[i+1]
		want := source[h.start:h.end]
		found := 0
		for j, p := range got {
			if !p.is("p") || p.words() != h.words() {
				continue
			}
			found++
			props := p.direct("pPr")
			if len(props) != 1 {
				t.Fatal("fixed heading properties changed")
			}
			breaks := props[0].direct("pageBreakBefore")
			if !targets[i] {
				if len(breaks) != 0 || j == 0 || !bytes.Equal(data[got[j-1].start:got[j-1].end], carrier) || !bytes.Equal(fontTags.ReplaceAll(data[p.start:p.end], []byte("FONT")), fontTags.ReplaceAll(want, []byte("FONT"))) {
					t.Fatal("untargeted carrier or heading changed")
				}
				continue
			}
			if len(breaks) == 0 && j > 0 && bytes.Equal(data[got[j-1].start:got[j-1].end], carrier) {
				continue
			}
			if len(breaks) != 1 || !bytes.Equal(data[breaks[0].start:breaks[0].end], []byte(`<w:pageBreakBefore/>`)) {
				t.Fatal("fixed heading page start changed")
			}
			restored := bytes.Replace(data[p.start:p.end], []byte(`<w:pageBreakBefore/>`), nil, 1)
			if !bytes.Equal(fontTags.ReplaceAll(restored, []byte("FONT")), fontTags.ReplaceAll(want, []byte("FONT"))) {
				t.Fatal("fixed heading changed beyond page start")
			}
			edits = append(edits, edit{p.start, p.start, carrier}, edit{breaks[0].start, breaks[0].end, nil})
		}
		if found != 1 {
			t.Fatalf("fixed heading matched %d times", found)
		}
	}
	out, err := splice(data, edits)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
