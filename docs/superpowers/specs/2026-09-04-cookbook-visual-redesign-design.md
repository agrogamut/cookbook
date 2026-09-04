# Cookbook visual redesign - design

## 1. Why

Both books currently share an editorial/clinical visual language: white paper, hairline
rules, small-caps sans labels, two muted brand palettes (book1 teal/navy, book2 plum/rose).
Reviewed against the actual product - a book a family takes home after a paediatric
consultation - this reads as "very formal, hospital-like" rather than as a cookbook a parent
wants to keep on a kitchen counter.

Direction was set iteratively against real references, not invented from scratch:

- Three Canva screenshots (a wave-divider recipe card, a scalloped-edge recipe card, a cover
  with stacked line-art food illustrations) set the target register: warm, illustrated,
  colour-blocked.
- Two hex colours (`#ff5858` coral, `#fffcf6` cream) and twelve PNG illustrations were
  supplied directly and are checked into the repo root pending relocation into
  `internal/book/assets/` (Task 2).
- `~/Downloads/Book1_Joyshree_FINAL.pdf` (a real family's document, produced outside this
  engine) supplied the front-cover structure - full-bleed photo behind a centred identity
  card - which this design adopts for Book 1's actual cover mechanism, not as decoration:
  **the front cover background is the child's own uploaded photo, and a new back-cover
  background is a second uploaded photo of the parents.** No content, name, or clinical
  detail from that real document is reused anywhere; only its layout bones informed the
  approach, and every mockup built during exploration used placeholder data.
- Requested font was Gotham; it is a paid commercial typeface and cannot be embedded (this
  project ships font files with zero print-time network access - see
  `internal/book/pdf.go`'s `--host-resolver-rules=MAP * ~NOTFOUND`). Poppins (Google Fonts,
  OFL) is the substitute: a geometric grotesque in the same register, free to embed. A
  cursive/script pairing (Parisienne) was tried and explicitly rejected by the user as
  illegible; there is no script typeface anywhere in the final direction.
- A first illustrated pass used soft pastel pill badges for "Book One"/"Book Two" labels and
  category tags. Rejected as a generic AI-template tell. Replaced with typographic devices
  that carry more intent: a notched ribbon flag for section kickers, a coloured-dot legend
  list, a dashed-divider ticket strip for recipe metadata.
- All twelve supplied PNGs shipped with solid white backgrounds baked in (confirmed via
  `PIL`/`numpy`: every file is RGB, no alpha channel). Used as-is they render as visible
  white boxes wherever they overlap the cream page. They need one-time background removal
  (border-connected flood fill, not a global threshold, to avoid punching holes in interior
  white details like eyes or teeth) before they are embeddable as clean cutouts.

## 2. Decisions

### 2.1 Palette: one unified scheme, not two

`tokens.css`'s `.book1`/`.book2` blocks currently define distinct `--brand`/`--brand-deep`/
`--surface`/`--tint` values (teal/navy vs plum/rose). Both move to the supplied coral/cream
scheme:

- `--brand: #ff5858` (coral)
- `--surface: #fffcf6` (cream)
- `--brand-deep`: a darkened coral for AA-contrast heading text on cream, tuned against a
  printed proof rather than guessed on a screen (mockup value `#c23c34` is the starting
  point, not the final word - Task 3 includes a contrast check).

`--warning-strong`/`--warning-soft` (clinical warning red) are **untouched**. They are
semantically distinct from decorative brand colour, and `avoid_color_only_meaning` requires
they stay visually distinct from it - a warning that shared its hue with the cover's brand
accent would stop reading as a warning.

### 2.2 Typography: Poppins for Latin text, Noto untouched for Indic script

Poppins (weights 400/500/600/700/800, plus italic 600/700 for short accent lines) replaces
the system-font Latin stack (`--font-serif`/`--font-sans`) for headings, labels, body prose
and table content. It is embedded as self-hosted `.woff2` files (mirroring how
`internal/book/watermark.go` embeds `water1.jpeg` today - `go:embed` + a package-level
base64 `template.CSS`/`template.URL` var), because the print browser cannot reach Google
Fonts' CDN.

`--font-indic`/`--font-indic-sans` (Noto Serif/Sans Bengali and Devanagari) are **not
touched, at all**. This is the one non-negotiable line in the whole redesign: this project
renders through headless Chromium specifically because Bengali needs real conjunct/matra
shaping, and every ingredient name, every Bengali table cell, keeps rendering in the
existing Noto stack exactly as today.

There is no script/cursive typeface anywhere in this design. An italic cut of Poppins
(`font-style: italic`) stands in for the softer, "handwritten" register a script face would
have carried, on short accent lines only - never on a paragraph.

### 2.3 Illustration policy: two different claims, two different sources

Two visually similar things carry different honesty obligations, and the redesign keeps
them separate rather than blurring them:

- **The twelve supplied PNGs** (mooncake, dim sum, tteokbokki, bibimbap, and others - East
  Asian dishes, not Bengali/Indian) are **decoration only**. They appear as corner-bleed
  ornament only on pages that are guaranteed, single, non-fragmenting print pages by
  construction: both covers (`.cover`), Book 1's back cover (`end.html`'s
  `<section class="page-break">`, small fixed content, always fits one sheet), and Book 2's
  chapter openers (`B2-SECTION-01`, explicitly built as "a whole page on purpose" per its own
  code comment, for exactly this reason). They do **not** appear on any block rendered inside
  Book 1's flowing, multi-section content pages (e.g. `B1-GROWTH-01`) - those pages are not
  single-page containers, several `Section`s can share one sheet, and a table can itself
  split across a page boundary, which is exactly the kind of fragmentation interaction
  `pagefit_test.go` was built to catch and this plan does not re-tune. They never appear
  captioned as "this is what your dish looks like," and they never appear on a recipe page,
  which does make that claim.
