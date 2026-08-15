-- Hand-written ingredient aliases to IFCT 2017.
--
-- Automatic name matching reached 26 of the 94 ingredients a recipe actually uses. The
-- rest carry regional names the composition table does not use -- "Kochu shak" against
-- "Colocasia leaves", "Katla" against "Catla", "Ragi" against a row IFCT also calls Ragi
-- but which the tokeniser could not separate from "Finger millet". Fuzzy matching cannot
-- close that gap without also inventing matches, so it is closed by hand instead.
--
-- Every row here is one person deciding two names mean the same food. That is a
-- reviewable claim: the provider name, the IFCT food code and the reason are all in the
-- row, and the integrity suite checks every code resolves.
--
-- Deliberately absent, because IFCT 2017 has no counterpart and a near-enough food would
-- be an invented value:
--
--   Curd/Yogurt, Chhena       IFCT carries only milk, paneer and khoa
--   Oats                      not an IFCT food
--   Tofu, soy chunks          processed soy products are not listed
--   Turnip, Chayote           not listed
--   Pointed gourd (parwal)    not listed, despite being a staple Bengali vegetable
--   Foxtail/Barnyard millet   IFCT lists Samai and Varagu but not these
--   Buckwheat, Seabuckthorn   not listed
--   Most named fish           IFCT lists 92 marine and 10 freshwater species, but not
--                             Bhetki, Pabda, Punti, Mola, Tengra, Koi or Shing
--
-- Those ingredients keep the provider's provisional values and are reported as
-- unverified. That is the honest outcome, not a defect in this table.

CREATE TABLE ingredient_ifct_alias (
    ingredient_english_name text PRIMARY KEY,
    food_code               text NOT NULL,
    match_note              text NOT NULL,
    exactness               text NOT NULL
        CHECK (exactness IN ('same-food', 'same-food-different-form', 'closest-variety'))
);

COMMENT ON TABLE ingredient_ifct_alias IS
    'Hand-written provider-name to IFCT-2017 food code mapping. exactness records how '
    'literal the claim is: same-food is an identity, closest-variety is a judgement call '
    'and should be read as such.';

COMMENT ON COLUMN ingredient_ifct_alias.exactness IS
    'same-food: the two names denote the same food. '
    'same-food-different-form: same food, different processing (flour vs whole grain) -- '
    'the numbers will differ and that difference is real, not an error. '
    'closest-variety: IFCT lists varieties the provider does not distinguish.';

