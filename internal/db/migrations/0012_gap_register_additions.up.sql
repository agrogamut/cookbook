-- Four holes the register did not carry, found while specifying the Phase 3 foundation.
--
-- The register's stated purpose is that a missing value is visible rather than silently
-- absent. Four things were absent from the register itself, which is the same failure one
-- level up. Two of them re-measure on every import (see internal/importer/gaps.go); two
-- cannot be counted by any query, because there is no way to measure the absence of rows
-- nobody has written, and they follow GAP-010's precedent of a seeded NULL count.
--
-- This migration also takes the register from sixteen rows to twenty. Sixteen already
-- exist: twelve seeded in migration 0002, plus GAP-013..GAP-016 which internal/enrich/
-- gaps.go upserts on every enrichment run and which no migration ever writes. That is why
-- the new rows start at GAP-017 rather than GAP-013.

INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows, measured_by, ui_behaviour, resolution_path) VALUES

    ('GAP-017', 'blocker', 'Allergen screening', 'allergen_tag_vocabulary', 'corpus_tag',
     'Four declared allergen groups (Tree nuts, Crustacean/Mollusc, Mustard, Sulphites) have no matching tag anywhere in the recipe or ingredient corpus. Declaring one is accepted and excludes zero recipes, because nothing carries the tag. This is an absent screen, not a passing filter.',
     NULL, 'importer',
     'The group stays selectable and is returned in EngineResult.unscreened_allergens. Any client rendering a result set must show a persistent "not screened - no corpus coverage" state beside the field and on the results.',
     'Provider tags the ingredient corpus for these four groups. The count reaches zero on its own when they do; no code change is needed to close it.'),

    ('GAP-018', 'major', 'Book 1 assembly', NULL, NULL,
     'The Book Assembly Logic sheet in MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx numbers its rows 1-11, then 17-19, then 15-16. Nothing numbered 12, 13 or 14 exists in the workbook. The Book 1 pipeline specification therefore has a three-step hole between generating parent-writable pages and inserting daily-life modules.',
     NULL, 'seed',
     'Not surfaced until a Book 1 assembler exists. Recorded so the hole is not rediscovered as a bug in the assembler.',
     'Ask the provider whether steps 12-14 were dropped, renumbered, or never written. Outstanding question 11.'),

    ('GAP-019', 'blocker', 'Clinical scope', 'clinical_rule_master', 'trigger_field',
     'Down syndrome, cerebral palsy, congenital heart disease, cleft lip and palate, autism and intellectual disability have no rule row. Each changes feeding through texture, energy density, oral-motor ability, mealtime behaviour and sometimes fluid restriction. A child with one of them is currently scored like any other child.',
     NULL, 'seed',
     'No behaviour today: the engine cannot know about a condition with no trigger_field. It holds only for conditions the masters name.',
     'Provider extends clinical_rule_master. The list cannot be written on this side without inventing clinical scope. Outstanding question 10.'),

    ('GAP-020', 'blocker', 'Clinical escalation', 'clinical_rule_master', 'human_approval_level',
     'Clinical rules the provider marks Specialist clinical approval that the engine does not escalate. Before the fix this was three rules across two trigger fields: Diabetes_Type (CR-DM-001, CR-DM-002) and Multiple_Food_Allergies (CR-ALL-003), all invisible to the engine because the rule query filtered on hard_exclude_yn = Y, which the diabetes rules are not, and excluded the Food Allergy domain outright.',
     NULL, 'importer',
     'Escalated rules return blocked = true with the rule id, the domain and the provider''s own specialist_required text verbatim. A rule the engine loads that fires but sits at neither the specialist tier nor in escalationOnlyDomains is not passed through: the engine refuses the whole profile with an explicit classification error naming the rule, because acting on it would half-apply a clinical filter no recipe-side column can express.',
     'Closed in code by taking the union of the specialist tier and the hand-written domain map. The measure that remains counts specialist-tier rules the rule query drops before any escalation check can see them (domains Age/Feeding and Data Quality); it reads zero and must stay zero.');
