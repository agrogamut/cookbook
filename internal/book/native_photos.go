package book

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/madamgy/recipie/internal/bookdocx"
)

func NativePhotos(n *NativeSet, set Set) error {
	for _, upload := range []struct {
		module string
		photo  *ChildPhoto
	}{{"B1-01", set.Book1.Child.Photo}, {"B1-63", set.Book1.Metadata.ParentsPhoto}} {
		if upload.photo == nil {
			continue
		}
		validated, err := ParsePhoto(string(upload.photo.DataURI), upload.photo.Caption)
		if err != nil {
			return fmt.Errorf("%s photograph: %w", upload.module, err)
		}
		if validated == nil {
			return fmt.Errorf("%s photograph is empty", upload.module)
		}
		header, payload, _ := strings.Cut(string(validated.DataURI), ",")
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return fmt.Errorf("%s photograph bytes: %w", upload.module, err)
		}
		m := n.Books[0].Modules[upload.module]
		m.Blocks = append(m.Blocks, bookdocx.Block{Image: &bookdocx.Image{Data: data, MIMEType: strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), Alt: validated.Caption}})
		if validated.Caption != "" {
			m.Blocks = append(m.Blocks, nativeText(validated.Caption))
		}
		n.Books[0].Modules[upload.module] = m
	}
	return nil
}
