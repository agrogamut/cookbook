-- Stop IFCT 2017's zero-energy oil rows from zeroing the energy of 406 real recipes.
--
-- Found by @data's corpus-fill verification running the 4/4/9 macro-consistency check the
-- integrity suite already applies to recipe_master, against derived recipe totals instead.
-- Measured here before acting on it: 406 recipes in the real corpus carry one of the four
-- aliased oils, losing an average of 38.9 kcal each (worst 45.0), and 293 of them are
-- reported fully_verified with ingredient_coverage = 1.000. A confidently-wrong number
-- presented as measured is precisely the failure the hard rule exists to prevent, and this
-- one reaches a printed page: Book 2's recipe nutrition panel prints the recomputed figure
-- with its coverage bar.
--
-- The ranker is NOT affected and was checked: recipe_nutrition_normalised bands on
-- recipe_master's own per-serving columns, not on recipe_nutrition_recomputed, so no recipe
-- was mis-ranked. The harm was to what an operator and a family read, which is enough.
--
-- ingredient_master is still not modified -- the provider's numbers stay exactly as shipped
-- and TestProviderNutritionIsNeverModified still asserts it. This changes only the corrected
-- view, which is where correction has always lived.

CREATE OR REPLACE VIEW ingredient_nutrition_corrected AS
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

       CASE
           -- IFCT 2017's oils-and-fats group (food codes T001-T014) ships energy = 0 against
           -- a correct fat = 100. Verified in the raw file, not an import artefact: all 14
           -- pure-fat rows, and no other row in the table, state zero energy while stating
           -- real macros. The column was simply never populated for that group.
           --
           -- coalesce could not catch it, because 0 is not NULL: IFCT's zero won, and four
           -- aliased oils (Ghee, Mustard, Groundnut, Sesame) reached 406 real recipes as
           -- verified = true, coverage = 1.000, energy = 0. Soybean oil, which has no alias,
           -- kept the provider's correct 884 -- so the alias was making the data worse.
           --
           -- The replacement is a derivation over IFCT's OWN numbers, not a substitution of
           -- someone else's: the Atwater factors, 4 kcal/g protein, 9 kcal/g fat, 4 kcal/g
           -- carbohydrate, over the same row's stated macros. For a pure fat that is
           -- 9 * 100 = 900 kcal/100g. It is the identical 4/4/9 rule the integrity suite
           -- already applies to recipe_master, and energy_basis below records where each
           -- row's figure came from so a derived one is never mistaken for a measured one.
           WHEN f.energy_kcal_100g = 0
                AND (coalesce(f.protein_g_100g,0) + coalesce(f.fat_g_100g,0)
                     + coalesce(f.carb_g_100g,0)) > 0
           THEN 4*coalesce(f.protein_g_100g,0) + 9*coalesce(f.fat_g_100g,0)
                + 4*coalesce(f.carb_g_100g,0)
           ELSE coalesce(f.energy_kcal_100g, i.energy_kcal_100g)
       END                                                AS energy_kcal_100g,
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
       i.data_quality     AS provider_data_quality,

       -- Where this row's energy figure actually comes from. 'ifct' is IFCT's own stated
       -- value; 'ifct-atwater' is derived from IFCT's own macros by the 4/4/9 rule above
       -- because IFCT stated zero; 'provider' is the unverified group placeholder, on a
       -- food with no IFCT counterpart at all. A derived value that cannot be told apart
       -- from a measured one is the thing this project's hard rule forbids, so the basis
       -- travels with the number rather than living only in this comment.
       CASE
           WHEN f.food_code IS NULL THEN 'provider'
           WHEN f.energy_kcal_100g = 0
                AND (coalesce(f.protein_g_100g,0) + coalesce(f.fat_g_100g,0)
                     + coalesce(f.carb_g_100g,0)) > 0 THEN 'ifct-atwater'
           ELSE 'ifct'
       END                                                AS energy_basis
FROM ingredient_master i
JOIN resolved rv ON rv.ingredient_id = i.ingredient_id
LEFT JOIN external_food_composition f ON f.food_code = rv.food_code;

