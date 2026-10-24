package bookdocx

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/bookmasters"
)

func request(t *testing.T, book int, language string) Request {
	t.Helper()
	m, err := Bindings(book, language, "v1")
	if err != nil {
		t.Fatal(err)
	}
	r := Request{Book: book, Language: language, Version: "v1", Modules: map[string]Module{}}
	for _, b := range m.Bindings {
		r.Modules[b.Code] = Module{Gap: "SOURCE GAP " + b.Code}
	}
	return r
}

func parts(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		out[f.Name] = b
	}
	return out
}

func repack(t *testing.T, data []byte, part string, value []byte) []byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range z.File {
		if f.Name != part {
			if err := w.Copy(f); err != nil {
				t.Fatal(err)
			}
			continue
		}
		h := f.FileHeader
		dst, err := w.CreateHeader(&h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dst.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestBindings(t *testing.T) {
	catalog, err := bookmasters.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, master := range catalog {
		m, err := Bindings(master.Book, master.Language, master.Version)
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := bookmasters.Read(master.Book, master.Language, master.Version)
		if err != nil {
			t.Fatal(err)
		}
		if m.SHA256 != hash(data) {
			t.Fatal("hash mismatch")
		}
		p := parts(t, data)
		cells, err := locate(p["word/document.xml"], m)
		if err != nil {
			t.Fatal(err)
		}
		want := 63
		if master.Book == 2 {
			want = 56
		}
		if len(cells) != want {
			t.Fatalf("got %d modules", len(cells))
		}
		total += len(cells)
		root, _ := parseXML(p["word/document.xml"])
		codes := map[string]int{}
		root.walk(func(n *node) {
			if n.is("tc") && regexp.MustCompile(`^B[12]-[0-9]{2}$`).MatchString(n.words()) {
				codes[n.words()]++
			}
		})
		if len(codes) != len(cells) {
			t.Fatal("unbound module")
		}
		for _, b := range m.Bindings {
			if codes[b.Code] != 1 {
				t.Fatal("module must have one source row", b.Code)
			}
		}
		m.Bindings[0].Row++
		if _, err := locate(p["word/document.xml"], m); err == nil {
			t.Fatal("stale row accepted")
		}
	}
	if total != 357 {
		t.Fatal(total)
	}
}

var fontTags = regexp.MustCompile(`<w:rFonts\b[^>]*/>|<a:(?:latin|ea|cs|font)\b[^>]*/>`)

func maskBindings(t *testing.T, data []byte, m Manifest) []byte {
	t.Helper()
	root, err := parseXML(data)
	if err != nil {
		t.Fatal(err)
	}
	tables := root.direct("document")[0].direct("body")[0].direct("tbl")
	var edits []edit
	for _, b := range m.Bindings {
		cell := tables[b.Table-1].direct("tr")[b.Row-1].direct("tc")[2]
		start := cell.openEnd
		if props := cell.direct("tcPr"); len(props) > 0 {
			start = props[0].end
		}
		edits = append(edits, edit{start, cell.closeStart, []byte("BINDING")})
	}
	out, err := splice(data, edits)
	if err != nil {
		t.Fatal(err)
	}
	return restorePageStarts(t, out, m)
}

func TestPreservation(t *testing.T) {
	for _, lang := range []string{"en", "bn", "hi"} {
		for _, book := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d-%s", book, lang), func(t *testing.T) {
				r := request(t, book, lang)
				key := fmt.Sprintf("B%d-01", book)
				r.Modules[key] = Module{Blocks: []Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "Synthetic ", Bold: true}, {Text: "only <&> 0007 12.50 g"}}}}, {Table: &Table{Rows: [][]string{{"ID", "Amount"}, {"SYN-0007", "12.50 g"}}, Header: true}}}}
				doc, err := Fill(context.Background(), r)
				if err != nil {
					t.Fatal(err)
				}
				original, _, _ := bookmasters.Read(book, lang, "v1")
				before, after := parts(t, original), parts(t, doc.DOCX)
				m, _ := Bindings(book, lang, "v1")
				if len(before) != len(after) {
					t.Fatal("package part count changed")
				}
				for name, src := range before {
					got := after[name]
					if name == "word/document.xml" {
						src = maskBindings(t, src, m)
						got = maskBindings(t, got, m)
					}
					src = fontTags.ReplaceAll(src, []byte("FONT"))
					got = fontTags.ReplaceAll(got, []byte("FONT"))
					if !bytes.Equal(src, got) {
						t.Errorf("fixed bytes changed in %s", name)
					}
				}
				footer := after["word/footer1.xml"]
				if !bytes.Contains(footer, []byte("PAGE")) {
					t.Fatal("page field removed")
				}
				again, _, _ := bookmasters.Read(book, lang, "v1")
				if !bytes.Equal(original, again) {
					t.Fatal("master mutated")
				}
				doc2, err := Fill(context.Background(), r)
				if err != nil || !bytes.Equal(doc.DOCX, doc2.DOCX) {
					t.Fatal("DOCX is not deterministic", err)
				}
			})
		}
	}
}

