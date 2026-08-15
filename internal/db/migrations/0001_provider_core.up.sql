-- Provider tables. One table per source sheet, columns verbatim from the workbook
-- header row (snake_cased). Nothing here is edited, scored or enriched: these tables
-- are the reference copy of what the clinical data provider shipped.
--
-- Review_Status / Data_Quality / Validation_Required columns are preserved exactly as
-- authored. Nothing is ever marked approved on our side.

CREATE TABLE recipe_master (
    recipe_id               text PRIMARY KEY,
    recipe_name             text NOT NULL,
    country                 text NOT NULL,
    region_culture          text NOT NULL,
    age_group               text NOT NULL,
    min_age_months          integer NOT NULL,
    max_age_months          integer NOT NULL,
    meal_type               text NOT NULL,
    diet_type               text NOT NULL,
    texture                 text NOT NULL,
    season                  text NOT NULL,
    ingredient_ids          text NOT NULL,
    ingredient_names        text NOT NULL,
    ingredient_quantities_g text NOT NULL,
    preparation_method_full text NOT NULL,
    prep_time_min           integer NOT NULL,
    cook_time_min           integer NOT NULL,
    serving_size_g          integer NOT NULL,
    energy_kcal             numeric NOT NULL,
    protein_g               numeric NOT NULL,
    carb_g                  numeric NOT NULL,
    fat_g                   numeric NOT NULL,
    fibre_g                 numeric NOT NULL,
    iron_mg                 numeric NOT NULL,
    calcium_mg              numeric NOT NULL,
    growth_target           text NOT NULL,
    clinical_tag            text NOT NULL,
    allergen_tags           text NOT NULL,
    post_vaccine_context    text NOT NULL,
    budget_band             text NOT NULL,
    cost_per_serving_inr    numeric NOT NULL,
    storage_rule            text NOT NULL,
    safety_rule             text NOT NULL,
    ai_adaptation_rule      text NOT NULL,
    book1_use               text NOT NULL,
    book2_use               text NOT NULL,
    review_status           text NOT NULL,
    CONSTRAINT recipe_age_order CHECK (min_age_months <= max_age_months)
);

CREATE INDEX recipe_master_age_idx    ON recipe_master (min_age_months, max_age_months);
CREATE INDEX recipe_master_region_idx ON recipe_master (region_culture);
CREATE INDEX recipe_master_meal_idx   ON recipe_master (meal_type);
CREATE INDEX recipe_master_diet_idx   ON recipe_master (diet_type);

CREATE TABLE ingredient_source_register (
    source_id   text PRIMARY KEY,
    source_name text NOT NULL,
    coverage    text,
    use         text,
    url         text,
    authority   text,
    status      text
);

CREATE TABLE ingredient_master (
    ingredient_id             text PRIMARY KEY,
    english_name              text NOT NULL,
    bengali_name              text,
    hindi_name                text,
    food_group                text NOT NULL,
    diet_type                 text NOT NULL,
    min_age_months            integer NOT NULL,
    energy_kcal_100g          numeric NOT NULL,
    protein_g_100g            numeric NOT NULL,
    carb_g_100g               numeric NOT NULL,
    fat_g_100g                numeric NOT NULL,
    fibre_g_100g              numeric NOT NULL,
    iron_mg_100g              numeric NOT NULL,
    calcium_mg_100g           numeric NOT NULL,
    zinc_mg_100g              numeric,
    vita_ug_rae_100g          numeric,
    vitc_mg_100g              numeric,
    folate_ug_100g            numeric,
    b12_ug_100g               numeric,
    allergen_tags             text NOT NULL,
    region_availability       text,
    season                    text,
    cost_level                text,
    substitute_ingredient_ids text,
    source_id                 text REFERENCES ingredient_source_register (source_id),
    review_status             text NOT NULL,
    data_quality              text NOT NULL,
    validation_required       text,
    preparation_safety_note   text
);

CREATE INDEX ingredient_master_source_idx ON ingredient_master (source_id);

