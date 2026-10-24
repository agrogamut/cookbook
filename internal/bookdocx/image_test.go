package bookdocx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strconv"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/bookmasters"
)

func imageFixture(t *testing.T, mime string) []byte {
	t.Helper()
	if mime == "image/webp" {
		// Lossless 60x40 geometric pattern, produced locally with ImageMagick.
		b, err := base64.StdEncoding.DecodeString("UklGRkYAAABXRUJQVlA4TDoAAAAvO8AJABcgEEikAhtySLFgojvQoDoFAmlIJYpRnv/4Fgxq2jaCOh+/g/jjDICOEf2fgOcQmyO5ns0B")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 60, 40))
	for y := range 40 {
		for x := range 60 {
			c := color.RGBA{0x24, 0x68, 0xac, 0xff}
			if x >= 30 && y < 20 {
				c = color.RGBA{0xf8, 0xc4, 0x36, 0xff}
			}
			if x < 30 && y >= 20 {
				c = color.RGBA{0xbc, 0x34, 0x56, 0xff}
			}
			pixels.SetRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	var err error
	if mime == "image/jpeg" {
		err = jpeg.Encode(&out, pixels, &jpeg.Options{Quality: 95})
	} else {
		err = png.Encode(&out, pixels)
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func imageBlocks(t *testing.T) []Block {
	t.Helper()
	var blocks []Block
	for _, mime := range []string{"image/png", "image/jpeg", "image/webp"} {
		blocks = append(blocks, Block{Paragraph: &Paragraph{Runs: []Run{{Text: "SYNTHETIC IMAGE " + mime}}}}, Block{Image: &Image{Data: imageFixture(t, mime), MIMEType: mime, Alt: `Synthetic <&> "pattern"`, WidthTwips: 2160, HeightTwips: 1440}})
	}
	return blocks
}

func stripImageAdditions(t *testing.T, data []byte) []byte {
	t.Helper()
	root, err := parseXML(data)
	if err != nil {
		t.Fatal(err)
	}
	var edits []edit
	root.walk(func(n *node) {
		if n.name.Space == relsNS && n.name.Local == "Relationship" && strings.HasPrefix(n.attr("Id"), "rIdBookdocxImage") || n.name.Space == typesNS && n.name.Local == "Override" && strings.HasPrefix(n.attr("PartName"), "/word/media/bookdocx-image-") {
			edits = append(edits, edit{n.start, n.end, nil})
		}
	})
	out, err := splice(data, edits)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestImagesPreservation(t *testing.T) {
	for _, lang := range []string{"en", "bn", "hi"} {
		for _, book := range []int{1, 2} {
			t.Run(fmt.Sprintf("%d-%s", book, lang), func(t *testing.T) {
				r := request(t, book, lang)
				r.Modules[fmt.Sprintf("B%d-01", book)] = Module{Entries: []Entry{{ID: "IMAGE-00042", Blocks: imageBlocks(t)}}}
				doc, err := Fill(context.Background(), r)
				if err != nil {
					t.Fatal(err)
				}
				src, _, _ := bookmasters.Read(book, lang, "v1")
				before, after := parts(t, src), parts(t, doc.DOCX)
				m, _ := Bindings(book, lang, "v1")
				if len(after) != len(before)+3 {
					t.Fatal("wrong added media count")
				}
				for name, a := range before {
					b := after[name]
					if name == "word/document.xml" {
						a = maskBindings(t, a, m)
						b = maskBindings(t, b, m)
					}
					if name == relsPart || name == typesPart {
						b = stripImageAdditions(t, b)
					}
					if !bytes.Equal(fontTags.ReplaceAll(a, []byte("FONT")), fontTags.ReplaceAll(b, []byte("FONT"))) {
						t.Errorf("fixed bytes changed: %s", name)
					}
				}
				for i, mime := range []string{"image/png", "image/jpeg", "image/webp"} {
					ext := "png"
					if mime == "image/jpeg" {
						ext = "jpeg"
					}
					name := fmt.Sprintf("word/media/bookdocx-image-%d.%s", i+1, ext)
					data, ok := after[name]
					if !ok {
						t.Fatal("missing media", name)
					}
					if mime != "image/webp" && !bytes.Equal(data, imageFixture(t, mime)) {
						t.Fatal("original media bytes changed", mime)
					}
					if mime == "image/webp" {
						got, err := png.Decode(bytes.NewReader(data))
						if err != nil {
							t.Fatal(err)
						}
						want, _ := png.Decode(bytes.NewReader(imageFixture(t, "image/png")))
						for y := range 40 {
							for x := range 60 {
								if got.At(x, y) != want.At(x, y) {
									gr, gg, gb, ga := got.At(x, y).RGBA()
									wr, wg, wb, wa := want.At(x, y).RGBA()
									if gr != wr || gg != wg || gb != wb || ga != wa {
										t.Fatal("WebP pixels changed")
									}
								}
							}
						}
					}
					if !bytes.Contains(after[relsPart], []byte(`Target="`+strings.TrimPrefix(name, "word/")+`"`)) || !bytes.Contains(after[typesPart], []byte(`PartName="/`+name+`"`)) {
						t.Fatal("unlinked media")
					}
				}
				root, err := parseXML(after["word/document.xml"])
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				seen := map[string]bool{}
				root.walk(func(n *node) {
					if n.name.Space == drawingNS && n.name.Local == "extent" {
						count++
						cx, _ := strconv.Atoi(n.attr("cx"))
						cy, _ := strconv.Atoi(n.attr("cy"))
						if cx <= 0 || cy <= 0 || cx > 2160*635 || cy > 1440*635 || cx*2 != cy*3 {
							t.Errorf("bad aspect ratio or bounds %dx%d", cx, cy)
						}
					}
					if n.name.Space == drawingNS && n.name.Local == "docPr" {
						if seen[n.attr("id")] || n.attr("descr") != `Synthetic <&> "pattern"` {
							t.Error("duplicate ID or lost alt text")
						}
						seen[n.attr("id")] = true
					}
				})
				if count != 3 {
					t.Fatal("wrong drawing count", count)
				}
				again, err := Fill(context.Background(), r)
				if err != nil || !bytes.Equal(doc.DOCX, again.DOCX) {
					t.Fatal("image package not deterministic", err)
				}
			})
		}
	}
}

func headerDimensions(t *testing.T, w, h uint32) []byte {
	b := bytes.Clone(imageFixture(t, "image/png"))
	binary.BigEndian.PutUint32(b[16:20], w)
	binary.BigEndian.PutUint32(b[20:24], h)
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
	return b
}

func TestImagesReject(t *testing.T) {
	good := imageFixture(t, "image/png")
	animated := []byte("RIFF\x16\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	cases := []struct {
		name string
		img  Image
		want string
	}{
		{"empty", Image{MIMEType: "image/png"}, "bytes"},
		{"encoded-limit", Image{MIMEType: "image/png", Data: make([]byte, MaxImageBytes+1)}, "bytes"},
		{"unsupported", Image{MIMEType: "image/svg+xml", Data: good}, "MIME"},
		{"mismatch", Image{MIMEType: "image/jpeg", Data: good}, "header"},
		{"truncated", Image{MIMEType: "image/png", Data: good[:len(good)-10]}, "pixels"},
		{"axis-limit", Image{MIMEType: "image/png", Data: headerDimensions(t, 8193, 1)}, "dimensions"},
		{"pixel-limit", Image{MIMEType: "image/png", Data: headerDimensions(t, 5000, 5000)}, "dimensions"},
		{"negative-width", Image{MIMEType: "image/png", Data: good, WidthTwips: -1}, "twips"},
		{"large-height", Image{MIMEType: "image/png", Data: good, HeightTwips: 7201}, "twips"},
		{"alt-xml", Image{MIMEType: "image/png", Data: good, Alt: "\x00"}, "XML"},
		{"webp-truncated", Image{MIMEType: "image/webp", Data: imageFixture(t, "image/webp")[:20]}, "container"},
		{"webp-animated", Image{MIMEType: "image/webp", Data: animated}, "animated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := request(t, 1, "en")
			r.Modules["B1-01"] = Module{Blocks: []Block{{Image: &c.img}}}
			_, err := Fill(context.Background(), r)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("expected %s: %v", c.want, err)
			}
		})
	}
	for _, s := range []imageSet{{inputBytes: MaxTotalImageBytes}, {outputBytes: MaxTotalImageBytes}, {pixels: MaxTotalImagePixels}, {items: make([]embeddedImage, MaxImages)}} {
		if err := s.add(&Image{Data: good, MIMEType: "image/png"}); err == nil {
			t.Fatal("aggregate image limit bypassed")
		}
	}
	b := budget{}
	if err := b.validateBlocks([]Block{{Image: &Image{Data: good, MIMEType: "image/png"}, Paragraph: &Paragraph{Runs: []Run{{Text: "x"}}}}}); err == nil {
		t.Fatal("multiple block kinds accepted")
	}
}

func TestImagesBounds(t *testing.T) {
	for _, dims := range [][2]int{{60, 40}, {40, 60}, {8192, 1}, {1, 8192}} {
		s := &imageSet{items: []embeddedImage{{width: dims[0], height: dims[1], maxWidth: 7200, maxHeight: 7200}}}
		root, err := parseXML([]byte(`<w:document xmlns:w="` + wordNS + `">` + s.drawing("", 2000) + `</w:document>`))
		if err != nil {
			t.Fatal(err)
		}
		root.walk(func(n *node) {
			if n.name.Space == drawingNS && n.name.Local == "extent" {
				cx, _ := strconv.Atoi(n.attr("cx"))
				cy, _ := strconv.Atoi(n.attr("cy"))
				if cx <= 0 || cy <= 0 || cx > (2000-216)*635 || cy > 7200*635 {
					t.Errorf("drawing outside cell: %dx%d", cx, cy)
				}
			}
		})
	}
}

func TestImagesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	img := &Image{Data: imageFixture(t, "image/png"), MIMEType: "image/png"}
	s := &imageSet{ctx: ctx}
	if err := s.add(img); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := png.Decode(imageReader{ctx, bytes.NewReader(img.Data)}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	w := &imageWriter{ctx: ctx}
	if _, err := w.Write(img.Data); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r := request(t, 1, "en")
	r.Modules["B1-01"] = Module{Blocks: []Block{{Image: img}}}
	if _, err := Fill(ctx, r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
