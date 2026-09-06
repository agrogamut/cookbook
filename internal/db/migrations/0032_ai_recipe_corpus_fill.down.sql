DROP VIEW IF EXISTS ai_recipe_derived;
DROP VIEW IF EXISTS ai_recipe_fruitveg;
DROP VIEW IF EXISTS ai_recipe_diversity;
DROP VIEW IF EXISTS ai_recipe_cost;
DROP VIEW IF EXISTS ai_recipe_diet;
DROP VIEW IF EXISTS ai_recipe_allergen;
DROP VIEW IF EXISTS ai_recipe_nutrition;

ALTER TABLE ai_recipe
    DROP CONSTRAINT IF EXISTS ai_recipe_age_band_check,
    DROP COLUMN IF EXISTS max_age_months,
    DROP COLUMN IF EXISTS texture,
    DROP COLUMN IF EXISTS region_culture,
    DROP COLUMN IF EXISTS age_group;
