package bookmasters

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestMasters(t *testing.T) {
	want := map[string]string{
		"1/bn": "6ecb9a89f356d6c3bb6472f50d0bb592bc8d324c8965130b1bfd79bfbee7bd70",
		"1/en": "04ac1c951c0600fc77918a761b39314281822f08e889e96dcaa0ece333376dc7",
		"1/hi": "685083a537341614c85c158d241d97d0c1a01e2225f58e3eaea4ff208ffbd7cf",
		"2/bn": "f51a2aceae03df027a8ca456e82de20bceb95cb14c7557dc7e9dc3cc5932603d",
		"2/en": "4cdb351dd8b51b3026f46c33660bbb5430e8176f8c091879c979b5e0b3d53731",
		"2/hi": "12a7ea7cde8c032d8d2bb4a3dca45e93ed9c2b8652db84371d19521b7f672d38",
	}
	entries, err := Catalog()
	if err != nil || len(entries) != 6 {
		t.Fatalf("catalog: %v, entries=%d", err, len(entries))
	}
	for _, e := range entries {
		key := fmt.Sprintf("%d/%s", e.Book, e.Language)
		b, m, err := Read(e.Book, e.Language, "v1")
		if err != nil || m.SHA256 != want[key] || fmt.Sprintf("%x", sha256.Sum256(b)) != want[key] {
			t.Fatalf("master %s: %v, metadata=%+v", key, err, m)
		}
		delete(want, key)
		b[0] ^= 0xff
		again, _, err := Read(e.Book, e.Language, "v1")
		if err != nil || b[0] == again[0] {
			t.Fatalf("caller mutated embedded master %s", key)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing masters: %v", want)
	}
	entries[0].SHA256 = "changed"
	if _, _, err := Read(1, "bn", "v1"); err != nil {
		t.Fatal("catalog mutation leaked", err)
	}
	for _, key := range []Master{{Book: 1, Language: "en", Version: "v2"}, {Book: 3, Language: "en", Version: "v1"}, {Book: 1, Language: "../en", Version: "v1"}} {
		if _, _, err := Read(key.Book, key.Language, key.Version); err == nil {
			t.Fatalf("unknown key accepted: %+v", key)
		}
	}
}
