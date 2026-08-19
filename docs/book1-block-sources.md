# Which provider rows fill each Book 1 block

Checked against the imported tables on 2026-08-20, row by row, before the seed in
`0018_book1_block_sources.up.sql` was written. This file is what a reviewer checks the seed
against.

## Why the map is hand-written

The workbook carries no foreign key between `book1_content_block` and the four tables that
hold its content. The join is by meaning: `DL-SLEEP-01`'s domain "Sleep routine" against
`B1-025`'s section "Sleep & Routine". A fuzzy name match would put a toilet-training red flag
on a dental page, which is the same class of failure the recipe method join already
demonstrated at scale. Thirty-eight rows read once by a person beats a matcher nobody can
audit - the argument that produced `culture_region_map` in migration `0002`.

## What each source table actually contains

Verified by reading every row, not by counting them.

| Table | Rows | Populated columns | Blank columns |
|---|---|---|---|
| `book1_daily_life_module` | 13 | `domain`, `suggested_age_context`, `readiness_or_reference`, `progress_goal`, `concern_or_red_flag`, `approach_doctor_or_specialist`, `book1_display`, `ai_limit` | `parent_actual_status`, `date_or_frequency`, `parent_example_or_note` |
| `book1_monitoring_template` | 18 | all ten | none |
| `book1_illness_feeding_block` | 5 | all eight | none |
| `book1_evidence_source` | 13 | `authority`, `topic`, `reference`, `url`, `how_used`, `important_limitation` | none |

**The three blank daily-life columns are the finding.** `parent_actual_status`,
`date_or_frequency` and `parent_example_or_note` are empty on all 13 rows, and they are empty
by design: they are the columns the *parent* fills in. The provider shipped the reference
side of an ideal-versus-actual table and left the actual side blank. Rendering them as
writing lines is reading the table as it was built, not filling a gap.

**`ai_limit` is a per-row prohibition** and every row carries one: "Do not diagnose sleep
disorder", "No psychiatric diagnosis", "No weight-loss or supplement prescription", "Texture
safety rules override independence goals", "Do not declare child 'late' by age alone". These
print on the page they constrain. A limit honoured silently is a limit the next person to
touch the template does not know about.

## The map

| Block | Section | Template | Source rows | Checked |
|---|---|---|---|---|
| B1-001 | Child Profile | `B1-PROFILE-01` | child profile | built |
| B1-002 | Consultation Summary | `B1-TRACKER-01` | own `writable_fields`: Parent priority; target date; notes | goals have no input table; the form is the honest output |
| B1-003 | Growth Monitoring | `B1-GROWTH-01` | `child_growth_measurement` | built |
| B1-004 | Growth trend | **none** | - | **stays omitted: declared input is a z-score engine this project does not carry** |
| B1-005 | Personal Nutrition Target | `B1-STAGE-01` | `age_feeding_stage_master` for age | 10 stages, no age gaps 0-228mo |
| B1-006 | Meal schedule | `B1-STAGE-01` | same row, meal-frequency columns | breastfed and non-breastfed are separate columns and stay separate rows |
| B1-007 | Feeding Approach | `B1-STAGE-01` | same row, `responsive_feeding_rule`, `food_variety_rule` | full sentences, verbatim |
| B1-008 | Feeding comparison | `B1-STAGE-01` | adjacent stages either side | shows what changes next |
| B1-009 | Vaccination | `B1-VAX-01` | `book1_vaccine_schedule` (44) | built |
| B1-010 | Vaccine reaction log | `B1-TRACKER-01` | `PM-VAX-01` + own `writable_fields` | |
| B1-011 | Development Monitoring | `B1-DEV-01` | `book1_development_milestone` (33) | built |
| B1-012 | Development red flags | `B1-RED-01` | milestone `concern_or_red_flag` | built |
| B1-013 | Age-specific Monitoring | `B1-TRACKER-01` | `PM-DEV-01`, `PM-GROW-01/02/03` | |
| B1-014 | Development by Age | `B1-DEV-01` | `book1_development_milestone` | built |
| B1-015 | Common Illness Feeding | `B1-ILLNESS-01` | `IF-001`..`IF-005` | fever, diarrhoea, vomiting, constipation, recovery |
| B1-016 | Illness supportive table | `B1-ILLNESS-01` | `IF-001`..`IF-005`, `PM-ILL-01` | |
| B1-017 | Food + bowel tracker | `B1-TRACKER-01` | `PM-BOWEL-01` | |
| B1-018 | Allergy & Safety | `B1-SAFETY-01` | `child_allergen`, `allergy_safety_master`, `choking_texture_safety` | the child's own, not the corpus |
| B1-019 | Food Acceptance | `B1-TRACKER-01` | `PM-FOOD-01`, `PM-DIV-01` | |
| B1-020 | Weekly Monitoring | `B1-TRACKER-01` | resolved in Go over age-relevant `PM-*` | dashboard, not a fixed list |
| B1-021 | Follow-up | `B1-TRACKER-01` | `PM-FU-01` | |
| B1-022 | Reference & Disclaimer | `B1-REFS-01` | all 13 `book1_evidence_source` | seeded by SELECT, not by 13 literals |
| B1-023 | Toilet Training readiness | `B1-DAILY-01` | `DL-TOIL-01`, `DL-TOIL-02`; `PM-TOIL-01` | readiness must precede progress |
| B1-024 | Toilet Training red flag | `B1-DAILY-01` | `DL-TOIL-03`; `PM-TOIL-01` | night-time dryness |
| B1-025 | Sleep & Routine | `B1-DAILY-01` | `DL-SLEEP-01`; `PM-SLEEP-01` | |
| B1-026 | Oral & Dental | `B1-DAILY-01` | `DL-DENT-01`; `PM-DENT-01` | |
| B1-027 | Self-Care & Adaptive | `B1-DAILY-01` | `DL-SELF-01`, `DL-SELF-02`; `PM-SELF-01` | self-feeding before dressing |
| B1-028 | Screen & Digital | `B1-DAILY-01` | `DL-SCREEN-01`, `DL-SCREEN-02`; `PM-SCR-01` | meal screen before bedtime screen |
| B1-029 | Physical Activity | `B1-DAILY-01` | `DL-ACT-01`; `PM-ACT-01` | |
| B1-030 | School & Behaviour | `B1-DAILY-01` | `DL-SCHOOL-01`, `DL-BEH-01`; `PM-SCHOOL-01`, `PM-BEH-01` | the block title names both |
| B1-031 | Daily-Life Dashboard | `B1-TRACKER-01` | resolved in Go over age-relevant `PM-*` | |
| B1-032 | Adolescent Self-Management | `B1-DAILY-01` | `DL-ADO-01` | age 120-216mo, so absent from a young child's book |

## Rows deliberately mapped to more than one block

`PM-TOIL-01` feeds both B1-023 and B1-024, because the workbook splits toilet training into
a readiness block and a red-flag block while shipping one tracker for both. The tracker
prints on each, which is what a parent using either page needs; the alternative - printing it
once and cross-referencing - assumes a page number the template cannot know.

## Rows that reach no block

None. All 13 daily-life modules, 18 monitoring templates, 5 illness blocks and 13 evidence
sources are reachable, asserted in both directions by
`TestBlockSourceSeedResolvesBothWays`.
