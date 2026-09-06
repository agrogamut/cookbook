-- Revert to coalesce-only energy, restoring IFCT's stated zero for the oils-and-fats group.
--
-- This puts 406 recipes back to under-reporting energy by an average of 38.9 kcal while
-- still reporting fully_verified. Reversible for the sake of being reversible, not because
-- the previous behaviour was defensible.
--
-- Drop-and-recreate rather than CREATE OR REPLACE: the up migration appends an energy_basis
-- column, and REPLACE can add a trailing column but never remove one. The two views 0010
-- builds on top of ingredient_nutrition_corrected have to come down with it and go back up
-- after, so both are recreated below verbatim from 0010.

DROP VIEW IF EXISTS recipe_nutrition_confidence;
DROP VIEW IF EXISTS recipe_nutrition_recomputed;
DROP VIEW IF EXISTS ingredient_nutrition_corrected;

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


CREATE VIEW recipe_nutrition_recomputed AS
WITH per_ingredient AS (
    SELECT m.recipe_id,
           m.quantity                                   AS quantity_g,
           c.value_source,
           m.quantity / 100.0 * c.energy_kcal_100g      AS energy_kcal,
           m.quantity / 100.0 * c.protein_g_100g        AS protein_g,
           m.quantity / 100.0 * c.fat_g_100g            AS fat_g,
           m.quantity / 100.0 * c.carb_g_100g           AS carb_g,
           m.quantity / 100.0 * c.fibre_g_100g          AS fibre_g,
           m.quantity / 100.0 * c.iron_mg_100g          AS iron_mg,
           m.quantity / 100.0 * c.calcium_mg_100g       AS calcium_mg
    FROM recipe_ingredient_mapping m
    JOIN ingredient_nutrition_corrected c ON c.ingredient_id = m.ingredient_id
), totals AS (
    SELECT recipe_id,
           sum(quantity_g)                                                    AS total_mass_g,
           sum(quantity_g) FILTER (WHERE value_source = 'ifct')               AS verified_mass_g,
           count(*)                                                           AS n_ingredients,
           count(*) FILTER (WHERE value_source = 'ifct')                      AS n_verified,
           sum(energy_kcal) AS energy_kcal, sum(protein_g)  AS protein_g,
           sum(fat_g)       AS fat_g,       sum(carb_g)     AS carb_g,
           sum(fibre_g)     AS fibre_g,     sum(iron_mg)    AS iron_mg,
           sum(calcium_mg)  AS calcium_mg
    FROM per_ingredient GROUP BY recipe_id
)
SELECT r.recipe_id,
       r.recipe_name,
       r.age_group,
       r.region_culture,

       round(t.energy_kcal, 1) AS energy_kcal,
       round(t.protein_g,   2) AS protein_g,
       round(t.fat_g,       2) AS fat_g,
       round(t.carb_g,      2) AS carb_g,
       round(t.fibre_g,     2) AS fibre_g,
       round(t.iron_mg,     3) AS iron_mg,
       round(t.calcium_mg,  1) AS calcium_mg,

       t.total_mass_g,
       t.n_ingredients,
       t.n_verified,
       round(coalesce(t.verified_mass_g, 0) / NULLIF(t.total_mass_g, 0), 3) AS ingredient_coverage,
       (t.n_verified = t.n_ingredients)                                     AS fully_verified,

       -- Provider figures, carried alongside and unmodified.
       r.energy_kcal AS provider_energy_kcal,
       r.protein_g   AS provider_protein_g,
       r.iron_mg     AS provider_iron_mg,
       r.calcium_mg  AS provider_calcium_mg,
       round((t.energy_kcal - r.energy_kcal) / NULLIF(r.energy_kcal, 0) * 100, 1) AS energy_pct_diff,
       round((t.iron_mg     - r.iron_mg)     / NULLIF(r.iron_mg, 0)     * 100, 1) AS iron_pct_diff,

       'derived'::text AS value_kind,
       'sum(quantity_g / 100 * ingredient value per 100 g) over ingredient_nutrition_corrected'::text AS formula
FROM recipe_master r
JOIN totals t ON t.recipe_id = r.recipe_id;

COMMENT ON VIEW recipe_nutrition_recomputed IS
    'Recipe nutrition rebuilt from ingredient quantities and the corrected ingredient '
    'layer. Derived: never present it as the provider''s or a clinician''s figure. '
    'ingredient_coverage states what fraction of the recipe mass is IFCT-backed; a row '
    'below 1.0 is partly built on the provider''s group-level placeholders.';

-- Which recipes are safe to show recomputed numbers for, and which are not.
CREATE VIEW recipe_nutrition_confidence AS
SELECT CASE
           WHEN fully_verified              THEN 'fully verified'
           WHEN ingredient_coverage >= 0.75 THEN 'mostly verified'
           WHEN ingredient_coverage >= 0.25 THEN 'partly verified'
           ELSE                                  'mostly provider placeholder'
       END AS band,
       count(*) AS recipes
FROM recipe_nutrition_recomputed
GROUP BY 1;
