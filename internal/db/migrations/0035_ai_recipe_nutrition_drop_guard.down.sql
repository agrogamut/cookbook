CREATE OR REPLACE VIEW ai_recipe_nutrition AS
SELECT ar.recipe_id,
       sum(ai.quantity_g)                                              AS total_mass_g,
       sum(ai.quantity_g / 100.0 *
           CASE WHEN c.energy_kcal_100g = 0 AND c.fat_g_100g > 0
                THEN c.provider_energy_kcal_100g
                ELSE c.energy_kcal_100g
           END)                                                        AS energy_kcal,
       sum(ai.quantity_g / 100.0 * c.protein_g_100g)                   AS protein_g,
       sum(ai.quantity_g / 100.0 * c.fat_g_100g)                       AS fat_g,
       sum(ai.quantity_g / 100.0 * c.carb_g_100g)                      AS carb_g,
       sum(ai.quantity_g / 100.0 * c.fibre_g_100g)                     AS fibre_g,
       sum(ai.quantity_g / 100.0 * c.iron_mg_100g)                     AS iron_mg,
       sum(ai.quantity_g / 100.0 * c.calcium_mg_100g)                  AS calcium_mg,
       coalesce(sum(ai.quantity_g) FILTER (WHERE c.value_source = 'ifct'), 0)
           / NULLIF(sum(ai.quantity_g), 0)                             AS ingredient_coverage
FROM ai_recipe ar
JOIN ai_recipe_ingredient ai ON ai.recipe_id = ar.recipe_id
JOIN ingredient_nutrition_corrected c ON c.ingredient_id = ai.ingredient_id
GROUP BY ar.recipe_id;
