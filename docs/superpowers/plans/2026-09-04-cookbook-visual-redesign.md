# Cookbook visual redesign - implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the clinical/editorial visual language of both printed books with a warm,
illustrated cookbook aesthetic - a unified coral/cream palette, a self-hosted Poppins
typeface, real-photo front and back covers for Book 1, and decorative illustration on the
handful of pages that are guaranteed single, non-fragmenting print pages - while leaving the
Bengali/Devanagari type stack, the fixed-table-layout rule, and every page-fit-tuned break
rule exactly as they are.

**Architecture:** Two new embedded-asset packages (`internal/book/fonts.go`,
`internal/book/coverart.go`) mirror the existing `watermark.go` pattern - `go:embed` a
checked-in binary file, base64 it once into a package-level `template.URL`/`template.CSS`
var, thread it through `renderContext` into `base.html`. The palette and font-family change
is a `tokens.css`-only edit (no template changes needed for it to apply everywhere). The two
covers and the back cover are template-level changes that reuse the existing
`ChildPhoto`/`ParsePhoto` validation path for both real-photo slots. The dish-format mark's
return to the recipe page reverses one prior, explicit, commented decision and updates the
test that pinned it, by name.

**Tech Stack:** Go 1.26, `html/template`, chromedp + headless Chromium, `github.com/madamgy/recipie/internal/book`,
Python 3 + `numpy`/`scipy`/`Pillow` (one-time asset prep, not a runtime dependency).

**Spec:** `docs/superpowers/specs/2026-09-04-cookbook-visual-redesign-design.md`

## Global Constraints

Every task's requirements implicitly include this section.

- **No print-time network access.** The print browser runs with
  `--host-resolver-rules=MAP * ~NOTFOUND` (`internal/book/pdf.go`). Every font and every
  image this plan adds must be embedded in the binary via `go:embed` and reach the page as a
  `data:` URI. Never add a `<link>` to a CDN in `base.html` or `tokens.css`.
- **Bengali/Devanagari text is untouched.** `--font-indic`/`--font-indic-sans` in
  `tokens.css` keep their existing values. No task in this plan may change the font-family
  used for ingredient names, table content in Bengali, or any other Indic-script text.
- **`table-layout: fixed` is load-bearing and must never be removed** (`tokens.css`'s own
  comment on `table {}` explains the 79%-scale-factor incident this prevents).
- **No new content is invented.** The back cover prints the same real facts `end.html`
  already prints (book version, release ID, generation date). No new page, no new provider
  data field, no invented quote or category index.
- **Corner-bleed illustration only on guaranteed single-page containers**: `.cover`
  (both books), `end.html`'s back-cover section, `B2-SECTION-01`. Never on a flowing,
  multi-`Section` Book 1 content page.
- **`Review_Status`/`Data_Quality` print verbatim, unaffected by this plan.**

---

## File structure

New files:

- `internal/book/assets/fonts/poppins-{400,600,700,800,600italic}.woff2` - self-hosted font
  files.
- `internal/book/fonts.go` - embeds the five files above, exposes `fontFacesCSS
  template.CSS`.
- `internal/book/assets/cover/*.png` - the twelve supplied illustrations, background
  removed, renamed and relocated from the repo root.
- `internal/book/coverart.go` - embeds the twelve files above, exposes a
  `coverArt map[string]template.URL` keyed by a short name (`"mooncake"`, `"dimsum"`, ...).

Modified files:

- `internal/book/templates/tokens.css` - palette tokens, font-family tokens, new
  `.cover-bg`/`.backcover`/`.ticket` rules.
- `internal/book/templates/base.html` - one new `<style>` block for the embedded
  `@font-face` rules.
- `internal/book/render.go` - `renderContext` gains a `FontFaces template.CSS` field.
- `internal/book/types.go` - `Metadata` gains `ParentsPhoto *ChildPhoto`.
- `internal/book/templates/book1/cover.html` - full-bleed child photo, ribbon-styled kicker.
- `internal/book/templates/book2/cover.html` - full-bleed decorative illustration
  background, kids-cooking hero image.
- `internal/book/templates/book1/end.html` - `with-photo`/`no-photo` back-cover states.
- `internal/book/templates/book2/section.html` - chapter-opener corner illustration.
- `internal/book/templates/book2/recipe.html` - restore the dish-format mark.
- `internal/book/render_test.go` - update
  `TestARecipePagePrintsNoPictureEvenWhenMarkAndPhotoAreSet`.
- `internal/api/handlers/book_generate.go` - `ParentsPhotoDataURI`/`ParentsPhotoCaption` on
  `generateRequest`, second `ParsePhoto` call in `decodeGenerate`.
- `internal/api/handlers/book_set.go` - `renderSetWithPhoto` becomes
  `renderSetWithPhotos`, sets `Metadata.ParentsPhoto`.

---

### Task 1: Embed Poppins as a self-hosted font

**Files:**
- Create: `internal/book/assets/fonts/poppins-400.woff2`, `poppins-600.woff2`,
  `poppins-700.woff2`, `poppins-800.woff2`, `poppins-600italic.woff2`
- Create: `internal/book/fonts.go`
- Test: `internal/book/fonts_test.go`

**Interfaces:**
- Produces: `fontFacesCSS template.CSS` (package-level var), consumed by Task 4's
  `render.go` change.

- [ ] **Step 1: Download the five Poppins weights as real `.woff2` files**

Google's own CSS2 API returns the exact `fonts.gstatic.com` URLs for pinned weights; fetch
those URLs directly rather than guessing a path, since Google rotates the hash in the
filename:

```bash
mkdir -p internal/book/assets/fonts
for spec in "400:" "600:" "700:" "800:" "600:1"; do
  weight="${spec%%:*}"; italic="${spec##*:}"
  if [ "$italic" = "1" ]; then
    css_url="https://fonts.googleapis.com/css2?family=Poppins:ital,wght@1,${weight}&display=swap"
    out="internal/book/assets/fonts/poppins-${weight}italic.woff2"
  else
    css_url="https://fonts.googleapis.com/css2?family=Poppins:wght@${weight}&display=swap"
    out="internal/book/assets/fonts/poppins-${weight}.woff2"
  fi
  font_url=$(curl -s -A "Mozilla/5.0" "$css_url" | grep -o "https://fonts.gstatic.com/[^)]*woff2" | head -1)
  curl -s -o "$out" "$font_url"
  echo "$out <- $font_url"
done
ls -la internal/book/assets/fonts/
```

Confirm all five files are non-empty and each is a real `.woff2` (magic bytes `wOF2`):

```bash
for f in internal/book/assets/fonts/*.woff2; do
  head -c4 "$f" | xxd | grep -q "774f 4632" && echo "$f: ok" || echo "$f: BAD"
done
```

- [ ] **Step 2: Write the failing test**

