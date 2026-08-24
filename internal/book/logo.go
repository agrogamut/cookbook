package book

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
)

// The MadamGY wordmark: water2.jpeg, the full lockup (icon plus "MadamGY" text) rather than
// the icon-alone mark watermark.go embeds as the repeating page watermark. Printed once on
// the cover and once on the closing page of each book -- the two pages a reader actually
// looks at as a document, rather than repeated on every sheet the way the watermark is.
//
// Same trust tier as watermark.go's water1.jpeg: a checked-in repository file, embedded at
// build time via embed.FS, never anything from the database or a request, so it needs no
// validation the way an operator-uploaded ChildPhoto does.
//
//go:embed assets/water2.jpeg
var logoJPEG []byte

// logoDataURI is template.URL for the same reason watermarkDataURI and ChildPhoto.DataURI
// are: html/template refuses to emit a data: URI in a src attribute unless the type says
// "already checked," and this is a fixed, trusted source rather than validated user input.
var logoDataURI = template.URL(
	fmt.Sprintf("data:image/jpeg;base64,%s", base64.StdEncoding.EncodeToString(logoJPEG)))