CREATE TABLE recipe_ingredient_mapping (
    mapping_id                     text PRIMARY KEY,
    recipe_id                      text NOT NULL REFERENCES recipe_master (recipe_id),
    recipe_name                    text NOT NULL,
    sequence_no                    integer NOT NULL,
    ingredient_id                  text NOT NULL REFERENCES ingredient_master (ingredient_id),
    ingredient_name_en             text NOT NULL,
    ingredient_name_bn             text,
    ingredient_name_hi             text,
    quantity                       numeric NOT NULL,
    unit                           text NOT NULL,
    preparation_state              text,
    role_in_recipe                 text,
    food_group                     text,
    ingredient_diet_type           text,
    ingredient_min_age_months      integer,
    recipe_min_age_months          integer,
    recipe_max_age_months          integer,
    recipe_age_group               text,
    meal_type                      text,
    recipe_diet_type               text,
    recipe_texture                 text,
    region_culture                 text,
    country                        text,
    recipe_season                  text,
    ingredient_allergen_tag        text,
    ingredient_region_availability text,
    ingredient_season              text,
    ingredient_cost_range          text,
    approved_substitute_ids        text,
    optional_yn                    text,
    mandatory_yn                   text,
    choking_preparation_rule       text,
    evidence_source_id             text,
    ingredient_review_status       text,
    recipe_review_status           text,
    id_link_check                  text,
    age_compatibility_check        text,
    allergen_review_check          text,
    mapping_status                 text,
    notes                          text,
    UNIQUE (recipe_id, sequence_no)
);

CREATE INDEX rim_recipe_idx     ON recipe_ingredient_mapping (recipe_id);
CREATE INDEX rim_ingredient_idx ON recipe_ingredient_mapping (ingredient_id);

CREATE TABLE culture_location_master (
    culture_code                      text PRIMARY KEY,
    country                           text NOT NULL,
    state_province                    text,
    subregion_city_cluster            text,
    geographic_zone                   text,
    urban_rural_context               text,
    cuisine_cluster                   text NOT NULL,
    primary_languages                 text,
    secondary_languages               text,
    climate_profile                   text,
    main_seasons                      text,
    staple_grains                     text,
    common_pulses_legumes             text,
    common_vegetables_greens          text,
    common_fruits                     text,
    common_dairy_or_alternatives      text,
    common_egg_meat_poultry           text,
    common_fish_seafood               text,
    common_nuts_seeds                 text,
    common_cooking_fats               text,
    common_cooking_methods            text,
    common_breakfast_formats          text,
    common_lunch_dinner_formats       text,
    common_snack_tiffin_formats       text,
    fermented_traditional_foods       text,
    typical_spice_intensity           text,
    texture_tendencies                text,
    child_friendly_local_foods        text,
    local_protein_strength            text,
    iron_rich_local_options           text,
    calcium_rich_local_options        text,
    energy_dense_local_options        text,
    seasonal_food_strength            text,
    budget_friendly_local_options     text,
    school_tiffin_compatibility       text,
    vegetarian_adaptability           text,
    vegan_adaptability                text,
    fish_adaptability                 text,
    egg_adaptability                  text,
    halal_adaptability                text,
    jain_adaptability                 text,
    family_declared_restriction_rule  text,
    religious_cultural_inference_rule text,
    local_ingredient_availability     text,
    ingredient_substitution_priority  text,
    recipe_localization_rule          text,
    recipe_name_localization          text,
    ai_ranking_priority               text,
    book1_use                         text,
    book2_use                         text,
    regional_reviewer_required        text,
    evidence_source_type              text,
    evidence_status                   text,
    review_status                     text NOT NULL
);

CREATE TABLE age_feeding_stage_master (
    stage_code                    text PRIMARY KEY,
    stage_name                    text NOT NULL,
    age_from_months               integer NOT NULL,
    age_to_months                 integer NOT NULL,
    display_age                   text,
    recipe_master_age_group       text,
    feeding_phase                 text,
    complementary_food_status     text,
    breastfeeding_or_milk_context text,
    breastfed_meal_frequency      text,
    breastfed_snack_frequency     text,
    nonbreastfed_meal_frequency   text,
    texture_minimum               text,
    texture_progression           text,
    finger_food_status            text,
    family_food_status            text,
    self_feeding_level            text,
    caregiver_feeding_mode        text,
    responsive_feeding_rule       text,
    quantity_progression_rule     text,
    food_variety_rule             text,
    animal_source_food_rule       text,
    fruit_vegetable_rule          text,
    meal_consistency_rule         text,
    utensil_hygiene_rule          text,
    choking_risk_level            text,
    key_choking_control           text,
    hard_exclusion_or_block       text,
    honey_rule                    text,
    salt_sugar_approach           text,
    illness_feeding_rule          text,
    recipe_engine_mode            text,
    allowed_recipe_types          text,
    restricted_recipe_types       text,
    personalization_priority      text,
    clinical_escalation_triggers  text,
    book1_use                     text,
    book2_use                     text,
    evidence_basis                text,
    source_url_1                  text,
    source_url_2                  text,
    evidence_status               text,
    review_status                 text NOT NULL,
    CONSTRAINT age_stage_order CHECK (age_from_months <= age_to_months)
);