```go
// internal/book/fonts_test.go
package book

import (
	"strings"
	"testing"
)

func TestFontFacesCSSEmbedsAllFiveWeightsWithNoNetworkReference(t *testing.T) {
	css := string(fontFacesCSS)
	for _, weight := range []string{"400", "600", "700", "800"} {
		if !strings.Contains(css, "font-weight: "+weight) {
			t.Errorf("expected an @font-face rule for weight %s, got:\n%s", weight, css)
		}
	}
	if !strings.Contains(css, "font-style: italic") {
		t.Error("expected an italic @font-face rule")
	}
	if strings.Contains(css, "fonts.googleapis.com") || strings.Contains(css, "fonts.gstatic.com") {
		t.Fatal("font-face rules must embed data: URIs, never reference a network host -- " +
			"the print browser runs with --host-resolver-rules=MAP * ~NOTFOUND")
	}
	if !strings.Contains(css, "data:font/woff2;base64,") {
		t.Fatal("expected at least one embedded data: URI")
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./internal/book/... -run TestFontFacesCSSEmbedsAllFiveWeights -v`
Expected: FAIL, `undefined: fontFacesCSS` (the file doesn't exist yet).

- [ ] **Step 4: Write `internal/book/fonts.go`**

```go
package book

import (
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
)

// Poppins, self-hosted. Requested typeface was Gotham, a paid commercial face that cannot be
// embedded in a repository shipped to a print engine with no network access at render time
// (see pdf.go's --host-resolver-rules). Poppins is the common open substitute: a geometric
// grotesque in the same register, OFL-licensed, free to redistribute.
//
// Embedded the same way watermarkJPEG is in watermark.go: go:embed the checked-in binary,
// base64 it once at package init into a data: URI, so the print browser never fetches
// anything. This is Latin display/body type only -- see tokens.css's --font-indic/
// --font-indic-sans, which are untouched by this file and by every task in this plan.
//
//go:embed assets/fonts/poppins-400.woff2
var poppins400 []byte

//go:embed assets/fonts/poppins-600.woff2
var poppins600 []byte

//go:embed assets/fonts/poppins-700.woff2
var poppins700 []byte

//go:embed assets/fonts/poppins-800.woff2
var poppins800 []byte

//go:embed assets/fonts/poppins-600italic.woff2
var poppins600Italic []byte

func fontFaceRule(weight int, italic bool, data []byte) string {
	style := "normal"
	if italic {
		style = "italic"
	}
	return fmt.Sprintf(`@font-face {
  font-family: "Poppins";
  font-style: %s;
  font-weight: %d;
  font-display: swap;
  src: url(data:font/woff2;base64,%s) format("woff2");
}`, style, weight, base64.StdEncoding.EncodeToString(data))
}

// fontFacesCSS is every @font-face rule this project ships, concatenated once at package
// init. template.CSS marks it trusted for the same reason RenderHTML trusts tokens.css: it
// is built entirely from embedded repository files, never from the database or a request.
var fontFacesCSS = template.CSS(
	fontFaceRule(400, false, poppins400) + "\n" +
		fontFaceRule(600, false, poppins600) + "\n" +
		fontFaceRule(700, false, poppins700) + "\n" +
		fontFaceRule(800, false, poppins800) + "\n" +
		fontFaceRule(600, true, poppins600Italic),
)
```

- [ ] **Step 5: Run the test again to verify it passes**

Run: `go test ./internal/book/... -run TestFontFacesCSSEmbedsAllFiveWeights -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/book/assets/fonts/ internal/book/fonts.go internal/book/fonts_test.go
git commit -m "book: embed Poppins as a self-hosted font, no print-time network needed"
```

---

### Task 2: Process and embed the twelve decorative illustrations

**Files:**
- Create: `internal/book/assets/cover/*.png` (twelve files, see mapping below)
- Create: `internal/book/coverart.go`
- Test: `internal/book/coverart_test.go`

**Interfaces:**
- Produces: `coverArt map[string]template.URL`, keyed by the names in the table below.
  Consumed by Task 7 (`book2/cover.html`) and Task 10 (`book2/section.html`).

The twelve source PNGs are currently at the repo root and are all `RGB` with a flat white
background baked in (confirmed: `PIL.Image.open(...).mode == "RGB"` for every one, no alpha
channel). Used as-is, each renders as a visible white rectangle wherever it overlaps the
cream page background. They need one-time background removal before they're embeddable.

| Source (repo root) | Target name | Asset key |
|---|---|---|
| `1.png` | `mooncake.png` | `mooncake` |
| `2.png` | `dimsum.png` | `dimsum` |
| `3.png` | `noodle-roll.png` | `noodle-roll` |
| `22244.png` | `tteokbokki.png` | `tteokbokki` |
| `2323.png` | `bibimbap.png` | `bibimbap` |
| `fffff.png` | `coconut.png` | `coconut` |
| `fsdsd.png` | `banana-leaf-rice.png` | `banana-leaf-rice` |
| `fffffffffffffffff.png` | `herb-sauce.png` | `herb-sauce` |
| `Yellow Illustrated Healthyffff Recipes Book Cover.png` | `wrapped-dumpling.png` | `wrapped-dumpling` |
| `Yellow Illustrated Healthy Recipes Book Cover.png` | `egg-noodle-bowl.png` | `egg-noodle-bowl` |
| `Green Black Organic Illustrative Recipe eBook.png` | `hotpot.png` | `hotpot` |
| `2kidscooking.png` | `kids-cooking.png` | `kids-cooking` |

- [ ] **Step 1: Remove the flat white background from all twelve, via border-connected flood fill**

A global "make every near-white pixel transparent" threshold is wrong here -- it punches
holes in interior white details (eyes, teeth, apron straps). Flood-fill from the four
border edges instead, so only the background region connected to the page edge becomes
transparent:

```bash
python3 -c "
import numpy as np
from scipy.ndimage import label
from PIL import Image
import os

pairs = [
    ('1.png', 'mooncake.png'),
    ('2.png', 'dimsum.png'),
    ('3.png', 'noodle-roll.png'),
    ('22244.png', 'tteokbokki.png'),
    ('2323.png', 'bibimbap.png'),
    ('fffff.png', 'coconut.png'),
    ('fsdsd.png', 'banana-leaf-rice.png'),
    ('fffffffffffffffff.png', 'herb-sauce.png'),
    ('Yellow Illustrated Healthyffff Recipes Book Cover.png', 'wrapped-dumpling.png'),
    ('Yellow Illustrated Healthy Recipes Book Cover.png', 'egg-noodle-bowl.png'),
    ('Green Black Organic Illustrative Recipe eBook.png', 'hotpot.png'),
    ('2kidscooking.png', 'kids-cooking.png'),
]

out_dir = 'internal/book/assets/cover'
os.makedirs(out_dir, exist_ok=True)
structure = np.array([[0,1,0],[1,1,1],[0,1,0]])

for src, dst in pairs:
    im = Image.open(src).convert('RGB')
    arr = np.array(im)
    h, w, _ = arr.shape
    white_mask = np.all(arr > 245, axis=2)
    labeled, _ = label(white_mask, structure=structure)
    border_labels = set(labeled[0,:].tolist()) | set(labeled[-1,:].tolist()) | \
                    set(labeled[:,0].tolist()) | set(labeled[:,-1].tolist())
    border_labels.discard(0)
    bg_mask = np.isin(labeled, list(border_labels))
    rgba = np.dstack([arr, np.full((h,w), 255, dtype=np.uint8)])
    rgba[bg_mask, 3] = 0
    Image.fromarray(rgba, 'RGBA').save(os.path.join(out_dir, dst))
    pct = round(100*bg_mask.sum()/(h*w), 1)
    print(f'{dst}: {pct}% background removed')
"
```

Every line printed must show a plausible removal percentage (roughly 15-70% based on how
much of each source frame is background) -- a `0.0%` means that file's background was not a
uniform white the flood fill could find (inspect it manually before proceeding), and a
`100.0%` means the whole image vanished (the white threshold ate the subject).

- [ ] **Step 2: Confirm every output file is a real RGBA PNG with a genuine alpha channel**

```bash
python3 -c "
from PIL import Image
import os
for f in sorted(os.listdir('internal/book/assets/cover')):
    im = Image.open(os.path.join('internal/book/assets/cover', f))
    assert im.mode == 'RGBA', f'{f}: mode is {im.mode}, expected RGBA'
    lo, hi = im.getchannel('A').getextrema()
    assert lo == 0, f'{f}: alpha never reaches 0, background removal did nothing'
    assert hi == 255, f'{f}: alpha never reaches 255, subject was made transparent too'
    print(f'{f}: ok')
"
```

- [ ] **Step 3: Write the failing test**

```go
// internal/book/coverart_test.go
package book

import (
	"bytes"
	"image"
	_ "image/png"
	"strings"
	"testing"
)

func TestCoverArtHasRealTransparencyForEveryKey(t *testing.T) {
	wantKeys := []string{
		"mooncake", "dimsum", "noodle-roll", "tteokbokki", "bibimbap", "coconut",
		"banana-leaf-rice", "herb-sauce", "wrapped-dumpling", "egg-noodle-bowl",
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
		if cfg.ColorModel != image.NRGBAModel && cfg.ColorModel != image.NRGBA64Model {
			t.Errorf("%s: decoded color model is %T, expected an alpha-carrying model -- "+
				"background removal did not run on this asset", key, cfg.ColorModel)
		}
	}
}
```

(Add `"encoding/base64"` and `"image/png"` to the imports in the same block as the others.)

- [ ] **Step 4: Run it to verify it fails**

Run: `go test ./internal/book/... -run TestCoverArtHasRealTransparency -v`
Expected: FAIL, `undefined: coverArt`.

- [ ] **Step 5: Write `internal/book/coverart.go`**

```go
package book

import (
	"embed"
	"encoding/base64"
	"html/template"
)

// The twelve supplied decorative illustrations, background-removed (see the plan's Task 2
// step 1 -- border-connected flood fill, not a global threshold, so interior white details
// like eyes and apron straps survive). These are decoration only: they carry no dish-format
// claim, unlike the SVG marks in marks.go, and they never appear on a recipe page for exactly
// that reason -- see the design spec's section 2.3. They appear only on the pages named
// there: both covers, Book 1's back cover, and Book 2's chapter openers -- every one a
// guaranteed single, non-fragmenting print page, so their placement is a paint concern only
// and never interacts with pagefit_test.go's break-rule budgets.
//
//go:embed assets/cover/*.png
var coverArtFS embed.FS

var coverArtFiles = map[string]string{
	"mooncake":          "mooncake.png",
	"dimsum":            "dimsum.png",
	"noodle-roll":       "noodle-roll.png",
	"tteokbokki":        "tteokbokki.png",
	"bibimbap":          "bibimbap.png",
	"coconut":           "coconut.png",
	"banana-leaf-rice":  "banana-leaf-rice.png",
	"herb-sauce":        "herb-sauce.png",
	"wrapped-dumpling":  "wrapped-dumpling.png",
	"egg-noodle-bowl":   "egg-noodle-bowl.png",
	"hotpot":            "hotpot.png",
	"kids-cooking":      "kids-cooking.png",
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
```

- [ ] **Step 6: Run the test again to verify it passes**

Run: `go test ./internal/book/... -run TestCoverArtHasRealTransparency -v`
Expected: PASS

- [ ] **Step 7: Remove the twelve source files from the repo root and commit**

```bash
git add internal/book/assets/cover/ internal/book/coverart.go internal/book/coverart_test.go
git rm "1.png" "2.png" "3.png" "22244.png" "2323.png" "fffff.png" "fsdsd.png" \
  "fffffffffffffffff.png" "2kidscooking.png" \
  "Yellow Illustrated Healthyffff Recipes Book Cover.png" \
  "Yellow Illustrated Healthy Recipes Book Cover.png" \
  "Green Black Organic Illustrative Recipe eBook.png"
git commit -m "book: embed decorative cover illustrations, background-removed"
```

---

### Task 3: Wire the embedded fonts into the render pipeline

**Files:**
- Modify: `internal/book/render.go:46-52` (`renderContext`), `:77-83` (its construction)
- Modify: `internal/book/templates/base.html:6`
- Test: `internal/book/render_test.go` (new test)

**Interfaces:**
- Consumes: `fontFacesCSS template.CSS` from Task 1.
- Produces: `renderContext.FontFaces`, consumed by `base.html`.

- [ ] **Step 1: Write the failing test**

```go
// add to internal/book/render_test.go
func TestRenderedDocumentEmbedsThePoppinsFontFaces(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, Metadata{Language: "en"}, nil); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "@font-face") {
		t.Fatal("rendered document must embed @font-face rules for Poppins")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/book/... -run TestRenderedDocumentEmbedsThePoppinsFontFaces -v`
Expected: FAIL (base.html has nowhere to print `@font-face` yet).

- [ ] **Step 3: Add the field to `renderContext` and its construction**

In `internal/book/render.go`, change:

```go
type renderContext struct {
	Metadata  Metadata
	BookClass string
	CSS       template.CSS
	Watermark template.URL
	Data      any
}
```

to:

```go
type renderContext struct {
	Metadata   Metadata
	BookClass  string
	CSS        template.CSS
	FontFaces  template.CSS
	Watermark  template.URL
	Data       any
}
```

and in the `ctx := renderContext{...}` literal a few lines below, add `FontFaces:
fontFacesCSS,` alongside the existing `CSS: template.CSS(css),`.

- [ ] **Step 4: Print it in `base.html`**

In `internal/book/templates/base.html`, change:

```html
<style>{{ .CSS }}</style>
```

to:

```html
<style>{{ .FontFaces }}
{{ .CSS }}</style>
```

- [ ] **Step 5: Run the test again to verify it passes**

Run: `go test ./internal/book/... -run TestRenderedDocumentEmbedsThePoppinsFontFaces -v`
Expected: PASS

- [ ] **Step 6: Run the full book test suite to confirm nothing else broke**

Run: `go build ./... && go vet ./... && go test ./internal/book/...`
Expected: all PASS (this step only adds bytes to `<style>`, it changes no selector).

- [ ] **Step 7: Commit**

```bash
git add internal/book/render.go internal/book/templates/base.html internal/book/render_test.go
git commit -m "book: thread the embedded Poppins font-face rules into every rendered document"
```

---

### Task 4: Palette and typeface swap in `tokens.css`

**Files:**
- Modify: `internal/book/templates/tokens.css` (the `:root`, `.book1`, `.book2` blocks)
- Test: `internal/book/tokens_test.go` (new file)

**Interfaces:**
- Consumes: nothing new (this is a pure CSS token edit).
- Produces: the new `--brand`/`--surface`/`--brand-deep`/`--font-serif`/`--font-sans`
  values every later task's markup styles against.

- [ ] **Step 1: Write the failing test**

This is a string-level regression test on the embedded stylesheet -- cheap, and it is the
only thing standing between "someone reverts the palette by accident in a later edit" and a
silent regression, since no existing test asserts specific hex values (only that a `book1`/
`book2` class is present).

```go
// internal/book/tokens_test.go
package book

import (
	"strings"
	"testing"
)

func TestTokensUseTheUnifiedCoralCreamPalette(t *testing.T) {
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	s := string(css)

	for _, want := range []string{"#ff5858", "#fffcf6"} {
		if !strings.Count(s, want) < 1 {
			continue
		}
		if !strings.Contains(s, want) {
			t.Errorf("expected the unified palette colour %s in tokens.css", want)
		}
	}
	for _, gone := range []string{"#0f6466", "#123a5f", "#8d4a70", "#5c2650"} {
		if strings.Contains(s, gone) {
			t.Errorf("old per-book brand colour %s should have been replaced by the unified palette", gone)
		}
	}
	// The clinical warning palette is untouched -- it must stay visually distinct from the
	// new decorative brand colour, per avoid_color_only_meaning.
	if !strings.Contains(s, "#9b2226") {
		t.Error("--warning-strong must be unchanged")
	}
}

func TestTokensUsePoppinsForLatinTextAndNotoForIndicText(t *testing.T) {
	css, err := templateFS.ReadFile("templates/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	s := string(css)

	if !strings.Contains(s, `"Poppins"`) {
		t.Error("--font-serif/--font-sans should reference Poppins")
	}
	// Untouched, verbatim, both stacks.
	for _, want := range []string{"Noto Serif Bengali", "Noto Serif Devanagari", "Noto Sans Bengali", "Noto Sans Devanagari"} {
		if !strings.Contains(s, want) {
			t.Errorf("--font-indic/--font-indic-sans must still reference %s", want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/book/... -run TestTokensUse -v`
Expected: FAIL (old hex values and old font stack are still present).

- [ ] **Step 3: Edit `tokens.css`'s `:root` block**

Change the type-scale font declarations:

```css
--font-serif: "Noto Serif", "Liberation Serif", Georgia, serif;
--font-sans: "Noto Sans", "Liberation Sans", "Segoe UI", system-ui, sans-serif;
```

to:

```css
--font-serif: "Poppins", "Noto Serif", "Liberation Serif", Georgia, serif;
--font-sans: "Poppins", "Noto Sans", "Liberation Sans", "Segoe UI", system-ui, sans-serif;
```

Leave `--font-indic` and `--font-indic-sans` exactly as they are -- do not touch these two
lines.

- [ ] **Step 4: Replace the two brand palettes with the unified one**

Change:

```css
/* book1_palette: deep navy, teal, warm cream, muted gold */
.book1 {
  --brand: #0f6466;
  --brand-deep: #123a5f;
  --surface: #f6f1e4;
  --accent: #a8791d;
  --tint: #eef4f4;
}

/* book2_palette: deep plum, warm rose, cream, muted gold */
.book2 {
  --brand: #8d4a70;
  --brand-deep: #5c2650;
  --surface: #f8f1ea;
  --accent: #a8791d;
  --tint: #f8eef3;
}
```

to:

```css
/* cookbook_palette: one scheme across both books, supplied directly rather than derived --
   see the design spec section 2.1. --brand-deep is a darkened coral for AA-contrast heading
   text on the cream ground; check it against a printed proof (Task 4 step 6) before treating
   it as final. */
.book1, .book2 {
  --brand: #ff5858;
  --brand-deep: #c23c34;
  --surface: #fffcf6;
  --accent: #f2a53d;
  --tint: #fdeee9;
}
```

- [ ] **Step 5: Run the tests again to verify they pass**

Run: `go test ./internal/book/... -run TestTokensUse -v`
Expected: PASS

- [ ] **Step 6: Check the contrast of the derived heading colour against the cream ground**

```bash
python3 -c "
def luminance(hex_color):
    hex_color = hex_color.lstrip('#')
    r, g, b = (int(hex_color[i:i+2], 16) / 255 for i in (0, 2, 4))
    def lin(c):
        return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4
    r, g, b = lin(r), lin(g), lin(b)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b

def contrast(c1, c2):
    l1, l2 = luminance(c1) + 0.05, luminance(c2) + 0.05
    return max(l1, l2) / min(l1, l2)

ratio = contrast('#c23c34', '#fffcf6')
print(f'contrast ratio: {ratio:.2f} (WCAG AA for normal text needs >= 4.5)')
assert ratio >= 4.5, 'brand-deep does not meet AA contrast on the cream surface'
"
```

If this fails, darken `--brand-deep` (e.g. step through `#b23730`, `#a3322c`) and re-run
this check until it passes, then re-run Step 5's tests (they don't assert the exact shade,
only that the old ones are gone, so a darker value still passes).

