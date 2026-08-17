# Clinical intake model

**Status: proposal. None of this is built.**

Written 18 August 2026. Specifies the full input a clinician provides, how each field
behaves in the engine, and which fields are gates rather than data.

Supersedes nothing. `docs/engine-inputs.md` describes what the engine accepts **today**;
this describes what it must accept to produce Book 1 and Book 2.

---

## 1. Who enters what

**Clinical fields are entered by a clinician only.** That is a product decision with two
consequences, and they pull in opposite directions - both matter.

### What it relaxes

Nothing needs explaining at the point of entry. A clinician entering a BMI-for-age z-score
does not need the term defined, a tooltip, or a confirmation step. The interface can be
dense, abbreviated and fast, exactly as `CLAUDE.md` already argues for the operator
console.

### What it does not relax

**The safety boundary does not move because a doctor is typing.** Steps 1 (age) and 2
(allergy) stay hard filters with no override. A clinician can supply a
`feeding_stage_override` - a documented, attributed clinical decision - but there is no
"ignore the allergy filter" control for anybody, at any privilege level. Making the
machinery visible to an expert is not the same as making the boundary adjustable, and the
same reasoning applies here that `CLAUDE.md` applies to the operator console.

### The distinction that actually matters

**Who typed a fact is not the same as where the fact came from.** A clinician types the
family's declared diet practice, but the *authority* for that fact is the family, not the
clinician. The SRS is careful about this and so should the schema be:

| Provenance | Fields | Rule |
|---|---|---|
| **Family-declared** | diet practice, cultural/religious restriction, likes/dislikes, cooking equipment, budget | Recorded verbatim as declared. **Never inferred.** The SRS forbids inferring sensitive dietary identity from culture or location |
| **Clinician-observed** | growth measurements, clinical conditions, feeding-stage override, red flags, development observations | Attributed to the clinician, timestamped, immutable once approved |
| **Clinician-relayed** | allergy history, vaccine history, illness history | Carries a `source` field: parent-reported vs documented. Changes how much the engine trusts it |
| **Derived** | age in months, feeding stage, active nutrition target | Computed, never entered. Labelled `derived` |

Every clinically important field records **who set it and when**. The SRS requires it and
release reproducibility depends on it.

### Roles

The SRS defines nine roles. At minimum this project needs three distinguished:

| Role | Enters | Cannot |
|---|---|---|
| Clinician | Everything in section 2 | Bypass a hard safety filter |
| Operator / staff | Runs lookups, reads results | Enter or amend clinical fields |
| Reviewer | Approves per-section and at release | Author clinical content |

`CLAUDE.md` currently describes a single undifferentiated "staff" audience. That was
accurate for a recipe-search console and is not accurate for book generation. **Open
question: does the operator console keep an unauthenticated single-role model, or does
role separation land before Phase 3?**

---

## 2. The fields

### 2.1 Identity

| Field | Type | Notes |
|---|---|---|
| `child_id` | string | Stable across follow-ups |
| `case_id` | string | Links to the consultation case |
| `display_name` | string, **max 100** | Book 1 schema constraint. Used on covers and in prose - sparingly, per the anti-templated-appearance rules |
| `date_of_birth` | date | **Source of truth for age.** `age_months` is derived, never entered |
| `sex` | enum | Required for WHO growth references only |

**`date_of_birth` rather than `age_months` is the important change.** A book generated
today and read in six months has a stale age on every page. Store DOB, derive age at
generation time, and stamp the generation date into `book_metadata` so a reader can tell
how old the personalization is.

**Sex affects growth interpretation only.** All 13 nutrition targets are
`sex_applicability = All`, so sex never changes recipe ranking. It should not be collected
as though it does.

### 2.2 Language and culture

| Field | Type | Notes |
|---|---|---|
| `language_id` | string | Must exist in the Language Master, which is **not imported** |
| `culture_location_ids` | string[] | `culture_location_master.culture_code`; ranking and localization only |
| `region_culture` | string | The 8 in-scope regions |

The culture master carries 21 distinct `primary_languages` values across the 27 in-scope
culture codes. None are wired to anything. **A language that is not production-approved
blocks release but not draft generation** - SRS edge-case table.

### 2.3 Declared food practice - family-sourced