-- Engine step 5. The ten recipe_score_* columns are the provider's weighted rubric;
-- they are the authoritative ranker weights and are never recomputed locally.
CREATE TABLE nutrition_target_master (
    target_code                 text PRIMARY KEY,
    target_name                 text NOT NULL,
    target_category             text,
    age_from_months             integer NOT NULL,
    age_to_months               integer NOT NULL,
    sex_applicability           text,
    trigger_input               text,
    trigger_logic               text,
    primary_objective           text,
    secondary_objectives        text,
    energy_action               text,
    protein_action              text,
    fat_quality_action          text,
    carbohydrate_action         text,
    fiber_action                text,
    hydration_action            text,
    iron_action                 text,
    calcium_action              text,
    vitamin_d_action            text,
    vitamin_a_action            text,
    vitamin_c_action            text,
    folate_b12_action           text,
    zinc_action                 text,
    iodine_action               text,
    food_diversity_action       text,
    meal_frequency_action       text,
    meal_density_action         text,
    portion_action              text,
    texture_action              text,
    animal_source_food_action   text,
    vegetarian_action           text,
    vegan_action                text,
    fruit_vegetable_action      text,
    ultraprocessed_action       text,
    free_sugar_action           text,
    sodium_action               text,
    recipe_score_energy         integer NOT NULL,
    recipe_score_protein        integer NOT NULL,
    recipe_score_iron           integer NOT NULL,
    recipe_score_calcium        integer NOT NULL,
    recipe_score_fruitveg       integer NOT NULL,
    recipe_score_diversity      integer NOT NULL,
    recipe_score_low_upf        integer NOT NULL,
    recipe_score_texture_safety integer NOT NULL,
    recipe_score_culture        integer NOT NULL,
    recipe_score_cost           integer NOT NULL,
    hard_exclusions             text,
    soft_penalties              text,
    clinical_override           text,
    book1_output                text,
    book2_output                text,
    evidence_level              text,
    source_1                    text,
    source_2                    text,
    review_status               text NOT NULL
);

CREATE TABLE nt_growth_trigger_reference (
    rule_code     text PRIMARY KEY,
    age_range     text,
    indicator     text,
    threshold     text,
    engine_action text,
    meaning       text,
    source_url    text,
    status        text
);

CREATE TABLE nt_engine_priority_logic (
    priority_order   integer PRIMARY KEY,
    rule_class       text NOT NULL,
    engine_behaviour text,
    example          text
);

CREATE TABLE clinical_rule_master (
    rule_id                    text PRIMARY KEY,
    clinical_domain            text,
    rule_name                  text NOT NULL,
    age_from_months            integer NOT NULL,
    age_to_months              integer NOT NULL,
    trigger_source             text,
    trigger_field              text,
    trigger_operator           text,
    trigger_value              text,
    rule_priority              text NOT NULL,
    ai_action_permission       text,
    engine_action              text,
    hard_exclude_yn            text,
    allowed_or_preferred       text,
    restricted_or_excluded     text,
    required_modification      text,
    nutrition_target_link      text,
    age_stage_link             text,
    recipe_filter_action       text,
    recipe_ranking_action      text,
    book1_action               text,
    book2_action               text,
    clinical_escalation_yn     text,
    escalation_reason          text,
    red_flag_examples          text,
    specialist_required        text,
    human_approval_level       text,
    do_not_do                  text,
    evidence_id                text,
    evidence_interpretation    text,
    operational_vs_guideline   text,
    version                    text,
    review_status              text NOT NULL
);

