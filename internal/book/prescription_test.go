package book

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/aidraft"
)

func TestAppendPrescriptionWithoutUploadKeepsBytes(t *testing.T) {
	pdf := []byte("%PDF-1.4\nunchanged stored document")
	got, _, err := AppendPrescription(context.Background(), pdf, Book1{}, aidraft.Disabled)
	if err != nil || !bytes.Equal(got, pdf) {
		t.Fatalf("no-upload path changed bytes: %v", err)
	}
}

func TestPrescriptionMixedScriptNativeFonts(t *testing.T) {
	if os.Getenv("BOOK_READER_NATIVE") != "1" {
		t.Skip("set BOOK_READER_NATIVE=1 for mixed-script annex fonts")
	}
	defer ShutdownBrowser()
	var scan bytes.Buffer
	if err := png.Encode(&scan, image.NewRGBA(image.Rect(0, 0, 20, 30))); err != nil {
		t.Fatal(err)
	}
	photo, err := ParsePhoto("data:image/png;base64,"+base64.StdEncoding.EncodeToString(scan.Bytes()), "LatinRetention 0.005 g")
	if err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"en", "bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			b := Book1{Metadata: Metadata{Language: language, PrescriptionPhoto: photo}, Child: ChildSummary{DisplayName: "নব্য 72 व्यक्तिगत"}}
			drafter := &translateFakeDrafter{transform: func(s string) string { return "Localized " + s }}
			document, footer, _, err := prescriptionDocument(context.Background(), b, drafter)
			if err != nil {
				t.Fatal(err)
			}
			browser, err := sharedBrowser()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			pdf, err := printIn(ctx, browser, document, b.Metadata, footer)
			if err != nil {
				t.Fatal(err)
			}
			latinFamily := "Poppins"
			if language == "bn" {
				latinFamily = "NotoSans"
			}
			assertMixedScriptPDFFonts(t, pdf, latinFamily)
		})
	}
}

func prescriptionPageResources(t *testing.T, pdf []byte) []any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "document.pdf")
	if err := os.WriteFile(path, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "qpdf", append(args, path)...).CombinedOutput()
		if err != nil {
			t.Fatalf("qpdf %v: %v: %s", args, err, out)
		}
		return out
	}
	run("--check")
	var document struct {
		Pages []struct{ Object string }
		Qpdf  []map[string]any
	}
	if err := json.Unmarshal(run("--json"), &document); err != nil {
		t.Fatal(err)
	}
	objects := map[string]map[string]any{}
	for key, value := range document.Qpdf[1] {
		objects[key] = value.(map[string]any)
	}
	ref := regexp.MustCompile(`^\d+ \d+ R$`)
	cache := map[string]any{}
	var resolve func(any) any
	resolve = func(v any) any {
		switch v := v.(type) {
		case string:
			if !ref.MatchString(v) {
				return v
			}
			if cached, ok := cache[v]; ok {
				return cached
			}
			cache[v] = "cycle"
			object := objects["obj:"+v]
			value := object["value"]
			if stream, ok := object["stream"].(map[string]any); ok {
				dict := stream["dict"].(map[string]any)
				delete(dict, "/Length")
				delete(dict, "/Filter")
				delete(dict, "/DecodeParms")
				value = map[string]any{"dict": resolve(dict), "bytes": string(run("--show-object="+strings.Fields(v)[0], "--filtered-stream-data"))}
			} else {
				value = resolve(value)
			}
			cache[v] = value
			return value
		case map[string]any:
			out := map[string]any{}
			for k, value := range v {
				out[k] = resolve(value)
			}
			return out
		case []any:
			out := make([]any, len(v))
			for i, value := range v {
				out[i] = resolve(value)
			}
			return out
		default:
			return v
		}
	}
	var pages []any
	for _, page := range document.Pages {
		dict := objects["obj:"+page.Object]["value"].(map[string]any)
		selected := map[string]any{}
		for _, key := range []string{"/MediaBox", "/CropBox", "/Rotate", "/Resources", "/Contents"} {
			if value, ok := dict[key]; ok {
				selected[key] = value
			}
		}
		pages = append(pages, resolve(selected))
	}
	return pages
}

func TestPrescriptionMergePreservesPageResources(t *testing.T) {
	if !browserOnPath() {
		t.Skip("no Chromium installed")
	}
	var scan bytes.Buffer
	if err := png.Encode(&scan, image.NewRGBA(image.Rect(0, 0, 20, 30))); err != nil {
		t.Fatal(err)
	}
	docs := []string{`<p style="font-size:12pt">Original source 0.005 g</p>`, fmt.Sprintf(`<p>Original scan caption</p><img src="data:image/png;base64,%s" width="20" height="30">`, base64.StdEncoding.EncodeToString(scan.Bytes()))}
	var parts [][]byte
	var expected []any
	for _, html := range docs {
		pdf, err := PrintPDF(context.Background(), []byte(html), Metadata{})
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, pdf)
		expected = append(expected, prescriptionPageResources(t, pdf)...)
	}
	merged, err := mergeBookPDF(context.Background(), parts[0], parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if got := prescriptionPageResources(t, merged); !reflect.DeepEqual(got, expected) {
		t.Fatal("merge changed decoded drawing, font, image or page geometry resources")
	}
}

