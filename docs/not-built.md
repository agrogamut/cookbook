# Not built

A register of everything known to be missing, wrong or unresolved, with what it costs.
Compiled 18 August 2026 from a live query of the development database plus the provider's
Phase 3 deliverables.

This complements the `gap_register` table, which counts missing *values*. This document
records missing *capability and decisions*.

Sections are ordered by how much damage the gap does, not by effort.

---

## 1. Safety-adjacent

### 1.1 Four declared allergens filter nothing

`allergen_tag_vocabulary` bridges the 11 `allergen_mapping.allergen_group` values to the
tags that actually appear in the ingredient corpus. Four have no corpus tag at all:

| Allergen group | Corpus tag | Filters |
|---|---|---|
| Tree nuts | none | nothing |
| Crustacean/Mollusc | none | nothing |
| Mustard | none | nothing |
| Sulphites | none | nothing |

Declaring one of these is accepted by the API, applies no exclusion, and returns a result
list identical to declaring nothing. The operator has every reason to believe the allergen
was applied.

**This is the most dangerous known gap.** It fails in the unsafe direction: the interface
implies a protection that does not exist.

Options, none chosen:
- Remove the four from any picker, and say why at the point of entry.
- Keep them selectable but render an explicit "not screened - no corpus coverage" state
  next to the field and on every result page.
- Have the provider tag the corpus, which is the real fix and is their work.

Until one is chosen, the four should not appear in an operator-facing picker.

### 1.2 317 of 406 ingredients are untagged for allergens

`"None identified in starter tagging"` is the provider's own admission that tagging is
incomplete, not a result. An unscreened ingredient must never render as "no allergens".
Open Food Facts was named in `CLAUDE.md` as a cross-check source and has not been loaded.

### 1.3 Congenital and complex conditions have no hold behaviour

The clinical masters cover `Metabolic_Disorder`, `Prematurity_or_Complexity`,
`Dysphagia_Suspected` and `Coeliac_Status`. They do **not** cover Down syndrome, cerebral
palsy, congenital heart disease, cleft lip and palate, autism, or intellectual disability -
every one of which changes feeding through texture, energy density, oral-motor ability,
mealtime behaviour and sometimes fluid restriction.

Today the engine has no way to know about any of them, so a child with an uncorrected
congenital heart defect and no dietitian input gets a confidently-generated recipe list
like any other child.

**The correct behaviour is to hold generation, not to filter harder.** The SRS agrees: its
hard-stop list includes "specialist therapeutic target is required but has not been
entered/approved", and its edge-case table holds specialized personalization for complex
conditions lacking specialist targets. `EngineResult.Blocked` and `BlockReason` already
exist and work - nothing is wired to them.

**We cannot invent which conditions require a specialist.** Extending
`clinical_rule_master` is provider work. See `docs/clinical-intake-model.md` §2.5 for the
proposed model and `docs/not-built.md` §6 question 10.

### 1.4 Nothing is approved

All 940 recipes carry `Review_Status = Draft - Culinary/Nutrition/Clinical Review
Required`. All 406 ingredients are `Needs Validation` with `Data_Quality = Provisional
planning value`. 939 of 3317 mappings are flagged `Allergen_Review_Check = REVIEW`.

The SRS states that presence in a master does not equal production approval, and that only
records carrying a `Release_Eligible` flag may appear in a released customer book. There
is no such flag in the schema, and if there were, nothing would qualify.

**Under the provider's own rule, nothing in this database may currently ship to a parent.**

---

## 2. Missing inputs

### 2.1 Fields the books need that nothing accepts

None of these exist in the database, the API or the form. Sourced from SRS section 8 and
the two book JSON schemas.

| Input | Needed for | Blocks |
|---|---|---|
| `display_name` | Both covers, profile cards | Every page of personalization |
| `date_of_birth` | Age display, growth reference | "4 years 3 months" on the cover |
| `sex` | Growth reference charts | Book 1 growth comparison |
| `language_id` | Localization layer | Any non-English book |
| dated `weight`, `height`, `head_circumference` | Growth tables and trend | Book 1 sections B1-04 to B1-06 |
| `feeding_stage_override` | Prematurity, dysphagia | Clinician texture control |
| `likes`, `dislikes`, `accepted_foods` | Preference ranking (SRS weight 10) | Acceptance-driven ordering |
| `equipment` | Feasibility ranking (SRS weight 5) | No recipe-side column exists either |
| `vaccine_history[]` | Book 1 tracker rows | `B1-VAX-01` renders blank |
| `development_observations[]` | Milestone comparison | `B1-DEV-01` renders blank |
| `priority_goals[]` | Goal cards, selection reasons | The "why these recipes" page |
| `consultation_date`, `reviewed_by` | Consultation summary | Required by the Book 1 schema |
| `clinician_approval_id` | Release gate | Generation cannot legally start |

