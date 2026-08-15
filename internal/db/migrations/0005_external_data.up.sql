-- External data layer.
--
-- Rule 1 of the accuracy rules: never overwrite a provider column. Everything here lives
-- in its own tables, keyed back to the provider row, carrying the dataset name, the row
-- id inside that dataset, the URL and a match confidence. The provider's data stays the
-- reference; this is annotation.
--
-- A reader looking at any external value can answer three questions from the row alone:
-- where did it come from, how confident is the match, and what did the provider say
-- instead. If any of the three is missing the value does not belong here.

CREATE TABLE external_source (
    source_key    text PRIMARY KEY,
    name          text NOT NULL,
    publisher     text NOT NULL,
    url           text NOT NULL,
    licence       text NOT NULL,
    region_scope  text NOT NULL,
    retrieved_on  date NOT NULL,
    local_file    text NOT NULL,
    sha256        text NOT NULL,
    rows_loaded   integer,
    used_for      text NOT NULL
);

COMMENT ON TABLE external_source IS
    'Provenance register for every external dataset. sha256 pins the exact file that was '
    'loaded, so a value can be traced back to a byte-identical source months later.';

INSERT INTO external_source
    (source_key, name, publisher, url, licence, region_scope, retrieved_on, local_file, sha256, used_for) VALUES
    ('IFCT-2017',
     'Indian Food Composition Tables 2017',
     'ICMR-National Institute of Nutrition, Hyderabad',
     'https://huggingface.co/datasets/NUTRIC/IFCT-2017-Data',
     'Open, NIN Hyderabad. Non-commercial research and education.',
     'India',
     '2026-08-15',
     'data/external/ifct2017_index.csv',
     '678c9285b01290ad5cc318a560fa5634618ebd413dd9fe3164d5539125c4aeef',
     'Nutrition audit of ingredient_master. The provider already cites IFCT-2017 as the source for 320 of its 406 ingredients, so this checks its numbers against the table it says it used.'),

    ('INDIAN-RECIPES',
     'Cleaned Indian Food Dataset',
     'Anupam007 (Hugging Face), derived from Archana''s Kitchen',
     'https://huggingface.co/datasets/Anupam007/indian-recipe-dataset',
     'Unstated upstream; used here under non-commercial academic scope only.',
     'India',
     '2026-08-15',
     'data/external/indian_food_dataset.csv',
     '2c188cdcbf7b8c3b5d2aee9a8ce70c49c690943cab8c045336691449e019028c',
     'Suggested preparation method text, to sit beside the provider''s 6 boilerplate texts. Method and cuisine label only -- never nutrition.');

-- ---------------------------------------------------------------------------
-- Which external cuisine labels belong to which of our regions.
--
-- Hand-written from the 82 distinct Cuisine values in the recipe dataset. A cuisine
-- with no region is out of scope and its recipes are never loaded: Continental, Italian,
-- Mexican, Thai, Chinese and the rest are not a source of Bengali method text, and a
-- high ingredient overlap against one of them would be a wrong match, not a lucky one.
-- ---------------------------------------------------------------------------
CREATE TABLE external_cuisine_region_map (
    external_cuisine text PRIMARY KEY,
    region_culture   text,          -- NULL means out of scope, do not load
    note             text
);

COMMENT ON COLUMN external_cuisine_region_map.region_culture IS
    'NULL means the cuisine is out of the India + Bangladesh scope and its recipes are '
    'not loaded at all. Matching against them would produce confident wrong answers.';

