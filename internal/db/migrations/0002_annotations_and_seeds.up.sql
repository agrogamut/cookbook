-- Annotation layer: everything in this migration is either hand-written from the two
-- provider ID lists (and verifiable by eye), or a documented computation over provider
-- columns. No value here is estimated, guessed or scraped.
--
-- Every table carries provenance. Derived tables carry the formula reference in
-- derivation_note so a reader can reproduce the number from the provider rows.

-- ---------------------------------------------------------------------------
-- Blocker 4: Recipe_Master.Region_Culture is free text and matches no field on
-- Culture_Location_Master. This is the bridge. Hand-written from the 9 region strings
-- and the 37 culture codes; 29 codes map, 8 have zero recipes and are excluded.
-- ---------------------------------------------------------------------------
-- No foreign key is declared on culture_code. The seed below is hand-written and ships
-- with the migration, which runs before the provider tables are populated, so a
-- declared FK would fail at migrate time and would only be satisfiable after an import.
-- Both directions of this join are asserted instead by the integrity suite
-- (TestCultureRegionMapResolves), which is the Phase 1 exit gate anyway.
CREATE TABLE culture_region_map (
    region_culture text NOT NULL,
    culture_code   text NOT NULL,
    PRIMARY KEY (region_culture, culture_code)
);

COMMENT ON TABLE culture_region_map IS
    'Hand-written bridge from Recipe_Master.Region_Culture (9 free-text values) to '
    'Culture_Location_Master.Culture_Code (37 codes). Source: both provider workbooks, '
    'matched on country + state/province + geographic zone. Not derived, not fuzzy.';

INSERT INTO culture_region_map (region_culture, culture_code) VALUES
    ('West Bengal / East India', 'CL-IN-WB-KOL'),
    ('West Bengal / East India', 'CL-IN-WB-RUR'),
    ('West Bengal / East India', 'CL-IN-OD'),
    ('West Bengal / East India', 'CL-IN-BR'),
    ('West Bengal / East India', 'CL-IN-JH'),
    ('South India',              'CL-IN-TN'),
    ('South India',              'CL-IN-KL'),
    ('South India',              'CL-IN-KA'),
    ('South India',              'CL-IN-APTG'),
    ('North India',              'CL-IN-PB'),
    ('North India',              'CL-IN-HR'),
    ('North India',              'CL-IN-UP'),
    ('North India',              'CL-IN-DL'),
    ('North India',              'CL-IN-RJ'),
    ('West India',               'CL-IN-GJ'),
    ('West India',               'CL-IN-MH'),
    ('West India',               'CL-IN-GA'),
    ('Northeast India',          'CL-IN-AS'),
    ('Northeast India',          'CL-IN-NE'),
    ('Northeast India',          'CL-IN-SIK'),
    ('Central / Tribal India',   'CL-IN-MP'),
    ('Central / Tribal India',   'CL-IN-CG'),
    ('Himalayan India',          'CL-IN-JK'),
    ('Himalayan India',          'CL-IN-HP'),
    ('Himalayan India',          'CL-IN-UK'),
    ('Bangladesh',               'CL-BD-DHA'),
    ('Bangladesh',               'CL-BD-COX'),
    ('Nepal',                    'CL-NP-KTM'),
    ('Nepal',                    'CL-NP-HILL');

-- ---------------------------------------------------------------------------
-- Geographic scope. The project targets India and Bangladesh; West Bengal / East India
-- is the home region and ranks first. Nepal recipes are kept verbatim (deleting
-- provider rows is not our call) but sit outside the enrichment scope, so no external
-- dataset join is attempted against them and they rank last.
--
-- rank_weight is a DERIVED ranker multiplier applied at engine step 7, never a filter.
-- Formula: tier 1 -> 1.00, tier 2 -> 0.90, tier 3 -> 0.80. Flat 0.10 per tier, chosen
-- so region preference can reorder a result list but can never outweigh the nutrition
-- rubric (weights 1-5) or remove a recipe.
-- ---------------------------------------------------------------------------
CREATE TABLE region_focus (
    region_culture   text PRIMARY KEY,
    country          text NOT NULL,
    focus_tier       integer NOT NULL CHECK (focus_tier BETWEEN 1 AND 3),
    rank_weight      numeric NOT NULL,
    enrichment_scope boolean NOT NULL,
    rationale        text NOT NULL,
    derivation_note  text NOT NULL
);

COMMENT ON TABLE region_focus IS
    'Project scope and region ranking tiers. rank_weight is derived (see formula in '
    'CLAUDE.md, "Region focus"); it multiplies the culture ranker only and cannot '
    'exclude a recipe. enrichment_scope=false means no external dataset is joined.';

