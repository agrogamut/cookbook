package bookdocx

import (
	"encoding/binary"
	"fmt"
	"sync"
)

var coverageOnce sync.Once
var coverage map[rune]bool
var coverageError error

// The embedded fonts use Unicode cmap formats 4 and 12. A zero glyph is .notdef.
func loadCoverage() {
	coverage = make(map[rune]bool)
	records, err := fonts()
	if err != nil {
		coverageError = err
		return
	}
	for _, r := range records {
		data, err := fontAssets.ReadFile("fonts/" + r.File)
		if err != nil {
			coverageError = err
			return
		}
		if err := readCmap(data, coverage); err != nil {
			coverageError = fmt.Errorf("font %s: %w", r.File, err)
			return
		}
	}
}

func readCmap(data []byte, out map[rune]bool) error {
	if len(data) < 12 {
		return fmt.Errorf("short font header")
	}
	u16 := func(i int) int { return int(binary.BigEndian.Uint16(data[i : i+2])) }
	u32 := func(i int) int { return int(binary.BigEndian.Uint32(data[i : i+4])) }
	tables := u16(4)
	if 12+tables*16 > len(data) {
		return fmt.Errorf("short font directory")
	}
	for i := range tables {
		p := 12 + i*16
		if string(data[p:p+4]) != "cmap" {
			continue
		}
		start, size := u32(p+8), u32(p+12)
		if start < 0 || size < 4 || start > len(data)-size {
			return fmt.Errorf("invalid cmap offset")
		}
		end := start + size
		records := u16(start + 2)
		if start+4+records*8 > end {
			return fmt.Errorf("short cmap directory")
		}
		for j := range records {
			q := start + 4 + j*8
			platform, encoding := u16(q), u16(q+2)
			if platform != 0 && !(platform == 3 && (encoding == 1 || encoding == 10)) {
				continue
			}
			sub := start + u32(q+4)
			if sub < start || sub+2 > end {
				return fmt.Errorf("invalid cmap subtable")
			}
			switch u16(sub) {
			case 4:
				if sub+14 > end {
					return fmt.Errorf("short cmap format 4")
				}
				n := u16(sub+6) / 2
				if sub+16+8*n > end {
					return fmt.Errorf("short cmap segments")
				}
				for k := range n {
					lo, hi := u16(sub+16+2*n+2*k), u16(sub+14+2*k)
					delta := u16(sub + 16 + 4*n + 2*k)
					ro := sub + 16 + 6*n + 2*k
					offset := u16(ro)
					for cp := lo; cp <= hi; cp++ {
						glyph := (cp + delta) & 0xffff
						if offset != 0 {
							at := ro + offset + 2*(cp-lo)
							if at+2 > end {
								return fmt.Errorf("invalid glyph offset")
							}
							glyph = u16(at)
							if glyph != 0 {
								glyph = (glyph + delta) & 0xffff
							}
						}
						if glyph != 0 {
							out[rune(cp)] = true
						}
					}
				}
			case 12:
				if sub+16 > end {
					return fmt.Errorf("short cmap format 12")
				}
				groups := u32(sub + 12)
				if groups < 0 || groups > (end-sub-16)/12 {
					return fmt.Errorf("invalid cmap groups")
				}
				for k := range groups {
					at := sub + 16 + k*12
					lo, hi, glyph := u32(at), u32(at+4), u32(at+8)
					if hi > 0x10ffff || lo > hi {
						return fmt.Errorf("invalid Unicode range")
					}
					for cp := lo; cp <= hi; cp++ {
						if glyph+cp-lo != 0 {
							out[rune(cp)] = true
						}
					}
				}
			}
		}
		return nil
	}
	return fmt.Errorf("font has no character map")
}

func validateGlyphs(data []byte) error {
	coverageOnce.Do(loadCoverage)
	if coverageError != nil {
		return coverageError
	}
	root, err := parseXML(data)
	if err != nil {
		return err
	}
	var missing rune
	root.walk(func(n *node) {
		if !n.is("t") {
			return
		}
		for _, r := range n.text {
			if r == '\t' || r == '\n' || r == '\r' || r == 0x200c || r == 0x200d {
				continue
			}
			if !coverage[r] && missing == 0 {
				missing = r
			}
		}
	})
	if missing != 0 {
		return fmt.Errorf("approved fonts have no glyph for U+%04X", missing)
	}
	return nil
}
