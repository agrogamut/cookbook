# AI recipe union: bring `ai_recipe` into the engine pipeline

**Status:** designed, not started. Blocked on `ai_recipe`'s migrations reaching git.

**Goal:** a verified AI recipe becomes a real candidate the engine filters and ranks like any
other, instead of `invented.go` generating one live through Gemini at print time.

## Why this exists

Three batches of corpus fill produced 71 `ai_recipe` rows, every one grounded in real
`ingredient_master` ingredients, macro-consistency checked, allergen re-derived from its own
ingredients, and reversible per batch. Every reachable Breakfast/Lunch/Dinner cell across all
four age bands now sits at or above the provider's 25-recipe target.

None of it reaches a book. Step 1 builds the candidate pool from `SELECT recipe_id FROM
recipe_master`, so an `ai_recipe` row is not a candidate and never was.

The gap this closes is measurable and was the original complaint. A 2-5 year old's Breakfast
chapter has 17 in-band real recipes against a 25 target, so `applyAgeRank`'s partition runs out
of in-band recipes and the chapter fills with out-of-band ones - a 13-18 year recipe in a
toddler's book. The union makes an in-band AI recipe available before that happens.

## The two decisions this design rests on

### 1. AI recipes partition below real ones; they never interleave by score

`recipe_target_score` normalises min-max **within an age band**, reading
`recipe_nutrition_normalised`, which bands on `recipe_master`'s own per-serving columns. Those
are the provider's group-level placeholder values - 406 ingredients carrying 76 distinct value
sets. `ai_recipe_derived` computes real nutrition from ingredient quantities against
`ingredient_nutrition_corrected`.

So a 0.72 derived from real numbers and a 0.72 derived from placeholders are not the same
measurement, and min-max makes this worse rather than better: the scale is defined entirely by
the two extreme values, so one placeholder at the top of a band sets the ceiling every other
recipe is measured against.

This is the same shape as the age-band problem, and takes the same answer the 2026-09-05
amendment already gave: **partition, do not score across incomparable bases.** No constant in
`rank.go`'s family can be correct here for the same reason it could not be correct there.

AI recipes score among themselves, within their own partition, against their own normalisation.

### 2. Age dominates source

Two partitions now exist. They are ordered:

```
1. age in band          (safety-adjacent; was a hard filter until 2026-09-05)
2. provider over AI     (data-quality preference)
3. ranked score         (within the resulting group)
```

An **in-band AI recipe beats an out-of-band real recipe**, deliberately. That ordering is the
entire value of the corpus fill: without it a short chapter still reaches for a teenage recipe
before it reaches for an age-appropriate AI one, and the 71 rows buy nothing.

The reverse ordering was considered and rejected. Preferring provider data is a preference
about provenance; printing an age-inappropriate recipe is a fact about the child.

## Design

### Two views

`engine_candidate` - `recipe_master` UNION ALL the shape-matching columns of
`ai_recipe_derived`, plus `source` (`'provider'` | `'ai'`). `ai_recipe_derived` was built to
match `recipe_master`'s column shape for exactly this.

`engine_candidate_ingredient` - `recipe_ingredient_mapping` UNION ALL `ai_recipe_ingredient`.
The two are not the same shape and the difference matters: `recipe_ingredient_mapping` carries
a denormalised `ingredient_allergen_tag`, `ai_recipe_ingredient` carries only `recipe_id`,
`ingredient_id`, `quantity_g`. The tag is recovered for AI rows by joining `ingredient_id` to
`ingredient_master.allergen_tags` - which is arguably better than the corpus's copy, since it
cannot drift from `ingredient_master`.

### Repointing

Every engine query reading `recipe_master` by candidate id moves to `engine_candidate`:
`steps_hard.go` (3), `diet.go` (2), `rank.go` (3+). This is not optional and it is the part
most likely to be got wrong quietly.

**Step 2's allergen filter is the one to be careful with.** It is a keep-list -
`SELECT r.recipe_id FROM recipe_master r WHERE r.recipe_id = ANY($1) AND NOT EXISTS (...)` -
so an AI id put into the pool at step 1 and left unrepointed here is not returned, and vanishes
silently and completely. A half-done union looks like it works and returns zero AI recipes. It
fails safe rather than dangerously, but it fails invisibly, so the conservation check below
matters more than usual.

### Scoring the AI partition

`recipe_target_score` reads `recipe_nutrition_normalised` and `recipe_composition_normalised`,
both keyed on `recipe_master`. AI rows have no score at all today.

Under decision 1 they only need ordering among themselves, so a parallel normalisation over
`ai_recipe_derived` is sufficient and explicitly does **not** need to be commensurable with the
real one. This is the piece that makes the partition cheap: were the two required to be
comparable, the whole placeholder problem would come back.

### `invented.go`

Live Gemini invention stays as the last resort, but it is reached far less often. Its parallel
allergen re-check goes: once AI rows pass step 2 with every other candidate, re-checking them
separately is a second implementation of a safety filter, which is the thing most worth having
only one of.

## What this does not touch

- **Confirmed allergens (step 2) and declared diet (step 4) stay hard filters**, applied to AI
  rows identically. The union's whole safety argument is that AI rows go through the same
  filters rather than around them.
- **The hard rule on inventing data values.** `ai_recipe` rows are constrained to real
  `ingredient_master` rows and their nutrition is derived, not asserted.
- **Chapter reachability.** `Snack` and `Recovery Meal` map to no Book 2 chapter, so recipes in
  those categories stay unreachable whatever the union does. That is GAP-023 and a separate
  ruling - see below.

## Adjacent open question, not part of this work

`meal_category_recipe_map` maps three of seven provider chapters, so 354 of 940 real recipes
reach no chapter. The provider's own `include_logic` resolves most of it:

- **MC-04 "Tiffin / school snack"** - "When daycare/school/portable meal relevant", min 18
  months, "School allergy/storage rules apply". One-to-one with the corpus's `School Tiffin`,
  99 recipes. A reading of the provider's own definition.
- **MC-02, MC-05, MC-07** - all conditional on the *feeding schedule* ("Only if feeding
  schedule includes it", "When schedule requires", "Only if nutrition plan specifically
  includes it"), which this project does not collect. The corpus's single generic `Snack` type
  spans all three; assigning it to one would invent a schedule fact. MC-07's own note says
  "Do not create an unnecessary extra feed merely to fill book."

Recommendation is to accept MC-04 and reject the rest. It is not done here because migration
`0016` restricted `basis` to `provider-identical-name` and `provider-ruling` and stated
plainly, in a column comment, "No other basis is permitted - an inferred mapping is an invented
one." MC-04 is neither. Adding a third basis reverses a deliberate recorded decision, and this
project's convention is that those are re-decided in the open.

## Verification

The failure mode is silence, so the checks are conservation checks:

- A profile whose in-band real pool is short must return AI recipes, and they must be in band.
- A confirmed allergen must exclude an AI recipe carrying that allergen through an ingredient,
  by the same step 2 that excludes a real one - asserted against an AI row specifically, not
  inferred from the real corpus passing.
- A declared diet must exclude a non-matching AI recipe at step 4.
- Every AI recipe in a result must sort below every in-band real recipe, and above every
  out-of-band one.
- The step list must account for AI candidates at every step, so a silent drop shows up as a
  count that does not reconcile rather than as a shorter book.
