package book

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"
	"sync"
)

// The drawn dish-format marks, and the reason Book 2 has drawings rather than photographs.
//
// No photograph of any recipe in this corpus exists and none is coming from a dataset. That is
// GAP-025, and it survived a real look at the alternative: data/external/indian_food_dataset.csv
// carries an image-url column across the 3,970 rows this project loaded, so the option had to be
// refused on the record rather than overlooked.
//
//  1. The image is of a different dish. The format map licenses an external row to supply method
//     text for the same format -- a khichdi method for a khichdi. A photograph is a stronger
//     claim: it says this is what your dish looks like. It is the wrong-match failure already
//     measured at four calibration thresholds, in the one medium where a reader cannot check it.
//  2. That corpus has an unstated upstream licence, already an open blocker, and its images are
//     third-party photographs from recipe sites. Printing them into a document handed to a family
//     is a clearer infringement than quoting the text.
//  3. Printing would have to fetch arbitrary remote URLs, the same question CLAUDE.md already
//     declines for bulk cover photographs.
//  4. The print browser blocks all network by design (--host-resolver-rules=MAP * ~NOTFOUND).
//     Remote images cannot load, and unblocking it to fetch pictures would undo a security fix.
//
// A mark depicts the dish *format*, which is a provider-recorded value read out of the recipe
// name, and the page captions it with that format so its claim is explicit and checkable against
// the title above it. Twenty-eight formats cover all 940 in-scope recipes and collapse to these
// eleven archetypes; the map is recipe_format_mark, migration 0019.
//
//go:embed marks/*.svg
var markFS embed.FS

// DishMark is the drawn illustration for one recipe's dish format.
type DishMark struct {
	ID          string `json:"id"`
	FormatLabel string `json:"format_label"`
	// SVG is the mark's markup, injected into the page unescaped.
	//
	// template.HTML is an escape hatch and it is sound here for one reason only: these are
	// checked-in files in this repository, validated by TestMarksCannotFetchOrScript, and
	// nothing from the database or from a request ever reaches this field. The same rule
	// ChildPhoto.DataURI lives under.
	SVG template.HTML `json:"-"`
}

var (
	marksOnce sync.Once
	marks     map[string]template.HTML
	marksErr  error
)

// loadMarks reads the embedded artwork once.
func loadMarks() (map[string]template.HTML, error) {
	marksOnce.Do(func() {
		entries, err := fs.ReadDir(markFS, "marks")
		if err != nil {
			marksErr = fmt.Errorf("book: read marks: %w", err)
			return
		}
		marks = make(map[string]template.HTML, len(entries))
		for _, e := range entries {
			b, err := markFS.ReadFile(path.Join("marks", e.Name()))
			if err != nil {
				marksErr = fmt.Errorf("book: read mark %s: %w", e.Name(), err)
				return
			}
			marks[strings.TrimSuffix(e.Name(), ".svg")] = template.HTML(b)
		}
	})
	return marks, marksErr
}

// Mark returns the artwork for a mark id, or nil when the id has none.
//
// nil rather than a placeholder: a recipe page with no mark prints without one, the same way a
// cover with no photograph prints without a silhouette. A missing mark is a seed that names
// artwork this repository does not carry, which TestEveryMarkIDHasArtworkAndViceVersa catches
// before a book does.
func Mark(id, formatLabel string) *DishMark {
	if id == "" {
		return nil
	}
	all, err := loadMarks()
	if err != nil {
		return nil
	}
	svg, ok := all[id]
	if !ok {
		return nil
	}
	return &DishMark{ID: id, FormatLabel: formatLabel, SVG: svg}
}

// MarkIDs is every mark this repository carries artwork for.
func MarkIDs() []string {
	all, err := loadMarks()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(all))
	for id := range all {
		out = append(out, id)
	}
	return out
}
