-- Recipe nutrition recomputed from ingredient quantities.
--
-- The provider's per-recipe nutrition inherits whatever is in Ingredient_Master, and
-- Ingredient_Master carries one value set per food group rather than per ingredient. So
-- recipe nutrition is only as good as its inputs, and this view rebuilds it from the
-- corrected ingredient layer instead.
--
-- Formula, applied per ingredient and summed:
--
--     contribution = quantity_g / 100 * value_per_100g
--
-- Straight mass balance. It ignores cooking losses and water uptake, which is the same
-- assumption the provider's own figures make -- their kcal already agrees with a 4/4/9
-- macro calculation on every recipe, which only holds if nothing was adjusted for
-- cooking.
--
-- ingredient_coverage is the fraction of the recipe's mass whose nutrition comes from
-- IFCT rather than from a group placeholder. It is the number that says how much to
-- trust the row, and it is never hidden: a recipe at 0.4 coverage is 40% verified and
-- the view says so rather than presenting a total as though it were measured.

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
