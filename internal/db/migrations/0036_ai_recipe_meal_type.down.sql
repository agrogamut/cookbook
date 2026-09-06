DROP VIEW ai_recipe_derived;

CREATE VIEW ai_recipe_derived AS
SELECT ar.recipe_id,
       ar.title,
       ar.meal_category_id,
       ar.age_group,
       ar.region_culture,
       ar.texture,
       ar.min_age_months,
       ar.max_age_months,
       'Draft - Culinary/Nutrition/Clinical Review Required'::text AS review_status,
       ad.diet_type,
       aa.allergen_tags,
       coalesce(an.energy_kcal, 0)  AS energy_kcal,
       coalesce(an.protein_g, 0)   AS protein_g,
       coalesce(an.fat_g, 0)       AS fat_g,
       coalesce(an.carb_g, 0)      AS carb_g,
       coalesce(an.fibre_g, 0)     AS fibre_g,
       coalesce(an.iron_mg, 0)     AS iron_mg,
       coalesce(an.calcium_mg, 0)  AS calcium_mg,
       an.ingredient_coverage,
       ac.cost_per_serving_inr,
       ac.cost_coverage,
       cv.distinct_food_groups,
       cv.distinct_ingredients,
       fv.fruitveg_share,
       'ai-invented'::text AS source
FROM ai_recipe ar
LEFT JOIN ai_recipe_nutrition  an ON an.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_allergen   aa ON aa.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_diet       ad ON ad.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_cost       ac ON ac.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_diversity  cv ON cv.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_fruitveg   fv ON fv.recipe_id = ar.recipe_id;

ALTER TABLE ai_recipe
    ALTER COLUMN meal_category_id SET NOT NULL,
    DROP COLUMN meal_type;