func TestPrescriptionMergeFailureAndCancellationCleanup(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish unavailable for subprocess control")
	}
	for _, tc := range []struct {
		name, script string
		cancel       bool
	}{
		{"error", "echo controlled-merge-error >&2\nexit 2\n", false},
		{"cancel", "exec sleep 30\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, temp := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "qpdf"), []byte("#!"+fish+"\n"+tc.script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TMPDIR", temp)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			_, err := mergeBookPDF(ctx, []byte("%PDF-1.4"), []byte("%PDF-1.4"))
			if err == nil {
				t.Fatal("controlled failure succeeded")
			}
			if tc.cancel && ctx.Err() == nil {
				t.Fatal("merge did not reach cancellation")
			}
			if !tc.cancel && !strings.Contains(err.Error(), "controlled-merge-error") {
				t.Fatal(err)
			}
			files, err := os.ReadDir(temp)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 || len(mergeSlots) != 0 {
				t.Fatal("merge retained temporary files or concurrency slot")
			}
		})
	}
}

func TestPrescriptionMergeRejectsInvalidInput(t *testing.T) {
	if _, err := mergeBookPDF(context.Background(), []byte("not PDF"), []byte("%PDF-1.4")); err == nil {
		t.Fatal("invalid component accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mergeBookPDF(ctx, []byte("%PDF-1.4"), []byte("%PDF-1.4")); err == nil {
		t.Fatal("cancelled merge succeeded")
	}
}

func TestPrescriptionAnnexLanguages(t *testing.T) {
	if !browserOnPath() {
		t.Skip("no Chromium installed")
	}
	var scan bytes.Buffer
	if err := png.Encode(&scan, image.NewRGBA(image.Rect(0, 0, 20, 30))); err != nil {
		t.Fatal(err)
	}
	photo, err := ParsePhoto("data:image/png;base64,"+base64.StdEncoding.EncodeToString(scan.Bytes()), "Original supplied caption")
	if err != nil {
		t.Fatal(err)
	}
	original, err := PrintPDF(context.Background(), []byte("<p>Original native page</p>"), Metadata{})
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "bn", "hi"} {
		t.Run(lang, func(t *testing.T) {
			d := &translateFakeDrafter{transform: func(s string) string { return "Localized" + strings.ToUpper(lang) }}
			b := Book1{Metadata: Metadata{Language: lang, BookVersion: "v1", ReleaseID: "release-original", GenerationDate: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), PrescriptionPhoto: photo}, Child: ChildSummary{DisplayName: "Original child"}}
			result, omissions, err := AppendPrescription(context.Background(), original, b, d)
			if err != nil {
				t.Fatal(err)
			}
			if len(omissions) != 0 {
				t.Fatal(omissions)
			}
			text, err := pdfText(t, result)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{"Original native page", "Original child", "Original supplied caption", "v1", "release-original", "2026-10-24"} {
				if !strings.Contains(text, value) {
					t.Errorf("lost original value %q: %s", value, text)
				}
			}
			text = strings.Split(text, "\f")[1]
			if lang == "en" {
				if d.calls != 0 || !strings.Contains(text, "Prescription & Recommendation") {
					t.Fatal("English path changed")
				}
				return
			}
			if d.calls == 0 {
				t.Error("annex bypassed configured translator")
			}
			if strings.Count(text, "Localized"+strings.ToUpper(lang)) < 7 {
				t.Errorf("annex wrapper/footer untranslated: %s", text)
			}
			for _, english := range []string{"Prescription & Recommendation", "Clinical", "Footer page numbers restart", "prescription annex", "generated"} {
				if strings.Contains(text, english) {
					t.Errorf("untranslated application wrapper %q", english)
				}
			}
		})
	}
}

func TestPrescriptionLocalizationFailuresAndUploadFidelity(t *testing.T) {
	var scan bytes.Buffer
	if err := png.Encode(&scan, image.NewRGBA(image.Rect(0, 0, 20, 30))); err != nil {
		t.Fatal(err)
	}
	photo, err := ParsePhoto("data:image/png;base64,"+base64.StdEncoding.EncodeToString(scan.Bytes()), "Original caption, 0.005 mg")
	if err != nil {
		t.Fatal(err)
	}
	b := Book1{Metadata: Metadata{Language: "bn", PrescriptionPhoto: photo}, Child: ChildSummary{DisplayName: "Original <child>"}}
	for _, tc := range []struct {
		name             string
		drafter          aidraft.Drafter
		wantGap, wantErr bool
	}{
		{"available", &translateFakeDrafter{transform: func(s string) string { return "Localized " + s }}, false, false},
		{"unchanged labels", &translateFakeDrafter{transform: func(s string) string { return s }}, true, false},
		{"unavailable", aidraft.Disabled, true, false},
		{"numeric invention", &translateFakeDrafter{transform: func(s string) string { return s + " 99" }}, false, true},
		{"missing labels", &translateFakeDrafter{transform: strings.ToUpper, mismatch: true}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, _, omissions, err := prescriptionDocument(context.Background(), b, tc.drafter)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error %v", err)
			}
			if err != nil {
				return
			}
			if (len(omissions) > 0) != tc.wantGap {
				t.Fatal(omissions)
			}
			for _, original := range []string{string(photo.DataURI), photo.Caption, "Original &lt;child&gt;"} {
				if !strings.Contains(string(document), original) {
					t.Fatalf("upload or identity changed: %q", original)
				}
			}
			if tc.wantGap && !strings.Contains(html.UnescapeString(string(document)), omissions[0]) {
				t.Fatal("localization gap absent from annex")
			}
		})
	}
}
