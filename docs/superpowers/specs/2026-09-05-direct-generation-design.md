# Direct generation: doctor-operated books with no stop gates

**Date:** 2026-09-05
**Status:** approved, not started
**Supersedes in part:** the "Safety, unchanged" and "The special-care stop gate" sections
of `CLAUDE.md`, to the extent named below and no further.

## The decision

The operating model changed. This engine was designed for an **operator** - clinic staff
serving families, not clinically qualified themselves - who could repeat a wrong number to
a parent without knowing it was wrong. Every generation-stopping gate in this codebase was
sized against that operator: a block never puts an unsafe recipe in front of someone who
cannot evaluate it, so blocking was always the safe direction.

The input is now a **registered, verified doctor**. That inverts the calculation for the
gates and only for the gates. A doctor who declares cerebral palsy and receives HTTP 409
with "route to a mandatory reviewer" has been handed nothing by a system that just told
the reviewer to go find themselves. The stop was protecting against an absent clinical
judgement that is now present at the point of input.

So: **the gates go, the filters that execute the doctor's own input stay, and nothing about
inventing data values changes.**

### Deployment premise, stated so it is on the record

There is no authentication in this codebase. `internal/api/router.go` has no auth
middleware, no session, no JWT. "Only verified doctors" is enforced by deployment - the
service is reachable only on a private network - not by anything in this repository. That
is the project owner's explicit call, recorded here rather than argued: if the service is
ever exposed publicly, this spec's entire justification lapses and the gates are the wrong
thing to have removed.

### What does not change

Three things a reader might expect this spec to relax, and it does not:

1. **The hard rule on inventing data values.** Nutrition figures, allergen tags,
   min-ages, costs - all still traceable to a verified source or absent. This spec is
   about *gates* and *inputs*, never about fabricating a number.
2. **The confirmed-allergen hard filter (step 2).** A doctor typed "peanut allergy".
   Excluding peanut *is* executing that instruction. Removing the filter would mean the
   field they filled in does nothing, which is ignoring the doctor rather than trusting
   them. Same reasoning for the diet filter (step 4).
3. **`book1_content_block.ai_can_draft = 'N'`.** The five gated blocks (vaccination
   schedule, milestone surveillance, developmental red flags, development-by-age,
   reference/disclaimer) stay closed. This spec was explicitly scoped to exclude them.

## Sub-project decomposition

Four sub-projects, strictly ordered. Each produces working, verifiable software on its
own. Later ones are useless before earlier ones: a clinical-flag input field (SP2) earns a
409 until SP1 lands; an inference pass (SP3) needs the field set final; a fact-check pass
(SP4) has nothing to check until inference exists.

| SP | Name | Depends on |
|----|------|------------|
| 1 | Gate removal | - |
| 2 | Frontend field completion | SP1 |
| 3 | AI profile inference | SP2 |
| 4 | Fact-check pass | SP3 |

---

## SP1 - Gate removal

### Current blocking surface, verified live

| # | Site | Effect today |
|---|------|--------------|
| A | `engine/special_care.go` `specialCareGate` | 6 conditions → `Blocked=true` → HTTP 409, zero books |
| B | `engine/special_care.go` `SpecialCareBlock` | same gate, called from `book1.go:190` so Book 1 blocks too |
| C | `engine/clinical.go` `clinicalFilter` escalation branch | 10 domains + the specialist approval tier → HTTP 409, zero books |
| D | `engine/clinical.go` unclassified-rule branch | a fired `hard_exclude_yn='Y'` rule in neither set → hard error, HTTP 500 |
| E | `engine/steps_hard.go` `ageFilter` | hard filter, can empty the candidate pool |
| F | `book/templates/book1/illness.html:22` `EngineLimit` | prints a scope caveat per illness situation |
| G | `book/templates/book1/daily.html:48` `AILimit` | prints "Scope of this page: …" per daily-life domain |
| H | `book/templates/book1/stage.html:179` | prints "This is the last stage the provider's feeding master defines." |
| I | `book/templates/book1/refs.html:17,26` | "Stated limitation" column on the evidence table |
| J | `web/child-input-form.tsx:431-436` | "Generation will halt and no book is produced" under the special-care dropdown |
| K | `web/book-generator.tsx:257-269` | the blocked Alert |

