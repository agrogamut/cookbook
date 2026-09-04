package book

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
)

// Poppins, self-hosted. Requested typeface was Gotham, a paid commercial face that cannot be
// embedded in a repository shipped to a print engine with no network access at render time
// (see pdf.go's --host-resolver-rules). Poppins is the common open substitute: a geometric
// grotesque in the same register, OFL-licensed, free to redistribute.
//
// Embedded the same way watermarkJPEG is in watermark.go: go:embed the checked-in binary,
// base64 it once at package init into a data: URI, so the print browser never fetches
// anything. This is Latin display/body type only -- see tokens.css's --font-indic/
// --font-indic-sans, which are untouched by this file and by every task in this plan.
//
//go:embed assets/fonts/poppins-400.woff2
var poppins400 []byte

//go:embed assets/fonts/poppins-600.woff2
var poppins600 []byte

//go:embed assets/fonts/poppins-700.woff2
var poppins700 []byte

//go:embed assets/fonts/poppins-800.woff2
var poppins800 []byte

//go:embed assets/fonts/poppins-600italic.woff2
var poppins600Italic []byte

func fontFaceRule(weight int, italic bool, data []byte) string {
	style := "normal"
	if italic {
		style = "italic"
	}
	return fmt.Sprintf(`@font-face {
  font-family: "Poppins";
  font-style: %s;
  font-weight: %d;
  font-display: swap;
  src: url(data:font/woff2;base64,%s) format("woff2");
}`, style, weight, base64.StdEncoding.EncodeToString(data))
}

// fontFacesCSS is every @font-face rule this project ships, concatenated once at package
// init. template.CSS marks it trusted for the same reason RenderHTML trusts tokens.css: it
// is built entirely from embedded repository files, never from the database or a request.
var fontFacesCSS = template.CSS(
	fontFaceRule(400, false, poppins400) + "\n" +
		fontFaceRule(600, false, poppins600) + "\n" +
		fontFaceRule(700, false, poppins700) + "\n" +
		fontFaceRule(800, false, poppins800) + "\n" +
		fontFaceRule(600, true, poppins600Italic),
)