INSERT INTO ingredient_ifct_alias (ingredient_english_name, food_code, match_note, exactness) VALUES
    -- Cereals and millets
    ('Ragi/Finger millet',                 'A010', 'IFCT calls it Ragi.', 'same-food'),
    ('Mandua/Finger millet Himalayan',     'A010', 'Mandua is the Himalayan name for ragi.', 'same-food'),
    ('Bajra/Pearl millet',                 'A003', 'IFCT calls it Bajra.', 'same-food'),
    ('Jowar/Sorghum',                      'A005', 'IFCT calls it Jowar.', 'same-food'),
    ('Little millet',                      'A016', 'IFCT calls it Samai.', 'same-food'),
    ('Samai/Little millet Tamil',          'A016', 'Tamil name for the same millet.', 'same-food'),
    ('Kodo millet',                        'A017', 'IFCT calls it Varagu.', 'same-food'),
    ('Varagu/Kodo millet Tamil',           'A017', 'Tamil name for the same millet.', 'same-food'),
    ('Amaranth grain/Rajgira',             'A002', 'Pale brown amaranth seed.', 'closest-variety'),
    ('Cornmeal/Makki atta',                'A006', 'Ground dry maize.', 'same-food-different-form'),
    ('Flattened rice (poha/chire)',        'A011', 'IFCT calls it Rice flakes.', 'same-food'),
    ('Chiura/Beaten rice',                 'A011', 'Nepali name for flattened rice.', 'same-food'),
    ('Puffed rice (muri)',                 'A012', 'IFCT calls it Rice puffed.', 'same-food'),
    ('Rice',                               'A015', 'Milled white rice, the default in these recipes.', 'closest-variety'),
    ('Basmati rice',                       'A015', 'IFCT does not separate aromatic varieties.', 'closest-variety'),
    ('Gobindobhog rice',                   'A015', 'Bengali aromatic short-grain; IFCT lists no varietal row.', 'closest-variety'),
    ('Kalijira rice',                      'A015', 'Bengali aromatic short-grain; IFCT lists no varietal row.', 'closest-variety'),
    ('Chinigura rice',                     'A015', 'Bangladeshi aromatic short-grain; IFCT lists no varietal row.', 'closest-variety'),
    ('Joha rice',                          'A015', 'Assamese aromatic rice; IFCT lists no varietal row.', 'closest-variety'),
    ('Idli rice',                          'A014', 'Parboiled milled rice, which is what idli rice is.', 'same-food'),
    ('Parboiled rice',                     'A014', 'IFCT: Rice, parboiled, milled.', 'same-food'),
    ('Red rice',                           'A013', 'Unmilled rice; brown is the closest IFCT row.', 'closest-variety'),
    ('Black rice',                         'A013', 'Unmilled rice; brown is the closest IFCT row.', 'closest-variety'),
    ('Sticky rice/Glutinous rice',         'A015', 'IFCT lists no glutinous varietal.', 'closest-variety'),
    ('Wheat flour (atta)',                 'A019', 'IFCT: Wheat flour, atta.', 'same-food'),
    ('Semolina (suji)',                    'A022', 'IFCT: Wheat, semolina.', 'same-food'),
    ('Barley',                             'A004', 'Direct.', 'same-food'),
    ('Quinoa',                             'A009', 'Direct.', 'same-food'),

    -- Pulses and legumes
    ('Masoor dal',                         'B013', 'IFCT: Lentil dal.', 'same-food'),
    ('Moong dal',                          'B010', 'IFCT: Green gram, dal.', 'same-food'),
    ('Green gram whole',                   'B011', 'Direct.', 'same-food'),
    ('Black gram/Urad dal',                'B003', 'IFCT: Black gram, dal.', 'same-food'),
    ('Chana dal',                          'B001', 'IFCT: Bengal gram, dal.', 'same-food'),
    ('Bengal gram whole',                  'B002', 'Direct.', 'same-food'),
    ('Black chickpea/Kala chana',          'B002', 'Whole bengal gram.', 'same-food'),
    ('Chickpea/Kabuli chana',              'B002', 'IFCT does not separate kabuli from desi.', 'closest-variety'),
    ('Toor/Arhar dal',                     'B021', 'IFCT: Red gram, dal.', 'same-food'),
    ('Kidney beans/Rajma',                 'B020', 'IFCT: Rajmah, red.', 'closest-variety'),
    ('Cowpea/Lobia',                       'B006', 'IFCT: Cowpea, white.', 'closest-variety'),
    ('Horse gram/Kulthi',                  'B012', 'IFCT: Horse gram, whole.', 'same-food'),
    ('Kollu/Horse gram Tamil',             'B012', 'Tamil name for horse gram.', 'same-food'),
    ('Gahat/Kulath Himalayan horse gram',  'B012', 'Himalayan name for horse gram.', 'same-food'),
    ('Moth bean/Matki',                    'B016', 'IFCT: Moth bean.', 'same-food'),
    ('Rice bean',                          'B023', 'IFCT: Ricebean.', 'same-food'),
    ('Soybean/soy chunks',                 'B025', 'Whole soya bean. Soy chunks are extruded defatted flour, so this is the bean, not the chunk.', 'same-food-different-form'),
    ('Bhatt/Black soybean Himalayan',      'B024', 'IFCT: Soya bean, brown.', 'closest-variety'),
    ('Garden pea dried',                   'B017', 'IFCT: Peas, dry.', 'same-food'),
    ('Besan/Gram flour',                   'B001', 'Besan is milled bengal gram dal. Milling changes little beyond particle size.', 'same-food-different-form'),
    ('Sattu (roasted gram flour)',         'B002', 'Roasted and milled whole bengal gram. Roasting drives off moisture, so expect the provider value to sit above IFCT.', 'same-food-different-form'),

    -- Vegetables, roots and greens
    ('Brinjal/Eggplant',                   'D031', 'IFCT: Brinjal - all varieties, the pooled row across its 21 varietal entries. Correcting the defect where the provider row carries egg''s nutrition.', 'same-food'),
    ('Green peas',                         'D061', 'IFCT: Peas, fresh.', 'same-food'),
    ('Potato',                             'F006', 'IFCT separates by skin colour and size.', 'closest-variety'),
    ('Sweet potato',                       'F013', 'IFCT: Sweet potato, brown skin.', 'closest-variety'),
    ('Colocasia/Arbi',                     'F004', 'IFCT: Colocasia.', 'same-food'),
    ('Taro corm/Kochu',                    'F004', 'IFCT: Colocasia. The corm, not the leaf or stolon.', 'same-food'),
    ('Taro stolons/Kochur loti',           'D041', 'IFCT: Colocasia, stem, green.', 'same-food'),
    ('Colocasia stem',                     'D041', 'IFCT: Colocasia, stem, green.', 'same-food'),
    ('Raw banana/Plantain',                'D063', 'IFCT: Plantain, green.', 'same-food'),
    ('Green jackfruit',                    'D051', 'IFCT: Jack fruit, raw.', 'same-food'),
    ('Raw jackfruit',                      'D051', 'IFCT: Jack fruit, raw.', 'same-food'),
    ('Bamboo shoots',                      'D002', 'IFCT: Bamboo shoot, tender.', 'same-food'),
    ('Cluster beans/Guar',                 'D039', 'IFCT: Cluster beans.', 'same-food'),
    ('Tinda/Apple gourd',                  'D073', 'IFCT: Tinda, tender.', 'same-food'),
    ('Drumstick/Moringa pods',             'D046', 'IFCT: Drumstick, the pod.', 'same-food'),
    ('Broad beans/Sem',                    'D032', 'IFCT: Broad beans.', 'same-food'),

    -- Leafy vegetables
    ('Pui shak/Malabar spinach',           'C007', 'IFCT: Basella leaves.', 'same-food'),
    ('Malabar spinach/Pui shaak',          'C007', 'IFCT: Basella leaves.', 'same-food'),
    ('Green leafy bathua',                 'C008', 'IFCT: Bathua leaves.', 'same-food'),
    ('Mustard greens/Sarson',              'C026', 'IFCT: Mustard leaves.', 'same-food'),
    ('Northeast lai saag/mustard greens',  'C026', 'IFCT: Mustard leaves.', 'same-food'),
    ('Kochu shak/Taro leaves',             'C018', 'IFCT: Colocasia leaves, green.', 'same-food'),
    ('Colocasia leaves',                   'C018', 'IFCT: Colocasia leaves, green.', 'same-food'),
    ('Amaranth leaves',                    'C002', 'IFCT: Amaranth leaves, green.', 'closest-variety'),
    ('Lal shak/Red amaranth',              'C003', 'IFCT: Amaranth leaves, red mix.', 'same-food'),
    ('Chaulai red amaranth',               'C003', 'IFCT: Amaranth leaves, red mix.', 'same-food'),
    ('Cholai/Amaranth greens',             'C002', 'IFCT: Amaranth leaves, green.', 'same-food'),
    ('Moringa leaves',                     'C019', 'IFCT: Drumstick leaves.', 'same-food'),

    -- Nuts, seeds and fats
    ('Peanut',                             'H012', 'IFCT: Ground nut. This is the nut, not groundnut oil.', 'same-food'),
    ('Fresh coconut flesh',                'H007', 'IFCT: Coconut, kernel, fresh.', 'same-food'),
    ('Almond',                             'H001', 'Direct.', 'same-food'),
    ('Cashew',                             'H005', 'IFCT: Cashew nut.', 'same-food'),
    ('Walnut',                             'H021', 'Direct.', 'same-food'),
    ('Sesame seeds',                       'H011', 'IFCT: Gingelly seeds, white.', 'closest-variety'),
    ('Mustard seeds',                      'H013', 'Direct.', 'same-food'),
    ('Sunflower seeds',                    'H020', 'Direct.', 'same-food'),

    -- Dairy
    ('Cow''s milk',                        'L002', 'IFCT: Milk, whole, Cow.', 'same-food'),
    ('Buffalo milk',                       'L001', 'IFCT: Milk, whole, Buffalo.', 'same-food'),
    ('Paneer',                             'L003', 'Direct.', 'same-food'),

    -- Fish
    ('Rohu fish',                          'S006', 'IFCT: Rohu.', 'same-food'),
    ('Rui fish',                           'S006', 'Rui is the Bengali name for rohu.', 'same-food'),
    ('Rohu (Pakistan)',                    'S006', 'Same species.', 'same-food'),
    ('Katla fish',                         'S002', 'IFCT: Catla.', 'same-food'),
    ('Catfish/Magur',                      'S001', 'IFCT: Cat fish.', 'same-food'),
    ('Pangas fish',                        'S005', 'IFCT: Pangas.', 'same-food');

