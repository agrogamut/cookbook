package book

import (
	"strings"
	"testing"
)

func TestTokensUseTheUnifiedCoralCreamPalette(t *testing.T) {
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	s := string(css)

	for _, want := range []string{"#ff5858", "#fffcf6"} {
		if !strings.Contains(s, want) {
			t.Errorf("expected the unified palette colour %s in tokens.css", want)
		}
	}
	for _, gone := range []string{"#0f6466", "#123a5f", "#8d4a70", "#5c2650"} {
		if strings.Contains(s, gone) {
			t.Errorf("old per-book brand colour %s should have been replaced by the unified palette", gone)
		}
	}
	// The clinical warning palette is untouched -- it must stay visually distinct from the
	// new decorative brand colour, per avoid_color_only_meaning.
	if !strings.Contains(s, "#9b2226") {
		t.Error("--warning-strong must be unchanged")
	}
}

func TestTokensUsePoppinsForLatinTextAndNotoForIndicText(t *testing.T) {
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	s := string(css)

	if !strings.Contains(s, `"Poppins"`) {
		t.Error("--font-serif/--font-sans should reference Poppins")
	}
	// Untouched, verbatim, both stacks.
	for _, want := range []string{"Noto Serif Bengali", "Noto Serif Devanagari", "Noto Sans Bengali", "Noto Sans Devanagari"} {
		if !strings.Contains(s, want) {
			t.Errorf("--font-indic/--font-indic-sans must still reference %s", want)
		}
	}
}

func TestIndicFontStacksHaveNoCSSVariableCycle(t *testing.T) {
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	s := string(css)

	for _, tc := range []struct {
		name      string
		forbidden string
		want      string
	}{
		{"--font-indic", "var(--font-serif)", `"Noto Serif Bengali"`},
		{"--font-indic-sans", "var(--font-sans)", `"Noto Sans Bengali"`},
	} {
		declaration := cssCustomProperty(s, tc.name)
		if declaration == "" {
			t.Fatalf("missing %s declaration", tc.name)
		}
		if strings.Contains(declaration, tc.forbidden) {
			t.Fatalf("%s must not fall back to the variable it replaces: %s", tc.name, declaration)
		}
		if !strings.Contains(declaration, tc.want) {
			t.Fatalf("%s must explicitly prefer a Bengali-capable Noto family: %s", tc.name, declaration)
		}
	}
}

func TestBengaliFurnitureRemovesLatinTrackingOnlyForBengali(t *testing.T) {
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	s := string(css)

	want := `html[lang="bn"] :is(.label, [class*="kicker"], th, h3, .contents-group, .toc-group) {
  letter-spacing: normal;
}`
	if !strings.Contains(s, want) {
		t.Fatalf("Bengali labels, kickers, headings, and table headers must disable Latin tracking")
	}
	for _, latinRule := range []string{
		"letter-spacing: 0.08em;",
		"letter-spacing: 0.14em;",
		"letter-spacing: 0.24em;",
	} {
		if !strings.Contains(s, latinRule) {
			t.Fatalf("Latin tracking rule %q must remain available outside the Bengali override", latinRule)
		}
	}
}

func cssCustomProperty(css, name string) string {
	start := strings.Index(css, name+":")
	if start == -1 {
		return ""
	}
	declaration := css[start+len(name)+1:]
	if end := strings.IndexByte(declaration, ';'); end >= 0 {
		declaration = declaration[:end]
	}
	return strings.TrimSpace(declaration)
}