- [ ] **Step 7: Run the full suite and the print-fit guard**

Run:
```bash
go build ./...
go vet ./...
scripts/dev_db.fish up
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
BOOK_PAGE_DUMP=/tmp/bookdump go test ./internal/book/... -run TestPrintPDFProducesAPDF -v
```
Expected: all PASS. Open the PDFs in `/tmp/bookdump` and confirm by eye: no page reads as
low-contrast, headings are legible, Bengali ingredient names still render in the Noto face
(unaffected by this task, but worth a glance since this is the one task that touches every
page in both books at once via shared tokens).

- [ ] **Step 8: Commit**

```bash
git add internal/book/templates/tokens.css internal/book/tokens_test.go
git commit -m "book: unify the palette to coral/cream and swap Latin type to Poppins"
```

---

### Task 5: Restyle the recipe metadata strip as a ticket, not floating badges

**Files:**
- Modify: `internal/book/templates/tokens.css` (`.metastrip`/`.metacell` rules)
- Test: manual visual check via `BOOK_PAGE_DUMP` (no new automated test -- this is a pure
  visual restyle of existing, already-tested markup; `book2/recipe.html` is untouched by
  this task)

**Interfaces:** none (CSS-only, no new class names introduced into any template in this
task -- reuses the existing `.metastrip`/`.metacell`/`.label`/`.value` selectors already in
`book2/recipe.html`).