INSERT INTO external_cuisine_region_map (external_cuisine, region_culture, note) VALUES
    -- West Bengal / East India
    ('Bengali Recipes',           'West Bengal / East India', 'Home region. Highest-value rows in the whole corpus.'),
    ('Oriya Recipes',             'West Bengal / East India', 'Odisha, mapped to East India alongside CL-IN-OD.'),
    ('Bihari',                    'West Bengal / East India', 'Bihar, mapped alongside CL-IN-BR.'),
    ('Jharkhand',                 'West Bengal / East India', 'Mapped alongside CL-IN-JH.'),
    -- South India
    ('South Indian Recipes',      'South India', NULL),
    ('Tamil Nadu',                'South India', NULL),
    ('Kerala Recipes',            'South India', NULL),
    ('Karnataka',                 'South India', NULL),
    ('North Karnataka',           'South India', NULL),
    ('Andhra',                    'South India', NULL),
    ('Chettinad',                 'South India', 'Tamil Nadu regional.'),
    ('Udupi',                     'South India', 'Karnataka coastal.'),
    ('Mangalorean',               'South India', 'Karnataka coastal.'),
    ('Coorg',                     'South India', 'Kodagu, Karnataka.'),
    ('Malabar',                   'South India', 'Kerala coastal.'),
    ('Hyderabadi',                'South India', 'Telangana.'),
    ('South Karnataka',           'South India', NULL),
    ('Coastal Karnataka',         'South India', NULL),
    ('Kongunadu',                 'South India', 'Western Tamil Nadu.'),
    -- North India
    ('North Indian Recipes',      'North India', NULL),
    ('Punjabi',                   'North India', NULL),
    ('Rajasthani',                'North India', NULL),
    ('Awadhi',                    'North India', 'Uttar Pradesh.'),
    ('Mughlai',                   'North India', 'Delhi/UP courtly cuisine.'),
    ('Lucknowi',                  'North India', 'Uttar Pradesh.'),
    ('Uttar Pradesh',             'North India', NULL),
    ('Haryana',                   'North India', NULL),
    ('Delhi',                     'North India', NULL),
    -- West India
    ('Maharashtrian Recipes',     'West India', NULL),
    ('Gujarati Recipes',          'West India', NULL),
    ('Goan Recipes',              'West India', NULL),
    ('Konkan',                    'West India', NULL),
    ('Malvani',                   'West India', 'Konkan coastal.'),
    ('Parsi Recipes',             'West India', 'Mumbai/Gujarat Parsi community.'),
    ('Sindhi',                    'West India', 'Post-partition Sindhi communities are concentrated in western India.'),
    -- Northeast India
    ('North East India Recipes',  'Northeast India', NULL),
    ('Assamese',                  'Northeast India', NULL),
    ('Nagaland',                  'Northeast India', NULL),
    ('Sikkim',                    'Northeast India', NULL),
    -- Central / Tribal India
    ('Madhya Pradesh',            'Central / Tribal India', NULL),
    ('Chhattisgarhi',             'Central / Tribal India', NULL),
    -- Himalayan India
    ('Kashmiri',                  'Himalayan India', NULL),
    ('Himachal',                  'Himalayan India', NULL),
    ('Uttarakhand-North Kumaon',  'Himalayan India', NULL),
    -- Pan-Indian: usable, but only as a fallback for any region
    ('Indian',                    'Pan-India', 'Unlabelled Indian. Usable for any region but ranked below a region-specific match.'),
    ('Fusion',                    NULL, 'Indian-fusion, but the base cuisine is unstated. Too ambiguous to attribute to a region.'),
    ('Indo Chinese',              NULL, 'Indian-Chinese restaurant cuisine. Not a source of home pediatric method text.'),
    -- Explicitly out of scope
    ('Continental',               NULL, 'Not South Asian.'),
    ('Italian Recipes',           NULL, 'Not South Asian.'),
    ('Mexican',                   NULL, 'Not South Asian.'),
    ('Thai',                      NULL, 'Not South Asian.'),
    ('Chinese',                   NULL, 'Not South Asian.'),
    ('Asian',                     NULL, 'Not South Asian, and too vague to place.'),
    ('French',                    NULL, 'Not South Asian.'),
    ('Middle Eastern',            NULL, 'Not South Asian.'),
    ('Mediterranean',             NULL, 'Not South Asian.'),
    ('European',                  NULL, 'Not South Asian.'),
    ('Greek',                     NULL, 'Not South Asian.'),
    ('African',                   NULL, 'Not South Asian.'),
    ('Japanese',                  NULL, 'Not South Asian.'),
    ('Korean',                    NULL, 'Not South Asian.'),
    ('Vietnamese',                NULL, 'Not South Asian.'),
    ('American',                  NULL, 'Not South Asian.'),
    ('British',                   NULL, 'Not South Asian.'),
    ('Caribbean',                 NULL, 'Not South Asian.'),
    ('Sichuan',                   NULL, 'Not South Asian.'),
    ('Shandong',                  NULL, 'Not South Asian.'),
    ('Hunan',                     NULL, 'Not South Asian.'),
    ('Cantonese',                 NULL, 'Not South Asian.'),
    ('Indonesian',                NULL, 'Not South Asian.'),
    ('Malaysian',                 NULL, 'Not South Asian.'),
    ('Burmese',                   NULL, 'Not South Asian.'),
    ('Arab',                      NULL, 'Not South Asian.'),
    ('Jewish',                    NULL, 'Not a regional cuisine in this corpus.'),
    ('World',                     NULL, 'Not a cuisine.'),
    ('World Breakfast',           NULL, 'Not a cuisine.'),
    -- Meal-slot labels that the corpus stores in the cuisine column. They say nothing
    -- about region, so they cannot be attributed to one.
    ('Snack',                     NULL, 'Meal slot, not a cuisine.'),
    ('Appetizer',                 NULL, 'Meal slot, not a cuisine.'),
    ('Brunch',                    NULL, 'Meal slot, not a cuisine.'),
    ('Lunch',                     NULL, 'Meal slot, not a cuisine.'),
    ('Dinner',                    NULL, 'Meal slot, not a cuisine.'),
    ('Side Dish',                 NULL, 'Meal slot, not a cuisine.'),
    ('Dessert',                   NULL, 'Meal slot, not a cuisine.'),
    ('Sri Lankan',                NULL, 'South Asian but out of scope.'),
    ('Nepalese',                  NULL, 'Out of scope.'),
    ('Pakistani',                 NULL, 'Out of scope.'),
    ('Afghan',                    NULL, 'Out of scope.');

