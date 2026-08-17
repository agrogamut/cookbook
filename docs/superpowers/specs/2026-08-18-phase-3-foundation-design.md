# Phase 3 foundation - design

Written 18 August 2026. Covers the work that stands between the built Phase 2 engine and a
Book Engine, restricted to what is **not blocked on an unanswered provider question**.

Phase 1 (data) and Phase 2 (Go API + Next.js operator console) are built and merged.
`docs/phase-3-book-engine.md` describes the destination. This document describes the
foundation: the safety behaviour, the inputs, the Book 1 data layer and the persisted child
profile that any book assembler will need, none of which depend on a decision somebody else
has to make.

---

## Scope

### In

| # | Area | Why now |
|---|---|---|
| 1 | Allergen screening state made structural | Safety-adjacent, and the only item settleable unilaterally |
| 2 | Specialist-hold wiring | Provider columns already imported, read by nothing |
| 3 | The six accepted-but-unexposed engine inputs | No new data, no new endpoints |
| 4 | Three reference endpoints | Precondition for a form that cannot offer an absent value |
| 5 | Diet ranker | Deferred from the 18 August diet fix |
| 6 | `Book1_Content_Master` import, all nine sheets | The gate for every Book 1 capability |
| 7 | Canonical child profile and dated growth measurements | The gate for every book page |

### Out, and why

| Excluded | Blocked on |
|---|---|
| Reconciling NT00-NT12 with the SRS component weights | Provider ruling (`not-built.md` §4.1) |
| Meal-category name reconciliation | Provider ruling (`not-built.md` §3.5) |
| Book 1 / Book 2 assemblers | Both of the above |
| Content-block variant authoring | Unassigned (`not-built.md` §4.3) |
| Generation state machine, review gates, release records | Assemblers first |
| PDF renderer and templates | Assemblers first |
| Multilingual layer | Language Master does not exist in any imported table |
| Extending the congenital condition list | Provider work; cannot be invented (`not-built.md` §1.3) |
| Anything marking data approved | Clinical sign-off outstanding |

Nothing here writes recipe text, fills a nutrition gap, or marks a row reviewed. The hard
rule in `CLAUDE.md` governs every line of it.

---

## 1. Allergen screening becomes a structural field

### What is already true

`docs/not-built.md` §1.1 is stale and should be corrected as part of this work. Migration
`0011_allergen_tag_vocabulary` already exists and already:

- bridges `allergen_mapping.allergen_group` to the corpus tag strings, fixing the real
  naming bug (`Wheat` -> `Gluten-containing cereal`, which previously excluded zero
  recipes);
- records `corpus_tag IS NULL` for the four groups the corpus genuinely does not tag
  (Tree nuts, Crustacean/Mollusc, Mustard, Sulphites);
- and `internal/engine/steps_hard.go` already queries it, already rejects an unrecognized
  allergen with `ErrInvalidProfile`, and already names the unscreened groups.

### What is still wrong

The naming lands in `StepResult.Note` - free text, on step 2, visible only if an operator
opens the "why this result" sheet. Three consequences:

1. A UI can render a complete, plausible result page with no indication that a declared
   allergen screened nothing.
2. Nothing type-checks. A frontend that forgets the note is not a compile error and not a
   test failure.
3. The profile form's allergen input is a comma-separated free-text box. `Tree nut`
   (singular) returns 400; `Tree nuts` is accepted and screens nothing. The operator cannot
   tell those two outcomes apart from the input alone.

### Decision

**Promote the unscreened set from a note to a field on `EngineResult`.**

```go
// UnscreenedAllergens names declared allergen groups that have no tag anywhere in the
// recipe corpus (allergen_tag_vocabulary.corpus_tag IS NULL). These excluded zero recipes
// because nothing carries the tag, not because the filter passed. Any client rendering a
// result set MUST render this; a result page that omits it implies a screening that did
// not happen.
UnscreenedAllergens []string `json:"unscreened_allergens,omitempty"`
```

Rationale: `CLAUDE.md`'s frontend rule 3 is "provenance is a column, never a footnote". An
absent screen is provenance. A field survives refactors and can be asserted; a sentence in
a note cannot. The existing `StepResult.Note` stays as the human-readable long form - this
adds a machine-readable short form beside it, it does not replace it.