- [ ] **Step 1: Read the current rule**

`tokens.css` currently sets (search for `.metastrip {`):

```css
.metastrip {
  display: flex;
  flex-wrap: wrap;
  gap: 6mm;
  border-top: 0.5mm solid var(--rule-strong);
  border-bottom: 0.2mm solid var(--rule);
  padding: 2.5mm 0;
  margin: 3mm 0 4mm;
}
.metacell { min-width: 22mm; }
```

- [ ] **Step 2: Add dashed dividers between cells, ticket-stub style**

Replace the `.metastrip` rule above with:

```css
.metastrip {
  display: flex;
  flex-wrap: wrap;
  border: 0.4mm solid var(--rule-strong);
  border-radius: 3mm;
  padding: 2.5mm 0;
  margin: 3mm 0 4mm;
}
.metacell {
  min-width: 22mm;
  flex: 1;
  padding: 0 4mm;
  position: relative;
  text-align: center;
}
.metacell:not(:last-child)::after {
  content: "";
  position: absolute;
  right: 0;
  top: 15%;
  bottom: 15%;
  border-right: 0.3mm dashed var(--ink-faint);
}
```

This changes nothing about `.label`/`.value` typography (already Poppins after Task 4) and
introduces no new HTML -- `book2/recipe.html`'s existing `{{ range .Meta }}` loop over
`.metacell` divs needs no change.

- [ ] **Step 3: Print and look**

Run: `BOOK_PAGE_DUMP=/tmp/bookdump go test ./internal/book/... -run TestPrintPDFProducesAPDF -v`

Open a recipe page in `/tmp/bookdump` and confirm the strip reads as one bordered bar with
dashed dividers between prep/cook/serves/texture/cost, centred text in each cell, no
overflow past 170mm (the fixed-width text block).

- [ ] **Step 4: Run `go test ./internal/book/...` to confirm no existing assertion broke**

