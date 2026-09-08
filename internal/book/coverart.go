package book

import (
	"embed"
	"encoding/base64"
	"html/template"
)

// Eleven of the twelve supplied decorative illustrations, background-removed (see the plan's
// Task 2 step 1 -- border-connected flood fill, not a global threshold, so interior white
// details like eyes and apron straps survive). These are decoration only: they carry no
// dish-format claim, unlike the SVG marks in marks.go, and they never appear on a recipe page
// for exactly that reason -- see the design spec's section 2.3. They appear only on the pages
// named there: both covers, Book 1's back cover, and Book 2's chapter openers -- every one a
// guaranteed single, non-fragmenting print page, so their placement is a paint concern only
// and never interacts with pagefit_test.go's break-rule budgets.
//
// The twelfth, "banana-leaf-rice", was dropped entirely (2026-09-08): unlike every other
// asset here it is drawn as a tall, off-centre wedge rather than a roughly round, top-down
// dish, and at every size and rotation this project actually used it at -- the 50mm Book 2
// chapter-art slot, the full-opacity 62mm Book 1 cover corner -- it read as an unrecognisable
// sideways blob rather than a dish. The asset file (assets/cover/banana-leaf-rice.png) is
// deleted along with its map entry, not just unreferenced, so nothing can reintroduce it by
// copying an old call site.
//
//go:embed assets/cover/*.png
var coverArtFS embed.FS

var coverArtFiles = map[string]string{
	"mooncake":         "mooncake.png",
	"dimsum":           "dimsum.png",
	"noodle-roll":      "noodle-roll.png",
	"tteokbokki":       "tteokbokki.png",
	"bibimbap":         "bibimbap.png",
	"coconut":          "coconut.png",
	"herb-sauce":       "herb-sauce.png",
	"wrapped-dumpling": "wrapped-dumpling.png",
	"egg-noodle-bowl":  "egg-noodle-bowl.png",
	"hotpot":           "hotpot.png",
	"kids-cooking":     "kids-cooking.png",
}

// coverArt maps a short name to its data: URI, built once at package init from the embedded
// files above. Panics on a read failure, which can only mean the embed directive above and
// this map disagree -- a build-time defect, not a runtime one, so failing loudly at init is
// correct rather than returning an error every caller would have to check.
var coverArt = buildCoverArt()

func buildCoverArt() map[string]template.URL {
	out := make(map[string]template.URL, len(coverArtFiles))
	for key, filename := range coverArtFiles {
		data, err := coverArtFS.ReadFile("assets/cover/" + filename)
		if err != nil {
			panic("book: embedded cover art " + filename + " missing: " + err.Error())
		}
		out[key] = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(data))
	}
	return out
}
