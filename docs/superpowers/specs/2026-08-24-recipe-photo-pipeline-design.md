# Recipe photo pipeline — design

Closes GAP-025 (recipe photography, currently 0 of 940 recipes). Precedes Plan 4 (visual
redesign) deliberately — Plan 4's Book 2 direction depends on photos existing to lay out
around, and building that layout against a placeholder-shaped hole was rejected in
brainstorming as guessing an aspect ratio and crop behavior before there is a real photo to
measure against.

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

**1. Source datasets: Khana + FoodBD, following the `data/external/` convention exactly.**
Khana (131k images, 80 dish labels, India) covers the India-scope recipes; FoodBD (3,523 meal
images) covers the 100 Bangladeshi recipes region_focus already scopes in. Both were already
named as Tier 2 candidates in CLAUDE.md's external-datasets section, for this exact use.
Storage: `data/external/photos/`, gitignored, checksummed into `SHA256SUMS`, pulled by
`scripts/fetch_data.fish` alongside every other external source. Two new `external_source` rows
record dataset name and fetch date, matching how IFCT/BD-FCT/the recipe corpus are already
tracked.

**2. `credit`/`licence` are filled with a placeholder, not researched per-dataset.** The user
explicitly waived the licensing business risk ("we don't care if it's licensed, we're very
small"). Per that waiver, this design does not spend a research pass confirming Khana's and
FoodBD's exact terms before use. The fields stay populated (schema keeps them `NOT NULL`) so no
row is silently missing data, but the *content* is a fixed placeholder string
(`"internal use — recipe photo match pipeline"`) rather than a researched value. This was raised
explicitly in brainstorming — the operator console surfaces `credit`/`licence` per CLAUDE.md's
provenance rule, and a placeholder there is honest about being a placeholder (it does not read
as a real credit line) rather than a fabricated-looking one.

**3. Matching: dish-format bucket, reusing `recipe_format_mark`.** Migration `0019` already
collapses 28 provider dish formats into 11 drawn-mark archetypes. The photo pipeline buckets
Khana/FoodBD images into the same 11 archetypes (by their own dataset labels, hand-mapped once
the same way `external_format_map` was), then assigns any recipe row — real (`recipe_master`)
or AI-invented (`ai_recipe`, both carry a resolvable dish-format) — a candidate image from its
bucket. This is deliberately the same granularity as the method-text join, not tighter: format
match only, no per-dish ingredient re-ranking, on the reasoning that a khichdi photo for a
khichdi recipe is the same order of claim the text join already ships today. A `-sample 20` flag
on the new command lets an operator hand-check a batch before trusting it at scale, matching the
project's own established rule for every fuzzy join.

**4. Schema: extend `recipe_photo`, do not replace it.** A small migration (`0026` — `0024` and
`0025` are claimed by concurrent work on this branch) adds three nullable columns:

```sql
ALTER TABLE recipe_photo
  ADD COLUMN source_dataset  text,
  ADD COLUMN source_row_id   text,
  ADD COLUMN match_confidence numeric;
```

Null on all three means a genuinely commissioned photo (today's intended path, still fully
supported). Populated means an algorithmic format-bucket match. `added_by` is
`'photo-match-pipeline'` for pipeline-written rows, an operator's name for a real commission —
same column, two honestly-distinguishable populations, matching how `recipe_method_card`
already separates a provider value from a suggested one via source columns rather than a
parallel table.

**5. Render/print: reuse the cover-photo embedding mechanism, not a new one.** At book
assembly, a recipe with a `recipe_photo` row gets its `bytes` base64-encoded into a `data:` URI,
typed `template.URL` for the same reason `ChildPhoto.DataURI` is — `html/template` silently
rewrites a plain string used in an image `src` to `#ZgotmplZ`, and that failure mode is worse
than absence. `recipe.html`'s `.dish-mark` slot gets a minimal conditional: photo if present,
the existing drawn mark otherwise, same footprint either way. No layout change — bigger
photo-forward treatment is Plan 4's job once photos exist to design around.

## Architecture

```
data/external/photos/          raw Khana + FoodBD images, gitignored, checksummed
        │  scripts/fetch_data.fish (extended)
        ▼
cmd/photomatch/                new command, cmd/enrich's shape (config.Load, signal.NotifyContext,
                                flag package, -sample N)
        │
        ├─ buckets source images into the 11 recipe_format_mark archetypes
        ├─ for each recipe_master + ai_recipe row: pick a candidate from its bucket
        ├─ writes recipe_photo (bytes, credit/licence placeholder, source_dataset,
        │  source_row_id, match_confidence, added_by='photo-match-pipeline')
        ▼
recipe_photo (extended, migration 0026)
        │  read at book assembly time only
        ▼
internal/book/photo.go (extended) — base64 → data: URI → template.URL
        ▼
recipe.html — <img> if present, .dish-mark SVG otherwise
```

`internal/importer`'s upsert-and-sweep never touches `recipe_photo` — it iterates provider
workbook sheets only, same reason `ai_recipe` (Plan 2) is a parallel table rather than living in
a swept one.

## Data flow and error handling

- A recipe whose format has no bucket (no Khana/FoodBD image labelled anything in that
  archetype) gets no photo, same as the method-text join's "no mapping, no suggestion, ever."
  Not an error, not a placeholder image — a gap, same discipline as everywhere else in this
  project.
- A bucket match below the confidence threshold is rejected, not forced — mirrors the ≈0.6
  Jaccard floor already used for the recipe-text join, tuned during the hand-check pass rather
  than picked in advance.
- `cmd/photomatch` is idempotent the same way `cmd/import`/`cmd/enrich` are: re-running upserts
  on `recipe_id` rather than duplicating rows, and a recipe already carrying a **commissioned**
  photo (`source_dataset IS NULL`) is never overwritten by a pipeline match — a real photograph
  always wins over a matched one.
- GAP-025's `measured_by = 'importer'` wiring (already built, migration `0021`'s trailing
  comment) starts reporting a real, moving count instead of the static 940 the moment this
  pipeline writes its first row — no separate gap-register change needed.

