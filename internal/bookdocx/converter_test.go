package bookdocx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	mode := filepath.Base(os.Args[0])
	if strings.HasPrefix(mode, "bookdocx-helper-") {
		switch strings.TrimPrefix(mode, "bookdocx-helper-") {
		case "error":
			fmt.Fprint(os.Stderr, "intentional converter failure")
			os.Exit(23)
		case "sleep":
			time.Sleep(time.Minute)
			os.Exit(0)
		case "empty":
			os.Exit(0)
		case "delay":
			time.Sleep(150 * time.Millisecond)
			os.Exit(0)
		case "descendant":
			if len(os.Args) > 1 && os.Args[1] == "child" {
				time.Sleep(time.Minute)
				os.Exit(0)
			}
			child := exec.Command(os.Args[0], "child")
			if err := child.Start(); err != nil {
				os.Exit(2)
			}
			cwd, _ := os.Getwd()
			if err := os.WriteFile(filepath.Join(filepath.Dir(cwd), "descendant.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
				os.Exit(2)
			}
			time.Sleep(time.Minute)
			os.Exit(0)
		}
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func fakeConverter(t *testing.T, mode string, timeout time.Duration, slots int) *Converter {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bookdocx-helper-"+mode)
	if err := os.Link(exe, path); err != nil {
		t.Fatal(err)
	}
	c, err := NewConverter(ConverterOptions{Executable: path, TempDir: dir, Timeout: timeout, MaxConcurrent: slots})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func assertClean(t *testing.T, c *Converter) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(c.tempDir, "bookdocx-*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if !strings.Contains(filepath.Base(n), "helper-") {
			t.Error("temporary path leaked", n)
		}
	}
}

func TestConverterFailures(t *testing.T) {
	doc, err := Fill(context.Background(), request(t, 1, "en"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ mode, contains string }{{"error", "intentional converter failure"}, {"empty", "did not produce a PDF"}} {
		c := fakeConverter(t, tc.mode, 10*time.Second, 1)
		_, err := c.PDF(context.Background(), doc)
		if err == nil || !strings.Contains(err.Error(), tc.contains) {
			t.Fatalf("%s: %v", tc.mode, err)
		}
		assertClean(t, c)
	}
	c := fakeConverter(t, "sleep", 100*time.Millisecond, 1)
	start := time.Now()
	_, err = c.PDF(context.Background(), doc)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatal("timeout not bounded", err)
	}
	assertClean(t, c)
	c = fakeConverter(t, "descendant", 100*time.Millisecond, 1)
	_, err = c.PDF(context.Background(), doc)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("descendant cancellation", err)
	}
	pid, readErr := os.ReadFile(filepath.Join(c.tempDir, "descendant.pid"))
	if readErr != nil {
		t.Fatal("child process was not started", readErr)
	}
	if stat, readErr := os.ReadFile("/proc/" + string(pid) + "/stat"); readErr == nil {
		fields := strings.Fields(string(stat))
		if len(fields) < 3 || fields[2] != "Z" {
			t.Fatal("child process survived cancellation", string(stat))
		}
	}
	assertClean(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.PDF(ctx, doc)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertClean(t, c)
	doc.DOCX[0] ^= 1
	if _, err := c.PDF(context.Background(), doc); err == nil {
		t.Fatal("modified document accepted")
	}
	for _, o := range []ConverterOptions{{Executable: "/does/not/exist"}, {Timeout: 6 * time.Minute}, {MaxConcurrent: 5}, {MaxConcurrent: -1}} {
		if _, err := NewConverter(o); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}

func TestConverterConcurrency(t *testing.T) {
	doc, err := Fill(context.Background(), request(t, 1, "en"))
	if err != nil {
		t.Fatal(err)
	}
	c := fakeConverter(t, "delay", 10*time.Second, 1)
	var wg sync.WaitGroup
	start := time.Now()
	for range 3 {
		wg.Go(func() {
			if _, err := c.PDF(context.Background(), doc); err == nil {
				t.Error("expected missing output")
			}
		})
	}
	wg.Wait()
	if time.Since(start) < 450*time.Millisecond {
		t.Fatal("conversion slot bound was bypassed")
	}
	assertClean(t, c)
	c.slots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.PDF(ctx, doc); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("queue did not cancel", err)
	}
	<-c.slots
}

func TestConverterOutputChecks(t *testing.T) {
	good := "name type encoding emb sub uni object ID\n---\nABCDEF+NotoSansBengali-Regular CID TrueType Identity-H yes yes yes 1 0\n"
	if err := validatePDFFonts(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", strings.ReplaceAll(good, "NotoSansBengali-Regular", "FreeSans"), strings.ReplaceAll(good, "yes yes yes", "no yes yes")} {
		if err := validatePDFFonts(bad); err == nil {
			t.Fatal("font check accepted bad inventory")
		}
	}
	path := filepath.Join(t.TempDir(), "bad.pdf")
	if err := os.WriteFile(path, []byte("not a PDF"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPDF(path); err == nil {
		t.Fatal("invalid PDF accepted")
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxPDFBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := readPDF(path); err == nil {
		t.Fatal("oversized PDF accepted")
	}
}
