package book

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/madamgy/recipie/internal/aidraft"
)

// AppendPrescription retains the uploaded scan at readable page width after the unchanged master.
func AppendPrescription(ctx context.Context, nativePDF []byte, b Book1, drafter aidraft.Drafter) ([]byte, []string, error) {
	if b.Metadata.PrescriptionPhoto == nil {
		return nativePDF, nil, nil
	}
	document, footer, omissions, err := prescriptionDocument(ctx, b, drafter)
	if err != nil {
		return nil, nil, err
	}
	browser, err := sharedBrowser()
	if err != nil {
		return nil, nil, err
	}
	attachment, err := printIn(ctx, browser, document, b.Metadata, footer)
	if err != nil {
		return nil, nil, fmt.Errorf("prescription print: %w", err)
	}
	merged, err := mergeBookPDF(ctx, nativePDF, attachment)
	return merged, omissions, err
}

func prescriptionDocument(ctx context.Context, b Book1, drafter aidraft.Drafter) ([]byte, string, []string, error) {
	validated, err := ParsePhoto(string(b.Metadata.PrescriptionPhoto.DataURI), b.Metadata.PrescriptionPhoto.Caption)
	if err != nil {
		return nil, "", nil, fmt.Errorf("prescription upload: %w", err)
	}
	if validated == nil {
		return nil, "", nil, fmt.Errorf("prescription upload is empty")
	}
	labels := []string{
		"Supplied prescription annex. Footer page numbers restart within this annex.",
		"Clinical", "Prescription & Recommendation",
		"The doctor's own prescription, attached as provided.", "Child",
		"Prescription annex", "Release identifier", "Generated on",
	}
	var omissions []string
	if target, ok := targetLanguageName[b.Metadata.Language]; ok {
		translated, err := translateProtectedTexts(ctx, labels, target, drafter)
		if errors.Is(err, aidraft.ErrDraftingUnavailable) {
			omissions = append(omissions, "Prescription annex localization into "+target+" is unavailable; wrapper text remains in English. The supplied scan and caption are unchanged.")
		} else if err != nil {
			return nil, "", nil, fmt.Errorf("prescription annex localization: %w", err)
		} else {
			for i, label := range labels {
				if strings.EqualFold(strings.TrimSpace(translated[i]), label) {
					omissions = append(omissions, fmt.Sprintf("Prescription annex label %q was returned unchanged by the %s translator; that label remains in English.", label, target))
				}
			}
			labels = translated
		}
	}
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		return nil, "", nil, fmt.Errorf("prescription style: %w", err)
	}
	t, err := template.New("upload").Parse(`<!doctype html><html lang="{{.Language}}"><head><meta charset="utf-8"><style>{{.FontFaces}}{{.CSS}} .page-head{margin-bottom:2mm} .page-head h2{font-size:16pt} .page-head .kicker{margin-bottom:1mm} .page-head::after{margin-top:1mm} .block-purpose{margin:1mm 0} .prescription-photo{margin-top:2mm} .prescription-photo img{max-height:200mm;object-fit:contain} .annex-note{margin:0 0 2mm}</style></head><body class="book1"><p class="annex-note">{{index .Labels 0}}</p>{{range .Omissions}}<p>{{.}}</p>{{end}}<section><div class="page-head"><p class="kicker">{{index .Labels 1}}</p><h2>{{index .Labels 2}}</h2><p class="block-purpose">{{index .Labels 3}}</p><p class="block-purpose">{{index .Labels 4}}: {{.Child}}</p></div><figure class="prescription-photo"><img src="{{.Photo.DataURI}}" alt="">{{with .Photo.Caption}}<figcaption>{{.}}</figcaption>{{end}}</figure></section></body></html>`)
	if err != nil {
		return nil, "", nil, fmt.Errorf("prescription layout: %w", err)
	}
	data := struct {
		CSS, FontFaces    template.CSS
		Language, Child   string
		Photo             *ChildPhoto
		Labels, Omissions []string
	}{template.CSS(css), fontFacesCSS, b.Metadata.Language, b.Child.DisplayName, validated, labels, omissions}
	var document bytes.Buffer
	if err := t.Execute(&document, data); err != nil {
		return nil, "", nil, fmt.Errorf("prescription render: %w", err)
	}
	release := ""
	if b.Metadata.ReleaseID != "" {
		release = " | " + html.EscapeString(labels[6]) + " " + html.EscapeString(b.Metadata.ReleaseID)
	}
	// Footer labels translate; supplied names, release identifiers and dates never enter translation.
	footer := fmt.Sprintf(`<div style="font-size:7pt;width:100%%;padding:0 20mm;display:flex;justify-content:space-between;align-items:baseline;font-family:'Noto Sans','Noto Sans Bengali','Noto Sans Devanagari',sans-serif;color:#5a5a5a"><span>MadamGY &middot; %s %s%s &middot; %s %s</span><span class="pageNumber" style="font-size:9pt;color:#1a1a1a"></span></div>`, html.EscapeString(b.Metadata.BookVersion), html.EscapeString(labels[5]), release, html.EscapeString(labels[7]), b.Metadata.GenerationDate.Format("2006-01-02"))
	return document.Bytes(), footer, omissions, nil
}

var mergeSlots = make(chan struct{}, 2)

func mergeBookPDF(ctx context.Context, first, attachment []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	select {
	case mergeSlots <- struct{}{}:
		defer func() { <-mergeSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	for _, part := range [][]byte{first, attachment} {
		if len(part) > 64<<20 || !bytes.HasPrefix(part, []byte("%PDF-")) {
			return nil, fmt.Errorf("invalid or oversized prescription PDF component")
		}
	}
	dir, err := os.MkdirTemp("", "book-prescription-")
	if err != nil {
		return nil, fmt.Errorf("prescription directory: %w", err)
	}
	defer os.RemoveAll(dir)
	paths := []string{filepath.Join(dir, "book.pdf"), filepath.Join(dir, "prescription.pdf"), filepath.Join(dir, "complete.pdf")}
	for i, b := range [][]byte{first, attachment} {
		if err := os.WriteFile(paths[i], b, 0600); err != nil {
			return nil, fmt.Errorf("prescription PDF component: %w", err)
		}
	}
	args := append([]string{"--as=4294967296", "--fsize=134217728", "--", "pdfunite"}, paths...)
	cmd := exec.CommandContext(ctx, "prlimit", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("merge supplied prescription: %w (%s)", err, output)
	}
	info, err := os.Stat(paths[2])
	if err != nil {
		return nil, fmt.Errorf("merged prescription PDF: %w", err)
	}
	if info.Size() > 128<<20 {
		return nil, fmt.Errorf("merged prescription PDF exceeds size limit")
	}
	b, err := os.ReadFile(paths[2])
	if err != nil {
		return nil, fmt.Errorf("read merged prescription PDF: %w", err)
	}
	return b, nil
}
