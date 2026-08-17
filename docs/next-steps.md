# Next steps

What to build, in an order that keeps each step verifiable. Written 18 August 2026.

`docs/not-built.md` lists everything missing. This file is opinionated about sequence.
Where the two disagree, this file is the argument and that one is the inventory.

Nothing here is committed to. It is a proposal for a later session to accept, reorder or
reject.

---

## Principle for ordering

Three rules decided the sequence below.

1. **Safety-misleading behaviour is fixed before anything is added.** An interface that
   implies a protection it does not provide is worse than one that provides nothing and
   says so.
2. **Cheap, already-possible work comes before new subsystems.** Five populated columns
   and six accepted-but-unexposed inputs cost hours. The Book Engine costs months.
3. **Do not build on unanswered questions.** Several items below are blocked on the
   provider, and starting them anyway means building twice.

---

## Step 0 - Decide, do not build

None of these are engineering. All of them block engineering.

| Question | Blocks | Who answers |
|---|---|---|
| Do the four unmapped allergens leave the picker, or render as unscreened? | Any operator-facing allergen input | Us, then the provider tags the corpus |
| How many content-block variants, written by whom? | All Book 1 personalization | Provider / product |
| NT00-NT12 or the SRS component weights? | Book 2 recipe selection | Provider |
| Which meal-category names are authoritative? | Book 2 chapter structure | Provider |
| Licence basis for the external recipe corpus | Whether `recipe_method_external` ships at all | Legal / provider |
| When does clinical sign-off happen? | Everything reaching a parent | Provider |

The first is the only one that can be settled unilaterally, and it should be settled
immediately.

---

## Step 1 - Stop the interface implying protections it lacks

**Small, and it is the one genuine safety item on this list.**

- Remove tree nuts, crustacean/mollusc, mustard and sulphites from any allergen picker,
  **or** render a persistent "not screened - no corpus coverage" state beside the field and
  on every result page.
- Add a test asserting that every allergen offered by the API's reference endpoint resolves
  to at least one corpus tag. That test fails today, which is the point.

Verify: declare each of the four, confirm the result count changes or the UI says it did
not.

---

## Step 2 - Wire the inputs that already work

The API accepts six fields the form does not send. No new data, no new endpoints.

| Field | Control | Why it matters |
|---|---|---|
| `region_culture` | Select, 8 options from `/api/reference/regions` | The whole of engine step 7; the West-Bengal-first default is currently unoverridable from the UI |
| `cuisine_code` | Combobox, 27 options from `/api/reference/cuisines` | Ranks by cuisine staples within the parent region |
| `max_prep_time_min` | 4-stop selector: 5 / 10 / 15 / 20 | Free minute entry implies precision the corpus lacks |
| `max_cook_time_min` | 6-stop selector: 10 / 15 / 20 / 25 / 30 / 35 | Same |
| `limit` | Number, default from `meal_category_target` | Currently fixed at 25 |
| `clinical_flags` | Deferred - needs a vocabulary endpoint first | See step 3 |

Verify: each control changes the result set, and the "why this result" panel shows the
corresponding step doing work.

---

## Step 3 - Reference endpoints for the vocabularies that have none

`clinical_flags` cannot get a sane UI until the 28 trigger fields are queryable. Same for
the allergen groups and the enum vocabularies.

Add:

- `GET /api/reference/allergens` - group, corpus tag, and whether it filters anything
- `GET /api/reference/clinical-markers` - the 28 `trigger_field` values with their rule ids
  and effects
- `GET /api/reference/enums` - `diet_type`, `meal_type`, `budget_band`, `season`,
  `texture`, `growth_target`, `post_vaccine_context` with live counts

These were noted as deferred during the Phase 2 final review. They are the precondition for
a form that cannot offer a value the corpus does not have.

---

## Step 4 - The diet ranker

Deferred from the 18 August diet fix. A family declaring non-vegetarian now correctly sees
all 940 recipes, but wants non-vegetarian dishes ranked up rather than merely permitted.

Implement as a rank boost when `recipe_master.diet_type` equals the declared practice
exactly. Magnitude should sit alongside the existing four downstream ranker adjustments in
`internal/engine/rank.go`, which are deliberately small (0.02 to 0.05) so region and budget
preference can never outweigh nutrition.

Verify: a non-vegetarian profile returns 940 candidates with non-vegetarian dishes
concentrated at the top, and a vegetarian profile is unchanged.

---

## Step 5 - Import the Book 1 Content Master

**This is the gate for all of Phase 3.** Output Book 1's entire general content layer -
32 guidance blocks, 44 vaccine rows, 33 milestone rows - exists in
`MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx` and in no database table.

The importer already handles this shape: add sheet-to-table bindings in
`internal/importer/spec.go`, write the DDL in a new migration, and the existing
header-matching and content-hashing machinery does the rest. The migration DDL is the
source of truth for column names, so a header with no column is an error rather than a
silent drop.

Expect the conditional-eligibility columns to be the interesting part - they are what
decides which sections fire for which child.

Verify: row counts match the workbook, a re-import produces identical content hashes, and
the integrity suite covers the new foreign keys.

---

## Step 6 - The canonical child profile

Everything from `ChildProfile` plus the thirteen fields the books need and nothing accepts
(`docs/not-built.md` §2.1). Two are structural:

- **Growth measurements need their own dated table.** One child, many rows. A single
  `weight` column destroys the trend, which is the clinical point of Book 1.
- **Equipment has no recipe-side column.** Accepting the input is useless until the
  provider adds one. Leave it out rather than storing something unusable.

This is also where `child_profile_snapshot` immutability starts mattering: the SRS requires
a released book to name the exact profile snapshot it was generated from.

---

## Step 7 onward - the Book Engine proper

At this point the sequence follows the provider's own roadmap, and the work stops being
incremental. See `docs/phase-3-book-engine.md` §13 for their phasing and §5-§8 for the
state machine, API surface, tables and review gates.

Rough order:

1. Generation job state machine and the `book_engine_case` / `generation_job` tables
2. Book 2 assembler - closest to what already exists, since the recipe engine feeds it
3. Book 1 assembler - depends on step 5
4. Reviewer preview UI - structurally similar to the Phase 2 console, and the natural
   next frontend
5. PDF renderer and the human-designed templates
6. Release governance: immutable records, file hashes, master-version snapshots
7. Multilingual layer

**Do not start 2 or 3 before the questions in step 0 are answered.** Both depend on which
ranking rubric governs and which meal-category names are authoritative, and building
against a guess means building twice.

---

## What this sequence deliberately does not do

- **It does not fill the 6-11 month iron gap.** There is no verified source for a recipe
  the provider did not supply. The honest output is the gap.
- **It does not write preparation text.** 6 unique method texts across 1000 recipes is a
  provider problem. The external corpus join already covers the 166 recipes where a valid
  same-format analogue exists; the remaining 774 correctly get no suggestion.
- **It does not mark anything approved.** `Review_Status` stays verbatim as shipped.
- **It does not add an operator override to the age or allergy filters.** Making the
  machinery visible is not the same as making the safety boundary adjustable.