INSERT INTO region_focus (region_culture, country, focus_tier, rank_weight, enrichment_scope, rationale, derivation_note) VALUES
    ('West Bengal / East India', 'India',      1, 1.00, true,  'Home region. Bengali cuisine, languages and ingredient availability are the primary target.', 'tier 1 -> 1.00'),
    ('Bangladesh',              'Bangladesh',  1, 1.00, true,  'Shared Bengali food culture, language and staples with West Bengal; in scope alongside it.',  'tier 1 -> 1.00'),
    ('North India',             'India',       2, 0.90, true,  'India-wide scope.', 'tier 2 -> 0.90'),
    ('South India',             'India',       2, 0.90, true,  'India-wide scope.', 'tier 2 -> 0.90'),
    ('West India',              'India',       2, 0.90, true,  'India-wide scope.', 'tier 2 -> 0.90'),
    ('Northeast India',         'India',       2, 0.90, true,  'India-wide scope.', 'tier 2 -> 0.90'),
    ('Central / Tribal India',  'India',       2, 0.90, true,  'India-wide scope.', 'tier 2 -> 0.90'),
    ('Himalayan India',         'India',       2, 0.90, true,  'India-wide scope.', 'tier 2 -> 0.90'),
    ('Nepal',                   'Nepal',       3, 0.80, false, 'Outside the India + Bangladesh scope. Provider rows retained verbatim; no external enrichment attempted.', 'tier 3 -> 0.80');

-- ---------------------------------------------------------------------------
-- Selectable cuisine options. Built from a COUNT(*) > 0 join so the UI can never offer
-- a cuisine that returns an empty page. The 8 orphan culture codes (Bhutan, Sri Lanka,
-- Pakistan x2, Maldives, Myanmar, Afghanistan, Andaman & Nicobar) have no recipes and
-- are absent by construction, not by a hard-coded exclusion list.
-- ---------------------------------------------------------------------------
CREATE VIEW cuisine_option AS
SELECT c.culture_code,
       c.cuisine_cluster,
       c.country,
       c.state_province,
       m.region_culture,
       f.focus_tier,
       f.rank_weight,
       count(r.recipe_id) AS recipe_count
FROM culture_location_master c
JOIN culture_region_map m ON m.culture_code = c.culture_code
JOIN region_focus f       ON f.region_culture = m.region_culture
JOIN recipe_master r      ON r.region_culture = m.region_culture
GROUP BY c.culture_code, c.cuisine_cluster, c.country, c.state_province,
         m.region_culture, f.focus_tier, f.rank_weight
HAVING count(r.recipe_id) > 0;

COMMENT ON VIEW cuisine_option IS
    'Cuisine dropdown source. A cuisine cluster is a RANKER, never a hard filter: '
    'selecting one filters on its parent region and ranks up recipes matching the '
    'culture row staples. Recipes are not tagged at cluster granularity.';

-- ---------------------------------------------------------------------------
-- Engine step 5 input. Nutrition normalised min-max WITHIN the recipe age band, so a
-- 6-month puree is compared against other 6-month purees rather than a teenage bowl.
--
-- Formula (documented in CLAUDE.md, "The nutrition ranker"):
--     norm(x) = (x - min(x within age_group)) / (max - min within age_group)
--     norm(x) = 0 when the band is degenerate (max = min)
-- Inputs are provider columns on Recipe_Master only. Nothing external, nothing filled.
-- ---------------------------------------------------------------------------
CREATE VIEW recipe_nutrition_normalised AS
WITH band AS (
    SELECT age_group,
           min(energy_kcal) AS e_lo, max(energy_kcal) AS e_hi,
           min(protein_g)   AS p_lo, max(protein_g)   AS p_hi,
           min(iron_mg)     AS f_lo, max(iron_mg)     AS f_hi,
           min(calcium_mg)  AS c_lo, max(calcium_mg)  AS c_hi,
           min(fibre_g)     AS b_lo, max(fibre_g)     AS b_hi,
           min(cost_per_serving_inr) AS k_lo, max(cost_per_serving_inr) AS k_hi
    FROM recipe_master
    GROUP BY age_group
)
SELECT r.recipe_id,
       r.age_group,
       CASE WHEN b.e_hi = b.e_lo THEN 0 ELSE (r.energy_kcal - b.e_lo) / (b.e_hi - b.e_lo) END AS norm_energy,
       CASE WHEN b.p_hi = b.p_lo THEN 0 ELSE (r.protein_g   - b.p_lo) / (b.p_hi - b.p_lo) END AS norm_protein,
       CASE WHEN b.f_hi = b.f_lo THEN 0 ELSE (r.iron_mg     - b.f_lo) / (b.f_hi - b.f_lo) END AS norm_iron,
       CASE WHEN b.c_hi = b.c_lo THEN 0 ELSE (r.calcium_mg  - b.c_lo) / (b.c_hi - b.c_lo) END AS norm_calcium,
       CASE WHEN b.b_hi = b.b_lo THEN 0 ELSE (r.fibre_g     - b.b_lo) / (b.b_hi - b.b_lo) END AS norm_fibre,
       -- cost is inverted: cheaper scores higher, matching Recipe_Score_Cost intent
       CASE WHEN b.k_hi = b.k_lo THEN 0 ELSE (b.k_hi - r.cost_per_serving_inr) / (b.k_hi - b.k_lo) END AS norm_cost_inverted