Two things that look like blockers and are not, verified by reading them: `applyMealFilter`
(`rank.go:21`) and `applyTimeFilter` (`rank.go:282`) already degrade gracefully - when the
filtered set is empty they return the full pool with a "closest fit" note. No change.

### Design

**A, B, C, D - blocks become recorded facts.**

`specialCareGate` keeps its lookup and keeps quoting the provider's `automatic_action` /
`mandatory_reviewer` / `stop_if` verbatim, because that text is real provider data and an
operator may still want it. What it stops doing is returning `blocked = true`. The
signature loses its `bool` and its reason string; it returns a `models.StepResult` whose
`Note` carries the provider text, and the pipeline continues past it.

`SpecialCareBlock` is deleted outright. Its only caller is `book1.go:190`, and that call
site is deleted with it. Book 1 stops consulting the gate at all - it carries no recipe, so
with the block gone it has nothing to ask.

`clinicalFilter` loses both its escalation branch and its unclassified-rule error. This is
not a loss of function, and that is worth stating plainly: **none of those rules had a
compilable recipe-side predicate in the first place.** `escalationOnlyDomains`' own comment
records why - no renal-safe, gluten-free, dysphagia-texture or FODMAP tag exists on any
table. The block was standing in for a filter that could never be written. With the block
gone, `clinicalFilter` becomes an honest recorded no-op: it validates flag keys against
`clinical_rule_master.trigger_field` (kept - a typo'd key must still be a 400, that is
input validation, not a gate) and passes every candidate through.

The clinical signal is not discarded. It already flows two other ways that stay:
`engine.SelectTarget` picks the nutrition target from the child's condition, and
`engine.ActiveClinicalRuleActions` feeds `aidraft.DraftModificationNote`, which writes the
per-recipe "if your child has diarrhoea, …" text. Conditions stop being a wall and become a
ranking and drafting input. That is a closer alignment to the child, not a looser one.

`escalationOnlyDomains` has a hand-copied twin in SQL, inside
`handlers.ReferenceClinicalMarkers`, deliberately duplicated because handlers must not
import engine internals. It outlives SP1 by one sub-project: **SP2 Task 1 removes it**, along
with the `escalates` column it computes. Named here so its survival reads as sequencing
rather than an oversight.

Deleted as a consequence: `escalationOnlyDomains`, `specialistApprovalLevel`,
`models.EngineResult.Blocked`, `models.EngineResult.BlockReason`, `book.ErrBlocked`,
`book.BlockedDetail`, `internal/book/blocked.go`, `internal/book/blocked_test.go`,
`handlers.writeBlocked`, all three `errors.Is(err, book.ErrBlocked)` branches, the 409
mapping in `api.ts:178`, `BookBlockedError`, and the `Blocked` problem variant in
`book-generator.tsx`.

**E - age becomes a stable partition, not a score penalty.**

`ageFilter` stops filtering and returns every `recipe_master.recipe_id`. A new
`applyAgeRank` runs immediately after `rankByTarget` (before the suspected-allergen ranker,
because age relevance is the coarser signal) and **stably partitions** the ranked list: all
recipes whose `[min_age_months, max_age_months]` contains the child's age first, in score
order, then everything else, in score order.

