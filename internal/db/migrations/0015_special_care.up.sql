-- The provider's Special-Care Condition Feeding & Recipe Engine Master V1, delivered
-- 18 August 2026. Nine data sheets from
-- data/provider/MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx.
--
-- Its governing rule, verbatim from the README & Engine Logic sheet:
--   "Condition is a STOP GATE, not a simple recipe filter."
-- A positive diagnosis pauses generation and routes to a clinician. That is what
-- internal/engine/special_care.go implements, and Condition Stop Gates is the only sheet
-- here with an engine implementation.
--
-- Nothing in this workbook is approved. Its own Coverage Dashboard reads
-- "Clinical production approval | 0 | NOT YET", and all 108 recipe candidates carry
-- Status = CANDIDATE-REVIEW. Zero of their 102 distinct names exist in recipe_master:
-- they are archetypes to be mapped onto validated recipes later, which is why there is no
-- foreign key from special_care_recipe_candidate to recipe_master and no engine path
-- selects from it. Storing them is honest; serving them would be inventing a clinical
-- recommendation.
--
-- Columns are the workbook's headers snake_cased, per the importer's contract. Every free
-- text column stays text: these are clinical instructions for a human reviewer, and
-- parsing them into flags would be this project deciding clinical scope.

CREATE TABLE special_care_condition_gate (
    condition_id             text PRIMARY KEY,
    condition                text NOT NULL,
    gate_level               text,
    automatic_action         text,
    mandatory_minimum_inputs text,
    stop_if                  text,
    proceed_if               text,
    feeding_route_logic      text,
    texture_logic            text,
    energy_volume_logic      text,
    mandatory_reviewer       text,
    book1_output             text,
    book2_output             text,
    source_ids               text
);

CREATE TABLE special_care_parameter (
    parameter_id           text PRIMARY KEY,
    parameter              text NOT NULL,
    data_type              text,
    allowed_values_or_unit text,
    required_when          text,
    hard_stop_if_missing   text,
    source                 text,
    reviewer               text,
    engine_use             text,
    book1_use              text,
    book2_use              text,
    notes                  text
);

CREATE TABLE special_care_food_type (
    food_type_id          text PRIMARY KEY,
    food_type             text NOT NULL,
    suitable_when         text,
    use_caution_when      text,
    hard_exclude_when     text,
    typical_conditions    text,
    texture_or_effort_tag text,
    nutrition_tag         text,
    volume_tag            text,
    sensory_tag           text,
    examples              text
);

-- No surrogate key in the sheet: a condition has several phenotype rows. The provider
-- ships 13 rows over 6 conditions and the pair is unique across them.
CREATE TABLE special_care_feeding_style (
    condition_id              text NOT NULL REFERENCES special_care_condition_gate(condition_id),
    phenotype_or_trigger      text NOT NULL,
    recommended_feeding_style text,
    food_texture_principle    text,
    meal_pattern_principle    text,
    caregiver_method          text,
    avoid_or_stop             text,
    escalation                text,
    engine_output_tag         text,
    source_ids                text,
    clinical_note             text,
    PRIMARY KEY (condition_id, phenotype_or_trigger)
);

CREATE TABLE special_care_recipe_candidate (
    candidate_id          text PRIMARY KEY,
    condition_id          text NOT NULL REFERENCES special_care_condition_gate(condition_id),
    recipe_name           text NOT NULL,
    meal_category         text,
    food_type_id          text REFERENCES special_care_food_type(food_type_id),
    age_band              text,
    default_texture_tag   text,
    sensory_profile       text,
    energy_density        text,
    protein_density       text,
    fluid_load            text,
    chewing_effort        text,
    culture_region        text,
    suitable_when         text,
    do_not_use_when       text,
    required_input_params text,
    reviewer_gate         text,
    status                text NOT NULL
);

CREATE TABLE special_care_output_rule (
    rule_id       text PRIMARY KEY,
    if_condition  text NOT NULL,
    then_action   text NOT NULL,
    hard_or_soft  text,
    book1_output  text,
    book2_output  text,
    reviewer_gate text,
    error_code    text,
    notes         text
);

CREATE TABLE special_care_evidence_source (
    source_id         text PRIMARY KEY,
    organization      text,
    topic             text,
    key_engine_use    text,
    current_reference text,
    url               text,
    accessed          text,
    notes             text
);

-- "check" is the sheet's header and a reserved word in SQL. Quoted rather than renamed:
-- the importer matches snake_cased headers against information_schema, so a renamed column
-- would silently fail to bind and the sheet's own vocabulary is what the provider reads.
CREATE TABLE special_care_qa_check (
    qa_id          text PRIMARY KEY,
    domain         text,
    "check"        text,
    mandatory      text,
    reviewer       text,
    failure_action text,
    status         text
);

CREATE TABLE special_care_coverage_metric (
    metric text PRIMARY KEY,
    value  text,
    status text,
    notes  text
);

-- GAP-021 and GAP-022 record what importing this workbook makes measurable. GAP-019 is
-- rewritten rather than deleted: the gap it named is real and is now sourced, not closed.
UPDATE gap_register SET
    description = 'Down syndrome, cerebral palsy, congenital heart disease, cleft lip and '
        || 'palate, autism and intellectual disability have no row in clinical_rule_master. '
        || 'The provider''s Special-Care master (delivered 18 August 2026) now defines all '
        || 'six as STOP-REVIEW gates, so the engine stops for them instead of ranking them '
        || 'like any other child. What is still missing is their representation in the '
        || 'clinical rule master itself, and any recipe validated for them.',
    ui_behaviour = 'The engine blocks and names the reviewer the provider''s sheet requires. '
        || 'No ranked list is produced for a child with one of these conditions.',
    resolution_path = 'Provider extends clinical_rule_master, and maps the 108 special-care '
        || 'recipe candidates onto validated Recipe Master rows. Outstanding question 10.'
WHERE gap_id = 'GAP-019';

INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows,
     measured_by, ui_behaviour, resolution_path)
VALUES
    ('GAP-021', 'blocker', 'Special care',
     'special_care_recipe_candidate', 'status',
     'All special-care recipe candidates carry Status = CANDIDATE-REVIEW and the '
     || 'workbook''s own Coverage Dashboard records clinical production approval as NOT '
     || 'YET. None of their recipe names exist in recipe_master, so they are archetypes '
     || 'rather than recipes: there is nothing to serve even if approval existed.',
     0, 'importer',
     'Candidates are never returned by any engine path. They are readable as reference '
     || 'only, with their CANDIDATE-REVIEW status shown.',
     'Provider maps each candidate onto a validated Recipe Master row and completes the '
     || 'multidisciplinary review its Review & QA sheet requires.'),
    ('GAP-022', 'major', 'Special care',
     'special_care_output_rule', 'if_condition',
     'Only OR-001, the condition-detected stop, has an engine implementation. OR-002 '
     || 'through OR-014 depend on inputs this project does not collect: feeding route, '
     || 'prescribed IDDSI level, cardiology fluid orders, post-operative status, pica and '
     || 'sensory profile. Implementing them against absent inputs would mean inventing '
     || 'the inputs.',
     0, 'importer',
     'The rules are readable in full. The engine acts on OR-001 only, and the stop it '
     || 'produces is what prevents the unimplemented rules from mattering: no ranked list '
     || 'is generated for a special-care child in the first place.',
     'Collect the special-care parameters in the intake form (30 are specified in '
     || 'special_care_parameter), then implement the rules whose inputs exist.');
