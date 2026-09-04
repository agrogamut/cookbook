package book

import (
	"strings"
	"testing"
)

func TestFontFacesCSSEmbedsAllFiveWeightsWithNoNetworkReference(t *testing.T) {
	css := string(fontFacesCSS)
	for _, weight := range []string{"400", "600", "700", "800"} {
		if !strings.Contains(css, "font-weight: "+weight) {
			t.Errorf("expected an @font-face rule for weight %s, got:\n%s", weight, css)
		}
	}
	if !strings.Contains(css, "font-style: italic") {
		t.Error("expected an italic @font-face rule")
	}
	if strings.Contains(css, "fonts.googleapis.com") || strings.Contains(css, "fonts.gstatic.com") {
		t.Fatal("font-face rules must embed data: URIs, never reference a network host -- " +
			"the print browser runs with --host-resolver-rules=MAP * ~NOTFOUND")
	}
	if !strings.Contains(css, "data:font/woff2;base64,") {
		t.Fatal("expected at least one embedded data: URI")
	}
}