Run: `go test ./internal/book/...`
Expected: PASS (no test currently asserts `.metastrip`'s exact CSS, only that `.metacell`
divs are present with `.label`/`.value` children, which is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/book/templates/tokens.css
git commit -m "book: restyle the recipe metadata strip as a bordered ticket, not floating badges"
```

---

### Task 6: Book 1 front cover - full-bleed child photo

**Files:**
- Modify: `internal/book/templates/book1/cover.html`
- Modify: `internal/book/templates/tokens.css` (`.cover`, `.cover-photo`, `.cover-main` rules)
- Test: `internal/book/render_test.go` (new test)

**Interfaces:**
- Consumes: `.Child.Photo *ChildPhoto` (already exists, `internal/book/photo.go`).

**Do not use flex to centre the identity card.** See the design spec section 2.5: this
exact construction was tried twice for this exact cover, measured, and rejected, because
Chromium re-resolves a flex distribution differently depending on how close the container's
content lands to the page's fragmentainer boundary. The existing `.cover.no-photo
.cover-main { margin-top: 96mm }` / `.cover.with-photo .cover-main { margin-top: 26mm }`
two-constant pattern is what actually works here, and this task keeps it.

- [ ] **Step 1: Write the failing test**

```go
// add to internal/book/render_test.go
func TestBook1CoverPrintsTheChildPhotoAsAFullBleedBackground(t *testing.T) {
	photo := &ChildPhoto{DataURI: template.URL("data:image/png;base64,iVBORw0KGgo=")}
	b := Book1{
		Metadata: Metadata{Language: "en"},
		Child:    ChildSummary{DisplayName: "Test Child", Photo: photo},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="cover-bg"`) {
		t.Fatal("with a photo present, the cover must carry a full-bleed background layer")
	}
	if !strings.Contains(out, string(photo.DataURI)) {
		t.Fatal("the child's own photo data URI must appear in the rendered cover")
	}
}

func TestBook1CoverWithNoPhotoPrintsNoBackgroundLayer(t *testing.T) {
	b := Book1{
		Metadata: Metadata{Language: "en"},
		Child:    ChildSummary{DisplayName: "Test Child"},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), `class="cover-bg"`) {
		t.Fatal("with no photo, the cover must not print an empty background layer")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/book/... -run TestBook1Cover -v`
Expected: FAIL (`cover-bg` does not exist yet).

- [ ] **Step 3: Rewrite `book1/cover.html`**

Replace the file's contents with:

```html
{{ define "B1-COVER-01" }}
{{/* Full-bleed child photo behind a centred identity card -- see the design spec section
     2.4/2.5. The photo is a plain absolutely-positioned layer inside .cover's own
     position: relative box, which participates in no flex/grid distribution and is
     therefore safe against the exact fragmentation defect documented on .cover in
     tokens.css: it is painted, not distributed.

     The with-photo/no-photo two-constant pattern for .cover-main's drop is unchanged from
     before this redesign -- only what sits behind the card is new. */}}
<div class="cover {{ if .Child.Photo }}with-photo{{ else }}no-photo{{ end }}">
  {{ with .Child.Photo }}
  <div class="cover-bg"><img src="{{ .DataURI }}" alt=""></div>
  {{ end }}

  <div class="cover-card">
    <div class="cover-kicker">MadamGY &middot; Growth, Nutrition &amp; Development</div>
    <h1>{{ .Child.DisplayName }}</h1>
    <p class="cover-sub">A working guide to feeding, growth, vaccination, development and
      everyday care, personalised to this child's age and recorded profile. Prepared after
      consultation.</p>

    <div class="cover-facts">
      {{ with .Child.DateOfBirth }}<div><span class="label">Date of birth</span>
           <span class="value mono">{{ . }}</span></div>{{ end }}
      <div><span class="label">Age at generation</span>
           <span class="value">{{ .Child.AgeLabel }}</span></div>
      <div><span class="label">Pages in this book</span>
           <span class="value mono">{{ .SectionCount }} sections</span></div>
    </div>
  </div>
</div>
{{ end }}
```

This drops the old small `<figure class="cover-photo">` portrait (a photo inside the card)
in favour of the photo as the whole page's background, with the identity card as an opaque
panel on top -- matching the reference document's actual structure per the design spec.

- [ ] **Step 4: Update the cover CSS in `tokens.css`**

This step replaces a wrapper class (`.cover-main`) with a new one (`.cover-card`) that
absorbs what `.cover-main` and the absolutely-positioned `.cover-facts` used to do
separately. Leaving the old rules in place after this edit would be dead, conflicting CSS
that a reviewer should flag -- delete them explicitly, in this order, all in
`internal/book/templates/tokens.css`:

1. Delete lines 444-449 (`.cover-logo` and the "Small and at the very top" comment above
   it) -- the redesigned cover carries no separate logo image; `MadamGY` prints as the
   existing `.cover-kicker` text.
2. Delete lines 493-494 exactly:
   ```css
   .cover.no-photo .cover-main { margin-top: 96mm; }
   .cover.with-photo .cover-main { margin-top: 26mm; }
   ```
   (`.cover-main` does not exist in the new markup from Step 3 -- `.cover-card` replaces it,
   with its own margin-top rules added below.)
3. Change `.cover .cover-facts` (lines 518-527) from absolutely-positioned-at-the-page-foot
   to normally flowing inside the card -- it moves with the card now, since the card itself
   is what sits at a tuned offset, not the facts row alone. Replace:
   ```css
   .cover .cover-facts {
     position: absolute;
     left: 0;
     right: 0;
     bottom: 0;
     display: grid;
     grid-template-columns: repeat(3, 1fr);
     gap: 4mm 6mm;
     border-top: 0.5mm solid var(--rule-strong);
     padding-top: 4mm;
   }
   ```
   with:
   ```css
   .cover .cover-facts {
     display: grid;
     grid-template-columns: repeat(3, 1fr);
     gap: 4mm 6mm;
     border-top: 0.5mm solid var(--rule-strong);
     padding-top: 4mm;
     margin-top: 5mm;
   }
   ```
   Leave `.cover .cover-facts .label`, `.cover .cover-facts > div`, and
   `.cover .cover-facts .value` (lines 529-531) untouched -- unaffected by this change.
4. In the `@media screen` block near the end of the file, find
   `.cover .cover-facts { position: static; margin-top: 24mm; }` and delete that line -- it
   was the screen-preview override for the print rule this step just removed, and now
   duplicates (with a different value) what `.cover .cover-facts`'s own `margin-top: 5mm`
   already does unconditionally.
5. Find the existing `.cover-photo`/`.cover-photo img`/`.cover-photo figcaption` rules
   (search for the comment `Cover portrait.`) and delete them -- the small in-card photo
   frame they styled no longer exists; the photo is now the full-bleed background.
6. Add, in the same section (near where `.cover-logo` used to be):

```css
/* Full-bleed photo behind the identity card. A plain absolutely-positioned layer, not a
   flex/grid participant -- see this task's own note above .cover-card for why. */
.cover-bg {
  position: absolute;
  inset: 0;
  z-index: 0;
}
.cover-bg img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.cover-card {
  position: relative;
  z-index: 1;
  background: var(--surface);
  border-radius: 8mm;
  padding: 14mm 12mm 10mm;
  box-shadow: 0 3mm 8mm rgba(40, 25, 15, 0.18);
  text-align: center;
}
.cover.no-photo .cover-card { margin-top: 96mm; }
.cover.with-photo .cover-card { margin-top: 26mm; }
```

`.cover-kicker`'s existing selector (`.cover .cover-kicker`) needs no change -- it still
matches, since `.cover-kicker` is now a descendant of `.cover` via `.cover-card` rather
than via the deleted `.cover-main`, and a descendant combinator does not care about the
intermediate element. Confirm visually in Step 6 that its letterspacing/colour still reads
well against the new `--surface` cream card rather than directly against a photo.

- [ ] **Step 5: Run the tests again to verify they pass**

Run: `go test ./internal/book/... -run TestBook1Cover -v`
Expected: PASS

- [ ] **Step 6: Print and look**

Run: `BOOK_PAGE_DUMP=/tmp/bookdump go test ./internal/book/... -run TestPrintPDFProducesAPDF -v`

Confirm: without a photo, the cover looks exactly as before structurally (card with 96mm
drop, no background layer). With a synthetic test photo, the photo fills the page and the
card sits legibly on top at the 26mm drop, matching the with-photo variant's existing
tuning.

- [ ] **Step 7: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./internal/book/...`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/book/templates/book1/cover.html internal/book/templates/tokens.css internal/book/render_test.go
git commit -m "book1: front cover background is the child's uploaded photo, full-bleed"
```

---

### Task 7: Book 2 cover - decorative illustration background and kids-cooking hero

**Files:**
- Modify: `internal/book/templates/book2/cover.html`
- Modify: `internal/book/render.go` (thread `coverArt` into the render context so Book 2's
  cover template can reach it)
- Test: `internal/book/render_test.go` (new test)

**Interfaces:**
- Consumes: `coverArt map[string]template.URL` from Task 2.

- [ ] **Step 1: Write the failing test**

```go
// add to internal/book/render_test.go
func TestBook2CoverPrintsTheDecorativeIllustrationsAndTheKidsCookingHero(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, Book2{
		Child: ChildSummary{DisplayName: "Test Child"},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, key := range []string{"bibimbap", "hotpot", "mooncake", "kids-cooking"} {
		if !strings.Contains(out, string(coverArt[key])) {
			t.Errorf("expected the %q illustration's data URI on Book 2's cover", key)
		}
	}
}
```

(`Book2{Child: ...}` is confirmed correct against `internal/book/types.go:653-668`:
`Book2.Child ChildSummary`, same type Book 1 uses.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/book/... -run TestBook2CoverPrints -v`
Expected: FAIL.

- [ ] **Step 3: Expose `coverArt` to templates via the render context**

`internal/book/coverart.go`'s `coverArt` var is already package-level and templates parsed
in the same package's `ParseFS` call can reference package-level Go values only through the
data passed to `Execute`, not directly -- so thread it the same way `Watermark`/`FontFaces`
are threaded. In `internal/book/render.go`, add a field:

```go
type renderContext struct {
	Metadata  Metadata
	BookClass string
	CSS       template.CSS
	FontFaces template.CSS
	Watermark template.URL
	CoverArt  map[string]template.URL
	Data      any
}
```

and in the `ctx := renderContext{...}` literal, add `CoverArt: coverArt,`.

Templates access it as `$.CoverArt.mooncake` -- but Go template map access with a
hyphenated key (`noodle-roll`) needs the `index` function, not dot access:
`{{ index $.CoverArt "noodle-roll" }}`. Use `index` for every lookup in this task and Task
10, even the keys without hyphens, for consistency.

- [ ] **Step 4: Rewrite `book2/cover.html`**

```html
{{ define "B2-COVER-01" }}
{{/* Book 2's cover carries no photo upload of any kind -- see the standing comment this
     replaces: a working recipe document, not the child's identification page. Its
     full-bleed background is decorative illustration (coverArt, background-removed PNGs,
     see coverart.go), corner-scattered, with the supplied kids-cooking illustration as a
     hero image inside the identity card. */}}
<div class="cover no-photo">
  <div class="cover-bg cover-bg-illustrated">
    <img class="corner-art corner-tl" src="{{ index $.CoverArt "bibimbap" }}" alt="">
    <img class="corner-art corner-tr" src="{{ index $.CoverArt "hotpot" }}" alt="">
    <img class="corner-art corner-bl" src="{{ index $.CoverArt "mooncake" }}" alt="">
    <img class="corner-art corner-br" src="{{ index $.CoverArt "egg-noodle-bowl" }}" alt="">
  </div>

  <div class="cover-card">
    <img class="cover-hero" src="{{ index $.CoverArt "kids-cooking" }}" alt="">
    <div class="cover-kicker">MadamGY &middot; Personalised Recipe Book</div>

    <h1>Recipes for<br>{{ .Child.DisplayName }}</h1>
    <p class="cover-sub">Selected against this child's feeding stage, declared food practice
      and allergy exclusions. Matched from the provider's recipe master, not written for
      this book.</p>

    <div class="cover-facts">
      <div><span class="label">Age at generation</span>
           <span class="value">{{ .Child.AgeLabel }}</span></div>
      <div><span class="label">Food practice</span>
           <span class="value">{{ if .Child.FoodPractice }}{{ .Child.FoodPractice }}{{ else }}not recorded{{ end }}</span></div>
      <div><span class="label">Recipes in this book</span>
           <span class="value mono">{{ .RecipeCount }}</span></div>
    </div>
  </div>
</div>
{{ end }}
```

- [ ] **Step 5: Add the corner-art and hero CSS to `tokens.css`**

```css
/* Book 2's cover: decorative illustration, not a photo. See design spec section 2.3 for why
   these carry no dish-format claim and never appear on a recipe page. Corner placement
   bleeds past the page edge deliberately -- this is paint only, .cover is a single
   non-fragmenting page, so there is no break-rule interaction to consider. */
.cover-bg-illustrated { position: absolute; inset: 0; z-index: 0; overflow: hidden; }
.corner-art {
  position: absolute;
  width: 62mm;
  filter: drop-shadow(0 2mm 3mm rgba(0, 0, 0, 0.1));
}
.corner-art.corner-tl { top: -12mm; left: -12mm; transform: rotate(-8deg); }
.corner-art.corner-tr { top: -14mm; right: -14mm; transform: rotate(10deg); }
.corner-art.corner-bl { bottom: -14mm; left: -12mm; transform: rotate(14deg); }
.corner-art.corner-br { bottom: -12mm; right: -12mm; transform: rotate(-11deg); }
.cover-hero {
  width: 90mm;
  margin: 0 auto 4mm;
  display: block;
  filter: drop-shadow(0 3mm 6mm rgba(40, 25, 15, 0.2));
}
```

- [ ] **Step 6: Run the tests again to verify they pass**

Run: `go test ./internal/book/... -run TestBook2CoverPrints -v`
Expected: PASS

- [ ] **Step 7: Print and look, then run the full suite**

```bash
BOOK_PAGE_DUMP=/tmp/bookdump go test ./internal/book/... -run TestPrintPDFProducesAPDF -v
go build ./... && go vet ./... && go test ./internal/book/...
```

Confirm visually: the four corner illustrations bleed off the page edges without obscuring
the identity card's text, the kids-cooking hero sits cleanly inside the card (no visible
white box edge -- if one appears, re-check Task 2's background removal on
`kids-cooking.png` specifically).

- [ ] **Step 8: Commit**

```bash
git add internal/book/templates/book2/cover.html internal/book/templates/tokens.css internal/book/render.go internal/book/render_test.go
git commit -m "book2: cover background is decorative illustration, kids-cooking hero on the card"
```

---

### Task 8: New back-cover feature - parents' photo on Book 1's closing page

**Files:**
- Modify: `internal/book/types.go` (`Metadata` struct)
- Modify: `internal/book/templates/book1/end.html`
- Modify: `internal/book/templates/tokens.css` (`.backcover` rules)
- Modify: `internal/api/handlers/book_generate.go` (`generateRequest`, `decodeGenerate`)
- Modify: `internal/api/handlers/book_set.go` (`renderSetWithPhoto` -> `renderSetWithPhotos`)
- Test: `internal/book/render_test.go`, `internal/api/handlers/book_generate_test.go` (if
  one exists -- check before assuming; if not, add assertions to whichever handler test
  file already covers `decodeGenerate`)

**Interfaces:**
- Consumes: `ParsePhoto(dataURI, caption string) (*ChildPhoto, error)` (unchanged,
  `internal/book/photo.go`) -- reused as-is for the parents' photo, since the validation
  rules (allowlist, size cap, base64, no SVG) apply identically to any uploaded cover image.
- Produces: `Metadata.ParentsPhoto *ChildPhoto`.

- [ ] **Step 1: Write the failing render test**

```go
// add to internal/book/render_test.go
func TestBook1BackCoverPrintsTheParentsPhotoAsAFullBleedBackground(t *testing.T) {
	photo := &ChildPhoto{DataURI: template.URL("data:image/png;base64,iVBORw0KGgo=")}
	meta := Metadata{Language: "en", ParentsPhoto: photo}
	b := Book1{Metadata: meta, Child: ChildSummary{DisplayName: "Test Child"}}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, meta, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `class="backcover-bg"`) {
		t.Fatal("with a parents' photo present, the back cover must carry a full-bleed background layer")
	}
	if !strings.Contains(out, string(photo.DataURI)) {
		t.Fatal("the parents' photo data URI must appear on the back cover")
	}
	// The real imprint facts still print -- this page invents nothing new.
	if !strings.Contains(out, "Book version") || !strings.Contains(out, "Generation date") {
		t.Fatal("the back cover must still print the real version/release/date facts end.html always has")
	}
}

func TestBook1BackCoverWithNoParentsPhotoPrintsThePlainImprintPage(t *testing.T) {
	meta := Metadata{Language: "en"}
	b := Book1{Metadata: meta, Child: ChildSummary{DisplayName: "Test Child"}}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, meta, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), `class="backcover-bg"`) {
		t.Fatal("with no parents' photo, the back cover must not print an empty background layer")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/book/... -run TestBook1BackCover -v`
Expected: FAIL (`Metadata.ParentsPhoto` doesn't exist).

- [ ] **Step 3: Add the field to `Metadata`**

In `internal/book/types.go`, in the `Metadata` struct (right after `Logo`):

```go
type Metadata struct {
	Title          string    `json:"title"`
	BookVersion    string    `json:"book_version"`
	ReleaseID      string    `json:"release_id"`
	GenerationDate time.Time `json:"generation_date"`
	Language       string    `json:"language"`
	Logo           template.URL `json:"-"`
	// ParentsPhoto is the back cover's optional full-bleed background, uploaded
	// independently of Child.Photo (the front cover's photo). Book 1 only -- see
	// book1/end.html, which is the only template that reads it. Reuses ChildPhoto/
	// ParsePhoto rather than a near-duplicate type: the validation rules (allowlist,
	// size cap, base64, no SVG) are identical for any uploaded cover image regardless
	// of who is in it.
	ParentsPhoto *ChildPhoto `json:"-"`
}
```

- [ ] **Step 4: Rewrite `book1/end.html`**

```html
{{ define "B1-END-01" }}
{{/* Book 1's own back page. See the design spec section 2.4: the background is the
     operator's uploaded photo of the parents when one is supplied, full-bleed, the same
     with-photo/no-photo two-state pattern the front cover uses -- and exactly the same
     real facts as before print on top of it. No new content: book_version, release_id and
     generation_date are the same three fields this page has always carried. */}}
<section class="page-break">
  <div class="backcover {{ if .ParentsPhoto }}with-photo{{ else }}no-photo{{ end }}">
    {{ with .ParentsPhoto }}
    <div class="backcover-bg"><img class="backcover-bg-img" src="{{ .DataURI }}" alt=""></div>
    {{ end }}

    <div class="backcover-card">
      <div class="page-head">
        <p class="kicker">About this copy</p>
        <h2>Version and generation</h2>
      </div>
      <div class="imprint">
        <dl>
          <dt>Book version</dt><dd class="mono">{{ .BookVersion }}</dd>
          <dt>Release ID</dt><dd class="mono">{{ if .ReleaseID }}{{ .ReleaseID }}{{ else }}<span class="write-line"></span>{{ end }}</dd>
          <dt>Generation date</dt><dd class="mono">{{ .GenerationDate.Format "2006-01-02" }}</dd>
        </dl>
      </div>
      <img class="closing-logo" src="{{ .Logo }}" alt="MadamGY">
    </div>
  </div>
</section>
{{ end }}
```

- [ ] **Step 5: Add the back-cover CSS to `tokens.css`**

```css
/* Book 1's back cover. Same photo-behind-card mechanism as the front cover (.cover-bg),
   under its own class so the two are never confused: this page has no drop-height
   variants tuned for it the way .cover does, because its card content is small and fixed
   (three dl rows, one logo) and comfortably fits either state without measurement. */
.backcover { position: relative; min-height: 255mm; }
.backcover-bg { position: absolute; inset: 0; z-index: 0; }
.backcover-bg-img { width: 100%; height: 100%; object-fit: cover; display: block; }
.backcover-card {
  position: relative;
  z-index: 1;
}
.backcover.with-photo .backcover-card {
  background: var(--surface);
  border-radius: 8mm;
  padding: 12mm;
  margin: 60mm 20mm;
  box-shadow: 0 3mm 8mm rgba(40, 25, 15, 0.18);
}
```

- [ ] **Step 6: Run the render tests again to verify they pass**

Run: `go test ./internal/book/... -run TestBook1BackCover -v`
Expected: PASS

- [ ] **Step 7: Wire the second photo through the API**

In `internal/api/handlers/book_generate.go`, add to `generateRequest`:

```go
	// ParentsPhotoDataURI is the back cover's portrait, of the parents rather than the
	// child. Validated by book.ParsePhoto, same as PhotoDataURI.
	ParentsPhotoDataURI string `json:"parents_photo_data_uri,omitempty"`
	ParentsPhotoCaption string `json:"parents_photo_caption,omitempty"`
```

Change `decodeGenerate`'s signature and body:

```go
func (h *Handlers) decodeGenerate(w http.ResponseWriter, r *http.Request) (profile.Stored, *book.ChildPhoto, *book.ChildPhoto, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxGenerateBody)

	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body: "+err.Error())
		return profile.Stored{}, nil, nil, false
	}

	s, err := req.toStored()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return profile.Stored{}, nil, nil, false
	}
	if msg, err := h.validateProfileVocabularies(r.Context(), s); err != nil {
		writeError(w, http.StatusInternalServerError, "vocabulary check failed: "+err.Error())
		return profile.Stored{}, nil, nil, false
	} else if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return profile.Stored{}, nil, nil, false
	}

	photo, err := book.ParsePhoto(req.PhotoDataURI, req.PhotoCaption)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return profile.Stored{}, nil, nil, false
	}
	parentsPhoto, err := book.ParsePhoto(req.ParentsPhotoDataURI, req.ParentsPhotoCaption)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return profile.Stored{}, nil, nil, false
	}
	return s, photo, parentsPhoto, true
}
```

Update every call site of `h.decodeGenerate(w, r)` (three, per the earlier grep: around
lines 202, 224, 270) from `s, photo, ok := h.decodeGenerate(w, r)` to `s, photo,
parentsPhoto, ok := h.decodeGenerate(w, r)`, and their following
`h.renderSetWithPhoto(w, r, s, photo)` calls to `h.renderSetWithPhotos(w, r, s, photo,
parentsPhoto)` (renamed in the next step).

- [ ] **Step 8: Rename and extend `renderSetWithPhoto` in `book_set.go`**

```go
func (h *Handlers) renderSet(w http.ResponseWriter, r *http.Request, s profile.Stored) (bookSetResponse, book.Set, bool) {
	return h.renderSetWithPhotos(w, r, s, nil, nil)
}