Two are structural rather than a simple column addition:

- **Growth measurements need their own dated table.** One child has many, and the trend is
  the clinical point. A single `weight` column would destroy the thing Book 1 exists to
  show.
- **Equipment has no recipe-side column to match against.** `recipe_master` carries no
  equipment field, so accepting the input is useless until the provider adds one.

### 2.2 Engine inputs the UI does not send

The API accepts 13 `ChildProfile` fields. The Phase 2 profile form sends 7. These six work
over the API and have no control:

`region_culture`, `cuisine_code`, `clinical_flags`, `max_prep_time_min`,
`max_cook_time_min`, `limit`

`region_culture` and `cuisine_code` are the most valuable of the six - they are the whole
of engine step 7, and the West-Bengal-first default cannot be overridden without them.

### 2.3 Data columns the engine never asks for

Populated on all 940 rows, needing no new data work, accepted by nothing:

| Column | Distinct values |
|---|---|
| `season` | 4 - All season, Summer-friendly, Monsoon-friendly, Winter-friendly |
| `growth_target` | 8 - energy, protein, iron, calcium, fibre adequacy; catch-up; balanced; healthy weight |
| `post_vaccine_context` | 5 |
| `texture` | 4 - derived from age today, safer as a clinician-only downward override |
| `clinical_tag` | 7 - **display badge only, never filter on it** (single-valued, causes filter collapse) |

---

## 3. Data the provider must fix

None of these can be fixed on this side without inventing values.

### 3.1 Preparation text is boilerplate

`preparation_method_full` holds **6 unique texts across 1000 recipes**, differing only in
liquid volume (100 mL / 120 mL / 50 mL). `safety_rule`, `storage_rule` and
`ai_adaptation_rule` hold **one unique value each**. Prep and cook times snap to 4 and 6
distinct values respectively.

This is the text a parent actually reads. Two different children currently receive
identical method paragraphs. The external corpus join covers 166 of 940 recipes with a
*suggested* method shown beside the provider's text; the rest have no valid analogue and
correctly get no suggestion.

**Question outstanding to the provider: are the preparation methods placeholders, and is a
real version coming?**

### 3.2 The 6-11 month iron gap

Zero iron-support recipes exist for 6-8 months or 9-11 months. That is the peak window for
infant iron deficiency in South Asia.

Do not fill it. There is no verified source for a 6-11 month iron-support recipe the
provider did not supply, and writing infant feeding content to fill a hole in a pediatric
dataset is exactly what the hard rule exists to prevent. Surface it as a gap.

**Question outstanding to the provider: is this deliberate or unfinished?**

### 3.3 Nutrition is group-level placeholder

406 ingredients carry only 76 distinct nutrition value sets, assigned per food group. 22
confirmed discrepancies against IFCT 2017 on exact-name matches. `Brinjal/Eggplant` is
listed at 143 kcal/100 g against IFCT's 27, in 89 recipes - a name lookup matching `egg`
inside `Eggplant`.

The corrected layer handles 139 of 406 ingredients; 267 keep the provider placeholder and
are flagged unverified. `ingredient_master` is never modified.

**`nutrition_discrepancy_report` is the deliverable to hand the provider and has not been
sent.**

### 3.4 The corpus has essentially no egg recipes

Exactly **one** recipe uses any egg ingredient (`ING0066` Egg, `ING0170` Duck egg,
`ING0300` Quail egg): `MG-R-00692`, tagged 6-9 years.

So an eggetarian child under six gets zero egg dishes, and always will until the provider
writes some. Offering `Eggetarian` as a distinct diet choice implies a corpus that does not
exist.

### 3.5 Meal category names do not line up

`meal_category_target` holds 7 categories, each with a default target of 25 recipes.
`recipe_master.meal_type` holds 6 values. Only three match exactly.

| Target category | Recipes behind it |
|---|---|
| Breakfast | 194 |
| Lunch | 199 |
| Dinner | 193 |
| Mid-morning | **0** |
| Evening snack | **0** |
| Supper / bedtime | **0** |

And three meal types have no target: `Snack` (182), `School Tiffin` (99),
`Recovery Meal` (73).