**Keep the four groups selectable.** Removing them means an operator with a tree-nut
allergic child has nowhere to record the fact and no signal that the corpus cannot screen
it. An explicit gap beats a silent absence - the same reasoning `CLAUDE.md` applies to
`null` nutrition values.

**Add `GET /api/reference/allergens`** returning `allergen_group`, `corpus_tag` and a
`screens bool`, so the picker is built from the vocabulary table and cannot offer a value
that 400s. This replaces the free-text input with a multi-select.

**Add a failing-today test** asserting every group offered by the reference endpoint has a
non-NULL `corpus_tag`. It fails on four rows. That is the point: it is the tracking
mechanism for the provider request, and it turns green only when they tag the corpus. It
must not break CI, so it skips with an explicit message naming the new gap id below - a
skipped test naming a gap is a live reminder; a deleted one is not.

**The gap register does not currently record this at all**, which is worth stating plainly
because the register's entire claim is that it accounts for every known hole. Add
`GAP-013`, severity `blocker`: four declared allergen groups have no corpus tag, measured
as `count(*) FROM allergen_tag_vocabulary WHERE corpus_tag IS NULL` so it re-counts on
every import and reaches zero on its own when the provider tags the corpus.

---

## 2. Specialist hold, from provider columns

### What exists

`clinical_rule_master` is imported with `specialist_required`, `human_approval_level`,
`clinical_escalation_yn`, `engine_action` and `do_not_do` populated. `internal/engine/`
reads none of them - `clinical.go` blocks on a hand-written `escalationOnlyDomains` map
instead. `EngineResult.Blocked` and `BlockReason` work and are already surfaced by the
console.

### Decision

**Drive the hold from `specialist_required` rather than the hand-written domain map.**
Reading a populated provider column is not inventing data; hardcoding a domain list in Go
that can drift from the workbook is the weaker position. The map becomes a fallback for
rows where the column is empty, and a test asserts the two agree on the current corpus so
a drift is caught rather than silently preferred.

**Do not extend the condition list.** Down syndrome, cerebral palsy, congenital heart
disease, cleft lip and palate, autism and intellectual disability have no row in
`clinical_rule_master`. Adding them here would be inventing clinical scope. The engine
covers what the masters name; the list stays outstanding to the provider
(`not-built.md` §6 question 10).

The register does not carry this either. Add `GAP-015`, severity `blocker`: named
congenital and neurodevelopmental conditions have no rule row, so a child who has one is
scored like any other child. Unlike `GAP-013` this one cannot re-count itself - there is no
query that measures the absence of conditions nobody has listed - so it is seeded with a
`NULL` count and a `resolution_path` naming the provider, in the same shape as the existing
`GAP-010` governance row.

**Surface `human_approval_level` in `BlockReason`.** An operator told "this needs a
specialist" should see which specialist the provider named.

---

## 3. Wire the six accepted-but-unexposed inputs

The API accepts 13 `ChildProfile` fields; the form sends 7. No new data, no new engine
work - these already run.

| Field | Control | Source of options |
|---|---|---|
| `region_culture` | Select | `GET /api/reference/regions` (exists) |
| `cuisine_code` | Combobox grouped by region | `GET /api/reference/cuisines` (exists) |
| `max_prep_time_min` | 4-stop selector: 5 / 10 / 15 / 20 | New `/api/reference/enums` |
| `max_cook_time_min` | 6-stop selector: 10 / 15 / 20 / 25 / 30 / 35 | New `/api/reference/enums` |
| `limit` | Number, default from `meal_category_target` | New `/api/reference/enums` |
| `clinical_flags` | Multi-select of trigger fields | New `/api/reference/clinical-markers` |

Time controls are **stop selectors, not free minute entry**. The corpus holds four and six
distinct values; a free field implies precision the data has not got.

`region_culture` and `cuisine_code` are the valuable pair - they are the whole of engine
step 7, and the West-Bengal-first default is currently unoverridable from the UI even
though `applyCultureRank` already implements the override.

---

## 4. Reference endpoints for the vocabularies with none

Three new read-only endpoints, same shape as the existing three:

| Endpoint | Returns |
|---|---|
| `GET /api/reference/allergens` | 11 groups, corpus tag, `screens bool` |
| `GET /api/reference/clinical-markers` | 28 `trigger_field` values, rule ids, `engine_action`, `specialist_required` |
| `GET /api/reference/enums` | `diet_type`, `meal_type`, `budget_band`, `season`, `texture`, `growth_target`, `post_vaccine_context`, plus distinct prep/cook times, each with a live `COUNT(*)` |