FROM recipe_master r
JOIN band b ON b.age_group = r.age_group;

-- ---------------------------------------------------------------------------
-- Recipe ingredient diversity, engine step 12 and the Recipe_Score_Diversity axis.
-- Derived: distinct food groups per recipe, from the mapping table only.
-- ---------------------------------------------------------------------------
CREATE VIEW recipe_diversity AS
SELECT m.recipe_id,
       count(DISTINCT m.food_group)    AS distinct_food_groups,
       count(DISTINCT m.ingredient_id) AS distinct_ingredients
FROM recipe_ingredient_mapping m
GROUP BY m.recipe_id;

-- ---------------------------------------------------------------------------
-- Gap register. Every known hole in the data, counted, with the reason it is a hole
-- and what would close it. Populated partly by seed (structural gaps found in the
-- audit) and partly by the importer (row counts measured at import time).
--
-- This table exists so that a missing value is visible rather than silently absent.
-- ---------------------------------------------------------------------------
CREATE TABLE gap_register (
    gap_id          text PRIMARY KEY,
    severity        text NOT NULL CHECK (severity IN ('blocker', 'major', 'minor', 'parked')),
    area            text NOT NULL,
    source_table    text,
    source_column   text,
    description     text NOT NULL,
    affected_rows   integer,
    measured_by     text NOT NULL CHECK (measured_by IN ('seed', 'importer')),
    ui_behaviour    text NOT NULL,
    resolution_path text NOT NULL,
    measured_at     timestamptz
);

COMMENT ON TABLE gap_register IS
    'Every known missing or provisional field. ui_behaviour states what the user sees '
    'in place of the missing value; the rule is an explicit gap, never a substitute.';

INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows, measured_by, ui_behaviour, resolution_path) VALUES
    ('GAP-001', 'blocker', 'Recipe content', 'recipe_master', 'preparation_method_full',
     'Only 6 unique preparation texts across 1000 recipes, differing only in liquid volume. This is the text users actually read.',
     1000, 'importer',
     'Show the provider text verbatim, plus a "suggested method" panel sourced externally and labelled with its dataset and match score. Never present the external text as the provider''s.',
     'Thresholded ingredient-overlap join to an Indian recipe corpus into suggested_method_external. Provider column untouched.'),

    ('GAP-002', 'blocker', 'Recipe content', 'recipe_master', 'safety_rule',
     'One unique Safety_Rule text across all 1000 recipes. Same for storage_rule and ai_adaptation_rule.',
     1000, 'importer',
     'Display verbatim as a general rule, not as recipe-specific guidance.',
     'Ask the provider whether per-recipe safety text is coming. No local substitute: safety text is not something to write ourselves.'),

    ('GAP-003', 'blocker', 'Clinical coverage', 'recipe_master', 'clinical_tag',
     'Zero iron-support recipes for 6-11 months, the peak window for infant iron deficiency in South Asia. Those two age bands carry only General and Recovery tags.',
     NULL, 'importer',
     'Show "limited clinical data for this age band" and return the nutrition-ranked list instead of an empty page. Do not synthesise infant recipes.',
     'Provider question. There is no verified source for a 6-11 month iron-support recipe they did not supply.'),

    ('GAP-004', 'blocker', 'Approval', 'recipe_master', 'review_status',
     'All 1000 recipes are Draft. All 406 ingredients are Needs Validation with Data_Quality = Provisional planning value. All 3317 mappings are draft.',
     NULL, 'importer',
     'Persistent "provisional data, academic demo" banner. Review_Status is displayed, never overwritten.',
     'Provider sign-off. Out of scope for an academic build; the flags stay in the schema.'),

    ('GAP-005', 'major', 'Filter collapse', 'recipe_master', 'clinical_tag',
     'Tag columns are single-valued: no recipe carries more than one value in clinical_tag, growth_target, season, meal_type, texture or post_vaccine_context. Stacking hard filters empties the result set.',
     1000, 'importer',
     'Clinical tag is a display badge only. Ranking is done by the NT00-NT12 rubric, which cannot return zero rows.',
     'Implemented: rubric as ranker. No tags invented.'),

    ('GAP-006', 'major', 'Allergen tagging', 'ingredient_master', 'allergen_tags',
     '317 of 406 ingredients carry the literal tag "None identified in starter tagging", which is an admission that tagging is incomplete rather than a result of screening.',
     317, 'importer',
     'Treat as unknown, not as safe. Allergy filtering stays a hard filter on declared allergens; unscreened ingredients are surfaced as unscreened.',
     'Cross-check obvious allergen classes (nuts, sesame, dairy, soy) against an open allergen source, recorded in *_External columns with confidence.'),

    ('GAP-007', 'minor', 'Ingredient coverage', 'ingredient_master', NULL,
     'Only 102 of 406 ingredients appear in any recipe. 304 are unused.',
     304, 'importer',
     'Ingredient browse lists only ingredients with recipes; the rest are reachable as substitutes.',
     'No action. The unused rows are harmless reference data.'),

    ('GAP-008', 'minor', 'Evidence citations', 'evidence_reference_master', 'evidence_id',
     'Evidence IDs are fragmented across five registers with colliding names (EV-WHO-IYCF vs EV-WHO-IYCF-2021). Book 1 uses a separate IAP-* namespace.',
     NULL, 'seed',
     'Citations are not displayed until the namespaces are reconciled.',
     'Only matters once citations are shown. Reconcile by authority + URL, not by name.'),

    ('GAP-009', 'parked', 'Book rendering', NULL, NULL,
     'Page Registry module IDs (B1-00..B1-58, 124 modules) do not overlap Book Content Master block IDs (B1-001..B1-032, 64 blocks).',
     NULL, 'seed',
     'Not surfaced. This is a PDF/book assembly concern.',
     'Parked. The website does not consume these tables.'),

    ('GAP-010', 'parked', 'Governance', NULL, NULL,
     'MadamGY_Review_Version_Control_Master_V1.xlsx is an empty scaffold: Release Gate has one row with 16 of 26 columns blank, Incident Log 17 of 19 blank, Reviewer Directory lists roles but no names. Not imported.',
     NULL, 'seed',
     'Not surfaced.',
     'Provider-owned. Would become live again if this were ever shown to real parents.'),

    ('GAP-011', 'minor', 'Geographic scope', 'recipe_master', 'region_culture',
     'Nepal (60 recipes) falls outside the India + Bangladesh project scope. Rows are retained verbatim but receive no external enrichment.',
     60, 'importer',
     'Ranked last via region_focus tier 3. Shown with no suggested-method panel.',
     'Deliberate scope decision, not a data fault. Recorded so the absence of enrichment is explainable.'),

    ('GAP-012', 'minor', 'Culture coverage', 'culture_location_master', 'culture_code',
     '8 of 37 culture codes have zero recipes: CL-BT, CL-LK-WP, CL-PK-PB, CL-PK-SD, CL-MV, CL-MM-YGN, CL-AF-KBL, CL-IN-AN. Six of these are outside the project scope anyway.',
     8, 'importer',
     'Absent from the cuisine dropdown by construction (cuisine_option requires COUNT(*) > 0).',
     'No action. CL-IN-AN (Andaman & Nicobar) is Indian and would need provider recipes to become selectable.');

-- ---------------------------------------------------------------------------
-- Import audit. One row per import run, so "did the re-import change anything?" is a
-- query rather than a guess.
-- ---------------------------------------------------------------------------
CREATE TABLE import_run (
    run_id      bigserial PRIMARY KEY,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    source_dir  text NOT NULL,
    ok          boolean NOT NULL DEFAULT false
);

CREATE TABLE import_table_stat (
    run_id       bigint NOT NULL REFERENCES import_run (run_id) ON DELETE CASCADE,
    table_name   text NOT NULL,
    source_file  text NOT NULL,
    source_sheet text NOT NULL,
    header_row   integer NOT NULL,
    rows_read    integer NOT NULL,
    rows_written integer NOT NULL,
    content_hash text NOT NULL,
    PRIMARY KEY (run_id, table_name)
);

COMMENT ON COLUMN import_table_stat.content_hash IS
    'SHA-256 over the ordered, normalised row values written to this table. Two runs '
    'over the same workbooks must produce identical hashes; the integrity suite asserts it.';
