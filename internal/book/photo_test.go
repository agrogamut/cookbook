package book

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// A fully decodable PNG fixture.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGOUC+hhYGBgYmBgYGBgAAALdAD+3y8XygAAAABJRU5ErkJggg=="

func TestParsePhotoAcceptsRealImages(t *testing.T) {
	for mt, encoded := range map[string]string{"image/png": onePixelPNG, "image/jpeg": "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wAARCAACAAIDASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDh6KKK+iPEP//Z", "image/webp": "UklGRjgAAABXRUJQVlA4ICwAAADwAQCdASoCAAIAAUAmJaACdLoB+AAETAAA/vSIh/5Z8/hs49/996A3gYAAAA=="} {
		got, err := ParsePhoto("data:"+mt+";base64,"+encoded, " family photo ")
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
		{"mislabeled image", "data:image/jpeg;base64," + onePixelPNG, "do not match"},
		{"truncated image", "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("PNG")), "do not match"},
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