// renderSetWithPhotos is renderSet with two optional cover portraits: the front cover's
// child photo and the back cover's parents' photo. Both are attached after assembly rather
// than carried through it -- neither changes a recipe, a filter or an omission, so
// threading either through the assemblers would put a decoration in the path of the
// clinical logic.
func (h *Handlers) renderSetWithPhotos(w http.ResponseWriter, r *http.Request, s profile.Stored, photo, parentsPhoto *book.ChildPhoto) (bookSetResponse, book.Set, bool) {
	ctx := r.Context()
	asOf := time.Now().UTC()

	set, err := book.AssembleSet(ctx, h.pool, s, asOf, book.WithDrafter(h.drafter))
	if err != nil {
		if errors.Is(err, book.ErrBlocked) {
			h.writeBlocked(w, r, s, asOf, err)
			return bookSetResponse{}, book.Set{}, false
		}
		writeError(w, http.StatusInternalServerError, "book set assembly failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}

	// Book 1 only, both photos -- Book 2 is a working recipe document, its cover carries
	// no photo upload of any kind.
	if photo != nil {
		set.Book1.Child.Photo = photo
	}
	if parentsPhoto != nil {
		set.Book1.Metadata.ParentsPhoto = parentsPhoto
	}

	var buf1, buf2 bytes.Buffer
	if err := book.RenderHTML(&buf1, book.Kind1, set.Book1.Metadata, set.Book1); err != nil {
		writeError(w, http.StatusInternalServerError, "book1 render failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}
	if err := book.RenderHTML(&buf2, book.Kind2, set.Book2.Metadata, set.Book2); err != nil {
		writeError(w, http.StatusInternalServerError, "book2 render failed: "+err.Error())
		return bookSetResponse{}, book.Set{}, false
	}

	return bookSetResponse{
		ChildID:          set.ChildID,
		AsOf:             set.AsOf,
		Book1:            buf1.String(),
		Book2:            buf2.String(),
		ProfileOmissions: set.ProfileOmissions,
		Book1Omissions:   set.Book1Omissions,
		Book2Omissions:   set.Book2Omissions,
	}, set, true
}
```

- [ ] **Step 9: Build and run the handlers package tests**

Run: `go build ./... && go vet ./... && go test ./internal/api/...`
Expected: PASS. If any existing test called `h.decodeGenerate` or `h.renderSetWithPhoto`
directly by name, update it to the new signature/name -- grep for both before considering
this step done:

```bash
grep -rn "decodeGenerate\|renderSetWithPhoto\b" internal/api/handlers/*_test.go
```

- [ ] **Step 10: Print and look, then run the full suite**

```bash
BOOK_PAGE_DUMP=/tmp/bookdump go test ./internal/book/... -run TestPrintPDFProducesAPDF -v
go build ./... && go vet ./... && go test ./...
```

Confirm visually: without a parents' photo, the back page looks exactly as it does today.
With a synthetic test photo, the photo fills the page and the imprint card sits legibly on
top.

- [ ] **Step 11: Commit**

```bash
git add internal/book/types.go internal/book/templates/book1/end.html internal/book/templates/tokens.css \
        internal/api/handlers/book_generate.go internal/api/handlers/book_set.go internal/book/render_test.go
git commit -m "book1: add a second, independent photo upload -- parents' photo on the back cover"
```

---

### Task 9: Restore the dish-format mark on the recipe page

**Files:**
- Modify: `internal/book/templates/book2/recipe.html`
- Modify: `internal/book/render_test.go`
  (`TestARecipePagePrintsNoPictureEvenWhenMarkAndPhotoAreSet`)

**Interfaces:**
- Consumes: `RecipeCard.Mark *DishMark` and `RecipeCard.Photo *RecipePhoto`, both already
  resolved by `loadRecipeCards`/`RepresentativePhoto` (`internal/book/types.go:519-533`).
  `RecipeCard.Photo`'s own doc comment already states the intended behaviour this task
  implements: "When present, the template shows it instead of Mark; when nil, Mark's drawn
  artwork prints as before" -- that intent was never wired into `recipe.html`, which
  currently shows neither. This task is completing already-decided, already-built behaviour
  (the archetype photo pipeline is `docs/superpowers/specs/2026-08-24-recipe-photo-pipeline-design.md`,
  out of scope to re-litigate here), not inventing new imagery policy.

This reverses one prior, explicit, commented decision
(`internal/book/templates/book2/recipe.html`'s own comment: "No picture prints on this
page... Removed by decision: the pictures were not needed"). Per the design spec section
2.3, that decision is superseded now that "no page reads as blank" is a stated requirement
and the format mark is the only recipe-page image that carries no invented dish-accuracy
claim.

- [ ] **Step 1: Read the currently-pinned test**

```bash
grep -n "TestARecipePagePrintsNoPictureEvenWhenMarkAndPhotoAreSet" -A 25 internal/book/render_test.go
```

Read its full body before editing -- it constructs a `RecipeCard` with both `.Mark` and
`.Photo` set and currently asserts neither prints. This step is read-only; note the exact
struct literal it builds so Step 3 below builds the same one.

- [ ] **Step 2: Add the mark markup to `recipe.html`**

In `internal/book/templates/book2/recipe.html`, change the opening of the `B2-RECIPE-01`
block from:

```html
<div class="recipe">
  <div class="recipe-head">
```

to:

```html
<div class="recipe">
  {{/* Photo if the archetype pipeline resolved one, else the drawn mark, else nothing --
       exactly what RecipeCard.Photo's own doc comment in types.go already specifies. */}}
  {{ if .Photo }}
  <figure class="recipe-mark">
    <img src="{{ .Photo.DataURI }}" alt="">
    {{ with .Photo.SourceLabel }}<figcaption>{{ . }}</figcaption>{{ end }}
  </figure>
  {{ else if .Mark }}
  <figure class="recipe-mark">
    {{ .Mark.SVG }}
    <figcaption>{{ .Mark.FormatLabel }}</figcaption>
  </figure>
  {{ end }}
  <div class="recipe-head">
```

and update the template's own top comment (the one explaining "No picture prints on this
page") to record the reversal rather than deleting the history:

```html
{{/* One recipe, one page.

     [... keep the existing paragraph about wet hands and page breaks unchanged ...]

     The dish-format mark prints again as of 2026-09-04 -- reversing the decision recorded
     below, which is kept for the history rather than deleted. "No picture prints on this
     page... Removed by decision: the pictures were not needed" was true until this book's
     visual redesign made "every page carries a small illustration, none look blank" a
     stated requirement (see docs/superpowers/specs/2026-09-04-cookbook-visual-redesign-design.md
     section 2.3). Still no per-recipe photograph (GAP-025 is unchanged) -- only the mark,
     which is format-accurate and already measured against this exact page's fragmentation
     behaviour (see marks.go and the float/clear notes in tokens.css's Recipe page section). */}}
```

- [ ] **Step 3: Replace the pinned test with two, matching the documented Photo-else-Mark intent**

The old test's premise -- neither Mark nor Photo ever prints -- was the interim state this
task retires, not a hard rule: `RecipeCard.Photo`'s own doc comment already specifies
"when present, the template shows it instead of Mark," and `RecipePhoto` is honestly
archetype-level (`SourceLabel` says so), which is a different, already-decided thing from
the per-exact-recipe photograph `GAP-025` forbids. Replace the one old test with two:

```go
// Replaces TestARecipePagePrintsNoPictureEvenWhenMarkAndPhotoAreSet. That test's premise --
// neither image ever prints -- was the interim state of the prior "pictures were not
// needed" decision, not a permanent rule; RecipeCard.Photo's own doc comment already
// specified this Photo-else-Mark behaviour, it was just never wired into the template.
func TestARecipePagePrefersThePhotoOverTheMarkWhenBothAreSet(t *testing.T) {
	card := RecipeCard{
		Number: 1,
		Title:  "Test Recipe",
		Mark:   &DishMark{FormatLabel: "Test format", SVG: template.HTML("<svg></svg>")},
		Photo:  &RecipePhoto{DataURI: template.URL("data:image/jpeg;base64,/9k="), SourceLabel: "test archetype"},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, Book2{
		MealSections: []MealSection{{Number: 1, Title: "Breakfast", Recipes: []RecipeCard{card}}},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, string(card.Photo.DataURI)) {
		t.Fatal("with both Mark and Photo set, the photo must print")
	}
	if strings.Contains(out, "<svg></svg>") {
		t.Fatal("with a photo present, the mark's SVG must not also print")
	}
}

func TestARecipePagePrintsTheMarkWhenThereIsNoPhoto(t *testing.T) {
	card := RecipeCard{
		Number: 1,
		Title:  "Test Recipe",
		Mark:   &DishMark{FormatLabel: "Test format", SVG: template.HTML("<svg></svg>")},
	}
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, Book2{
		MealSections: []MealSection{{Number: 1, Title: "Breakfast", Recipes: []RecipeCard{card}}},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "<svg></svg>") {
		t.Fatal("with no photo, the drawn mark must print")
	}
}
```

Confirm the exact `Book2`/`MealSection` construction here compiles against the real fields
read earlier in this task (`Book2.MealSections`, `MealSection.Recipes`) -- adjust any other
required fields (`Metadata`, `Child`) the same way Task 7's test needed them, since
`RenderHTML` executes the whole `body.html`, not just `B2-RECIPE-01` in isolation.

- [ ] **Step 4: Add the mark's CSS**

Check `tokens.css` for whether `.recipe-mark`-equivalent float rules already exist from
before the mark was disabled (search for `recipe-mark` and `dish mark` -- the design section
"Recipe page" in `tokens.css` has an extended comment about the mark's float/clear behaviour
that predates its removal from the template, so the CSS rule may already be present and
only the template needed the markup restored). If it exists, this step is a no-op beyond
confirming the class name in that existing CSS matches `recipe-mark` exactly (rename either
side if they don't match). If it genuinely doesn't exist, add it:

```css
.recipe-mark {
  float: left;
  width: 34mm;
  margin: 0 6mm 3mm 0;
}
.recipe-mark svg { width: 100%; display: block; }
.recipe-mark figcaption {
  font-family: var(--font-sans);
  font-size: var(--caption-size);
  color: var(--ink-soft);
  text-align: center;
  margin-top: 1mm;
}
```

- [ ] **Step 5: Run the updated tests**

Run: `go test ./internal/book/... -run TestARecipePagePrefersThePhotoOverTheMark -v`
Run: `go test ./internal/book/... -run TestARecipePagePrintsTheMarkWhenThereIsNoPhoto -v`
Expected: both PASS

- [ ] **Step 6: Print and look, then run the full suite**

```bash
BOOK_PAGE_DUMP=/tmp/bookdump go test ./internal/book/... -run TestPrintPDFProducesAPDF -v
go build ./... && go vet ./... && go test ./internal/book/...
```

Confirm visually against the printed PDF (not just the test): the mark sits beside the
production strip without pushing the tracker onto a second sheet, on the tallest recipe in
the test book (long title, long selection note, partly-verified nutrition panel -- the
combination `tokens.css`'s own comments identify as the tightest fit). If it spills, this is
the exact defect the pre-removal comments were tuned against; re-check the float width and
margins in Step 4 rather than assuming the old values still fit next to the Poppins type
scale from Task 4.

- [ ] **Step 7: Commit**

```bash
git add internal/book/templates/book2/recipe.html internal/book/templates/tokens.css internal/book/render_test.go
git commit -m "book2: restore the dish-format mark on the recipe page, reversing the prior removal"
```

---

### Task 10: Book 2 chapter opener - decorative corner illustration

**Files:**
- Modify: `internal/book/templates/book2/section.html`
- Test: `internal/book/render_test.go` (new test)

**Interfaces:**
- Consumes: `coverArt` via `$.CoverArt` (already threaded by Task 7).

- [ ] **Step 1: Write the failing test**

```go
// add to internal/book/render_test.go
func TestChapterOpenerPrintsADecorativeIllustration(t *testing.T) {
	var buf bytes.Buffer
	b2 := Book2{
		Child:        ChildSummary{DisplayName: "Test Child"},
		MealSections: []MealSection{{Number: 1, Title: "Breakfast", Recipes: nil}},
	}
	if err := RenderHTML(&buf, Kind2, Metadata{Language: "en"}, b2); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), `class="chapter-art"`) {
		t.Fatal("a chapter opener must print a decorative corner illustration")
	}
}
```

Confirmed field names (`internal/book/types.go:639-648`): `Book2.MealSections
[]MealSection`, `MealSection{MealCategoryID, Title, TargetRecipeCount, Recipes, Number}`.
`B2-SECTION-01` is dispatched once per `MealSection` from `book2/body.html`'s range over
`.MealSections` (confirm the exact range/dispatch line in `body.html` before editing, the
same way Task 9 confirms `RecipeCard`'s fields before use).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/book/... -run TestChapterOpenerPrints -v`
Expected: FAIL.

- [ ] **Step 3: Add the illustration to `section.html`**

In `internal/book/templates/book2/section.html`, change:

```html
<section class="page-break">
  <div class="chapter">
    <p class="chapter-num">{{ printf "%02d" .Number }}</p>
```

to:

```html
<section class="page-break">
  <div class="chapter">
    <img class="chapter-art" src="{{ index $.CoverArt "egg-noodle-bowl" }}" alt="">
    <p class="chapter-num">{{ printf "%02d" .Number }}</p>
```

Use a different `coverArt` key per meal category if `Section` (or `Chapter`, whichever the
real type is) carries a stable per-category identifier already -- check whether one exists
before hardcoding `"egg-noodle-bowl"` for every chapter; if there's no existing per-category
field to key off, hardcoding one illustration for every chapter opener is an acceptable
starting point (still satisfies "no page looks blank") and can be varied later.

- [ ] **Step 4: Add the CSS**

```css
.chapter-art {
  width: 70mm;
  margin: 0 auto 6mm;
  display: block;
  filter: drop-shadow(0 3mm 5mm rgba(40, 25, 15, 0.18));
}
```

- [ ] **Step 5: Run the test again to verify it passes**

Run: `go test ./internal/book/... -run TestChapterOpenerPrints -v`
Expected: PASS

- [ ] **Step 6: Print and look, then run the full suite**

```bash
BOOK_PAGE_DUMP=/tmp/bookdump go test ./internal/book/... -run TestPrintPDFProducesAPDF -v
go build ./... && go vet ./... && go test ./internal/book/...
```

Confirm the illustration doesn't push the chapter's recipe-count line or contents list off
the bottom margin -- `.chapter` in `tokens.css` currently declares `margin-top: 62mm` before
its first line; check whether the added image needs that constant reduced to keep the whole
opener vertically balanced, and if so, adjust `.chapter`'s `margin-top` and re-print rather
than leaving the page looking top-heavy.

- [ ] **Step 7: Commit**

```bash
git add internal/book/templates/book2/section.html internal/book/templates/tokens.css internal/book/render_test.go
git commit -m "book2: chapter openers carry a decorative illustration"
```

---

### Task 11: Final verification pass

**Files:** none (verification only).

- [ ] **Step 1: Full build and static checks**

```bash
go build ./...
go vet ./...
```
Expected: clean, no errors or warnings.

- [ ] **Step 2: Full test suite with a real database**

```bash
scripts/dev_db.fish up
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
```
Expected: all PASS, including `internal/db`'s integrity suite (unaffected by this plan, but
confirms nothing in the handler changes broke request decoding for the existing endpoints).

