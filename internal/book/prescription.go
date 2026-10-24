package book

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// AppendPrescription retains the uploaded scan at readable page width after the unchanged master.
func AppendPrescription(ctx context.Context, nativePDF []byte, b Book1) ([]byte, error) {
	if b.Metadata.PrescriptionPhoto == nil {
		return nativePDF, nil
	}
	validated, err := ParsePhoto(string(b.Metadata.PrescriptionPhoto.DataURI), b.Metadata.PrescriptionPhoto.Caption)
	if err != nil {
		return nil, fmt.Errorf("prescription upload: %w", err)
	}
	if validated == nil {
		return nil, fmt.Errorf("prescription upload is empty")
	}
	b.Metadata.PrescriptionPhoto = validated
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		return nil, fmt.Errorf("prescription style: %w", err)
	}
	t, err := template.New("upload").Funcs(templateFuncs).Parse(`<!doctype html><html><head><meta charset="utf-8"><style>{{.FontFaces}}{{.CSS}} .page-break{break-before:auto} .page-head{margin-bottom:2mm} .page-head h2{font-size:16pt} .page-head .kicker{margin-bottom:1mm} .page-head::after{margin-top:1mm} .block-purpose{margin:1mm 0} .prescription-photo{margin-top:2mm} .prescription-photo img{max-height:200mm;object-fit:contain} .annex-note{margin:0 0 2mm}</style></head><body class="book1"><p class="annex-note">Supplied prescription annex. Footer page numbers restart within this annex.</p>{{template "B1-PRESCRIPTION-01" .Data}}</body></html>`)
	if err != nil {
		return nil, fmt.Errorf("prescription layout: %w", err)
	}
	t, err = t.ParseFS(templateFS, "templates/book1/prescription.html")
	if err != nil {
		return nil, fmt.Errorf("prescription template: %w", err)
	}
	var html bytes.Buffer
	if err := t.ExecuteTemplate(&html, "upload", renderContext{CSS: template.CSS(css), FontFaces: fontFacesCSS, Data: b}); err != nil {
		return nil, fmt.Errorf("prescription render: %w", err)
	}
	metadata := b.Metadata
	metadata.BookVersion += " prescription annex"
	attachment, err := PrintPDF(ctx, html.Bytes(), metadata)
	if err != nil {
		return nil, fmt.Errorf("prescription print: %w", err)
	}
	return mergeBookPDF(ctx, nativePDF, attachment)
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
