package book

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
)

// The page watermark: water1.jpeg, the icon mark alone (not the full text lockup, which is
// not used anywhere in this renderer). Embedded as a data URI baked into the binary via
// embed.FS, the same trust tier as the SVG dish marks in marks.go -- a checked-in repository
// file, never anything from the database or a request, so it needs no validation the way an
// operator-uploaded ChildPhoto does.
//
// Placement is a position: fixed <img> with a negative z-index (base.html, .watermark in
// tokens.css), not a CSS background-image. Two things ruled out the alternatives: Chromium's
// print-to-PDF does not repeat a plain background-image past page 1 of a multi-page document,
// and CSS @page margin-box regions only cover the margin bands, not the content area a
// centered watermark needs. A real print spike (2026-08-24) confirmed position: fixed on a
// non-flow element repeats correctly on every physical page -- unlike the in-document
// position: fixed banner that was tried and rejected for the old provisional disclosure,
// which was a block-level element competing with document flow. This is not.
//
//go:embed assets/water1.jpeg
var watermarkJPEG []byte

// watermarkDataURI is template.URL for the same reason ChildPhoto.DataURI is: html/template
// refuses to emit a data: URI in a src attribute unless the type says "already checked", and
// this is the one place in the codebase that constructs one from a fixed, trusted source
// rather than validated user input.
var watermarkDataURI = template.URL(
	fmt.Sprintf("data:image/jpeg;base64,%s", base64.StdEncoding.EncodeToString(watermarkJPEG)))
