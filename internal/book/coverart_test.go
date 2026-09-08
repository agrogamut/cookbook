package book

import (
	"bytes"
	"encoding/base64"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestCoverArtHasRealTransparencyForEveryKey(t *testing.T) {
	wantKeys := []string{
		"mooncake", "dimsum", "noodle-roll", "tteokbokki", "bibimbap", "coconut",
		"herb-sauce", "wrapped-dumpling", "egg-noodle-bowl",
		"hotpot", "kids-cooking",
	}
	for _, key := range wantKeys {
		uri, ok := coverArt[key]
		if !ok {
			t.Errorf("missing cover art key %q", key)
			continue
		}
		s := string(uri)
		if !strings.HasPrefix(s, "data:image/png;base64,") {
			t.Errorf("%s: expected a data:image/png;base64, URI, got prefix %q", key, s[:min(40, len(s))])
			continue
		}
		payload := s[len("data:image/png;base64,"):]
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			t.Errorf("%s: payload does not decode: %v", key, err)
			continue
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			t.Errorf("%s: not a decodable PNG: %v", key, err)
			continue
		}
		if cfg.ColorModel != color.NRGBAModel && cfg.ColorModel != color.NRGBA64Model {
			t.Errorf("%s: decoded color model is %T, expected an alpha-carrying model -- "+
				"background removal did not run on this asset", key, cfg.ColorModel)
		}
	}
}
