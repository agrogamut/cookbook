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
