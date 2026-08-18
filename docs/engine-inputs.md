# Engine inputs

Every field the engine accepts, every legal value, and what each does to the result set.
Vocabularies queried live from the development database on 18 August 2026, against the 940
recipes inside India + Bangladesh scope.

Regenerate the counts with the queries at the bottom of this file after any re-import.

---

## The 13 fields

`internal/models/profile.go`. Only `age_months` is required. Every other field left empty
means **no preference** - never "none", never a default.

| Field | Type | Effect | Vocabulary | Exposed in UI |
|---|---|---|---|---|
| `age_months` | int | hard filter | `recipe_master.min/max_age_months` | yes |
| `allergens[]` | string[] | hard filter | `allergen_tag_vocabulary` | yes |
| `diet_type` | string | hard filter, **nested**, plus step 4 preference ranker | `recipe_master.diet_type` | yes |
| `vegan` | bool | hard filter | 7 animal-derived food groups + Milk allergen tag | yes |
| `meal_type` | string | ranker | `recipe_master.meal_type` | yes |
| `clinical_marker` | string | selects nutrition target | `nutrition_target_master` | yes |
| `budget_band` | string | ranker | `recipe_master.budget_band` | yes |
| `region_culture` | string | ranker | `region_focus` | yes |
| `cuisine_code` | string | ranker | `cuisine_option` | yes |
| `clinical_flags{}` | map | rules / escalation | `clinical_rule_master.trigger_field` | yes |
| `max_prep_time_min` | int | ranker | 4 distinct values exist | yes |
| `max_cook_time_min` | int | ranker | 6 distinct values exist | yes |
| `limit` | int | result count | `meal_category_target` default 25 | yes |

Four hard filters. Only **two are a safety boundary**: age and allergens. There is no
operator override for either, and no "show excluded anyway" toggle. Diet type and vegan are
also hard filters but enforce a family's declared practice, not a clinical limit.

Everything else ranks. A ranker reorders and can never empty a result set, which is what
solves the original filter-collapse blocker.

`diet_type` is recorded twice in the engine's step accounting: once as its hard filter
(step 4, `Kind: hard_filter`) and once as a ranker (step 4, `Kind: ranker`, name "Declared
food practice - preference") that runs after step 5 has scored the pool. Being permitted a
dish by the nested chain is not the same as wanting it first - a non-vegetarian family is
correctly permitted all 940 recipes, of which 828 are vegetarian, so without the ranker
page one of their result list is dal. The ranker applies a fixed +0.04 boost
(`internal/engine/rank.go`, `applyDietRank`) to every candidate whose `diet_type` matches
the declared practice exactly, sized between the budget boost (0.03) and the culture boost
(0.05) so it can reorder within a band of similar nutrition fitness and never override the
nutrition score. This is why a single `EngineResult.Steps` array carries 14 entries for
the spec's 13 automated steps (1-13; step 8 has no data source and is recorded as an
explicit no-op, step 14 is the human release gate and never runs in the engine).

---

## Values

Counts are recipes carrying that value, out of 940.

### diet_type - 3 values, hard filter, nested

| Value | Recipes tagged | A profile declaring this may eat |
|---|---|---|
| Vegetarian | 828 | 828 |
| Non-vegetarian | 111 | **940** |
| Eggetarian | 1 | **829** |

Diet is a nested permission chain, not a category:
`vegan ⊂ vegetarian ⊂ eggetarian ⊂ non-vegetarian`. See `docs/decisions.md`.

The corpus holds exactly one recipe using any egg ingredient (`MG-R-00692`, tagged 6-9
years), so an eggetarian child under six gets zero egg dishes.

### meal_type - 6 values, ranker

| Value | Recipes |
|---|---|
| Lunch | 199 |
| Breakfast | 194 |
| Dinner | 193 |
| Snack | 182 |
| School Tiffin | 99 |
| Recovery Meal | 73 |

These do **not** match `meal_category_target`'s 7 category names. See `docs/not-built.md`
section 3.5.

### age_group - derived from `age_months`, not entered

| Group | Months | Recipes | Feeding stage |
|---|---|---|---|
| 6-8 months | 6-8 | 132 | AF01, pureed / mashed |
| 9-11 months | 9-11 | 141 | AF02, mashed / minced |
| 12-23 months | 12-23 | 145 | AF03, soft family |
| 2-5 years | 24-59 | 140 | AF04-AF05, family |
| 6-9 years | 60-119 | 123 | AF06, regular family |
| 10-12 years | 120-155 | 121 | AF07, regular family |
| 13-18 years | 156-216 | 138 | AF08-AF09, regular family |

