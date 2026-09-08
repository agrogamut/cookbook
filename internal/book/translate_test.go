package book

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/madamgy/recipie/internal/aidraft"
)

// translateFakeDrafter answers TranslateTexts with a deterministic, order-preserving transform
// so a test can tell exactly which fragment produced which output, without a real network call.
type translateFakeDrafter struct {
	fakeDrafter
	transform func(string) string
	mismatch  bool // return one fewer string than requested, to exercise the length check
	fail      bool
	calls     int32
}

func (f *translateFakeDrafter) TranslateTexts(_ context.Context, req aidraft.TranslateRequest) (aidraft.TranslatedTexts, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.fail {
		return aidraft.TranslatedTexts{}, aidraft.ErrDraftingUnavailable
	}
	out := make([]string, 0, len(req.Texts))
	for _, t := range req.Texts {
		out = append(out, f.transform(t))
	}
	if f.mismatch && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return aidraft.TranslatedTexts{Texts: out, Source: "gemini"}, nil
}

func upper(s string) string { return strings.ToUpper(s) }

func TestTranslateHTMLTranslatesVisibleTextOnly(t *testing.T) {
	doc := []byte(`<!doctype html><html lang="bn"><head><title>t</title>` +
		`<style>.x { content: "should not translate"; }</style></head>` +
		`<body><p>hello world</p><span class="mono">MG-R-00042</span>` +
		`<script>console.log("also should not translate");</script></body></html>`)

	drafter := &translateFakeDrafter{transform: upper}
	out, err := TranslateHTML(context.Background(), doc, drafter, "bn")
	if err != nil {
		t.Fatalf("TranslateHTML: %v", err)
	}
	got := string(out)

	if !strings.Contains(got, "HELLO WORLD") {
		t.Errorf("expected translated prose HELLO WORLD in output, got:\n%s", got)
	}
	if !strings.Contains(got, "MG-R-00042") {
		t.Errorf("expected the id fragment translated (upper of itself is unchanged) to survive, got:\n%s", got)
	}
	if !strings.Contains(got, `should not translate`) {
		t.Errorf("style content must not be sent for translation, got:\n%s", got)
	}
	if !strings.Contains(got, `also should not translate`) {
		t.Errorf("script content must not be sent for translation, got:\n%s", got)
	}
	// Structure survives: same tags, same attributes.
	if !strings.Contains(got, `<span class="mono">`) {
		t.Errorf("expected span tag and class attribute preserved, got:\n%s", got)
	}
}

func TestTranslateHTMLBatchesLargeDocuments(t *testing.T) {
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"bn\"><body>")
	const n = 130 // > 2x translateBatchSize(60), forces three batches
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "<p>fragment %d</p>", i)
	}
	b.WriteString("</body></html>")

	drafter := &translateFakeDrafter{transform: upper}
	out, err := TranslateHTML(context.Background(), []byte(b.String()), drafter, "bn")
	if err != nil {
		t.Fatalf("TranslateHTML: %v", err)
	}
	for i := 0; i < n; i++ {
		want := fmt.Sprintf("FRAGMENT %d", i)
		if !strings.Contains(string(out), want) {
			t.Fatalf("missing translated fragment %d (%q) in output", i, want)
		}
	}
	wantBatches := int32((n + translateBatchSize - 1) / translateBatchSize)
	if drafter.calls != wantBatches {
		t.Errorf("calls = %d, want %d batches for %d fragments at batch size %d",
			drafter.calls, wantBatches, n, translateBatchSize)
	}
}

func TestTranslateHTMLRejectsLengthMismatch(t *testing.T) {
	doc := []byte(`<html lang="bn"><body><p>one</p><p>two</p></body></html>`)
	drafter := &translateFakeDrafter{transform: upper, mismatch: true}
	if _, err := TranslateHTML(context.Background(), doc, drafter, "bn"); err == nil {
		t.Fatal("expected an error when the drafter returns fewer strings than requested")
	}
}

func TestTranslateHTMLRejectsUnknownLanguage(t *testing.T) {
	doc := []byte(`<html><body><p>hi</p></body></html>`)
	drafter := &translateFakeDrafter{transform: upper}
	if _, err := TranslateHTML(context.Background(), doc, drafter, "fr"); err == nil {
		t.Fatal("expected an error for a language this project does not translate into")
	}
}

func TestBookLanguageRecognisesBengaliAnyCasing(t *testing.T) {
	cases := map[string]string{
		"":          "en",
		"English":   "en",
		"Hindi":     "en",
		"bn":        "bn",
		"BN":        "bn",
		"Bengali":   "bn",
		"bengali":   "bn",
		"Bangla":    "bn",
		"  bangla ": "bn",
	}
	for in, want := range cases {
		if got := bookLanguage(in); got != want {
			t.Errorf("bookLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
