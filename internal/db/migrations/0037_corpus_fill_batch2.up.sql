-- Second batch of the recipe-corpus fill (2026-09-06): 18 recipes targeting the two
-- genuinely thin 2-5y cells the book/engine session measured -- Breakfast (17 real recipes
-- against a 25 target) and Recovery Meal (14 real recipes, the thinnest cell in the corpus).
--
-- Breakfast rows use meal_category_id = 'MC-01' (a real, mapped chapter). Recovery Meal
-- rows use meal_category_id = NULL: meal_category_recipe_map (migration 0016) does not map
-- Recovery Meal to any chapter for the real corpus either -- this is GAP-023, not something
-- this batch invents or resolves. Assigning a fake chapter id to make the column non-null
-- would be inventing a mapping the project has not made for 940 real recipes in the same
-- position; leaving it null is the honest gap, matching the hard rule's own prescription for
-- a genuine absence, and it is recoverable later the same day this project extends
-- meal_category_recipe_map, without touching this data.
--
-- Recovery Meal recipes are deliberately bland and soft (khichdi, curd rice, mashed
-- banana-rice, thin soups) -- not because that is prescribed anywhere in the provider data,
-- but because it is the ordinary, undisputed sense of "recovery/illness food" for a young
-- child, the same register book1_illness_feeding_block's own real content uses. No clinical
-- claim is attached to any of these beyond that.
--
-- Same authoring discipline as batch 1 (migration 0033): name, method_steps, ingredient
-- choice and quantity_g are authored; ingredient_id is always a real ingredient_master row;
-- nutrition, allergen_tags, diet_type and cost are read back from ai_recipe_derived, never
-- typed by hand here. diet_type stored on the row was independently re-checked against
-- ai_recipe_derived's own live computation before this migration was written (0 mismatches).

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Vegetable Poha', 'MC-01', 'Breakfast', 'bowl-grain', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Rinse flattened rice briefly and drain.','Saute onion, carrot and green peas in groundnut oil with turmeric and cumin.','Add the flattened rice and toss gently until warmed through.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0009', 70), ('ING0038', 20), ('ING0018', 20), ('ING0029', 20), ('ING0063', 6), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Suji Halwa (Semolina Breakfast Pudding)', 'MC-01', 'Breakfast', 'bowl-porridge', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Roast semolina lightly in ghee until fragrant.','Add milk and cook, stirring, until it thickens to a soft pudding.','Stir in raisins and serve warm.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0008', 50), ('ING0060', 8), ('ING0056', 100), ('ING0089', 10)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Egg Bhurji with Roti', 'MC-01', 'Breakfast', 'flatbread', 'Eggetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Make a soft wheat flour dough and roll into small flatbreads; cook on a griddle.','Scramble egg with onion and tomato in a little mustard oil and turmeric.','Serve the egg alongside the flatbread.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0007', 50), ('ING0038', 20), ('ING0025', 15), ('ING0062', 5), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Moong Dal Chilla (Savoury Lentil Pancake)', 'MC-01', 'Breakfast', 'pancake', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Soak and blend moong dal to a thick batter with a little water.','Stir in finely chopped onion and coriander leaves.','Cook spoonfuls on a lightly oiled griddle until set on both sides.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0002', 60), ('ING0038', 15), ('ING0095', 3), ('ING0063', 6)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Banana and Milk Suji Porridge', 'MC-01', 'Breakfast', 'bowl-porridge', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Roast semolina lightly in ghee.','Add milk and cook to a soft porridge.','Stir in mashed banana just before serving.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0008', 40), ('ING0060', 5), ('ING0056', 120), ('ING0041', 40)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer Stuffed Paratha with Curd', 'MC-01', 'Breakfast', 'flatbread', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Mix crumbled paneer with coriander leaves and a pinch of cumin.','Stuff into wheat flour dough and roll into a soft flatbread.','Cook on a griddle with a little ghee and serve with plain curd.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 60), ('ING0007', 60), ('ING0095', 3), ('ING0093', 1), ('ING0060', 6), ('ING0057', 40)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Vegetable Upma', 'MC-01', 'Breakfast', 'plate-upma', 'Vegetarian', 24, 59, '2–5 years', 'South India', 'Family texture', ARRAY['Dry-roast semolina lightly.','Saute onion, carrot and green peas in groundnut oil with cumin.','Add water and the semolina, stirring until it thickens to a soft mass.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0008', 50), ('ING0038', 15), ('ING0018', 20), ('ING0029', 15), ('ING0063', 6), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Soft Boiled Egg with Mashed Potato', 'MC-01', 'Breakfast', 'dish-mash', 'Eggetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Boil an egg and potato until soft.','Mash the potato with a little ghee.','Halve the egg and serve on top of the mashed potato.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0016', 70), ('ING0060', 5)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Muri and Banana Bowl (Puffed Rice with Fruit)', 'MC-01', 'Breakfast', 'snack', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Lightly warm puffed rice with a little ghee.','Top with sliced banana and serve.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0010', 30), ('ING0041', 40), ('ING0060', 4)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken and Vegetable Semolina Upma', 'MC-01', 'Breakfast', 'plate-upma', 'Non-vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook finely diced chicken through in groundnut oil with turmeric.','Add onion and carrot and cook until soft.','Stir in roasted semolina and water, cooking until it thickens.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 50), ('ING0008', 40), ('ING0038', 15), ('ING0018', 15), ('ING0063', 6), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Plain Rice and Moong Dal Khichdi (Recovery)', NULL, 'Recovery Meal', 'pot-khichdi', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice and moong dal together with plenty of water until very soft.','Stir in a little ghee and a pinch of turmeric.','Serve warm and soft, without strong spice.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0001', 50), ('ING0002', 25), ('ING0060', 4), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Curd Rice (Recovery)', NULL, 'Recovery Meal', 'bowl-grain', 'Vegetarian', 24, 59, '2–5 years', 'South India', 'Family texture', ARRAY['Cook rice until very soft.','Mash lightly and mix with plain curd.','Serve at room temperature.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0001', 60), ('ING0057', 80)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Mashed Banana and Rice (Recovery)', NULL, 'Recovery Meal', 'dish-mash', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice until very soft.','Mash together with ripe banana until smooth.','Serve warm.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0001', 50), ('ING0041', 50)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Light Chicken and Rice Soup (Recovery)', NULL, 'Recovery Meal', 'bowl-grain', 'Non-vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Simmer chicken pieces with ginger in plenty of water until very tender.','Add rice and cook until soft, in a thin, mostly-liquid consistency.','Season lightly and serve warm.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 50), ('ING0001', 30), ('ING0040', 3)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Soft Egg and Mashed Potato (Recovery)', NULL, 'Recovery Meal', 'dish-mash', 'Eggetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Boil an egg and potato until very soft.','Mash together with a little ghee to a smooth consistency.','Serve warm.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0016', 60), ('ING0060', 4)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer and Rice Porridge (Recovery)', NULL, 'Recovery Meal', 'bowl-porridge', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice with extra water until it breaks down to a soft porridge.','Stir in finely crumbled paneer and a little ghee.','Serve warm and soft.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0001', 40), ('ING0058', 30), ('ING0060', 4)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Light Rohu Fish and Rice Congee (Recovery)', NULL, 'Recovery Meal', 'bowl-porridge', 'Non-vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Simmer rohu fish pieces gently until cooked through and flaking.','Cook rice with extra water to a soft, thin consistency and fold in the flaked fish.','Serve warm, lightly seasoned.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0069', 40), ('ING0001', 40), ('ING0040', 2)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Ginger-Cumin Rice Water Khichdi (Recovery)', NULL, 'Recovery Meal', 'pot-khichdi', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice and moong dal with plenty of water and a little ginger until very soft and thin.','Add a pinch of roasted cumin.','Serve warm; this is meant to be light and easy on the stomach.'], 'claude-authored', 'corpus-fill-2026-09-06-batch2')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0001', 40), ('ING0002', 20), ('ING0040', 3), ('ING0093', 1)) AS v(ingredient_id, quantity_g);