-- ---------------------------------------------------------------------------
-- The corrected nutrition layer.
--
-- This is what the application should read. It does NOT modify ingredient_master:
-- the provider's numbers stay exactly as shipped, and this view sits beside them.
--
-- Three states, always visible to the caller:
--
--   ifct        the value comes from IFCT 2017, with the food code recorded
--   provider    no IFCT match exists, so the provider's provisional value stands,
--               flagged as unverified
--
-- Nothing is averaged, interpolated or estimated. A food IFCT does not list keeps the
-- provider's group-level placeholder and says so.
-- ---------------------------------------------------------------------------
CREATE VIEW ingredient_nutrition_corrected AS
WITH resolved AS (
    -- A hand-written alias wins. Where none exists, fall back to the automatic matcher,
    -- but only on an exact name match: those already passed the full-coverage gate and
    -- the food-form guard, so they are the same food, not a near-enough one.
    SELECT i.ingredient_id,
           coalesce(a.food_code, x.food_code)                             AS food_code,
           CASE WHEN a.food_code IS NOT NULL THEN a.exactness
                WHEN x.food_code IS NOT NULL THEN 'same-food'
           END                                                            AS exactness,
           CASE WHEN a.food_code IS NOT NULL THEN 'alias'
                WHEN x.food_code IS NOT NULL THEN 'auto-exact'
           END                                                            AS resolved_by
    FROM ingredient_master i
    LEFT JOIN ingredient_ifct_alias a ON a.ingredient_english_name = i.english_name
    LEFT JOIN ingredient_nutrition_audit x
           ON x.ingredient_id = i.ingredient_id AND x.match_certainty = 'exact'
)
SELECT i.ingredient_id,
       i.english_name,
       i.bengali_name,
       i.food_group,
       f.food_code                                        AS ifct_food_code,
       f.food_name                                        AS ifct_food_name,
       rv.exactness                                       AS ifct_match_exactness,
       rv.resolved_by                                     AS ifct_resolved_by,
       CASE WHEN f.food_code IS NULL THEN 'provider' ELSE 'ifct' END AS value_source,
       f.food_code IS NOT NULL                            AS verified,

       coalesce(f.energy_kcal_100g, i.energy_kcal_100g)   AS energy_kcal_100g,
       coalesce(f.protein_g_100g,   i.protein_g_100g)     AS protein_g_100g,
       coalesce(f.fat_g_100g,       i.fat_g_100g)         AS fat_g_100g,
       coalesce(f.carb_g_100g,      i.carb_g_100g)        AS carb_g_100g,
       coalesce(f.fibre_g_100g,     i.fibre_g_100g)       AS fibre_g_100g,
       coalesce(f.iron_mg_100g,     i.iron_mg_100g)       AS iron_mg_100g,
       coalesce(f.calcium_mg_100g,  i.calcium_mg_100g)    AS calcium_mg_100g,
       coalesce(f.zinc_mg_100g,     i.zinc_mg_100g)       AS zinc_mg_100g,
       coalesce(f.vitc_mg_100g,     i.vitc_mg_100g)       AS vitc_mg_100g,

       -- The provider values, kept alongside so a reviewer can see what changed.
       i.energy_kcal_100g AS provider_energy_kcal_100g,
       i.protein_g_100g   AS provider_protein_g_100g,
       i.iron_mg_100g     AS provider_iron_mg_100g,
       i.calcium_mg_100g  AS provider_calcium_mg_100g,
       i.review_status    AS provider_review_status,
       i.data_quality     AS provider_data_quality