-- ---------------------------------------------------------------------------
-- The external recipe corpus, loaded verbatim from the CSV. Only rows whose cuisine
-- maps to a region are stored; the rest never enter the database.
-- ---------------------------------------------------------------------------
CREATE TABLE external_recipe (
    external_recipe_id  bigint PRIMARY KEY,     -- 1-based row number in the source CSV
    source_key          text NOT NULL REFERENCES external_source (source_key),
    recipe_name         text NOT NULL,
    cuisine             text NOT NULL,
    region_culture      text NOT NULL,          -- resolved via external_cuisine_region_map
    ingredients_raw     text,
    ingredients_cleaned text,
    ingredient_tokens   text[] NOT NULL,        -- normalised, deduplicated, for Jaccard
    instructions        text NOT NULL,
    total_time_min      integer,
    url                 text
);

CREATE INDEX external_recipe_region_idx ON external_recipe (region_culture);
CREATE INDEX external_recipe_tokens_idx ON external_recipe USING gin (ingredient_tokens);

-- ---------------------------------------------------------------------------
-- Blocker 3. One suggested method per provider recipe, or none.
--
-- The provider's preparation_method_full is NOT touched. This sits beside it and must be
-- labelled in the UI as a suggestion from a named external source, never as the
-- provider's or a clinician's instruction.
-- ---------------------------------------------------------------------------
CREATE TABLE recipe_method_external (
    recipe_id           text PRIMARY KEY REFERENCES recipe_master (recipe_id) ON DELETE CASCADE,
    external_recipe_id  bigint NOT NULL REFERENCES external_recipe (external_recipe_id),
    source_key          text NOT NULL REFERENCES external_source (source_key),
    source_url          text,
    match_confidence    numeric NOT NULL CHECK (match_confidence >= 0 AND match_confidence <= 1),
    match_method        text NOT NULL,
    matched_tokens      text[] NOT NULL,
    region_match        text NOT NULL CHECK (region_match IN ('same-region', 'pan-india')),
    suggested_method    text NOT NULL,
    value_kind          text NOT NULL DEFAULT 'external'
);

COMMENT ON TABLE recipe_method_external IS
    'Suggested preparation text from an external corpus, matched on dish FORMAT first '
    'and ingredients second. The suggestion is a method for the same kind of dish, not '
    'for the same dish: a khichdi method for a khichdi with different vegetables. '
    'Annotation only -- preparation_method_full is authoritative and unmodified, and a '
    'recipe with no row here simply has no suggestion.';

-- ---------------------------------------------------------------------------
-- IFCT 2017 composition, loaded with two documented unit conversions.
--
--   energy_kcal_100g = enerc_kj / 4.184     the source stores energy in kJ
--   mineral mg/100g  = source_value * 1000  the source stores minerals in g/100g
--
-- Both are exact conversions, not estimates. Verified against published IFCT values:
-- rice raw milled 1491 kJ -> 356.4 kcal, Ca 0.00749 g -> 7.49 mg, Fe 0.00065 g -> 0.65 mg.
-- ---------------------------------------------------------------------------
CREATE TABLE external_food_composition (
    food_code        text PRIMARY KEY,
    source_key       text NOT NULL REFERENCES external_source (source_key),
    food_name        text NOT NULL,
    scientific_name  text,
    local_names      text,
    food_group       text,
    energy_kj_100g   numeric,
    energy_kcal_100g numeric,
    protein_g_100g   numeric,
    fat_g_100g       numeric,
    carb_g_100g      numeric,
    fibre_g_100g     numeric,
    calcium_mg_100g  numeric,
    iron_mg_100g     numeric,
    zinc_mg_100g     numeric,
    vitc_mg_100g     numeric,
    name_tokens      text[] NOT NULL
);

