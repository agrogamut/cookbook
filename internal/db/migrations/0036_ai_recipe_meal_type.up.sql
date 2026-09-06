-- ai_recipe.meal_category_id has been a required real Book 2 chapter id (MC-01..MC-07)
-- since migration 0025, which is correct for the runtime per-child fallback: every call
-- site iterates the engine's own defined meal categories and always has a real one.
--
-- Batch corpus-fill is different. The book/engine session's own measured thin cells
-- (2026-09-06 cross-session message) are keyed on recipe_master.meal_type, which has six
-- real values -- Breakfast, Dinner, Lunch, Recovery Meal, School Tiffin, Snack -- and
-- meal_category_recipe_map (migration 0016) maps only three of them (Breakfast, Lunch,
-- Dinner) to a real chapter id. School Tiffin, Snack and Recovery Meal are exactly GAP-023:
-- present in the corpus, reachable by no chapter today, for real recipes and now for
-- generated ones the same way. batch1 already put rows in MC-04/MC-05, which are that same
-- unmapped state -- this was not visible until meal_type existed to check it against.
--
-- Adding a fake chapter id for "Recovery Meal" to satisfy a NOT NULL constraint would be
-- inventing a mapping this project has deliberately not made for the real corpus either.
-- The honest fix is the same one the hard rule always prescribes for a genuine gap: make
-- the column nullable, and leave it null where no real mapping exists yet. Loosening a NOT
-- NULL constraint cannot break any existing row.
ALTER TABLE ai_recipe
    ADD COLUMN meal_type text,
    ALTER COLUMN meal_category_id DROP NOT NULL;

COMMENT ON COLUMN ai_recipe.meal_type IS
    'Declared at generation time, from recipe_master''s own six-value vocabulary. '
    'meal_category_id is derived from this via meal_category_recipe_map''s real mapping '
    'where one exists (Breakfast/Lunch/Dinner today) and left null where it does not '
    '(School Tiffin, Snack, Recovery Meal -- GAP-023), rather than assigning a chapter this '
    'project has not decided those meal types belong to.';

-- Backfill batch1's 34 rows: their meal_category_id was chosen directly rather than derived
-- from a meal_type, so this recovers the meal_type each one was actually written for.
UPDATE ai_recipe SET meal_type = 'Breakfast'     WHERE meal_category_id = 'MC-01' AND model = 'claude-authored';
UPDATE ai_recipe SET meal_type = 'Lunch'         WHERE meal_category_id = 'MC-03' AND model = 'claude-authored';
UPDATE ai_recipe SET meal_type = 'School Tiffin' WHERE meal_category_id = 'MC-04' AND model = 'claude-authored';
UPDATE ai_recipe SET meal_type = 'Snack'         WHERE meal_category_id = 'MC-05' AND model = 'claude-authored';
UPDATE ai_recipe SET meal_type = 'Dinner'        WHERE meal_category_id = 'MC-06' AND model = 'claude-authored';

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
       'ai-invented'::text AS source,
       ar.meal_type
FROM ai_recipe ar
LEFT JOIN ai_recipe_nutrition  an ON an.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_allergen   aa ON aa.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_diet       ad ON ad.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_cost       ac ON ac.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_diversity  cv ON cv.recipe_id = ar.recipe_id
LEFT JOIN ai_recipe_fruitveg   fv ON fv.recipe_id = ar.recipe_id;
