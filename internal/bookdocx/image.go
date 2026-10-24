package bookdocx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"
	"strings"

	"golang.org/x/image/webp"
)

const (
	relsPart  = "word/_rels/document.xml.rels"
	typesPart = "[Content_Types].xml"
	relsNS    = "http://schemas.openxmlformats.org/package/2006/relationships"
	typesNS   = "http://schemas.openxmlformats.org/package/2006/content-types"
	drawingNS = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
)

type embeddedImage struct {
	data                                   []byte
	ext, mime, alt, name, rel              string
	width, height, maxWidth, maxHeight, id int
}

type imageSet struct {
	ctx                                     context.Context
	items                                   []embeddedImage
	inputBytes, outputBytes, pixels, cursor int
	updated                                 map[string][]byte
}

type imageReader struct {
	ctx context.Context
	*bytes.Reader
}

func (r imageReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

type imageWriter struct {
	ctx context.Context
	bytes.Buffer
}

func (w *imageWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > MaxImageBytes-w.Len() {
		return 0, fmt.Errorf("normalized image exceeds %d bytes", MaxImageBytes)
	}
	return w.Buffer.Write(p)
}

func (s *imageSet) add(img *Image) error {
	if s.ctx == nil {
		s.ctx = context.Background()
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if len(s.items) >= MaxImages {
		return fmt.Errorf("too many images (maximum %d)", MaxImages)
	}
	if len(img.Data) == 0 || len(img.Data) > MaxImageBytes {
		return fmt.Errorf("image requires 1 to %d bytes", MaxImageBytes)
	}
	if len(img.Data) > MaxTotalImageBytes-s.inputBytes {
		return fmt.Errorf("image input exceeds aggregate byte limit")
	}
	if img.WidthTwips < 0 || img.WidthTwips > MaxImageTwips || img.HeightTwips < 0 || img.HeightTwips > MaxImageTwips {
		return fmt.Errorf("image bounds must be 0 to %d twips", MaxImageTwips)
	}
	var config func(io.Reader) (image.Config, error)
	var decode func(io.Reader) (image.Image, error)
	ext := ""
	switch img.MIMEType {
	case "image/png":
		config, decode, ext = png.DecodeConfig, png.Decode, "png"
	case "image/jpeg":
		config, decode, ext = jpeg.DecodeConfig, jpeg.Decode, "jpeg"
	case "image/webp":
		if err := staticWebP(img.Data); err != nil {
			return err
		}
		config, decode, ext = webp.DecodeConfig, webp.Decode, "png"
	default:
		return fmt.Errorf("unsupported image MIME type %q", img.MIMEType)
	}
	cfg, err := config(imageReader{s.ctx, bytes.NewReader(img.Data)})
	if err != nil {
		return fmt.Errorf("decode %s header: %w", img.MIMEType, err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImageDimension || cfg.Height > MaxImageDimension || int64(cfg.Width)*int64(cfg.Height) > MaxImagePixels {
		return fmt.Errorf("image dimensions %dx%d exceed axis or pixel limits", cfg.Width, cfg.Height)
	}
	pixels := cfg.Width * cfg.Height
	if pixels > MaxTotalImagePixels-s.pixels {
		return fmt.Errorf("images exceed aggregate pixel limit")
	}
	decoded, err := decode(imageReader{s.ctx, bytes.NewReader(img.Data)})
	if err != nil {
		return fmt.Errorf("decode %s pixels: %w", img.MIMEType, err)
	}
	if decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return fmt.Errorf("decoded image dimensions disagree with header")
	}
	data := img.Data
	mime := img.MIMEType
	if mime == "image/webp" {
		w := &imageWriter{ctx: s.ctx}
		if err := png.Encode(w, decoded); err != nil {
			return fmt.Errorf("normalize WebP: %w", err)
		}
		data, mime = w.Bytes(), "image/png"
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if len(data) > MaxTotalImageBytes-s.outputBytes {
		return fmt.Errorf("embedded images exceed aggregate byte limit")
	}
	s.inputBytes += len(img.Data)
	s.outputBytes += len(data)
	s.pixels += pixels
	s.items = append(s.items, embeddedImage{data: bytes.Clone(data), ext: ext, mime: mime, alt: img.Alt, width: cfg.Width, height: cfg.Height, maxWidth: img.WidthTwips, maxHeight: img.HeightTwips})
	return nil
}

func staticWebP(data []byte) error {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return fmt.Errorf("invalid WebP container")
	}
	for pos := 12; pos < len(data); {
		if len(data)-pos < 8 {
			return fmt.Errorf("truncated WebP chunk")
		}
		size := uint64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		end := uint64(pos) + 8 + size + (size & 1)
		if end > uint64(len(data)) {
			return fmt.Errorf("truncated WebP chunk")
		}
		kind := string(data[pos : pos+4])
		if kind == "ANIM" || kind == "ANMF" || kind == "VP8X" && size > 0 && data[pos+8]&2 != 0 {
			return fmt.Errorf("animated WebP is not supported")
		}
		pos = int(end)
	}
	return nil
}

func readImagePart(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxDOCXBytes+1))
	closeErr := r.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > MaxDOCXBytes {
		return nil, fmt.Errorf("oversized package part %s", f.Name)
	}
	return data, nil
}

func appendImageXML(data []byte, local, ns, addition string) ([]byte, error) {
	root, err := parseXML(data)
	if err != nil {
		return nil, err
	}
	if len(root.children) != 1 || root.children[0].name.Local != local || root.children[0].name.Space != ns {
		return nil, fmt.Errorf("invalid %s root", local)
	}
	n := root.children[0]
	if n.closeStart < n.openEnd {
		return nil, fmt.Errorf("self-closing %s root is unsupported", local)
	}
	return splice(data, []edit{{n.closeStart, n.closeStart, []byte(addition)}})
}

func (s *imageSet) prepare(z *zip.Reader) error {
	if len(s.items) == 0 {
		return nil
	}
	names, ids, drawings := map[string]bool{}, map[string]bool{}, map[int]bool{}
	var rels, types []byte
	for _, f := range z.File {
		names[f.Name] = true
		if f.Name != relsPart && f.Name != typesPart && !(strings.HasPrefix(f.Name, "word/") && strings.HasSuffix(f.Name, ".xml")) {
			continue
		}
		if err := s.ctx.Err(); err != nil {
			return err
		}
		data, err := readImagePart(f)
		if err != nil {
			return err
		}
		if f.Name == typesPart {
			types = data
			continue
		}
		root, err := parseXML(data)
		if err != nil {
			return err
		}
		if f.Name == relsPart {
			rels = data
			root.walk(func(n *node) {
				if n.name.Space == relsNS && n.name.Local == "Relationship" {
					ids[n.attr("Id")] = true
				}
			})
		}
		root.walk(func(n *node) {
			if n.name.Space == drawingNS && n.name.Local == "docPr" {
				id, _ := strconv.Atoi(n.attr("id"))
				drawings[id] = true
			}
		})
	}
	var relationships, overrides strings.Builder
	next := 1
	for i := range s.items {
		img := &s.items[i]
		for {
			img.name = fmt.Sprintf("word/media/bookdocx-image-%d.%s", next, img.ext)
			img.rel = fmt.Sprintf("rIdBookdocxImage%d", next)
			img.id = next
			next++
			if !names[img.name] && !ids[img.rel] && !drawings[img.id] {
				break
			}
		}
		fmt.Fprintf(&relationships, `<Relationship xmlns="%s" Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="%s"/>`, relsNS, img.rel, strings.TrimPrefix(img.name, "word/"))
		fmt.Fprintf(&overrides, `<Override xmlns="%s" PartName="/%s" ContentType="%s"/>`, typesNS, img.name, img.mime)
	}
	var err error
	s.updated = map[string][]byte{}
	s.updated[relsPart], err = appendImageXML(rels, "Relationships", relsNS, relationships.String())
	if err != nil {
		return err
	}
	s.updated[typesPart], err = appendImageXML(types, "Types", typesNS, overrides.String())
	return err
}

func (s *imageSet) drawing(pPr string, cellWidth int) string {
	img := s.items[s.cursor]
	s.cursor++
	width := max(1, cellWidth-216)
	if img.maxWidth > 0 {
		width = min(width, img.maxWidth)
	}
	height := img.maxHeight
	if height == 0 {
		height = 4320
	}
	// Integer EMUs retain small aspect ratios without rounding a dimension to zero.
	cx, cy := int64(width)*635, int64(height)*635
	if cx*int64(img.height) <= cy*int64(img.width) {
		cy = max(1, cx*int64(img.height)/int64(img.width))
	} else {
		cx = max(1, cy*int64(img.width)/int64(img.height))
	}
	return fmt.Sprintf(`<w:p>%s<w:r><w:drawing><wp:inline xmlns:wp="%s" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:effectExtent l="0" t="0" r="0" b="0"/><wp:docPr id="%d" name="Picture %d" descr="%s"/><wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="%d" name="Picture %d" descr="%s"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`, pPr, drawingNS, cx, cy, img.id, img.id, escaped(img.alt), img.id, img.id, escaped(img.alt), img.rel, cx, cy)
}