## Testing

- `internal/enrich`-style unit tests for the bucket assignment (given a dataset label, resolves
  to the right of the 11 archetypes; an unrecognised label resolves to no bucket, not a wrong
  one).
- `cmd/photomatch -sample 20`: prints 20 random assigned pairs (recipe title/format vs. matched
  image's dataset label) for a human to read, same shape as `cmd/enrich -sample 20`.
- `TestRecipePhotographsAreCountedNotAsserted` (existing) starts asserting a count below 940
  once the pipeline has run against a seeded test database — pins that the gap is closing rather
  than static.
- A render test: a recipe with a `recipe_photo` row emits an `<img>` with a `data:` src; a
  recipe without one still emits the existing `.dish-mark` SVG. Confirms the fallback path
  never breaks for the ~940 minus however many the format-bucket coverage reaches.
- `go build ./...`, `go vet ./...`, `go test ./...` (`TEST_DATABASE_URL` set), plus
  `scripts/dev_db.fish` round-trip to confirm migration `0026` applies and rolls back cleanly.

## Files involved

- `data/external/photos/`, `data/external/SHA256SUMS` (extended), `scripts/fetch_data.fish`
  (extended)
- `internal/db/migrations/0026_recipe_photo_match_provenance.{up,down}.sql`
- `cmd/photomatch/main.go` (new)
- `internal/photomatch/` (new package: bucket.go, match.go, run.go — mirrors `internal/enrich`'s
  shape)
- `internal/book/photo.go` (extended: recipe-photo data-URI helper alongside the existing
  cover-photo one)
- `internal/book/templates/book2/recipe.html` (minimal conditional swap)
- `internal/importer/gaps.go` — confirm GAP-025's live-count wiring picks up the new table
  (should already work per migration `0021`'s own comment; verify rather than assume)

## Explicitly out of scope

- Plan 4's bigger photo-forward layout, warmer Book 2 treatment, accent colour — deferred until
  this pipeline has actually produced photos to design against.
- Tighter per-dish visual matching (embeddings, ingredient re-ranking within a format bucket) —
  format-only match is the deliberate first cut, same as the text join's first working
  threshold; can be revisited if the hand-check sample shows format-only is too loose.
- Researching Khana's/FoodBD's actual licence terms — placeholder per Decision 2, unless the
  user revisits the waiver.