Every one reads live counts. A vocabulary with a zero count is returned with its zero
rather than omitted, so a client can choose between hiding it and disabling it - the
`cuisine_option` precedent, which filters on `COUNT(*) > 0` inside the view because a
cuisine with no recipes is a broken option rather than an informative one. Enums are
different: `season = 'Winter-friendly'` with 220 recipes and a hypothetical zero-count
value are both facts about the corpus an operator may want to see.

---

## 5. Diet ranker

Deferred deliberately from the 18 August nesting fix. A family declaring non-vegetarian now
correctly sees all 940 recipes; page one is still dal.

Boost recipes whose `diet_type` equals the declared practice exactly. Magnitude sits with
the existing downstream adjustments in `internal/engine/rank.go` (0.02-0.05 in the
normalised space, well below the culture boost) so preference never outranks nutrition or
age-appropriateness.

New step in the recorded accounting, `Kind: "ranker"`, so the why-panel shows it working.

Verify: a non-vegetarian profile returns 940 candidates with non-vegetarian dishes
concentrated at the top; a vegetarian profile is byte-identical to today.

---

## 6. Import `Book1_Content_Master`

**This is the gate for every Book 1 capability, and the workbook is richer than
`CLAUDE.md` records.** Read live from the file, not from prose:

| Sheet | Rows | Header row | What it is |
|---|---|---|---|
| Book 1 Content Master | 32 blocks | 4 | The block registry, 24 columns |
| IAP Vaccination 2025 | 44 | 4 | IAP-ACVIP 2025 schedule, parent-writable |
| Development Milestones | 33 | 4 | Milestone surveillance references |
| Parent Monitoring Templates | 18 | 3 | Reference-vs-actual template definitions |
| Illness Feeding Content | 5 | 3 | Supportive feeding blocks per illness |
| Book Assembly Logic | 16 | 3 | **The Book 1 pipeline spec** |
| Evidence Register | 13 | 3 | Sources with URLs and limitations |
| Review Release Checklist | 15 | 3 | Release gate checks with `Blocks_Release_if_Fail` |
| Daily Life Development | 13 | 4 | Toilet / sleep / dental / screen / activity modules |

Header rows are mixed 3 and 4 exactly as `CLAUDE.md` warns. `internal/xlsx` already
declares header rows per sheet; this is configuration, not new machinery.

### Three findings that shape the design

**a. `Book Assembly Logic` is the Book 1 analogue of `Recipe Selection Logic`.** Sixteen
ordered steps, each with an input, an output, a `Hard_Stop_Condition` and a named reviewer.
It should be treated as authoritative for Book 1 the way the Book 2 sheet is for the
engine. **Steps 12, 13 and 14 are absent from the workbook** - the sheet numbers its rows
1-11, then 17-19, then 15-16, and nothing numbered 12, 13 or 14 exists. Add `GAP-014`,
severity `major`, and question 11 below.

**b. The link columns are guidance text, not foreign keys.** `Nutrition_Target_Link` holds
`NT00` on one row and `NT02/03/04/05`, `All active targets`, `Target-specific`,
`Age-specific` and `N/A` on others. `Clinical_Rule_Link` holds `CR-GROW-*`, `All active`,
`Vaccination clinical override`. These are instructions to a human, in the same shape as
`nutrition_target_master.hard_exclusions`, which the Phase 2 plan already ruled must be
surfaced verbatim rather than compiled into a predicate.

**Decision: store as text, render verbatim, write no parser.** A parser here would be
inventing a machine relationship the provider did not express. Hand-encoding a mapping
table later - the `culture_region_map` precedent - stays open as a deliberate, auditable
choice, but is not part of this work.

**c. `AI_Can_Draft` is a per-block gate with five real `N` values.** B1-009 (vaccination
schedule), B1-011 (milestone surveillance), B1-012 (developmental red flags), B1-014
(development by age) and B1-022 (reference and disclaimer) are marked `N` by the provider.

**Decision: import it as an enforced constraint, not a badge.** Write the test that asserts
no generated text can occupy an `AI_Can_Draft = 'N'` block **now**, before any assembler
exists. A constraint that predates the code it constrains is cheap; one retrofitted after
an assembler is written is an argument. This is also strictly narrower than the provider's
own permission and consistent with the 18 August ruling in `docs/decisions.md`.

