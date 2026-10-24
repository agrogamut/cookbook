package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/madamgy/recipie/internal/bookdocx"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Policy-route tests validate real bindings without repeating native conversion for every condition.
type bindingRenderer struct{}

func (bindingRenderer) Render(ctx context.Context, request bookdocx.Request) (bookdocx.Result, error) {
	doc, err := bookdocx.Fill(ctx, request)
	return bookdocx.Result{Document: doc, PDF: []byte("%PDF-1.4\n% route policy fixture\n%%EOF")}, err
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

func pdfImageCount(t *testing.T, pdf []byte) int {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("pdfimages", "-list", path).CombinedOutput()
	if err != nil {
		t.Fatalf("PDF image inspection: %v: %s", err, output)
	}
	count := 0
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 3 && fields[2] == "image" {
			count++
		}
	}
	return count
}