- [ ] **Step 3: Frontend suite (unaffected, confirm it still is)**

```bash
cd web && npm test
```
Expected: PASS unchanged -- this plan touches no frontend file.

- [ ] **Step 4: Print both books and re-check the page-fit budget**

```bash
BOOK_PAGE_DUMP=/tmp/bookdump-final go test ./internal/book/... -run TestNoPageContentOverflowsTheTextBlock -v
```

Expected: PASS, and the specific numbers this test reports (pages over the fill ceiling,
pages under the fill floor, orphaned headers) must not be worse than they were before this
plan started. If any budget regressed, the most likely causes, in order of likelihood
given this plan's changes: the restored dish mark (Task 9) pushing a recipe page's tracker
onto a second sheet; the chapter-opener illustration (Task 10) changing that page's vertical
balance; or the new font's line-height at the same point sizes running slightly taller than
the old system font stack did (Poppins and Noto Sans do not share identical metrics at the
same declared size). Re-open the dumped PDFs and read the flagged pages before adjusting
anything blind.

- [ ] **Step 5: Manual read of both books cover to cover**

Open every page in `/tmp/bookdump-final`. This is the step every defect log in this
project's history says matters most -- "every defect these guards exist for was found by
printing a book and reading it, never by a count." Specifically check:

- Bengali ingredient names on recipe pages still render correctly (Task 4 changed no Indic
  font token, but this is the one thing worth a direct look given how central it is to why
  this renderer exists).