`Book_Order` does not match block-id order - the daily-life blocks B1-023..B1-032 sort into
positions 15-24, displacing B1-015..B1-022 to 25-32. Store it and sort by it; never assume
id order.

### Method

New migration `0012_book1_content`, nine tables, columns verbatim from the headers, plus
sheet-to-table bindings in `internal/importer/spec.go`. The existing header-matching,
upsert-plus-sweep and content-hashing machinery does the rest. The migration DDL stays the
single source of truth for column names: a header with no column, or a `NOT NULL` column
with no header, is an error rather than a silent drop.

Verify: row counts match the workbook, a second import produces identical content hashes,
the integrity suite covers the new tables, and the gap register re-counts on every run.

---

## 7. The canonical child profile

### The empirical spec

`Personalization_Inputs` on the block registry states, per block, exactly which profile
facts that block needs. Aggregated across the 32 blocks it is a longer list than
`docs/clinical-intake-model.md` derived from the SRS prose, and it is authoritative in a
way prose is not, because it says which page goes blank without each field.

**Decision: derive the profile schema from that column, not from the SRS.** Build the
fields the imported blocks actually name; leave the rest until a block needs them.

### The separation that matters

`models.ChildProfile` is the **engine's query input** and does not change. The canonical
profile is a **new persistence layer** beside it. The SRS draws the same line - a
`child_profile_snapshot` that is stored and immutable, feeding a recipe engine that takes a
query.

So: the stored profile holds `date_of_birth`; `age_months` is derived at query time and
handed to the existing engine unchanged. A book generated today and read in six months has
a stale age on every page unless DOB is the stored truth, and the generation date is
stamped so a reader can tell how old the personalization is.

### Tables

`0013_child_profile`:

| Table | Shape | Notes |
|---|---|---|
| `child_profile` | one row per child | identity, DOB, sex, language, region, declared practice, `created_by`, `created_at` |
| `child_growth_measurement` | **many rows per child, dated** | weight, height, head circumference, `measured_on`, `measured_by` |
| `child_allergen` | many per child | group, `status` (confirmed / suspected / resolved), severity, source, last reaction |
| `child_preference` | many per child | ingredient id, `kind` (like / dislike / accepted) |
| `child_clinical_condition` | many per child | `trigger_field`, class, `onset_date`, `expires_after_days`, `entered_by`, `entered_at` |

Five decisions inside that:

**Growth is a dated table, never columns.** One child has many measurements and the trend
is the clinical point - the provider's own prototype puts reference against actual side by
side and reserves space for serial measurement. A single `weight` column destroys the thing
Book 1 exists to show.

**Z-scores and BMI classification are clinician-entered, never computed.** NT03, NT04 and
NT05 all activate on a z-score. Computing one locally means choosing a growth reference,
which is a clinical decision this project has no basis to make.

**Allergy gets three states.** Today `Allergens []string` treats every entry identically.
`AS-002` marks suspected allergy `hard_block = N`; unnecessary elimination is itself a
recognised cause of faltering growth, so treating suspicion as confirmation is a different
risk, not the cautious one. `resolved` does not exist in the data at all today, which means
an allergy recorded at age three excludes food permanently - a real defect, and outgrowing
milk and egg allergy is routine.

Confirmed and systemic stay hard filters. Suspected ranks down and raises a review flag.
Resolved keeps history and excludes nothing. **The four unscreened groups are unaffected by
any of this and must not be obscured by it.**

**Acute conditions expire.** A diarrhoea flag entered three weeks ago must stop pushing
NT12. `onset_date` plus `expires_after_days` are required for the acute class. The expiry
window per class is a clinical value and is a question for the provider; until they answer,
an acute condition with no expiry set is surfaced as stale rather than silently applied.

**Equipment is not collected.** `recipe_master` carries no equipment column, so there is
nothing to match against. Storing an input the engine cannot use is how a form starts lying
about what it does.

### Immutability

Nothing in this plan needs snapshots yet - snapshots belong with the generation state
machine, which is out of scope. But the tables are shaped so a snapshot is a copy rather
than a redesign: every clinically meaningful row carries who set it and when, per the SRS
and per release reproducibility.

---