Below 6 months returns nothing by rule: stage AF00 is milk feeding only and `CR-AGE-001`
blocks every complementary-food recipe. **An empty result there is correct output, not a
failure.**

### allergens - 11 groups, hard filter

| Declare | Corpus tag | Filters anything |
|---|---|---|
| Egg | Egg | yes |
| Fish | Fish | yes |
| Milk | Milk | yes |
| Peanut | Peanut | yes |
| Sesame | Sesame | yes |
| Soy | Soy | yes |
| Wheat | Gluten-containing cereal | yes |
| Tree nuts | none | **no** |
| Crustacean/Mollusc | none | **no** |
| Mustard | none | **no** |
| Sulphites | none | **no** |

`Wheat` is not a corpus string itself - it resolves through `allergen_tag_vocabulary`
(migration `0011`) to the literal tag the corpus actually carries, `Gluten-containing
cereal`. Before that migration a declared `Wheat` allergy silently excluded zero recipes
even though wheat-containing recipes exist and are tagged; the vocabulary table makes the
naming match explicit instead of leaving it embedded in `allergyFilter`'s SQL.

The last four are accepted and remove nothing - a genuine absence of any matching corpus
tag, not a naming mismatch. Every engine result carries this as data rather than a
document footnote: `EngineResult.unscreened_allergens` lists which of the profile's
declared allergen groups screened nothing on that call, so a client can render a
persistent "not screened - no corpus coverage" state next to the field and on the results
rather than a silent no-op. Tracked as `GAP-017` (blocker severity) in the gap register
until the provider tags the corpus for these four groups.

### budget_band - 3 values, ranker

| Value | Recipes |
|---|---|
| Low | 510 |
| Moderate | 382 |
| Premium | 48 |

### region_culture - 8 values, ranker

| Region | Country | Tier | Weight | Recipes |
|---|---|---|---|---|
| West Bengal / East India | India | 1 | 1.00 | 195 |
| Bangladesh | Bangladesh | 1 | 1.00 | 100 |
| South India | India | 2 | 0.90 | 180 |
| North India | India | 2 | 0.90 | 135 |
| West India | India | 2 | 0.90 | 110 |
| Northeast India | India | 2 | 0.90 | 80 |
| Central / Tribal India | India | 2 | 0.90 | 80 |
| Himalayan India | India | 2 | 0.90 | 60 |

Tier weight only breaks ties when no preference is stated. An explicit choice beats the
West-Bengal-first default outright. Nepal's 60 recipes are out of scope (`GAP-011`).

### cuisine_code - 27 selectable, ranker only

| Parent region | Codes |
|---|---|
| West Bengal / East India | `CL-IN-WB-KOL` `CL-IN-WB-RUR` `CL-IN-OD` `CL-IN-BR` `CL-IN-JH` |
| South India | `CL-IN-TN` `CL-IN-KL` `CL-IN-KA` `CL-IN-APTG` |
| North India | `CL-IN-PB` `CL-IN-HR` `CL-IN-UP` `CL-IN-DL` `CL-IN-RJ` |
| West India | `CL-IN-GJ` `CL-IN-MH` `CL-IN-GA` |
| Northeast India | `CL-IN-AS` `CL-IN-NE` `CL-IN-SIK` |
| Central / Tribal India | `CL-IN-MP` `CL-IN-CG` |
| Himalayan India | `CL-IN-JK` `CL-IN-HP` `CL-IN-UK` |
| Bangladesh | `CL-BD-DHA` `CL-BD-COX` |

**Cuisine can never be a hard filter.** Recipes are tagged at region level, not cuisine
level - pick "Tamil", hard-filter, get zero. Build the picker from `cuisine_option`, which
is already `COUNT(*) > 0`, never from the culture master, which offers ten cuisine codes
with no recipes behind them.

### clinical_marker - 13 nutrition targets

| Code | Target | Triggered by |
|---|---|---|
| NT00 | Routine age-appropriate | Fallback |
| NT01 | Complementary-feeding density | Automatic, age 6-23 months |
| NT02 | Growth faltering support | Clinician growth assessment |
| NT03 | Thinness / low BMI-for-age | BMI-for-age z-score |
| NT04 | Overweight risk under 5 | WHO growth z-score |
| NT05 | Overweight / obesity 5-19 | BMI-for-age z-score |
| NT06 | Iron-rich / anemia risk | Clinical history, lab or diet risk |
| NT07 | Calcium / bone health | Diet pattern or clinical risk |
| NT08 | High-protein growth/activity | Growth or activity target |
| NT09 | Vegetarian adequacy | Family diet declaration |
| NT10 | Vegan adequacy | Family diet declaration, review mandatory |
| NT11 | Picky eating / low variety | Parent questionnaire |
| NT12 | Illness / recovery support | Active or recent illness |