Partition rather than a score boost, deliberately. Every other ranker in `rank.go` adds a
constant (culture 0.05, availability 0.05, budget 0.03, suspected allergen -0.15) against a
`ranked_score` spread of roughly 0.65. No constant in that family can guarantee an in-band
recipe outranks an out-of-band one - `recipe_target_score` normalises *within* an age band,
so a teenage recipe scoring 0.9 in its own band beats a 6-month puree scoring 0.7 in its
own band under any penalty below 0.2, and a penalty large enough to guarantee correctness
(>= 1.0) is a hard filter wearing a ranker's clothes. A partition states the actual
intent: age-appropriate always sorts first, out-of-band is reachable only when the in-band
pool runs out, and no recipe is ever removed.

`models.RankedRecipe` gains `AgeInBand bool` so the console and the why-panel can show which
half a row came from.

**F through I - the printed caveats go.**

Four template edits. The struct fields (`AILimit`, `EngineLimit`, `Limitation`) stay
populated so an operator checking the JSON still sees them; they stop printing on the
family-facing page. This is the same line the 2026-08-25 amendment already drew for the
per-recipe `Draft` label: document-level and repeated per-section status text is this
project's call, the provider's own per-row data flags are not.

`refs.html` loses the "Stated limitation" column. `colwidth.go` sizing for that table is
recomputed against the remaining four columns.

**J, K - the frontend stops warning about a thing that no longer happens.**

### Verification

Beyond `go build` / `go vet` / `go test ./...`:

- A profile with `SpecialCareCondition = "SC-CP"` returns 200 with a non-empty recipe list
  and both books, and the step list carries the provider's stop text as a note.
- A profile with a clinical flag that used to escalate (`CKD`) returns 200 with recipes.
- A 7-month-old's ranked list has every in-band recipe above every out-of-band one, and
  is never shorter than it was before this change.
- `BOOK_PAGE_DUMP` print of both books, read by eye: no scope caveat on the illness page,
  the daily-life pages, the last feeding stage, or the references table; `pagefit_test.go`'s
  underfilled-page budget has not regressed (removing text can orphan a heading).

---

## SP2 - Frontend field completion

**Verified before specifying, and it changed the shape of this sub-project.** The Go API
already accepts almost everything missing. `handlers.profileDTO` carries `vegan`,
`religious_restriction`, `max_prep_time_min` and `max_cook_time_min`;
`generateRequest.Conditions` accepts any `trigger_field`, not only `Special_Care_Condition`;
`profile.Stored` holds all of them and `ToChildProfile` maps them into
`models.ChildProfile`. And `GET /api/reference/clinical-markers` already returns the 28
distinct `clinical_rule_master.trigger_field` values with each rule, operator and firing
value behind them.

So SP2 is a **frontend sub-project with two small backend items**, not the API build-out it
first looked like.

| Field | Backend | Form |
|---|---|---|
| `Vegan` | accepted end to end | absent |
| `ReligiousRestriction` | accepted end to end | absent |
| `MaxPrepTimeMin` / `MaxCookTimeMin` | accepted end to end | absent |
| `ClinicalFlags` (any trigger field) | accepted via `conditions[]` | absent - the form only ever sends `Special_Care_Condition` |
| `child_preference` likes/dislikes | table exists (migration 0014); engine step 8 is a hardcoded no-op at `pipeline.go:127` | absent |

Two fields deliberately **not** added, each for a reason found by reading the code:

- **`MealType`.** Book 2 sets it per chapter inside its own loop. A book-level meal type
  would fight that, and it belongs on the `/` engine console, which already has it.
- **`ClinicalMarker`.** Not on `profile.Stored` at all, so there is no path from a book
  request to it. Adding one is a separate change and `SelectTarget` already derives the
  target from the child's real conditions.

### Design

**The clinical section** replaces the single special-care dropdown: that select stays (its
SP1-removed warning gone), and beside it one control per `trigger_field`, built from
`/api/reference/clinical-markers` rather than a hardcoded list - the discipline every other
option list in `child-input-form.tsx` already follows, for the same reason: trigger fields
are provider data and a local copy drifts from the workbook. `in_list` renders a select over
its values, `equals` a select over the single value plus blank, `contains` a text input.
Unset means unset, never a default. Each set flag is sent as a `conditions[]` entry, which
the API already understands.

