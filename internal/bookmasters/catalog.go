// Package bookmasters provides the unchanged, versioned document masters.
package bookmasters

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed catalog.json masters/*/*.docx
var assets embed.FS

type Master struct {
	Book         int    `json:"book"`
	Language     string `json:"language"`
	Version      string `json:"version"`
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	OriginalName string `json:"original_name"`
}

func Catalog() ([]Master, error) {
	b, err := assets.ReadFile("catalog.json")
	if err != nil {
		return nil, fmt.Errorf("read master catalog: %w", err)
	}
	var entries []Master
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("decode master catalog: %w", err)
	}
	return entries, nil
}

// Read returns a fresh byte slice; callers cannot mutate the embedded master.
func Read(book int, language, version string) ([]byte, Master, error) {
	entries, err := Catalog()
	if err != nil {
		return nil, Master{}, err
	}
	for _, m := range entries {
		if m.Book != book || m.Language != language || m.Version != version {
			continue
		}
		b, err := assets.ReadFile(m.Path)
		if err != nil {
			return nil, Master{}, fmt.Errorf("read master: %w", err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != m.SHA256 {
			return nil, Master{}, fmt.Errorf("master hash mismatch: %s", m.Path)
		}
		return b, m, nil
	}
	return nil, Master{}, fmt.Errorf("unknown master: book %d language %q version %q", book, language, version)
}