| Field | Type | Behaviour |
|---|---|---|
| `diet_type` | enum | Hard filter, **nested** (see `docs/decisions.md`) |
| `vegan` | bool | Hard filter, additional to `diet_type` |
| `religious_restriction` | string[] | Halal, Jain, etc. Culture master carries adaptability flags per region |

**Never inferred from region, name or language.** The masters carry
`religious_cultural_inference_rule` precisely to forbid this.

### 2.4 Allergy - three states, not a flat list

Today `ChildProfile.Allergens` is `[]string` and treats every entry identically. The
provider's own masters distinguish more:

```go
type DeclaredAllergen struct {
    Group        string  // allergen_mapping.allergen_group
    Status       string  // confirmed | suspected | resolved
    Severity     string  // mild | systemic
    Source       string  // parent_reported | clinician_documented
    LastReaction *string // ISO date, nullable
}
```

| Status / severity | Rule | Engine behaviour |
|---|---|---|
| `confirmed` | `AS-001`, `CR-ALL-001` | **Hard filter** |
| `systemic` severity | `AS-004` | **Hard filter** + escalation |
| `suspected` | `AS-002` (`hard_block = N`) | Rank down, raise review flag - **not** a hard filter |
| `resolved` | *nothing models this today* | No exclusion; keep in history |
| 2 or more confirmed | `CR-ALL-003` | Specialist pathway |

Two things worth stating plainly:

**`suspected` is deliberately not a hard filter.** Unnecessary elimination is itself a
recognised cause of faltering growth. Treating every suspicion as confirmed is not the
cautious choice, it is a different risk.

**`resolved` does not exist in the data at all.** Outgrowing milk and egg allergy is
routine in pediatrics. With no way to express it, an allergy recorded at age three
excludes food permanently. This is a real defect in the current model.

**Four allergen groups still filter nothing** - tree nuts, crustacean/mollusc, mustard,
sulphites have no corpus tag. See `docs/not-built.md` §1.1. Adding status granularity does
not fix that and must not obscure it.

### 2.5 Clinical conditions - acute, chronic, congenital

The three-way split is right for **intake**. It is the wrong axis for the **engine**.

```go
type ClinicalCondition struct {
    Code               string  // clinical_rule_master.trigger_field
    Class              string  // acute | chronic | congenital
    OnsetDate          *string
    ExpiresAfterDays   *int    // acute only
    SpecialistTargetID *string // required before generation for held conditions
    EnteredBy          string
    EnteredAt          string
}
```

| Class | Examples | Duration |
|---|---|---|
| **Acute** | Diarrhoea, vomiting, fever, post-vaccine | Time-boxed, **must expire** |
| **Chronic** | CKD, liver disease, IBD, diabetes, coeliac | Persistent |
| **Congenital** | Metabolic disorders, prematurity, genetic and chromosomal conditions, structural anomalies | Lifelong |

What the engine actually needs is not duration but **action**, and the provider already
encodes it in `clinical_rule_master` columns nothing currently reads: `engine_action`,
`hard_exclude_yn`, `clinical_escalation_yn`, `specialist_required`, `human_approval_level`,
`do_not_do`.

| Action | Meaning | Result |
|---|---|---|
| **Hold** | Specialist targets required and absent | `blocked: true`, no recipe list |
| **Retarget** | Switch active nutrition target | e.g. acute illness -> NT12 |
| **Constrain** | Modify texture, exclude ingredients | Narrower candidate pool |
| **Rank** | Soft preference | Reordering only |

So class is an intake grouping layered over `engine_action`. One source of truth, and the
taxonomy stays a UI concern.

**Acute conditions need a time dimension nothing else in the model has.** A diarrhoea flag
entered three weeks ago must stop pushing NT12. Without `onset_date` and expiry, stale
acute flags silently distort every later generation.

#### Congenital and complex conditions are a stop, not a filter

The masters cover `Metabolic_Disorder`, `Prematurity_or_Complexity`, `Dysphagia_Suspected`
and `Coeliac_Status`. They do **not** cover Down syndrome, cerebral palsy, congenital heart
disease, cleft lip and palate, autism, or intellectual disability - all of which change
feeding substantially through texture, energy density, oral-motor ability, mealtime
behaviour and sometimes fluid restriction.

