# Recipe photo pipeline — design

**Correction (post-brainstorm, during planning):** this pipeline does not close GAP-025.
GAP-025 measures recipes with a commissioned, this-exact-dish photograph, and stays genuinely
open — nothing here claims a photo of *this specific recipe*. What it delivers is a lesser,
honest thing: real photography per dish-**format** (the same 11 archetypes the drawn marks
already use), reused across every recipe sharing a format, exactly the way one `.svg` mark
already illustrates every recipe of its format today. A new gap, GAP-029, is registered for
"no per-recipe commissioned photograph, format-level stock photo only" so the distinction is on
the record rather than implied. See Decision 4 for why a per-recipe table doesn't fit this data
at all (an AI-invented recipe has no `recipe_master` row to key one against).

Precedes Plan 4 (visual redesign) deliberately — Plan 4's Book 2 direction depends on photos
existing to lay out around, and building that layout against a placeholder-shaped hole was
rejected in brainstorming as guessing an aspect ratio and crop behavior before there is a real
photo to measure against.

## Context

`recipe_photo` (migration `0021`) exists and is empty by design: a slot a photograph can land
in without a schema change, `TestRecipePhotographsAreCountedNotAsserted` pinning the live
count against `count(*)` rather than a restated number. Its shape assumes a **human commissions
and uploads** a photo per recipe — `added_by` names an operator, `credit`/`licence` are real
attribution for a real photograph, and there is no column for an algorithmic match's confidence
or source, because the table was never designed to receive one.

CLAUDE.md's "Book 2 has pictures, and they are drawings" section gives four independent reasons
external/real photographs were refused outright rather than deprioritised:

1. A photograph of a *different dish* is a stronger, uncheckable claim than reused method text.
2. The Indian recipe text corpus's upstream licence is unstated — not this dataset's problem,
   but the reason "external photo" read as radioactive by association.
3. The print browser blocks all network by design (`--host-resolver-rules=MAP * ~NOTFOUND`).
4. Printing would mean this service fetching arbitrary remote files into a document handed to a
   family.

This design addresses all four directly rather than reversing the refusal wholesale:

1. is mitigated, not eliminated, by matching at dish-**format** granularity — the same
   discipline `external_format_map`/`recipe_method_card` already use for method text, including
   the hand-check-a-sample rule this project applies to every fuzzy join.
2. is sidestepped by sourcing from datasets with their own identifiable licence (Khana, FoodBD)
   rather than the recipe-text corpus's unstated one. The user has separately waived the
   business risk of operating on those terms without a legal read, so this design does not block
   on researching them further — see Decision 2.
3. and 4. are resolved architecturally, not overridden: the fetch happens once, offline, in an
   enrichment-style batch command — never at print time, never inside the Chromium print tab.
   What reaches the print tab is bytes already sitting in Postgres, embedded as a `data:` URI
   the same way `ChildPhoto.DataURI` already is for cover photos. No new network surface opens
   anywhere near the renderer.

## Decisions

**1. Source datasets: `bharat-raghunathan/indian-foods-dataset` + FoodBD.** Khana, the
originally-considered India source, was dropped after checking its actual licence
(CC BY-NC-ND 4.0 — explicitly NonCommercial and NoDerivatives, worse than the "unstated"
licence problem this project already has an open blocker for, and its paper names no real
download location). In its place: **bharat-raghunathan/indian-foods-dataset** on HuggingFace,
licensed **CC0 — Public Domain** (verified on the dataset card, not assumed) — 15 Indian dish
classes, 4,770 images, no restriction at all, not even attribution. FoodBD stays as planned:
3,523 Bangladeshi meal images, **CC BY 4.0** (verified), hosted on Mendeley Data
(DOI 10.17632/xh3ghf3jbg.2).

Both have a real, verified fetch mechanism, neither requiring a new toolchain dependency
(no Python, no `datasets` library):
- **bharat-raghunathan**: HuggingFace's public `datasets-server` REST API —
  `GET https://datasets-server.huggingface.co/rows?dataset=bharat-raghunathan%2Findian-foods-dataset&config=default&split=train&offset=0&length=100`
  (paginate `offset` in steps of 100) returns JSON rows, each with a `row.label` (an integer
  index into `features[1].type.names`, the 15 class names) and `row.image.src` (a signed,
  directly-downloadable JPEG URL, valid for the immediate fetch). Verified live against the
  real endpoint while writing this spec.
- **FoodBD**: Mendeley's public API — `GET https://data.mendeley.com/public-api/datasets/xh3ghf3jbg`
  returns one unpaginated JSON listing of all 7,054 files (3,523 `.jpg` images, 3,523 matching
  `.txt` YOLO polygon-segmentation labels, `data.yaml` with the 67 canonical class names,
  `FoodBD_Meta_data.csv` with a plain-English `instances` column per image — e.g.
  `FoodBD-0120.jpg,"chicken,khichuri"`), each file carrying its own `download_url` and
  `sha256_hash` already computed by Mendeley. Verified live; the metadata CSV's `instances`
  column is the actual per-image label source, not the polygon files — a FoodBD photo is
  usually a whole meal plate with several items, so **only images whose `instances` list is a
  single dish name are candidates** (a multi-item plate photo doesn't represent any one
  archetype cleanly, and forcing one would be the same wrong-match failure already measured on
  the method-text join).

