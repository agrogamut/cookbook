package book

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// A 1x1 PNG, the smallest real image, so the test exercises decoding rather than a string
// that merely looks like base64.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestParsePhotoAcceptsRealImages(t *testing.T) {
	for _, mt := range []string{"image/png", "image/jpeg", "image/webp"} {
		got, err := ParsePhoto("data:"+mt+";base64,"+onePixelPNG, " family photo ")
		if err != nil {
			t.Fatalf("%s: %v", mt, err)
		}
		if !strings.HasPrefix(string(got.DataURI), "data:"+mt+";base64,") {
			t.Fatalf("%s: media type lost: %.40q", mt, got.DataURI)
		}
		if got.Caption != "family photo" {
			t.Fatalf("caption should be trimmed, got %q", got.Caption)
		}
	}
}

// No photo is the common case and must not be an error: most books have none.
func TestParsePhotoOnEmptyInput(t *testing.T) {
	for _, in := range []string{"", "   "} {
		got, err := ParsePhoto(in, "")
		if err != nil || got != nil {
			t.Fatalf("empty input: got %v, %v; want nil, nil", got, err)
		}
	}
}

// The rejections that matter. SVG is the one with teeth: it is a document that can carry
// script, and it would be embedded into a page this service renders in a real browser.
func TestParsePhotoRejects(t *testing.T) {
	svg := base64.StdEncoding.EncodeToString(
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>fetch("//x")</script></svg>`))

	for _, c := range []struct{ name, in, wantIn string }{
		{"svg, which can carry script", "data:image/svg+xml;base64," + svg, "not a printable image type"},
		{"gif, which prints as one frame", "data:image/gif;base64," + onePixelPNG, "not a printable image type"},
		{"a remote url the printer would never fetch", "https://example.com/child.jpg", "must be a data URI"},
		{"a non-image type", "data:text/html;base64," + onePixelPNG, "not a printable image type"},
		{"data that is not base64", "data:image/png;base64,!!!not base64!!!", "not valid base64"},
		{"a uri with no comma", "data:image/png;base64", "no comma"},
		{"a uri that is not base64-encoded", "data:image/png,rawbytes", "only base64"},
		{"empty image data", "data:image/png;base64,", "empty"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParsePhoto(c.in, "")
			if err == nil {
				t.Fatalf("must be rejected, got %v", got)
			}
			if !errors.Is(err, ErrBadPhoto) {
				t.Fatalf("must be ErrBadPhoto, got %v", err)
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Fatalf("message must explain the rejection; want %q in %q", c.wantIn, err)
			}
		})
	}
}

// An oversized image is refused here rather than reaching the browser, where it is held in
// memory three times over on an instance that does not have room for it.
func TestParsePhotoRejectsOversizedImages(t *testing.T) {
	big := base64.StdEncoding.EncodeToString(make([]byte, maxPhotoBytes+1))
	_, err := ParsePhoto("data:image/png;base64,"+big, "")
	if err == nil || !errors.Is(err, ErrBadPhoto) {
		t.Fatalf("an oversized image must be rejected, got %v", err)
	}
	if !strings.Contains(err.Error(), "over the") {
		t.Fatalf("the message must say it is over the limit, got %q", err)
	}
}

// The returned URI is rebuilt from the bytes that were validated, so nothing that rode along
// in the submitted string survives into the document.
func TestParsePhotoReencodesFromValidatedBytes(t *testing.T) {
	got, err := ParsePhoto("data:image/png;base64,"+onePixelPNG+"\n", "")
	if err != nil {
		t.Fatalf("ParsePhoto: %v", err)
	}
	if strings.ContainsAny(string(got.DataURI), "\n\r ") {
		t.Fatalf("the rebuilt URI must carry no whitespace: %q", got.DataURI)
	}
}
