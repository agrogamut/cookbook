package book

import (
	"context"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/bookdocx"
	"github.com/madamgy/recipie/internal/profile"
)

func TestNativeIdentityFieldPassthrough(t *testing.T) {
	for _, name := range []string{"a", "May", "পরীক্ষা ১২", "नाम7"} {
		t.Run(name, func(t *testing.T) {
			s := profile.Stored{DisplayName: name}
			set := Set{Book1: Book1{Metadata: Metadata{Language: "bn"}, Letter: letterPage(name)}}
			n := NativeBindings(s, set)
			m := n.Books[0].Modules["B1-02"]
			m.Blocks = m.Blocks[:1]
			n.Books[0].Modules = map[string]bookdocx.Module{"B1-02": m}
			d := &translateFakeDrafter{transform: func(text string) string { return "translated " + text }}
			if err := LocalizeNative(context.Background(), &n, d); err != nil {
				t.Fatal(err)
			}
			if got := n.Books[0].Modules["B1-02"].Blocks[0].Paragraph.Runs[0].Text; got != name {
				t.Fatal("identity field was translated")
			}
			if d.calls != 0 {
				t.Fatal("identity field reached the provider")
			}
		})
	}
}

func TestNativeIdentityCommonWordOnlyGreetingProtected(t *testing.T) {
	s := profile.Stored{DisplayName: "a"}
	set := Set{Book1: Book1{Metadata: Metadata{Language: "bn"}, Letter: letterPage(s.DisplayName)}}
	n := NativeBindings(s, set)
	m := n.Books[0].Modules["B1-02"]
	n.Books[0].Modules = map[string]bookdocx.Module{"B1-02": m}
	path := nativeModulePath(1, "B1-02") + "/Blocks/1/Paragraph/Runs/0/Text"
	original := m.Blocks[1].Paragraph.Runs[0].Text
	span := n.fallback.Bindings[path].Identity
	masked, tokens, _, err := nativeProtect(original, nativeProtected, span)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(masked, "Dear ZXQ0QXZ's family,") || !strings.Contains(masked, "a guess dressed up as a fact") || tokens[0] != "a" {
		t.Fatal("ordinary matching word or greeting changed")
	}
	if restored, err := nativeRestore(masked, tokens); err != nil || restored != original {
		t.Fatal("source identity round trip")
	}
	d := &translateFakeDrafter{fail: true}
	if err := LocalizeNative(context.Background(), &n, d); err != nil {
		t.Fatal(err)
	}
	got := n.Books[0].Modules["B1-02"].Blocks[1].Paragraph.Runs[0].Text
	if !strings.Contains(got, s.DisplayName) || d.calls != 0 {
		t.Fatal("greeting identity missing")
	}
}
