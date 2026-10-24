package book

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/madamgy/recipie/internal/aidraft"
	"html"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"
)

func TestAppendPrescriptionWithoutUploadKeepsBytes(t *testing.T) {
	pdf := []byte("%PDF-1.4\nunchanged stored document")
	got, _, err := AppendPrescription(context.Background(), pdf, Book1{}, aidraft.Disabled)
	if err != nil || !bytes.Equal(got, pdf) {
		t.Fatalf("no-upload path changed bytes: %v", err)
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