CREATE INDEX external_food_tokens_idx ON external_food_composition USING gin (name_tokens);

-- ---------------------------------------------------------------------------
-- Task 6. Every provider ingredient checked against the composition table the provider
-- says it used. Nothing is corrected: a discrepancy is reported, not overwritten.
-- ---------------------------------------------------------------------------
CREATE TABLE ingredient_nutrition_audit (
    ingredient_id       text PRIMARY KEY REFERENCES ingredient_master (ingredient_id) ON DELETE CASCADE,
    food_code           text REFERENCES external_food_composition (food_code),
    source_key          text REFERENCES external_source (source_key),
    match_confidence    numeric CHECK (match_confidence >= 0 AND match_confidence <= 1),
    match_method        text,
    provider_energy     numeric,
    external_energy     numeric,
    energy_pct_diff     numeric,
    provider_protein    numeric,
    external_protein    numeric,
    protein_pct_diff    numeric,
    provider_iron       numeric,
    external_iron       numeric,
    iron_pct_diff       numeric,
    provider_calcium    numeric,
    external_calcium    numeric,
    calcium_pct_diff    numeric,
    verdict             text NOT NULL CHECK (verdict IN ('agrees', 'discrepancy', 'unmatched')),
    used_in_recipes     integer NOT NULL,
    value_kind          text NOT NULL DEFAULT 'derived'
);

COMMENT ON TABLE ingredient_nutrition_audit IS
    'Comparison only. Provider values in ingredient_master are never modified. verdict '
    '= discrepancy means a field differs by more than 20 percent and a human should '
    'look; unmatched means no confident name match was found, which is reported rather '
    'than papered over with an approximate food.';

-- What a reviewer actually reads: the discrepancies that affect a recipe someone can see.
CREATE VIEW nutrition_audit_report AS
SELECT a.ingredient_id,
       i.english_name,
       i.bengali_name,
       i.source_id           AS provider_claimed_source,
       f.food_name           AS matched_ifct_food,
       a.match_confidence,
       a.used_in_recipes,
       a.verdict,
       a.provider_energy,  a.external_energy,  a.energy_pct_diff,
       a.provider_protein, a.external_protein, a.protein_pct_diff,
       a.provider_iron,    a.external_iron,    a.iron_pct_diff,
       a.provider_calcium, a.external_calcium, a.calcium_pct_diff
FROM ingredient_nutrition_audit a
JOIN ingredient_master i ON i.ingredient_id = a.ingredient_id
LEFT JOIN external_food_composition f ON f.food_code = a.food_code
WHERE a.used_in_recipes > 0
ORDER BY a.verdict, abs(coalesce(a.iron_pct_diff, 0)) DESC;

-- Recipe card as it will be served: the provider's text, and the suggestion beside it,
-- each clearly attributed.
CREATE VIEW recipe_method_card AS
SELECT r.recipe_id,
       r.recipe_name,
       r.region_culture,
       r.preparation_method_full          AS provider_method,
       r.review_status                    AS provider_review_status,
       m.suggested_method                 AS suggested_method_external,
       m.source_key                       AS suggested_method_source,
       m.source_url                       AS suggested_method_url,
       m.match_confidence                 AS suggested_method_confidence,
       m.region_match                     AS suggested_method_region_match,
       CASE WHEN m.recipe_id IS NULL
            THEN 'No external method met the match threshold. Provider text only.'
            ELSE 'Method for a similar ' || m.region_match || ' dish from ' || m.source_key ||
                 ', matched on dish format and ingredients (confidence ' || m.match_confidence ||
                 '). Ingredients and quantities differ from this recipe. Not the provider''s text, not clinical advice.'
       END AS suggestion_disclosure
FROM recipe_master r
LEFT JOIN recipe_method_external m ON m.recipe_id = r.recipe_id;

-- ---------------------------------------------------------------------------
-- Enrichment audit, mirroring import_run. Every enrichment pass records what it matched
-- and at what threshold, so a later reader can tell whether a coverage number came from
-- better matching or from a lowered bar.
-- ---------------------------------------------------------------------------
CREATE TABLE enrichment_run (
    run_id             bigserial PRIMARY KEY,
    started_at         timestamptz NOT NULL DEFAULT now(),
    finished_at        timestamptz,
    method_threshold   numeric NOT NULL,
    nutrition_threshold numeric NOT NULL,
    recipes_matched    integer,
    recipes_total      integer,
    ingredients_matched integer,
    ingredients_total  integer,
    ok                 boolean NOT NULL DEFAULT false
);