- **The existing 11 dish-format SVG marks** (`internal/book/marks/*.svg`, format-accurate,
  captioned with the format they depict) are the only imagery ever allowed on a recipe page.
  `internal/book/templates/book2/recipe.html` currently prints neither - a prior, explicit,
  commented decision ("the pictures were not needed," `RecipeCard.Mark` left on the struct
  but unused by the template). This redesign reverses that one decision, because "every page
  carries a small illustration, none look blank" is now a stated requirement and the format
  mark is the only image on a recipe page that satisfies it without inventing a new
  dish-accuracy claim. `TestARecipePagePrintsNoPictureEvenWhenMarkAndPhotoAreSet`
  (`internal/book/render_test.go`) pins the old decision by name and is updated, not deleted,
  with a comment explaining the reversal - matching the standing convention in this codebase
  (see `CLAUDE.md`'s own amendment log) of naming a reversed decision rather than silently
  overwriting it.

### 2.4 Front and back cover are real uploaded photographs, not illustrations

Book 1's cover mechanism already exists and already does this correctly for one photo:
`internal/book/photo.go`'s `ParsePhoto`/`ChildPhoto` (PNG/JPEG/WebP allowlist, 8 MB cap,
`data:` URI embed, SVG refused because it can carry a script, nothing written to disk),
threaded in at `internal/api/handlers/book_set.go`'s `renderSetWithPhoto` - attached after
assembly, deliberately, because a decoration must never become an input to a clinical
decision.

New in this design: a **second, independent** upload of the same kind, for the parents,
rendered as the background of Book 1's existing closing page
(`internal/book/templates/book1/end.html`, already commented as "Book 1's own back page").
Same validation function, same never-stored handling, its own template slot
(`Metadata.ParentsPhoto`, alongside the existing `Metadata.Logo`). No new content is
invented for that page - it keeps printing the same real facts it does today (book version,
release ID, generation date), now over a full-bleed photo when one is supplied and as a
plain page when it is not, mirroring the front cover's existing `with-photo`/`no-photo`
two-state pattern.

Book 2's cover carries no photo upload of any kind (a working recipe document, not an
identification page - see the existing, explicit comment in `book2/cover.html`). Its
full-bleed background is the twelve decorative PNGs, corner-scattered, plus the user's
kids-cooking illustration as a hero image inside the identity card.

### 2.5 The centred floating card does not use flex

Every mockup iteration centred the identity card with `display:flex; align-items:center;
justify-content:center`. That is **not** how the real cover works and must not be ported
literally: `tokens.css`'s own extended comment on `.cover` documents that a flex column
distributing a variable-height card inside a fixed-height print fragmentainer was tried
twice for the existing cover, measured, and rejected - the same markup produced a foot block
on the bottom margin for one book's shorter content and 51mm higher for the other's taller
content, because Chromium re-resolves `flex: 1` differently depending on how close the
container's content lands to the fragmentainer boundary. The existing, working fix is a
declared, hand-measured constant (`.cover.no-photo .cover-main { margin-top: 96mm }` vs
`.cover.with-photo .cover-main { margin-top: 26mm }` - two content-height variants, two
tuned numbers, no distribution to redistribute). The redesigned cover keeps this exact
mechanism and extends it: the new full-bleed photo/illustration background sits behind the
card as a plain absolutely-positioned layer (`position: absolute; inset: 0` inside the
already-`position: relative` `.cover`), which participates in no fragmentation distribution
at all and is safe by the same reasoning the existing watermark's `position: fixed` is safe.

### 2.6 Corner-bleed illustrations do not interact with page-break logic

Every page a corner illustration is added to in this design (both covers, the back cover,
chapter openers) is already a single, non-fragmenting page by construction - `.cover`,
`.chapter`, and Book 1's back page are never split across a print boundary, unlike the
multi-page content blocks `pagefit_test.go`'s break rules were tuned against. A corner image
is therefore purely a paint concern (does it fit inside the 12mm safe margin's *visual*
bleed, does it obscure the identity card) and never a fragmentation concern (it does not
change where the page breaks, because there is no break on that page to move).

## 3. Out of scope

- **Recipe photography.** Unchanged and unaffected. GAP-025 stands exactly as documented;
  nothing in this redesign fetches, generates, or embeds a photograph of a specific dish.
- **Any new page content.** The back cover prints the same real facts `end.html` already
  prints. No invented "inside this book" category index, no invented quote, no new provider
  data field - the earlier design mockups explored a category-legend grid and a quote for
  layout-reference purposes only; neither is backed by a real `book1_content_block` grouping
  and neither ships.
- **Licence confirmation for the twelve supplied PNGs.** Flagged, not resolved. The user has
  not yet confirmed Canva Free vs Pro tier for these assets. Do not ship to a real family's
  book before that is confirmed; this plan's asset-embedding task proceeds because the assets
  are needed to build and review the visual design, not because the licence question is
  closed.
- **Interior Book 1 content pages beyond the growth/back-cover pages named above** (the
  daily-life domains, trackers, illness blocks, vaccination schedule, etc.). This redesign
  covers the palette and typeface change project-wide (they read from the same
  `tokens.css` tokens everywhere), but does not add new corner illustrations to every one of
  Book 1's ~25 interior templates - only the pages named in 2.3 get new imagery. Extending
  illustration further is a follow-up, not part of this plan.
