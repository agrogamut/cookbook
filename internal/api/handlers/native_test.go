package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/bookdocx"
)

// Policy-route tests validate real content without printing every condition.
func bindingReader(ctx context.Context, request bookdocx.Request, _ book.ReaderOptions) (book.ReaderResult, error) {
	doc, err := bookdocx.ReadContent(ctx, request)
	return book.ReaderResult{Result: bookdocx.Result{Document: doc.Document, PDF: []byte("%PDF-1.4\n% route policy fixture\n%%EOF")}, LayoutVersion: book.ReaderLayoutVersion}, err
}

func previewPDF(t *testing.T, doc string) []byte {
	t.Helper()
	_, encoded, ok := strings.Cut(doc, "data:application/pdf;base64,")
	if !ok {
		t.Fatal("native PDF preview missing")
	}
	encoded, _, _ = strings.Cut(encoded, `"`)
	pdf, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("invalid preview PDF: %v", err)
	}
	return pdf
}

func pdfText(t *testing.T, pdf []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	text, err := exec.Command("pdftotext", "-layout", path, "-").CombinedOutput()
	if err != nil {
		t.Fatalf("PDF text extraction: %v: %s", err, text)
	}
	return string(text)
}

func pdfContainsPNG(t *testing.T, pdf []byte, encoded string) bool {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	want, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "book.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("pdfimages", "-png", path, filepath.Join(dir, "image")).CombinedOutput()
	if err != nil {
		t.Fatalf("PDF image inspection: %v: %s", err, output)
	}
	files, err := filepath.Glob(filepath.Join(dir, "image-*.png"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if got.Bounds() != want.Bounds() {
			continue
		}
		equal := true
		for y := want.Bounds().Min.Y; y < want.Bounds().Max.Y && equal; y++ {
			for x := want.Bounds().Min.X; x < want.Bounds().Max.X; x++ {
				wr, wg, wb, wa := want.At(x, y).RGBA()
				gr, gg, gb, ga := got.At(x, y).RGBA()
				if wr != gr || wg != gg || wb != gb || wa != ga {
					equal = false
					break
				}
			}
		}
		if equal {
			return true
		}
	}
	return false
}
