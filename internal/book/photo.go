package book

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"strings"
)

// ErrBadPhoto means the uploaded image was rejected. Reported to the operator rather than
// dropped, because a cover printed without the photograph someone attached looks like the
// upload worked and the book simply has no picture.
var ErrBadPhoto = errors.New("book: photo rejected")

// maxPhotoBytes caps the decoded image.
//
// 8 MB is far more than a cover portrait needs and still well inside what a phone camera
// produces, so an operator is not made to resize a photograph before they can use it. The
// cap exists because the image is embedded in the HTML handed to Chromium: an unbounded one
// is held in memory three times over -- request, document, print -- and on a small instance
// that is the whole budget.
const maxPhotoBytes = 8 << 20

// photoTypes is the allowlist of image formats a cover may carry.
//
// An allowlist, not a blocklist, and SVG is deliberately absent. An SVG is a document: it can
// carry <script>, and it would be embedded into a page this service then renders in a real
// browser. Every format here is raster data that a decoder turns into pixels and cannot
// execute. GIF is excluded for a duller reason -- it prints as its first frame, so an
// operator who uploads an animation gets something they did not choose.
var photoTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// ChildPhoto is a validated cover image, ready to embed.
//
// It holds the data URI rather than raw bytes because that is what the template needs and
// what a print requires: the browser renders from the document alone and never fetches, so a
// photograph has to travel inside the HTML. It also means nothing is written to disk.
type ChildPhoto struct {
	// DataURI is template.URL, not string, and that type is load-bearing.
	//
	// html/template refuses to emit a data: URI in a src attribute -- it allows only http,
	// https, mailto and relative URLs, and silently rewrites everything else to #ZgotmplZ.
	// A plain string here therefore produced a cover with a broken image and no error
	// anywhere: the photo validated, embedded, and vanished at render.
	//
	// template.URL means "I have already checked this", so it is only sound because
	// ParsePhoto is the sole way to build one: the media type comes from an allowlist of
	// raster formats, the payload is base64 that decoded, and the URI handed on is rebuilt
	// from those bytes rather than passed through. Never construct this literally.
	DataURI template.URL `json:"data_uri"`
	Caption string       `json:"caption,omitempty"`
}

// ParsePhoto validates an uploaded data URI and returns it ready for a template.
//
// The input comes from an operator's file picker and reaches a real browser engine, so it is
// checked rather than trusted: the media type must be one this project prints, and the
// payload must be base64 that actually decodes. A URI that merely looks well-formed would
// otherwise reach Chromium and fail there, where the error is a blank box on a printed page
// instead of a message someone can act on.
//
// An empty input is not an error. Most books have no photograph, and a missing one is an
// ordinary absence rather than a failure.
func ParsePhoto(dataURI, caption string) (*ChildPhoto, error) {
	raw := strings.TrimSpace(dataURI)
	if raw == "" {
		return nil, nil
	}

	if !strings.HasPrefix(raw, "data:") {
		return nil, fmt.Errorf("%w: must be a data URI; a remote URL cannot be used because "+
			"the print engine never fetches, so the image would be missing from the page", ErrBadPhoto)
	}

	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return nil, fmt.Errorf("%w: malformed data URI, no comma separating header from data", ErrBadPhoto)
	}
	header, payload := raw[len("data:"):comma], raw[comma+1:]

	if !strings.HasSuffix(header, ";base64") {
		return nil, fmt.Errorf("%w: only base64 data URIs are accepted", ErrBadPhoto)
	}
	mediaType := strings.ToLower(strings.TrimSuffix(header, ";base64"))

	if !photoTypes[mediaType] {
		return nil, fmt.Errorf("%w: %q is not a printable image type; accepted types are "+
			"image/png, image/jpeg and image/webp", ErrBadPhoto, mediaType)
	}

	// Decoded, not merely measured: base64 that does not decode reaches the browser as a
	// broken image, which prints as an empty frame on the cover of a book someone hands to a
	// parent. Better to refuse it here, where there is somebody to tell.
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: image data is not valid base64: %v", ErrBadPhoto, err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("%w: image data is empty", ErrBadPhoto)
	}
	if len(decoded) > maxPhotoBytes {
		return nil, fmt.Errorf("%w: image is %d bytes, over the %d byte limit",
			ErrBadPhoto, len(decoded), maxPhotoBytes)
	}

	// Re-encoded from the bytes that were actually validated, so whitespace or a stray
	// fragment in the submitted string cannot survive into the document.
	return &ChildPhoto{
		DataURI: template.URL("data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(decoded)),
		Caption: strings.TrimSpace(caption),
	}, nil
}
