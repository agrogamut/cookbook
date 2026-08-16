# MadamGY Recipe Engine

A constraint-driven pediatric recipe finder. A parent or clinician enters a child profile
- age, diet pattern, allergies, clinical condition, region, budget - and the engine
returns the recipes that are safe and appropriate for that child.

Regional scope is **India and Bangladesh**, weighted toward West Bengal and Bengali
cuisine.

Internal tool. Staff use it to serve families; families never use it directly.

## Status

Phase 1 (make the data usable) is built. There is no API and no frontend yet - see
`CLAUDE.md` for the plan and the current task list.

## Running it

Requires Go 1.24+, Docker and fish.

```fish
scripts/fetch_data.fish          # download + checksum the external datasets
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)

go run ./cmd/import              # provider workbooks -> Postgres
go run ./cmd/enrich              # external datasets  -> annotation tables
go run ./cmd/enrich -sample 20   # read the weakest matches before trusting them

TEST_DATABASE_URL=$DATABASE_URL go test ./...
```

The importer is idempotent: running it twice over unchanged workbooks leaves an identical
database, verified by a content hash per table.

## Running the API

```fish
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/server        # listens on :8080 by default, set PORT to override
```

| Endpoint | Method | What it does |
|----------|--------|---------------|
| `/api/search` | POST | Runs the 14-step engine against a child profile, returns ranked recipes plus full step accounting |
| `/api/recipes/{id}` | GET | Provider method + external suggestion + provider/corrected nutrition |
| `/api/ingredients` | GET | Provider and IFCT-corrected nutrition side by side |
| `/api/audit/nutrition` | GET | The confirmed discrepancy findings to hand the provider |
| `/api/gaps` | GET | The full gap register |
| `/api/runs` | GET | Import history and content hashes |
| `/api/reference/regions` | GET | The 9 scoped regions and their tiers |
| `/api/reference/cuisines` | GET | Cuisine dropdown, guaranteed non-empty per entry |
| `/api/reference/nutrition-targets` | GET | The NT00-NT12 rubric, weights and guidance text |

Useful views once loaded:

| View | What it answers |
|------|-----------------|
| `recipe_method_card` | The provider's method and the external suggestion side by side, with the disclosure text |
| `nutrition_discrepancy_report` | Where the provider disagrees with IFCT 2017 by more than 20%, same-food matches only |
| `cuisine_option` | The cuisine dropdown, built so it can never offer an empty page |
| `ingredient_nutrition_corrected` | Per-ingredient nutrition with IFCT values substituted where the food is identified, provider values alongside |
| `recipe_nutrition_recomputed` | Recipe nutrition rebuilt from ingredient quantities, with a coverage fraction |
| `recipe_ranked` | Recipes scored against each nutrition target |
| `gap_register` | Every known hole, counted |

## The data

Thirteen Excel workbooks in this directory, authored by an external clinical data
provider: 1000 recipes, 406 ingredients, 3317 recipe-ingredient mappings, plus clinical
rule, allergy/safety, nutrition target, age/feeding stage and culture masters.

Scope is India and Bangladesh, so 940 of the 1000 recipes are loaded. Scope lives in one
seed table (`region_focus`); the workbooks are never edited.

**The provider's data is provisional.** Every recipe carries `Review_Status = Draft` and
every ingredient `Data_Quality = Provisional planning value`. Those flags are preserved
verbatim and surfaced in the UI; nothing is ever marked approved on this side.

The nutrition columns are the clearest case: 406 ingredients carry only 76 distinct value
sets, assigned per food group rather than per ingredient, so hilsa and tilapia get
identical numbers. A corrected layer substitutes IFCT 2017 values for the 139 ingredients
whose food can be identified, and flags the rest as unverified. `ingredient_master`
itself is never modified.

### Ground rule: nothing is invented

Every value that reaches a user traces to the provider's workbooks, to a named external
dataset with its row id and URL recorded, or to a documented computation over those -
labelled `derived`. Where data is missing the output is an explicit gap, never a
plausible-looking substitute. The known gaps are enumerated in the `gap_register` table
and re-counted on every import.

## Source attribution

### In use

**Indian Food Composition Tables 2017** - ICMR-National Institute of Nutrition,
Hyderabad. <https://www.nin.res.in/ebooks/IFCT2017.pdf>, obtained via
<https://huggingface.co/datasets/NUTRIC/IFCT-2017-Data>. 542 foods. The source the
provider cites for 320 of its 406 ingredients, and the reference for the nutrition audit.
Energy is converted from kJ (divide by 4.184) and minerals from g to mg per 100 g; both
conversions are exact and documented in the migration.

**Anupam007/indian-recipe-dataset** -
<https://huggingface.co/datasets/Anupam007/indian-recipe-dataset>. 5,938 Indian recipes
across 82 cuisines, derived from Archana's Kitchen; 3,970 fall inside the India +
Bangladesh scope and are loaded. Used for suggested preparation methods, shown alongside
the provider's text and never replacing it. Matched on dish format first and ingredients
second, so a suggestion is a method for the same kind of dish, not for the same dish.

### Named by the provider, not independently loaded

The ingredient master cites these as its own sources. They are recorded in
`ingredient_source_register` but are not currently used to verify anything, so nothing in
the database is derived from them.

- **Food Composition Table for Bangladesh 2013** - INFOODS / FAO. Cited for 29 ingredients.
- **USDA FoodData Central** - U.S. Department of Agriculture, public domain (CC0).
  <https://fdc.nal.usda.gov/> - cited for 57 ingredients.

### Planned, not yet used

- **Open Food Facts** - ODbL 1.0. <https://world.openfoodfacts.org/data> - intended for
  an allergen cross-check on the 317 ingredients tagged "None identified in starter
  tagging". Not loaded yet, so those ingredients remain unscreened and are shown as such.

### Clinical and normative guidance

- **WHO** guideline for complementary feeding of infants and young children 6-23 months
  (2023), WHO child growth standards, and WHO IYCF indicators (2021).
- **IAP / ACVIP** recommended immunization schedule (2025), Indian Pediatrics.

Evidence identifiers and URLs as cited by the provider are imported verbatim into
`evidence_reference_master` and the per-master evidence registers.

## Licence

Sources are attributed above because their licences ask for it. Note that the recipe
corpus carries no stated upstream licence, which is unresolved - see "Scope" in
`CLAUDE.md`. IFCT 2017 is openly published by ICMR-NIN.