## Sequencing

Each step is independently shippable and verifiable. Later steps do not silently depend on
earlier ones except where noted.

| Order | Step | Depends on | Rough size |
|---|---|---|---|
| 1 | Allergen field + reference endpoint + picker | - | small |
| 2 | Specialist hold from provider columns | - | small |
| 3 | Reference endpoints (clinical-markers, enums) | - | small |
| 4 | Wire the six inputs into the form | 3 | small |
| 5 | Diet ranker | - | small |
| 6 | `Book1_Content_Master` import, nine tables | - | medium |
| 7 | Canonical profile and growth tables | 6 (for the field list) | medium |

Steps 1-5 are console-visible within a day or two each. Steps 6-7 are the Phase 3
foundation and produce no visible UI on their own - a `/reference` tab showing the block
registry is the cheapest way to prove step 6 landed.

---

## Testing

Matching the existing suite's shape: table-driven, package-local, real database via
`TEST_DATABASE_URL`.

| Step | Assertion |
|---|---|
| 1 | Declaring `Tree nuts` populates `UnscreenedAllergens`; declaring `Peanut` does not; every reference-endpoint allergen resolves to a corpus tag (skipped, naming the gap) |
| 2 | A `specialist_required` rule blocks; `BlockReason` names the approval level; the column and the legacy domain map agree on the current corpus |
| 3 | Every enum value returned carries a live count; no endpoint invents a vocabulary word |
| 4 | Each new control changes the result set and appears in step accounting |
| 5 | Non-vegetarian returns 940 with non-veg concentrated at the top; vegetarian unchanged |
| 6 | Row counts match the workbook; re-import produces identical hashes; `AI_Can_Draft = 'N'` blocks are pinned; `Book_Order` sorting differs from id sorting |
| 7 | Many measurements per child preserve order; suspected allergy ranks but does not filter; resolved excludes nothing; an acute condition past expiry does not drive a target |

Existing invariants must stay green throughout, in particular the five persona queries, the
allergy-leak guard and `TestBrinjalIsNotEgg`.

---

## A documentation correction this work must make

`CLAUDE.md`, `docs/handover-2026-08-18.md`, `docs/not-built.md` and the Phase 2 frontend
plan all state the gap register holds **16 rows**. It holds **12**: migration `0002` seeds
twelve `GAP-0xx` rows and `internal/importer/gaps.go` only ever `UPDATE`s their counts - it
never inserts. Nothing is missing from the database; the number in the prose is wrong.

That matters more than an ordinary typo because the register's stated purpose is to account
for every known hole, so a reader checking "are all 16 there?" gets a wrong answer to the
one question the register exists to answer. Correct the four documents in the same change
that adds `GAP-013`, `GAP-014` and `GAP-015`, taking the count to fifteen.

`docs/not-built.md` §1.1 needs the same treatment for a different reason - it describes the
allergen bridge as unbuilt when migration `0011` built it. Both corrections are part of
step 1, not a separate tidy-up.

---

## What this deliberately does not do

- **Does not fill the 6-11 month iron gap.** No verified source exists for a recipe the
  provider did not supply.
- **Does not write preparation text.** Six unique method texts across 1000 recipes is a
  provider problem.
- **Does not mark anything approved.** `Review_Status` stays verbatim.
- **Does not add an operator override to age or allergy.** Making machinery visible is not
  making the safety boundary adjustable.
- **Does not parse the Book 1 link columns.** They are guidance text; a parser would invent
  a relationship the provider did not express.
- **Does not decide which ranking rubric wins.** That is the provider's ruling and two of
  the excluded items wait on it.

---

## Questions this adds to the provider list

Beyond the ten already in `docs/not-built.md` §6:

11. `Book Assembly Logic` is missing steps 12, 13 and 14. Were they dropped, renumbered, or
    never written?
12. What is the expiry window for each acute condition class? Needed before an acute flag
    can safely stop applying.

---

## See also

- `CLAUDE.md` - Phase 1 and Phase 2, and the hard rule that governs all of this
- `docs/phase-3-book-engine.md` - the destination
- `docs/not-built.md` - the register this narrows; §1.1 needs correcting per section 1 above
- `docs/clinical-intake-model.md` - the SRS-derived profile this supersedes with the
  workbook-derived one
- `docs/decisions.md` - the 18 August prose ruling that section 6c enforces
