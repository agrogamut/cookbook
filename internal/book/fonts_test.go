package book

import (
	"strings"
	"testing"
)

func TestFontFacesCSSEmbedsAllSixFacesWithNoNetworkReference(t *testing.T) {
	css := string(fontFacesCSS)
	for _, weight := range []string{"400", "600", "700", "800"} {
		if !strings.Contains(css, "font-weight: "+weight) {
			t.Errorf("expected an @font-face rule for weight %s, got:\n%s", weight, css)
		}
	}
	// Four CSS selectors (.page-head .standfirst, .domain-limit, .cover .cover-sub,
	// .chapter .chapter-sub) set font-style: italic at the browser default weight, 400.
	// Without a 400-italic face, CSS font matching (which filters by style before weight)
	// resolved those to the only italic face that existed, 600 -- printing them visibly
	// bolder than intended. Both italic weights must be present, not just "an" italic rule.
	for _, weight := range []string{"400", "600"} {
		if !strings.Contains(css, "font-style: italic;\n  font-weight: "+weight+";") {
			t.Errorf("expected an italic @font-face rule at weight %s, got:\n%s", weight, css)
		}
	}
	if got := strings.Count(css, "@font-face"); got != 6 {
		t.Errorf("expected 6 @font-face rules (400/600/700/800 normal, 400/600 italic), got %d", got)
	}
	if strings.Contains(css, "fonts.googleapis.com") || strings.Contains(css, "fonts.gstatic.com") {
		t.Fatal("font-face rules must embed data: URIs, never reference a network host -- " +
			"the print browser runs with --host-resolver-rules=MAP * ~NOTFOUND")
	}
	if !strings.Contains(css, "data:font/woff2;base64,") {
		t.Fatal("expected at least one embedded data: URI")
	}
}