func TestReject(t *testing.T) {
	for _, change := range []func(*Request){
		func(r *Request) { r.Book = 3 }, func(r *Request) { r.Language = "ta" }, func(r *Request) { r.Language = "" }, func(r *Request) { r.Version = "v2" },
		func(r *Request) { delete(r.Modules, "B1-01") }, func(r *Request) { r.Modules["B1-001"] = r.Modules["B1-01"]; delete(r.Modules, "B1-01") },
		func(r *Request) { r.Modules["B1-01"] = Module{} }, func(r *Request) { r.Modules["B1-01"] = Module{Gap: " "} },
		func(r *Request) { r.Modules["B1-01"] = Module{Gap: "gap", Blocks: []Block{{}}} },
		func(r *Request) { r.Modules["B1-01"] = Module{Blocks: []Block{{}}} },
		func(r *Request) {
			r.Modules["B1-01"] = Module{Blocks: []Block{{Paragraph: &Paragraph{}, Table: &Table{}}}}
		},
		func(r *Request) {
			r.Modules["B1-01"] = Module{Blocks: []Block{{Table: &Table{Rows: [][]string{{"one"}, {"two", "three"}}}}}}
		},
		func(r *Request) { r.Modules["B1-01"] = Module{Gap: "invalid\x00"} },
		func(r *Request) { r.Modules["B1-01"] = Module{Gap: string([]byte{0xff})} },
	} {
		r := request(t, 1, "en")
		change(&r)
		if _, err := Fill(context.Background(), r); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	r := request(t, 1, "en")
	master, _, _ := bookmasters.Read(1, "en", "v1")
	m, _ := Bindings(1, "en", "v1")
	master[10] ^= 1
	if _, err := fill(context.Background(), r, master, m); err == nil {
		t.Fatal("altered master accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fill(ctx, r); err == nil {
		t.Fatal("cancelled fill accepted")
	}
}

func TestContent(t *testing.T) {
	r := request(t, 1, "bn")
	text := " <&> \" ' 00042 12.50 g\tA\nB\r\nC"
	r.Modules["B1-01"] = Module{Blocks: []Block{{Paragraph: &Paragraph{Runs: []Run{{Text: text, Bold: true, Italic: true}, {Text: "বাংলা"}}}}}, Entries: []Entry{{ID: "SYN-00042", Blocks: []Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "last paragraph"}}}}}}}}
	doc, err := Fill(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	data := parts(t, doc.DOCX)["word/document.xml"]
	root, err := parseXML(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{" <&> \" ' 00042 12.50 g", "বাংলা", "SYN-00042", "last paragraph"} {
		if !strings.Contains(root.words(), want) {
			t.Fatal("lost text", want)
		}
	}
	if !bytes.Contains(data, []byte("<w:tab/>")) || !bytes.Contains(data, []byte("<w:bCs/>")) {
		t.Fatal("missing run formatting")
	}
	// Anchor lookup joins runs instead of matching a particular serialization.
	original, _, _ := bookmasters.Read(1, "bn", "v1")
	source := parts(t, original)["word/document.xml"]
	source = bytes.Replace(source, []byte("<w:t>B1-01</w:t>"), []byte("<w:t>B1-</w:t></w:r><w:r><w:t>01</w:t>"), 1)
	m, _ := Bindings(1, "bn", "v1")
	if _, err := locate(source, m); err != nil {
		t.Fatal("multi-run anchor rejected", err)
	}
	if len(doc.Omissions) != 62 || doc.Omissions[0].Module != "B1-02" {
		t.Fatal("wrong omissions", doc.Omissions)
	}
}

func TestLimits(t *testing.T) {
	r := request(t, 2, "en")
	r.Modules["B2-09"] = Module{Gap: strings.Repeat("x", MaxStringBytes+1)}
	if _, err := Fill(context.Background(), r); err == nil {
		t.Fatal("oversized string accepted")
	}
	entries := make([]Entry, MaxEntriesPerModule+1)
	for i := range entries {
		entries[i] = Entry{ID: fmt.Sprint(i), Blocks: []Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "x"}}}}}}
	}
	r.Modules["B2-09"] = Module{Entries: entries}
	if _, err := Fill(context.Background(), r); err == nil {
		t.Fatal("oversized repetition accepted")
	}
	r.Modules["B2-09"] = Module{Entries: entries[:20]}
	doc, err := Fill(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(parts(t, doc.DOCX)["word/document.xml"], []byte(`>19</w:t>`)) {
		t.Fatal("last entry lost")
	}
	entries[1].ID = entries[0].ID
	if _, err := Fill(context.Background(), r); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
}

func TestContentGlyphCoverage(t *testing.T) {
	for _, language := range []string{"en", "bn", "hi"} {
		r := request(t, 1, language)
		r.Modules["B1-01"] = Module{Gap: "Unsupported \U0002FFFF"}
		if _, err := Fill(context.Background(), r); err == nil || !strings.Contains(err.Error(), "no glyph") {
			t.Fatal("unsupported glyph accepted", err)
		}
	}
	if err := readCmap([]byte("not a font"), map[rune]bool{}); err == nil {
		t.Fatal("invalid font accepted")
	}
}

func TestLimitsStructural(t *testing.T) {
	b := budget{}
	row := []string{"", "", "", "", "", ""}
	rows := make([][]string, MaxTableRows)
	for i := range rows {
		rows[i] = row
	}
	blocks := []Block{{Table: &Table{Rows: rows}}, {Table: &Table{Rows: rows}}, {Table: &Table{Rows: rows}}, {Table: &Table{Rows: rows}}}
	if err := b.validateBlocks(blocks); err == nil {
		t.Fatal("empty-cell explosion accepted")
	}
	b = budget{runs: MaxRuns}
	if err := b.validateBlocks([]Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "x"}}}}}); err == nil {
		t.Fatal("run limit bypassed")
	}
	b = budget{text: MaxTextBytes}
	if err := b.string("x"); err == nil {
		t.Fatal("text limit bypassed")
	}
	b = budget{blocks: MaxBlocks}
	if err := b.validateBlocks([]Block{{Paragraph: &Paragraph{Runs: []Run{{Text: "x"}}}}}); err == nil {
		t.Fatal("block limit bypassed")
	}
}