- The two covers, with and without an uploaded photo.
- The back cover, with and without a parents' photo.
- No page reads as visibly blank or unbalanced.

- [ ] **Step 6: Confirm the outstanding licence question is still tracked, not silently resolved**

The twelve illustration assets' Canva Free-vs-Pro licence tier has not been confirmed. This
plan does not resolve it and must not be read as having done so. Leave the design spec's
"Out of scope" section 3 as the record of this open item, and do not merge this work into a
book a real family receives before it is answered.

---

## Self-review notes

- **Spec coverage:** Section 2.1 (palette) -> Task 4. Section 2.2 (typography) -> Tasks 1,
  3, 4. Section 2.3 (illustration policy) -> Tasks 2, 7, 9, 10. Section 2.4 (real photos on
  both covers) -> Tasks 6, 8. Section 2.5 (no flex centring) -> called out explicitly in
  Tasks 6 and 8's own text, not just the spec. Section 2.6 (no break-rule interaction) ->
  every illustration task is scoped to a guaranteed single-page container, stated in each
  task's own opening.
- **Placeholder scan:** the two structural fields flagged during drafting (`Book2`'s
  chapter-slice field, `RecipeCard`'s image fields) were verified by direct read
  (`internal/book/types.go:489-533,639-668`) before finalizing and are now used correctly
  and consistently in Tasks 7, 9 and 10: `Book2.MealSections []MealSection`,
  `MealSection.Recipes []RecipeCard`, `RecipeCard.Mark *DishMark`,
  `RecipeCard.Photo *RecipePhoto`. Task 9's restored logic (`Photo` preferred, `Mark`
  fallback) was corrected mid-draft to match `RecipeCard.Photo`'s own doc comment, which
  this plan's first draft had missed - it would have shipped Mark-only and left the
  already-built archetype-photo pipeline still unwired.
- **Type consistency:** `renderContext.CoverArt map[string]template.URL` (Task 7) is the
  same type `coverArt` (Task 2) already produces; every template lookup uses `index
  $.CoverArt "key"` consistently across Tasks 7 and 10 rather than dot-access, since several
  keys are hyphenated.