FROM ingredient_master i
JOIN resolved rv ON rv.ingredient_id = i.ingredient_id
LEFT JOIN external_food_composition f ON f.food_code = rv.food_code;

COMMENT ON VIEW ingredient_nutrition_corrected IS
    'Per-ingredient nutrition with IFCT 2017 values substituted where the food is '
    'identified, by hand-written alias first and exact automatic match second. '
    'value_source, verified and ifct_resolved_by state which is which on every row. The '
    'provider columns are carried alongside and are never modified.';

-- An alias naming an ingredient that does not exist is a typo, not a mapping. Catching
-- it at migration time is cheaper than discovering the ingredient was never corrected.
DO $$
DECLARE missing text;
BEGIN
    SELECT string_agg(a.ingredient_english_name, ', ') INTO missing
    FROM ingredient_ifct_alias a
    LEFT JOIN ingredient_master i ON i.english_name = a.ingredient_english_name
    WHERE i.ingredient_id IS NULL;

    -- ingredient_master is empty on a fresh migrate, before the first import. Only
    -- complain when there is something to check against.
    IF missing IS NOT NULL AND EXISTS (SELECT 1 FROM ingredient_master) THEN
        RAISE EXCEPTION 'ingredient_ifct_alias names ingredients that do not exist: %', missing;
    END IF;
END $$;