The target sets weights on seven scored axes and never removes a recipe. Sex is not an
input - all 13 targets are `sex_applicability = All`.

### clinical_flags - 28 trigger fields across 31 rules

Open map keyed by `clinical_rule_master.trigger_field`. Several stop generation rather
than filter it.

| Trigger field | Rule | Effect |
|---|---|---|
| `Age_Months` | `CR-AGE-001` | No complementary food before 6 months |
| `Texture_Skill` | `CR-AGE-002` | Texture must match feeding stage |
| `Confirmed_or_Highly_Suspected_Allergen` | `CR-ALL-001` | Declared immediate-type allergy |
| `Suspected_NonIgE_Allergy` | `CR-ALL-002` | Elimination needs supervision |
| `Multiple_Food_Allergies` | `CR-ALL-003` | Specialist pathway |
| `Coeliac_Status` | `CR-CEL-001/002` | No pre-diagnosis gluten-free; lifelong if confirmed |
| `CKD` | `CR-REN-001/002` | Specialist mode; no default protein restriction |
| `Chronic_Liver_Disease` | `CR-LIV-001` | Specialist nutrition mode |
| `Metabolic_Disorder` | `CR-MET-001` | Strict specialist mode |
| `IBD_or_Complex_GI` | `CR-IBD-001` | Specialist nutrition mode |
| `Diabetes_Type` | `CR-DM-001/002` | Requires diabetes team |
| `Growth_Faltering_Flag` | `CR-GROW-001` | Assessment before enrichment |
| `Severe_Wasting_or_Oedema` | `CR-GROW-002` | Not a routine pathway |
| `BMI_for_Age_Classification` | `CR-GROW-003` | No crash-diet logic |
| `Anemia_or_Iron_Risk` | `CR-IRON-001` | Support intake, never treat |
| `Bone_Health_Risk` | `CR-BONE-001` | Deficiency is not a recipe problem |
| `Acute_Diarrhoea` | `CR-GI-001` | Continue feeding and hydration |
| `Persistent_Vomiting` | `CR-GI-002` | Blocks routine generation |
| `Constipation_Support` | `CR-GI-003` | Supportive ranking only |
| `Dysphagia_Suspected` | `CR-FEED-002` | Specialist texture mode |
| `Force_Feeding_or_Cue_Issue` | `CR-FEED-001` | Responsive feeding rule |
| `Severe_Food_Aversion` | `CR-FEED-003` | Needs feeding-team input |
| `Eating_Disorder_Risk` | `CR-ED-001` | Blocks weight-loss ranking |
| `Prematurity_or_Complexity` | `CR-PREM-001` | Corrected-age review |
| `Diet_Type` | `CR-DIET-001` | Vegan needs adequacy review |
| `Post_Vaccine_Context` | `CR-VAX-001` | No vaccine-specific therapeutic diet |
| `Critical_Field_Completeness` | `CR-DATA-001` | Missing critical data stops evaluation |
| `Multiple_Active_Rules` | `CR-DATA-002` | Highest safety priority wins |