**The correct behaviour is to hold generation, not to personalize harder.** The SRS agrees
and is explicit: its hard-stop list includes "specialist therapeutic target is required but
has not been entered/approved", and its edge-case table holds specialized personalization
for complex conditions lacking specialist targets.

`EngineResult.Blocked` and `BlockReason` already exist and work. The response for an
unresolved complex condition is no list plus "a specialist must set nutrition targets for
this child" - not a shorter list.

A tool that confidently produces a recipe book for a child with an uncorrected congenital
heart defect and no dietitian input is worse than one that refuses.

**We cannot invent which conditions require a specialist.** Extending
`clinical_rule_master` is provider work. Until they do, the honest position is that the
engine covers the conditions the masters name and holds for anything else marked complex.

### 2.6 Feeding stage and texture

| Field | Type | Notes |
|---|---|---|
| `feeding_stage_override` | string | `age_feeding_stage_master.stage_code`, **downward only** |
| `texture_prescription` | string | Clinician-prescribed consistency, `AS-021` |

Texture is derived from age today, which is correct as a default. Conditions affecting
oral-motor control need a clinician to set it **below** chronological stage - `CR-FEED-002`
and `AS-021`. This is the one place texture becomes an input.

**Downward only.** No path raises a child above their assessed stage, whoever is typing.

### 2.7 Growth - needs its own dated table

```go
type GrowthMeasurement struct {
    MeasuredOn         string  // ISO date
    WeightKg           *float64
    HeightCm           *float64
    HeadCircumferenceCm *float64  // age-relevant only
    MeasuredBy         string
}
```

**One child has many rows and the trend is the clinical point.** A single `weight` column
destroys the thing Book 1 exists to show - the provider's own prototype puts
reference-versus-actual side by side and reserves space for serial measurement.

Interpretation (z-scores, BMI-for-age classification) is **clinician-entered, not
computed**. NT03, NT04 and NT05 all activate on a z-score the clinician supplies. Computing
it locally would mean choosing a growth reference, which is a clinical decision the project
has no basis to make.

### 2.8 Preferences - family-sourced, ranker only

```go
Likes    []string  // ingredient_id
Dislikes []string  // ingredient_id
Accepted []string  // foods already eaten without incident
```

- **Likes rank up.** SRS "preference match", weight 10.
- **Dislikes rank down, never filter.** A picky child with eight dislikes would empty a
  hard-filtered list - filter collapse in a new costume.
- **Severe aversion is different.** That is `CR-FEED-003` and needs feeding-team input, not
  a ranking tweak.
- `Accepted` feeds Book 2's tracker and closes the follow-up loop.

Magnitude sits with the existing downstream rankers in `internal/engine/rank.go`
(0.02-0.05), so preference can never outweigh nutrition or age-appropriateness.

### 2.9 Practical constraints - family-sourced

| Field | Type | Notes |
|---|---|---|
| `budget_band` | enum | Ranker, already supported |
| `max_prep_time_min` | int | 4 distinct corpus values: 5/10/15/20 |
| `max_cook_time_min` | int | 6 distinct corpus values: 10/15/20/25/30/35 |
| `equipment` | string[] | **No recipe-side column exists.** Unusable until the provider adds one |

Do not collect `equipment` until there is something to match it against. Storing input the
engine cannot use is how a form starts lying about what it does.

### 2.10 Vaccine history - Book 1 only

```go
type VaccineRecord struct {
    VaccineCode  string
    ScheduledAge string
    GivenDate    *string
    Brand        *string
    Batch        *string
    Reaction     *string
    NextDue      *string
}
```

Feeds the `B1-VAX-01` tracker. Schedule comes from the approved IAP-versioned master;
parents record actual administration.

**Never fabricate a date or a reaction.** The provider's own prototype states this on the
page. An unrecorded dose renders as a blank writable row, never as an inferred one.

The 44 vaccine rows live in `Book1_Content_Master`, which **is not imported**. See
`docs/not-built.md`.

### 2.11 Development observations - surveillance, not diagnosis

```go
type DevelopmentObservation struct {
    Domain      string  // gross motor | fine motor | language | social | cognition
    Observation string
    ObservedOn  string
    ConcernFlag bool
}
```

