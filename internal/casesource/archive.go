// Package casesource reads case deliveries into independent, immutable staging.
package casesource

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	SourceKey       = "madamgy-wb-cases"
	ParserVersion   = "1"
	MaxArchiveBytes = 32 << 20
	maxMemberBytes  = 16 << 20
	maxExpanded     = 128 << 20
)

type Archive struct {
	SHA256    string
	Workbooks []Workbook
}

type Workbook struct {
	Name   string
	SHA256 string
	Case   int
	Sheets []Sheet
}

type Sheet struct {
	Name    string
	Part    string
	Headers []string
	Rows    []Row
}

type Row struct {
	Number      int
	RecipeID    string
	Cells       []Cell
	Ingredients []Ingredient
}

type Cell struct {
	Ref      string `json:"ref"`
	Type     string `json:"type,omitempty"`
	RawValue string `json:"raw_value,omitempty"`
	Value    string `json:"value,omitempty"`
}

type Ingredient struct {
	Ordinal  int
	RawID    string
	RawName  string
	RawGrams string
	Grams    string
}

func hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func Read(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read case archive: %w", err)
	}
	if len(b) > MaxArchiveBytes {
		return nil, fmt.Errorf("case archive exceeds %d bytes", MaxArchiveBytes)
	}
	return b, nil
}

var memberName = regexp.MustCompile(`^MadamGY_WB_Case_([0-9]{2})_.+\.xlsx$`)

func Parse(ctx context.Context, data []byte) (*Archive, error) {
	if len(data) > MaxArchiveBytes {
		return nil, fmt.Errorf("case archive exceeds size limit")
	}
	files, err := packageFiles(data, 128)
	if err != nil {
		return nil, fmt.Errorf("case archive: %w", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	result := &Archive{SHA256: hash(data)}
	cases := map[int]bool{}
	remaining := uint64(maxExpanded)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		match := memberName.FindStringSubmatch(name)
		if match == nil {
			return nil, fmt.Errorf("unexpected case member %q", name)
		}
		n, _ := strconv.Atoi(match[1])
		if n == 0 || cases[n] {
			return nil, fmt.Errorf("invalid or repeated logical case %d", n)
		}
		cases[n] = true
		b, err := readMember(files[name])
		if err != nil {
			return nil, err
		}
		w, err := parseWorkbook(b, n, &remaining)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		w.Name, w.SHA256, w.Case = name, hash(b), n
		result.Workbooks = append(result.Workbooks, w)
	}
	return result, nil
}

func packageFiles(data []byte, maxFiles int) (map[string]*zip.File, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open ZIP: %w", err)
	}
	if len(z.File) == 0 || len(z.File) > maxFiles {
		return nil, fmt.Errorf("ZIP member count out of bounds: %d", len(z.File))
	}
	files := make(map[string]*zip.File, len(z.File))
	var total uint64
	for _, f := range z.File {
		name := f.Name
		if name == "." || path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "../") || !f.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe ZIP member %q", name)
		}
		if files[name] != nil {
			return nil, fmt.Errorf("duplicate ZIP member %q", name)
		}
		if f.Flags&1 != 0 || (f.Method != zip.Store && f.Method != zip.Deflate) {
			return nil, fmt.Errorf("unsupported ZIP member %q", name)
		}
		if f.UncompressedSize64 > maxMemberBytes || total+f.UncompressedSize64 > maxExpanded {
			return nil, fmt.Errorf("expanded ZIP size exceeds limit at %q", name)
		}
		total += f.UncompressedSize64
		files[name] = f
	}
	return files, nil
}

func readMember(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open ZIP member %s: %w", f.Name, err)
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, maxMemberBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read ZIP member %s: %w", f.Name, err)
	}
	if len(b) > maxMemberBytes || uint64(len(b)) != f.UncompressedSize64 {
		return nil, fmt.Errorf("ZIP member size mismatch: %s", f.Name)
	}
	return b, nil
}
