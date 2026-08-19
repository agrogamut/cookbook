package book

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// Skips rather than fails without a browser, so `go test ./...` stays green on a machine
// with no Chromium -- the same contract the integrity suite has with TEST_DATABASE_URL. A
// skipped test is not a passing test; run it with a browser before calling this done.
func TestPrintPDFProducesAPDF(t *testing.T) {
	// Every name a Chromium build ships under, because a probe that misses the one name
	// installed here would skip silently and a skipped test is not a passing test. Arch
	// packages the browser as google-chrome-stable, Debian as google-chrome, and the
	// open-source builds as chromium or chromium-browser.
	if !browserOnPath() {
		t.Skip("no chromium on PATH")
	}

	var doc bytes.Buffer
	meta := Metadata{
		Title: "t", Language: "en", ReviewStatus: "Draft",
		BookVersion: "V1", ReleaseID: "TEST", GenerationDate: time.Now(),
	}
	if err := RenderHTML(&doc, Kind1, meta, Book1{Metadata: meta}); err != nil {
		t.Fatalf("render: %v", err)
	}

	out, err := PrintPDF(context.Background(), doc.Bytes(), meta)
	if err != nil {
		if errors.Is(err, ErrChromiumUnavailable) {
			t.Skipf("chromium present but not runnable here: %v", err)
		}
		t.Fatalf("PrintPDF: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF, first bytes: %q", out[:min(8, len(out))])
	}
}
