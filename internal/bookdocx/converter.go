package bookdocx

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed fonts/*.ttf fonts/manifest.json
var fontAssets embed.FS

type ConverterOptions struct {
	Executable    string
	TempDir       string
	Timeout       time.Duration
	MaxConcurrent int
}

type Converter struct {
	executable, tempDir, limiter, fontInspector string
	timeout                                     time.Duration
	slots                                       chan struct{}
}

var processSlots = make(chan struct{}, 4)

func NewConverter(o ConverterOptions) (*Converter, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("native conversion requires Linux process isolation")
	}
	if o.Timeout == 0 {
		o.Timeout = 120 * time.Second
	}
	if o.MaxConcurrent == 0 {
		o.MaxConcurrent = 2
	}
	if o.Timeout < time.Millisecond || o.Timeout > 5*time.Minute || o.MaxConcurrent < 1 || o.MaxConcurrent > 4 {
		return nil, fmt.Errorf("invalid conversion timeout or concurrency")
	}
	if o.Executable == "" {
		o.Executable = "libreoffice"
	}
	executable, err := exec.LookPath(o.Executable)
	if err != nil {
		return nil, fmt.Errorf("find native converter: %w", err)
	}
	limiter, err := exec.LookPath("prlimit")
	if err != nil {
		return nil, fmt.Errorf("find process resource limiter: %w", err)
	}
	inspector, err := exec.LookPath("pdffonts")
	if err != nil {
		return nil, fmt.Errorf("find PDF font inspector: %w", err)
	}
	if err := verifyFonts(); err != nil {
		return nil, err
	}
	return &Converter{executable: executable, tempDir: o.TempDir, limiter: limiter, fontInspector: inspector, timeout: o.Timeout, slots: make(chan struct{}, o.MaxConcurrent)}, nil
}

type fontRecord struct{ File, SHA256, Version, Family string }

func fonts() ([]fontRecord, error) {
	b, err := fontAssets.ReadFile("fonts/manifest.json")
	if err != nil {
		return nil, err
	}
	var records []fontRecord
	if err := json.Unmarshal(b, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func verifyFonts() error {
	records, err := fonts()
	if err != nil {
		return fmt.Errorf("font manifest: %w", err)
	}
	if len(records) != 9 {
		return fmt.Errorf("font manifest requires nine font files")
	}
	for _, r := range records {
		b, err := fontAssets.ReadFile("fonts/" + r.File)
		if err != nil {
			return fmt.Errorf("read font %s: %w", r.File, err)
		}
		if hash(b) != r.SHA256 {
			return fmt.Errorf("font hash mismatch: %s", r.File)
		}
	}
	return nil
}

func prepareFonts(dir string) (string, error) {
	records, err := fonts()
	if err != nil {
		return "", err
	}
	fontDir := filepath.Join(dir, "fonts")
	if err := os.Mkdir(fontDir, 0700); err != nil {
		return "", err
	}
	for _, r := range records {
		b, err := fontAssets.ReadFile("fonts/" + r.File)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fontDir, r.File), b, 0600); err != nil {
			return "", err
		}
	}
	config := `<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd"><fontconfig><dir>` + escaped(fontDir) + `</dir><cachedir>` + escaped(filepath.Join(dir, "font-cache")) + `</cachedir><alias><family>sans-serif</family><prefer><family>Noto Sans</family><family>Noto Sans Bengali</family><family>Noto Sans Devanagari</family><family>Noto Sans Symbols 2</family></prefer></alias><alias><family>serif</family><prefer><family>Noto Sans</family></prefer></alias><alias><family>monospace</family><prefer><family>Noto Sans</family></prefer></alias></fontconfig>`
	path := filepath.Join(dir, "fonts.conf")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		return "", err
	}
	return path, nil
}

func acquire(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Converter) PDF(ctx context.Context, doc Document) (pdf []byte, err error) {
	if len(doc.DOCX) == 0 || len(doc.DOCX) > MaxDOCXBytes || sha256.Sum256(doc.DOCX) != doc.digest {
		return nil, fmt.Errorf("conversion requires an unmodified Document returned by Fill")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := acquire(ctx, c.slots); err != nil {
		return nil, fmt.Errorf("conversion queue: %w", err)
	}
	defer func() { <-c.slots }()
	if err := acquire(ctx, processSlots); err != nil {
		return nil, fmt.Errorf("process queue: %w", err)
	}
	defer func() { <-processSlots }()
	dir, err := os.MkdirTemp(c.tempDir, "bookdocx-")
	if err != nil {
		return nil, fmt.Errorf("conversion temporary directory: %w", err)
	}
	defer func() {
		if e := os.RemoveAll(dir); e != nil {
			pdf = nil
			err = errors.Join(err, fmt.Errorf("remove conversion directory: %w", e))
		}
	}()
	fontConfig, err := prepareFonts(dir)
	if err != nil {
		return nil, fmt.Errorf("prepare isolated fonts: %w", err)
	}
	input := filepath.Join(dir, "book.docx")
	if err := os.WriteFile(input, doc.DOCX, 0600); err != nil {
		return nil, fmt.Errorf("write conversion input: %w", err)
	}
	profile := (&url.URL{Scheme: "file", Path: filepath.Join(dir, "profile")}).String()
	env := []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "HOME=" + dir, "TMPDIR=" + dir, "XDG_CACHE_HOME=" + filepath.Join(dir, "cache"), "FONTCONFIG_FILE=" + fontConfig, "FONTCONFIG_PATH=" + dir, "SAL_USE_VCLPLUGIN=svp"}
	args := []string{"--fsize=67108864", "--as=4294967296", "--", c.executable, "-env:UserInstallation=" + profile, "--headless", "--nologo", "--nodefault", "--nofirststartwizard", "--norestore", "--convert-to", `pdf:writer_pdf_Export:{"SelectPdfVersion":{"type":"long","value":"2"},"UseTaggedPDF":{"type":"boolean","value":"true"},"EmbedStandardFonts":{"type":"boolean","value":"true"}}`, "--outdir", dir, input}
	conversionOutput, err := runBounded(ctx, c.limiter, args, env, dir)
	if err != nil {
		return nil, fmt.Errorf("native DOCX conversion: %w", err)
	}
	path := filepath.Join(dir, "book.pdf")
	pdf, err = readPDF(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, conversionOutput)
	}
	output, err := runBounded(ctx, c.fontInspector, []string{path}, env, dir)
	if err != nil {
		return nil, fmt.Errorf("PDF font inspection: %w", err)
	}
	if err := validatePDFFonts(output); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return pdf, nil
}

func readPDF(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("converter did not produce a PDF: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() < 5 || st.Size() > MaxPDFBytes {
		return nil, fmt.Errorf("invalid or oversized PDF output: %d bytes", st.Size())
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxPDFBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxPDFBytes || !strings.HasPrefix(string(b[:min(5, len(b))]), "%PDF-") {
		return nil, fmt.Errorf("invalid PDF output")
	}
	return b, nil
}

func validatePDFFonts(output string) error {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "name" || strings.HasPrefix(fields[0], "---") {
			continue
		}
		if len(fields) < 8 {
			return fmt.Errorf("unrecognized PDF font inventory")
		}
		name := fields[0]
		if i := strings.IndexByte(name, '+'); i >= 0 {
			name = name[i+1:]
		}
		approved := false
		for _, family := range []string{"NotoSans", "NotoSansBengali", "NotoSansDevanagari", "NotoSansSymbols2"} {
			for _, style := range []string{"", "-Regular", "-Bold", "-Italic", "-BoldItalic"} {
				if name == family+style {
					approved = true
				}
			}
		}
		if !approved || fields[len(fields)-5] != "yes" {
			return fmt.Errorf("unapproved or unembedded PDF font: %s", name)
		}
		count++
	}
	if count == 0 {
		return fmt.Errorf("PDF has no embedded font inventory")
	}
	return nil
}

type boundedOutput struct {
	mu sync.Mutex
	b  strings.Builder
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if n := 16384 - w.b.Len(); n > 0 {
		w.b.Write(p[:min(n, len(p))])
	}
	return len(p), nil
}

func runBounded(ctx context.Context, executable string, args, env []string, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	cmd.Env = env
	configureProcess(cmd)
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if ctx.Err() != nil {
		return output.b.String(), ctx.Err()
	}
	if err != nil {
		return output.b.String(), fmt.Errorf("%s: %w: %s", filepath.Base(executable), err, output.b.String())
	}
	return output.b.String(), nil
}