Storage: `data/external/photos/`, gitignored, checksummed into `SHA256SUMS`, pulled by a new
fetch step (Task detail in the plan). Two new `external_source` rows record both datasets,
matching how IFCT/BD-FCT/the recipe corpus are already tracked. Deliberately **not** a bulk
pull of either corpus — bounded to a handful of candidate images per archetype-relevant label
(see Decision 3), consistent with "a bigger jumbled corpus is worse than a small correct one."

**2. `credit`/`licence` are filled with a placeholder, not researched per-dataset.** The user
explicitly waived the licensing business risk ("we don't care if it's licensed, we're very
small"). Per that waiver, this design does not spend a research pass confirming Khana's and
FoodBD's exact terms before use. The fields stay populated (schema keeps them `NOT NULL`) so no
row is silently missing data, but the *content* is a fixed placeholder string
(`"internal use — recipe photo match pipeline"`) rather than a researched value. This was raised
explicitly in brainstorming — the operator console surfaces `credit`/`licence` per CLAUDE.md's
provenance rule, and a placeholder there is honest about being a placeholder (it does not read
as a real credit line) rather than a fabricated-looking one.

**3. Matching: dish-format bucket, reusing `recipe_format_mark`'s vocabulary — hand-written,
not fuzzy.** Migration `0019` already collapses 28 provider dish formats into 11 drawn-mark
archetypes (`bowl-grain`, `bowl-porridge`, `dish-mash`, `pot-khichdi`, `patty`, `pancake`,
`flatbread`, `wrap`, `finger-bites`, `plate-upma`, `snack`). Both source datasets have small,
closed label vocabularies (15 classes and 67 classes respectively), so the map is hand-written
once, the same way `recipe_format_mark` and `external_cuisine_region_map` are — not a fuzzy
string match. A label with no honest archetype fit is left unmapped (`mark_id IS NULL`, "do not
load"), same convention as `external_cuisine_region_map`'s `NULL` rows: `dal`, `paneer`
(ingredients, not a served format), `noodles`, `salad` never get pulled. See the plan for the
full hand-written table.

**4. Schema: a new `dish_format_photo` table, keyed to the archetype, not to a recipe.**
`recipe_photo` is left completely alone — its `recipe_id` FK targets `recipe_master` only, and
an AI-invented recipe (`ai_recipe`, `AR-#####`) has no `recipe_master` row to reference, so
extending `recipe_photo` the way the original brainstorm proposed cannot actually hold an
invented recipe's photo. Since matching is format-bucket-only anyway (Decision 3), a per-recipe
row was never earning its keep: the same representative photo for "khichdi" is correct for
every khichdi recipe, exactly the way one `.svg` mark file already illustrates every recipe
sharing that `mark_id` today (`marks.go`). `dish_format_photo` holds a handful of candidate
photos per archetype (`mark_id text NOT NULL`), not one per recipe; render time picks the
lowest `photo_id` for a given `mark_id` as today's representative image — deterministic and
reproducible. `credit`/`licence` stay `NOT NULL`, filled with the placeholder per Decision 2.
`recipe_photo` remains reserved for what it was built for: a real, per-recipe commissioned
photograph, added by an operator, still fully supported and untouched.

**5. Render/print: reuse the cover-photo embedding mechanism, not a new one.** At book
assembly, wherever a `mark_id` is already resolved (both `loadRecipeCards` for real recipes and
`inventCard` for AI-invented ones already compute one, to pick the drawn mark), a lookup against
`dish_format_photo` returns its `bytes` base64-encoded into a `data:` URI, typed `template.URL`
for the same reason `ChildPhoto.DataURI` is — `html/template` silently rewrites a plain string
used in an image `src` to `#ZgotmplZ`, and that failure mode is worse than absence.
`recipe.html`'s `.dish-mark` slot gets a minimal conditional: photo if the archetype has one,
the existing drawn mark otherwise, same footprint either way. No layout change — bigger
photo-forward treatment is Plan 4's job once photos exist to design around.

## Architecture

```
data/external/photos/          candidate images for archetype-relevant labels only, gitignored,
        │                       checksummed — NOT a bulk pull of either corpus
        │  cmd/photomatch -fetch  (new: HF datasets-server API + Mendeley public-api,
        │                          both verified live, no new toolchain dependency)
        ▼
photo_label_archetype_map      hand-written seed table (migration 0026): dataset label -> mark_id,
                                 NULL = deliberately unmapped, same shape as external_cuisine_region_map
        │
cmd/photomatch                 default mode: reads the manifest + the map, selects up to N
                                 candidates per archetype, writes dish_format_photo
        ▼
dish_format_photo (new, migration 0026)   -- keyed to mark_id, not recipe_id
        │  read at book assembly time only, by mark_id lookup
        ▼
internal/book/photo.go (extended) — base64 -> data: URI -> template.URL, RepresentativePhoto(markID)
        ▼
recipe.html — <img> if the archetype has a photo, .dish-mark SVG otherwise
```

`internal/importer`'s upsert-and-sweep never touches `dish_format_photo` or
`photo_label_archetype_map` — it iterates provider workbook sheets only, same reason `ai_recipe`
(Plan 2) is a parallel table rather than living in a swept one.

## Data flow and error handling

- An archetype with no mapped label in either dataset gets no photo, same as the method-text
  join's "no mapping, no suggestion, ever." Not an error, not a placeholder image — a gap, same
  discipline as everywhere else in this project.
- FoodBD candidates are restricted to images whose `FoodBD_Meta_data.csv` `instances` column
  names exactly one dish (see Decision 1) — a multi-item plate photo is rejected as a candidate
  for any single archetype rather than forced.
- `cmd/photomatch` (default mode) is idempotent the same way `cmd/import`/`cmd/enrich` are:
  re-running clears and rewrites `dish_format_photo` from the current manifest + map rather than
  accumulating duplicates.
- This pipeline does **not** move GAP-025 (see the correction at the top of this document) — it
  registers a new gap, GAP-029, for "no per-recipe commissioned photograph, format-level stock
  photo only," and that new gap is what should be re-measured as `dish_format_photo` gains rows.

## Testing

- Unit tests for the hand-written label→archetype map: every mapped label resolves to one of
  the 11 real `mark_id`s (fails loudly on a typo against `MarkIDs()`); every deliberately
  unmapped label resolves to no candidate rows, not a wrong one.
- A unit test for the FoodBD single-item filter: an `instances` value of `"khichuri"` is a
  candidate for `pot-khichdi`; `"chicken,khichuri"` is rejected for any archetype.
- `cmd/photomatch -sample 20`: prints 20 stored archetype/label/source pairs for a human to
  read, same shape as `cmd/enrich -sample 20`.
- A render test: a recipe whose `mark_id` has a `dish_format_photo` row emits an `<img>` with a
  `data:` src; a recipe whose archetype has none still emits the existing `.dish-mark` SVG —
  covers both real (`loadRecipeCards`) and AI-invented (`inventCard`) card paths, since both
  resolve a `mark_id` today.
- `go build ./...`, `go vet ./...`, `go test ./...` (`TEST_DATABASE_URL` set), plus
  `scripts/dev_db.fish` round-trip to confirm migration `0026` applies and rolls back cleanly.

## Files involved

- `data/external/photos/`, `data/external/SHA256SUMS` (extended)
- `internal/db/migrations/0026_dish_format_photo.{up,down}.sql` (new table
  `dish_format_photo`, new table `photo_label_archetype_map` + seed rows, two new
  `external_source` rows, one new `gap_register` row for GAP-029)
- `cmd/photomatch/main.go` (new)
- `internal/photomatch/` (new package: `photomatch.go` types/label-map/manifest,
  `fetch.go` the network stage, `match.go` the DB-writing stage — mirrors `internal/enrich`'s
  split between stages, not its exact filenames)
- `internal/book/photo.go` (extended: `RepresentativePhoto(ctx, pool, markID)` alongside the
  existing cover-photo `ParsePhoto`)
- `internal/book/types.go` (`RecipeCard.Photo *RecipePhoto`, parallel to `Mark`)
- `internal/book/book2.go` (`loadRecipeCards` resolves `card.Photo` alongside `card.Mark`)
- `internal/book/invented.go` (`inventCard`/`findStoredInventedCard` resolve `.Photo` the same
  way)
- `internal/book/templates/book2/recipe.html` (minimal conditional swap)

## Explicitly out of scope

- Plan 4's bigger photo-forward layout, warmer Book 2 treatment, accent colour — deferred until
  this pipeline has actually produced photos to design against.
- Tighter per-dish visual matching (embeddings, ingredient re-ranking within a format bucket) —
  format-only match is the deliberate first cut, same as the text join's first working
  threshold; can be revisited if the hand-check sample shows format-only is too loose.
- Researching bharat-raghunathan's/FoodBD's actual licence terms further — both were already
  checked directly (Decision 1: CC0 and CC BY 4.0 respectively), so there is nothing left to
  research; the placeholder in Decision 2 is about not citing the real terms *in the database*,
  not about not knowing them.
- A per-recipe commissioned photograph pipeline — that is `recipe_photo`'s original,
  untouched purpose and GAP-025's actual subject; nothing here automates it.