**Backend item one:** that endpoint's response carries `escalates`, computed from a SQL
copy of `escalationOnlyDomains` that SP1 deletes from the engine. Both the column and the
duplicated domain list come out - a field naming a behaviour that no longer exists is worse
than an absent one.

**Backend item two:** engine step 8 stops being a no-op. `applyPreferenceRank` boosts
recipes containing a liked ingredient and demotes ones containing a disliked one, at this
file's existing 0.05 / -0.05 magnitudes.

**The practice section gains** a vegan checkbox (enabled only when diet is Vegetarian,
mirroring the model's own rule that vegan implies `DietType = "Vegetarian"`), a religious
restriction input, and two numeric time budgets. **A new preferences section** carries
likes and dislikes against `ingredient_master`.

### Verification

- Every field the form sends round-trips into `models.ChildProfile` unchanged, asserted at
  the handler boundary.
- A non-special-care clinical flag set in the form reaches `clinicalFilter` and appears in
  the step list's note.
- A liked ingredient measurably reorders the result list; a disliked one measurably demotes.
- `npm test` covers: the clinical section renders from a mocked endpoint rather than a
  constant; the vegan checkbox is disabled unless diet is Vegetarian.

---

## SP3 - AI profile inference

New: when a doctor leaves a ranker-input field blank, infer it rather than falling through
to "no preference".

### The boundary, which is the whole design

Two lists, and the second is not negotiable.

**Inferable** - fields whose blank state means "no ranking preference", where a grounded
guess produces a book closer to this child: `RegionCulture`, `CuisineCode`, `BudgetBand`,
`MealType`, `MaxPrepTimeMin`, `MaxCookTimeMin`, and ingredient likes/dislikes.

**Never inferred** - `date_of_birth`, confirmed allergens, suspected allergens, growth
measurements, name and identity, `SpecialCareCondition`, `ClinicalFlags`. These are not
missing information. They are facts only the doctor holds, and each fails badly in both
directions: a guessed allergen that is wrong excludes safe food, a guessed *absence* of one
serves unsafe food, a guessed birth date changes the age band every page is written
against, and a guessed clinical flag puts a condition in a child's record that no clinician
recorded.

This is not the "never invent data" rule being relaxed. That rule governs values printed as
fact - a nutrition figure, an allergen tag, a cost. An inferred region is a **selection
constraint**, not a printed claim: it changes which real recipes are ranked highest and is
never itself rendered as something a clinician stated.

### Design

`aidraft.InferProfile(ctx, InferenceRequest) (InferredFields, error)`, following the exact
shape of the existing `DraftFoodGroupPriorities`: grounded on real closed vocabularies read
live from the database, a response schema constraining each field to an enum over that real
vocabulary, and a Go-side re-validation that drops any field failing the check rather than
failing the whole request.

Grounding sources, all read live, never hardcoded: `region_focus`, `cuisine_option`, the
`enums` endpoint's `budget_band` and `meal_type`, and `recipe_master`'s real
`prep_time_min` / `cook_time_min` value sets. The prompt is given the fields the doctor
*did* supply and forbidden from returning any value outside the fed-in vocabulary.

Each inferred field carries `{value, grounded_on, confidence}` on the struct.
`grounded_on` names the supplied field the inference reasoned from (language `bn` →
`RegionCulture` "West Bengal / East India"). A field with no grounding source stays blank -
inference fires where there is something real to reason from, and declines otherwise. That
is what keeps this from being "pick a plausible default", which is the thing the hard rule
forbids.

Inference runs once, in `AssembleSet`, before either assembler - so Book 1 and Book 2 are
built against the same inferred profile, the same way they already share one `asOf`.

Inferred fields are surfaced to the operator on `/books` and are **not** printed in either
book. They changed which recipes were selected; they are not facts about the child.

### Verification

- A profile supplying only DOB and language gets a region inferred; a profile supplying
  only DOB gets nothing inferred and generates exactly as it does today.
- Every "never inferred" field is asserted absent from the response type at compile time -
  `InferredFields` has no such field to set - and asserted unchanged by a test that feeds a
  profile with blank allergens and confirms the assembled book still reports them blank.
- A returned value outside the real vocabulary is dropped, not printed, and the drop is
  recorded.

---

## SP4 - Fact-check pass

Today's checking is two hand-rolled validators: `validateFoodGroupPriorities` in
`internal/book`, and the `ingredient_id` enum on `DraftInventedRecipe`. There is no pass
over drafted output as a step, and nothing an operator can look at.

### Design

A new `internal/factcheck` package, run in `AssembleSet` after both books are assembled and
before they are returned. It takes the assembled `Book1` and `Book2` plus the
`InferredFields` from SP3 and returns `[]Finding`, each naming the value, the source it was
checked against, and the verdict.

Four checks, each against a real corpus table:

| Check | Verified against |
|---|---|
| Inferred profile fields | `region_focus`, `cuisine_option`, `enums` - the same vocabularies SP3 grounded on, re-read independently |
| Invented-recipe ingredients | `ingredient_master`, plus the age and allergen filters a real recipe passes |
| Drafted modification notes | the `clinical_rule_master.book2_action` / `Required_Modification` text the note claims to paraphrase - a note naming a condition with no matching row is a finding |
| Printed nutrition figures | `recipe_nutrition_recomputed`, including that the printed `ingredient_coverage` matches the row |

A failing check **drops the offending unit** - one recipe card, one note, one inferred field
- and records the finding. It never withholds the book. That is the same
omit-rather-than-half-build convention every `Drafter` caller already follows, and it is
deliberately not a new gate: SP1 exists to remove gates, and re-introducing one under a
different name would undo it.

Findings surface on `/books` as an operator panel beside the existing omissions panels, and
in the JSON as `factcheck_findings`. Nothing about them prints in either book.

### Verification

- A deliberately corrupted invented recipe (an ingredient id not in `ingredient_master`) is
  dropped, the finding names it, and the book still generates.
- A book with no drafting enabled produces zero findings and is byte-identical to one
  generated before this sub-project.
- The pass adds no network calls - every check is a local query against already-open
  connections.

---

## Global constraints

These bind every task in every sub-project.

- **Never invent a data value.** Unchanged and outranking everything here. A gap is `null`,
  "not available", or a shorter list.
- **Confirmed allergens (step 2) and declared diet (step 4) stay hard filters.** No task in
  any sub-project relaxes them.
- **`ai_can_draft = 'N'` stays closed** on all five Book 1 blocks. The CHECK constraint and
  `TestAICanDraftGateIsPinned` are untouched.
- **No AI-generated images**, anywhere.
- **Provider per-row `Review_Status` / `Data_Quality` stay verbatim** in the data model and
  the JSON API. SP1 removes printed *caveats*, never a provider data flag.
- **No emojis. No em dashes or en dashes in prose.** No attribution to any AI tool in code,
  comments, commit messages, PR text or file headers.
- **`go build ./...`, `go vet ./...`, `go test ./...` green** with a real
  `TEST_DATABASE_URL`, plus `npx tsc --noEmit`, `npm test`, `npm run build` in `web/`,
  before any sub-project is called done.
- **Print a book and read it.** Every layout-affecting change (SP1's template edits above
  all) needs a `BOOK_PAGE_DUMP` print and a human read of the sheets. Every defect the
  page-fit guards exist for was found that way and never by a count.

## Open items

- **Licensing** of the external recipe corpus is still unresolved and unaffected by this
  spec.
- **Provider dataset sign-off** is still outstanding. This spec removes *this project's*
  gates on generation; it makes no claim that the provider's data has been reviewed, and
  the per-book physical signature page stays exactly as it is.