Book 2's meal sections are built from `meal_category_target`, so **three chapters would
generate empty**. Either the names get reconciled by the provider, or a mapping table is
hand-written the way `culture_region_map` was.

---

## 4. Unresolved decisions

### 4.1 Two ranking rubrics, no ruling

The provider has shipped two different scoring systems and has not said which wins.

| Rubric | Where | Shape |
|---|---|---|
| NT00-NT12 axis weights | `nutrition_target_master`, implemented in migration `0003` | 13 targets x 10 axes, weights 1-5, seven axes scored |
| SRS section 10 component weights | The new SRS | 9 components, weights 30/20/15/10/8/7/5/5 and a 0 to -20 duplicate penalty |

They are not obviously reconcilable - the first ranks recipes *within* a nutrition target,
the second ranks across all considerations including the target match itself. A plausible
reading is that NT weights feed the "nutrition target match: 30" component of the second,
but nobody has confirmed it.

### 4.2 Diet ranker not built

Diet filtering was corrected on 18 August to be nested rather than categorical (see
`docs/decisions.md`). A non-vegetarian family now correctly sees all 940 recipes.

What was **not** built is the follow-on: a family that declares non-vegetarian probably
wants non-vegetarian dishes ranked up, not merely permitted. The SRS's "preference match,
weight 10" is the right home for this. Deferred deliberately.

### 4.3 Variant authoring is unassigned

Section 3 of `docs/phase-3-book-engine.md` establishes that personalization depends on
provider-authored block variants. Nobody has said how many variants per block, or who
writes them. Until that is answered, the general half of both books reads identically for
every child.

### 4.4 Licensing on the recipe corpus

`Anupam007/indian-recipe-dataset` has an **unstated upstream licence** and was pulled from
a scraped collection. It supplies suggested method text for 166 recipes. Either terms are
established with the upstream source, or `recipe_method_external` is dropped and the
provider's text ships alone. IFCT 2017 is fine - openly published by ICMR-NIN.

### 4.5 Provider sign-off

The provider's `Review_Status = Draft` and `Data_Quality = Provisional` flags are honest
labelling, not clearance. Nothing has been through culinary, nutrition or clinical review.
Showing a per-row `Draft` badge to an operator is necessary and is not the same as
permission to act on the row.

---

## 5. Known non-blocking

Recorded so they are not rediscovered.

- 304 of 406 ingredients (75%) appear in zero recipes.
- Evidence ids are fragmented across 5 registers with colliding names
  (`EV-WHO-IYCF` vs `EV-WHO-IYCF-2021`). Only matters once citations are displayed.
- Page Registry module ids (`B1-00`..`B1-58`) do not match Book Content Master block ids
  (`B1-001`..`B1-032`). Zero overlap. This is a PDF/book rendering problem and becomes
  live in Phase 3, having been parked as website-irrelevant.
- West Bengal has the lowest external method coverage of any region (12.8%) despite being
  the home region - the corpus holds 150 Bengali recipes against 763 North Indian. A
  corpus limitation, and the strongest argument for finding a dedicated Bengali source.
- `Eggetarian` as a diet type resolves to 829 permitted recipes after the nesting fix, of
  which exactly 1 contains egg.
- Nepal's 60 recipes are out of scope by `region_focus`, logged as `GAP-011`.

---

## 6. Questions outstanding to the provider

Three are already listed in `CLAUDE.md` and remain unanswered. The Phase 3 delivery adds
more.

1. Are the preparation methods placeholders, and is a real version coming?
2. Is the 6-11 month clinical gap deliberate or unfinished?
3. The nutrition audit found 22 confirmed discrepancies against IFCT 2017, the table the
   ingredient master itself names as its source. Brinjal is listed at 143 kcal/100 g
   against IFCT's 27, in 89 recipes. `nutrition_discrepancy_report` has the list.
4. Which ranking rubric governs - NT00-NT12 or the SRS component weights?
5. `meal_category_target` names three categories with zero recipes and omits three that
   have recipes. Which set is authoritative?
6. Four allergen groups have no corpus tag. Will the ingredient corpus be tagged, or should
   those four be withdrawn from the interface?
7. Who authors the Book 1 content-block variants, and how many per block?
8. What is the licence position on the external recipe corpus?
9. When does clinical sign-off happen? Nothing may ship until it does.
10. Which congenital and neurodevelopmental conditions require a specialist hold? The
    masters name four; Down syndrome, cerebral palsy, congenital heart disease, cleft lip
    and palate, autism and intellectual disability are absent. This list cannot be
    invented on our side. See §1.3.