CREATE TABLE clinical_red_flag_escalation (
    escalation_code          text PRIMARY KEY,
    domain                   text,
    trigger_or_symptom       text,
    urgency_level            text,
    book_engine_action       text,
    parent_facing_action     text,
    clinician_action         text,
    recipe_generation_status text,
    examples                 text,
    evidence_id              text,
    status                   text
);

CREATE TABLE clinical_rule_priority_logic (
    priority_order   integer PRIMARY KEY,
    rule_class       text NOT NULL,
    examples         text,
    engine_behaviour text,
    can_ai_override  text,
    human_review     text
);

CREATE TABLE allergy_safety_master (
    safety_id           text PRIMARY KEY,
    domain              text,
    hazard_or_rule      text NOT NULL,
    age_from_months     integer,
    age_to_months       integer,
    trigger_source      text,
    trigger_field       text,
    trigger_operator    text,
    trigger_value       text,
    severity            text,
    engine_action       text,
    hard_block_yn       text,
    ingredient_filter   text,
    recipe_filter       text,
    substitution_rule   text,
    cross_contact_rule  text,
    preparation_safety  text,
    parent_book1_output text,
    recipe_book2_output text,
    escalation_level    text,
    human_approval      text,
    evidence_id         text,
    rule_type           text,
    version             text,
    review_status       text NOT NULL
);

CREATE TABLE allergen_mapping (
    allergen_id                          text PRIMARY KEY,
    allergen_group                       text NOT NULL,
    example_ingredients                  text,
    common_derivatives_or_hidden_sources text,
    default_action                       text,
    cross_contact_flag                   text,
    substitute_strategy                  text,
    nutrient_role_to_preserve            text,
    label_check_required                 text,
    clinical_notes                       text,
    status                               text
);

CREATE TABLE choking_texture_safety (
    hazard_id           text PRIMARY KEY,
    food_or_form        text NOT NULL,
    risk_form           text,
    age_band            text,
    default_action      text,
    safe_modification   text,
    recipe_requirement  text,
    ai_can_adapt        text,
    human_check         text,
    status              text
);

CREATE TABLE food_safety_sop (
    sop_id                   text PRIMARY KEY,
    area                     text,
    rule                     text NOT NULL,
    engine_check             text,
    recipe_output_requirement text,
    failure_action           text,
    evidence_id              text,
    status                   text
);

CREATE TABLE safety_engine_workflow (
    step          integer PRIMARY KEY,
    layer         text NOT NULL,
    question      text,
    if_pass       text,
    if_fail       text,
    ai_permission text,
    required_log  text
);

CREATE TABLE evidence_reference_master (
    evidence_id          text PRIMARY KEY,
    domain               text,
    subdomain            text,
    authority            text,
    title                text NOT NULL,
    year                 text,
    population_age       text,
    evidence_type        text,
    jurisdiction         text,
    primary_use          text,
    book1_use            text,
    book2_use            text,
    master_links         text,
    evidence_strength    text,
    commercial_use_check text,
    update_frequency     text,
    last_verified        text,
    next_review          text,
    source_url           text,
    status               text,
    notes                text
);

CREATE TABLE rule_evidence_mapping (
    mapping_id                       text PRIMARY KEY,
    engine_master                    text,
    rule_or_block_id                 text,
    rule_topic                       text,
    evidence_id                      text,
    evidence_role                    text,
    direct_or_adapted                text,
    clinical_interpretation_required text,
    can_ai_use_directly              text,
    release_requirement              text,
    status                           text
);

-- Engine spec, verbatim. Priority 1..14. The application's pipeline order is read
-- from this table rather than hard-coded, so a provider revision is a re-import.
CREATE TABLE recipe_selection_logic (
    priority                integer PRIMARY KEY,
    engine_filter_or_ranker text NOT NULL,
    type                    text NOT NULL,
    input_master            text,
    logic                   text,
    failure_action          text,
    output                  text
);

CREATE TABLE meal_category_target (
    meal_category_id       text PRIMARY KEY,
    meal_category          text NOT NULL,
    default_target_recipes text,
    minimum_age_months     integer,
    include_logic          text,
    diversity_target       text,
    notes                  text
);