`/api/reference/clinical-markers` offers all 28 trigger fields, one row per field, each
carrying a singular `trigger_operator` and a `values` array whose elements each carry
`value`, `rule_id`, `loadable` and `escalates` - escalation is a fact about a value, not
the field as a whole (`Coeliac_Status` escalates on `Confirmed` but not on
`Suspected_Not_Confirmed`, which the engine's query never loads at all).

**`Confirmed_or_Highly_Suspected_Allergen` (`CR-ALL-001`) behaves differently from every
other trigger field.** The rule requires excluding a confirmed allergen - which step 2
already does, but only for allergens also listed in the profile's `allergens` field.
Setting this flag alone, without the matching entry in `allergens`, is a half-specified
profile: the rule fires but is neither at the specialist tier nor in the engine's
escalation-domain map, so `internal/engine/clinical.go` refuses the whole profile with an
explicit classification error rather than half-apply a clinical filter. That is still true
of the engine today. It is not reachable through the console, though: `profile-form.tsx`
renders this marker inert (`trigger_operator = contains` has no case in the engine's own
`triggerFires` switch) with a note pointing the operator at the Declared allergens field
instead. An operator who meets the raw API error directly should read it the same way: list
the allergen in `allergens`, not in `clinical_flags`.

**14 of the 28 trigger fields are inert - setting them changes no result, because no rule
the engine's query can load exists behind them:** `Acute_Diarrhoea`, `Age_Months`,
`Anemia_or_Iron_Risk`, `BMI_for_Age_Classification`, `Bone_Health_Risk`,
`Constipation_Support`, `Critical_Field_Completeness`, `Diet_Type`,
`Force_Feeding_or_Cue_Issue`, `Growth_Faltering_Flag`, `Multiple_Active_Rules`,
`Post_Vaccine_Context`, `Severe_Food_Aversion`, `Texture_Skill`. Each of these carries a
rule row in `clinical_rule_master`, but every row behind it sits below the tier
`clinicalFilter`'s WHERE clause loads (neither `hard_exclude_yn = 'Y'` nor
`human_approval_level = 'Specialist clinical approval'`, or its domain is `Age/Feeding` /
`Data Quality`, both handled structurally elsewhere in the pipeline). Half of the clinical
vocabulary the provider recorded is, in that sense, decorative. The console renders these
inert rather than hiding them, so an operator can still see the field exists and read why
it does nothing.

### Time limits - ranker

`max_prep_time_min` - the corpus holds four values: `5, 10, 15, 20`.
`max_cook_time_min` - six values: `10, 15, 20, 25, 30, 35`.

A free-entry minute field implies precision the data has not got. A four-stop and six-stop
selector is honest.

---

## Columns the engine does not accept

Populated on all 940 rows. Adding any as an input needs no new data work.

**`season`** - All season 254, Summer-friendly 239, Monsoon-friendly 227, Winter-friendly
220.

**`growth_target`** - Healthy weight maintenance 127, Energy adequacy 125, Fibre adequacy
122, Calcium adequacy 117, Protein adequacy 116, Catch-up nutrition support 112, Iron
adequacy 111, Balanced growth support 110.

**`post_vaccine_context`** - Hydration-focused 209, Normal appetite 200, Mild
fever/discomfort 186, Reduced appetite 185, Not vaccine-linked 160.

**`texture`** - Family texture 522, Soft family texture 145, Mashed/Lumpy/Soft finger food
141, Pureed/Mashed 132. Already follows from age via the feeding stage, so exposing it as a
free choice lets an operator select below the child's stage. If it becomes an input it
should only ever be a clinician's downward override carrying `CR-FEED-002`.

**`clinical_tag`** - General 256, Recovery/low-appetite 199, Constipation-support 110,
Iron-deficiency-support 102, Underweight-support 97, Picky-eating adaptable 91,
Healthy-weight 85. **Never filter on this.** It is single-valued, no recipe carries two,
and stacking it with age and region is the original filter-collapse bug.

---

## Regenerating these counts

```fish
scripts/dev_db.fish up
set -x PGURL (scripts/dev_db.fish url)

psql $PGURL -c "select diet_type, count(*) from recipe_master group by 1 order by 2 desc"
psql $PGURL -c "select meal_type, count(*) from recipe_master group by 1 order by 2 desc"
psql $PGURL -c "select age_group, min(min_age_months), max(max_age_months), count(*) from recipe_master group by 1 order by 2"
psql $PGURL -c "select budget_band, count(*) from recipe_master group by 1 order by 2 desc"
psql $PGURL -c "select season, count(*) from recipe_master group by 1 order by 2 desc"
psql $PGURL -c "select clinical_tag, count(*) from recipe_master group by 1 order by 2 desc"
psql $PGURL -c "select growth_target, count(*) from recipe_master group by 1 order by 2 desc"
psql $PGURL -c "select post_vaccine_context, count(*) from recipe_master group by 1 order by 2 desc"
psql $PGURL -c "select texture, count(*) from recipe_master group by 1 order by 2 desc"
psql $PGURL -c "select allergen_group, coalesce(nullif(corpus_tag,''),'(NONE)') from allergen_tag_vocabulary order by 1"
psql $PGURL -c "select * from cuisine_option order by region_culture, culture_code"
psql $PGURL -c "select f.region_culture, f.country, f.focus_tier, f.rank_weight, count(r.recipe_id) from region_focus f left join recipe_master r using (region_culture) group by 1,2,3,4 order by 3, 5 desc"
psql $PGURL -c "select target_code, target_name, trigger_input from nutrition_target_master order by 1"
psql $PGURL -c "select distinct trigger_field, rule_id from clinical_rule_master order by 1"
```