Feeds `B1-DEV-01` as reference-versus-observed. The SRS calls this **surveillance, not
diagnosis**, and the prototype says development varies and only situations meeting
configured review rules get flagged.

The engine never interprets these. It renders the approved age-referenced milestone beside
what was observed, and flags only what the rules say to flag.

### 2.12 Goals

| Field | Type | Constraint |
|---|---|---|
| `priority_goals` | string[] | **1 to 6** - Book 1 schema `minItems`/`maxItems` |
| `follow_up_due` | date, nullable | |

Goals drive `B1-GOALS-01` cards, the "why these recipes" page, and section ordering. They
are the main lever for lever 4 in `docs/phase-3-book-engine.md` §3.

Book 2 recipe cards separately allow **1 to 4** `selection_reasons` per recipe.

### 2.13 Approval - a gate, not a field

| Field | Type | Notes |
|---|---|---|
| `consultation_date` | date | Required by the Book 1 schema |
| `reviewed_by` | string | Required by the Book 1 schema |
| `clinician_approval_id` | string | **Generation cannot start without it** |
| `profile_snapshot_version` | string | Immutable once approved |

`clinician_approval_id` is not another data field. It is the transition into
`CLINICALLY_APPROVED`, and the SRS forbids generation before it exists. It must be
timestamped and immutable.

Every released bundle records the exact profile snapshot it came from, so a delivered book
can always be reconstructed.

---

## 3. Behaviour summary

| Field group | Hard filter | Ranker | Hold | Display only |
|---|---|---|---|---|
| Age (from DOB) | yes | | | |
| Allergy - confirmed / systemic | yes | | | |
| Allergy - suspected | | yes | | flag |
| Allergy - resolved | | | | history |
| Diet practice | yes | | | |
| Religious restriction | yes | | | |
| Clinical - hold class | | | yes | |
| Clinical - retarget class | | via target | | |
| Clinical - constrain class | yes | | | |
| Feeding stage override | yes | | | |
| Likes | | yes | | |
| Dislikes | | yes | | |
| Region / cuisine | | yes | | |
| Budget, time | | yes | | |
| Growth measurements | | | | Book 1 |
| Vaccine history | | | | Book 1 |
| Development observations | | | | Book 1 |
| Goals | | ordering | | Book 1 + 2 |
| Approval id | | | **gate** | |

---

## 4. Build order

Roughly cheapest and safest first. Fits between steps 2 and 6 of `docs/next-steps.md`.

1. **Allergy status granularity.** Adds `resolved`, splits confirmed from suspected. Small,
   and fixes a real permanence defect.
2. **Likes and dislikes as rankers.** Cheapest, most visible to a family, no new
   subsystems.
3. **Identity and language.** `display_name`, `date_of_birth`, `sex`, `language_id`.
   Precondition for every book page.
4. **Growth measurements table.** Dated rows, clinician-entered interpretation.
5. **Clinical conditions with class, onset and expiry.** Needs the acute expiry rule.
6. **Hold behaviour for complex conditions.** Wire `specialist_required` to
   `EngineResult.Blocked`. Needs the provider to extend `clinical_rule_master` first.
7. **Vaccine and development records.** Blocked on importing `Book1_Content_Master`.
8. **Approval gate and profile snapshots.** Part of the generation state machine.

---

## 5. Open questions

1. Does role separation (clinician / operator / reviewer) land before Phase 3, or does the
   console stay single-role? `CLAUDE.md` currently describes one undifferentiated audience.
2. Which congenital and neurodevelopmental conditions require a specialist hold? We cannot
   invent the list - `clinical_rule_master` needs extending by the provider.
3. What is the expiry window for each acute condition class? Clinical, not arbitrary.
4. Is `suspected` allergy really never a hard filter, or does severity change that? `AS-002`
   says soft; `AS-004` says a systemic *history* is hard. The boundary between them for a
   suspected-but-severe case is unstated.
5. Who supplies the Language Master? It is named throughout the SRS and does not exist in
   any imported table.
6. Do the four untagged allergen groups get corpus tags, or leave the interface? Unchanged
   from `docs/not-built.md`, and this document depends on the answer.
