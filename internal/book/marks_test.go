package book

import (
	"io/fs"
	"strings"
	"testing"
)

func allMarks(t *testing.T) map[string]string {
	t.Helper()
	entries, err := fs.ReadDir(markFS, "marks")
	if err != nil {
		t.Fatalf("read marks: %v", err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		b, err := markFS.ReadFile("marks/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		out[e.Name()] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no marks are embedded; every guard below would pass vacuously")
	}
	return out
}

// A mark is inert markup.
//
// It is embedded into a page this service renders in a real browser, so it must have no way to
// fetch, script, or reference anything outside itself. The print tab already has script
// execution disabled and all network blocked, which makes this defence in depth rather than the
// only control -- but the marks are the first assets in this project authored as markup rather
// than as data, and an asset that would be dangerous if either of those controls were relaxed
// has no business being checked in.
func TestMarksCannotFetchOrScript(t *testing.T) {
	forbidden := []string{
		"<script", "<image", "<foreignobject", "<use", "xlink:href", "href=",
		"url(", "javascript:", "@import", "onload", "onerror", "<iframe", "<animate",
	}
	for name, svg := range allMarks(t) {
		lower := strings.ToLower(svg)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Errorf("%s contains %q", name, bad)
			}
		}
	}
}

// A mark is a drawing and must never be mistakable for a photograph.
//
// That distinction is load-bearing rather than aesthetic: no photograph of any recipe in this
// corpus exists, so a page that looked like it carried one would be making a claim the data
// cannot support. Stroked line art with no raster payload cannot.
func TestMarksAreLineArt(t *testing.T) {
	for name, svg := range allMarks(t) {
		if strings.Contains(svg, "base64") {
			t.Errorf("%s embeds raster data", name)
		}
		if !strings.Contains(svg, "stroke=") {
			t.Errorf("%s has no stroke: a mark is line art", name)
		}
		// currentColor, so a mark inherits the book's ink and cannot introduce a colour of its
		// own. avoid_color_only_meaning applies to a drawing as much as to a badge.
		if !strings.Contains(svg, "currentColor") {
			t.Errorf("%s does not use currentColor", name)
		}
		if !strings.Contains(svg, `viewBox="0 0 64 64"`) {
			t.Errorf("%s is not on the shared 64x64 grid; the set would not read as a set", name)
		}
		// <title> is the accessible name and is allowed. <text> is not: the page captions the
		// mark with the format it depicts, and a second label inside the artwork could disagree
		// with it.
		if strings.Contains(strings.ToLower(svg), "<text") {
			t.Errorf("%s draws its own text; the page carries the caption", name)
		}
		if !strings.Contains(svg, "<title>") {
			t.Errorf("%s has no <title>: the printed document needs an accessible name", name)
		}
	}
}

// Every mark id the seed names has artwork, and every piece of artwork is named by the seed.
//
// Both directions. A seeded id with no file prints a recipe page with a hole where the
// illustration goes; a file no seed names is dead weight nobody will notice going stale. The
// database half of this pairing is TestEveryRecipeResolvesOneFormatMark in internal/db.
func TestEveryMarkIDHasArtworkAndViceVersa(t *testing.T) {
	pool := testPool(t)
	rows, err := pool.Query(t.Context(), `SELECT DISTINCT mark_id FROM recipe_format_mark`)
	if err != nil {
		t.Fatalf("query seeded mark ids: %v", err)
	}
	defer rows.Close()

	seeded := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan mark id: %v", err)
		}
		seeded[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("mark id rows: %v", err)
	}
	if len(seeded) == 0 {
		t.Fatal("the seed names no marks; this guard would pass vacuously")
	}

	drawn := map[string]bool{}
	for _, id := range MarkIDs() {
		drawn[id] = true
	}
	for id := range seeded {
		if !drawn[id] {
			t.Errorf("recipe_format_mark names %q and this repository carries no artwork for it", id)
		}
	}
	for id := range drawn {
		if !seeded[id] {
			t.Errorf("marks/%s.svg is drawn and no seeded format uses it", id)
		}
	}
}

// Mark returns nothing rather than something wrong for an id it does not have.
func TestMarkIsAbsentRatherThanWrong(t *testing.T) {
	if got := Mark("", "Soft khichdi"); got != nil {
		t.Error("an empty id must yield no mark")
	}
	if got := Mark("no-such-mark", "Soft khichdi"); got != nil {
		t.Error("an unknown id must yield no mark, not a placeholder")
	}
	got := Mark("pot-khichdi", "Soft khichdi")
	if got == nil {
		t.Fatal("pot-khichdi should resolve")
	}
	if got.FormatLabel != "Soft khichdi" {
		t.Errorf("format label = %q, want the caption the page prints", got.FormatLabel)
	}
	if !strings.Contains(string(got.SVG), "<svg") {
		t.Error("the mark carries no markup")
	}
}
