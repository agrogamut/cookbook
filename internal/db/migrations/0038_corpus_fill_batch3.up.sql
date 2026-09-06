-- Third batch of the recipe-corpus fill (2026-09-06): 18 recipes targeting the cells
-- confirmed genuinely thin AND reachable today (mapped to a real Book 2 chapter via
-- meal_category_recipe_map), after correcting the batch-2 miscalculation that treated
-- Recovery Meal as worth targeting when no chapter reaches it at all:
--
--   6-9y Breakfast    18 real + 1 (batch 1/2) = 19, needs 6   -> 6 added here
--   13-18y Breakfast  16 real + 1 (batch 1/2) = 17, needs 8   -> 8 added here
--   6-9y Lunch        19 real + 4 (batch 1/2) = 23, needs 2   -> 2 added here
--   13-18y Lunch      22 real + 1 (batch 1/2) = 23, needs 2   -> 2 added here
--
-- 2-5y Lunch and Dinner were also proposed for this batch and are deliberately skipped:
-- recomputing real+mine showed both already past the 25 target once batch 1's own
-- contributions count (26 and 27), so adding more there would be wasted effort, not merely
-- unreachable-pending-a-ruling. Recovery Meal, School Tiffin and Snack are untouched this
-- batch, pending the user's ruling on extending meal_category_recipe_map (MC-04/MC-05) and
-- on whether Recovery Meal gets an invented chapter at all.
--
-- Same authoring discipline as batches 1 and 2: name, method_steps, ingredient choice and
-- quantity_g are authored; ingredient_id is always a real ingredient_master row; nutrition,
-- allergen_tags, diet_type and cost are read back from ai_recipe_derived, never typed by
-- hand. diet_type stored on each row was independently re-checked against ai_recipe_
-- derived's own live computation before this migration was written (0 mismatches).

-- ---------------- 6-9y Breakfast (6) ----------------
WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken and Vegetable Wrap', 'MC-01', 'Breakfast', 'wrap', 'Non-vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Make a soft flatbread from wheat flour.','Cook finely diced chicken with onion and tomato in mustard oil with turmeric.','Fill the flatbread with the chicken mixture and roll up.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 60), ('ING0007', 60), ('ING0038', 20), ('ING0025', 15), ('ING0062', 6), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer and Vegetable Paratha', 'MC-01', 'Breakfast', 'flatbread', 'Vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Mix crumbled paneer with grated carrot and coriander leaves.','Stuff into wheat flour dough and roll into a flatbread.','Cook on a griddle with a little ghee until golden.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 60), ('ING0007', 70), ('ING0018', 20), ('ING0095', 3), ('ING0060', 6)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Egg and Vegetable Poha', 'MC-01', 'Breakfast', 'bowl-grain', 'Eggetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Rinse flattened rice briefly and drain.','Saute onion, carrot and green peas in mustard oil with turmeric.','Push to one side, scramble in egg, then mix everything together with the flattened rice.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0009', 60), ('ING0038', 20), ('ING0018', 20), ('ING0029', 20), ('ING0062', 6), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Moong Dal and Vegetable Chilla', 'MC-01', 'Breakfast', 'pancake', 'Vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Soak and blend moong dal to a thick batter.','Stir in finely chopped onion, tomato and coriander leaves.','Cook spoonfuls on a lightly oiled griddle until set on both sides.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0002', 70), ('ING0038', 20), ('ING0025', 15), ('ING0095', 3), ('ING0063', 6)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Boiled Egg with Tomato Roti', 'MC-01', 'Breakfast', 'dish-mash', 'Eggetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Make a soft flatbread from wheat flour with a little chopped tomato worked into the dough.','Cook on a griddle with a little ghee.','Serve alongside a boiled, halved egg.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0007', 60), ('ING0025', 20), ('ING0060', 5)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Banana Semolina Upma with Cashew', 'MC-01', 'Breakfast', 'plate-upma', 'Vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Dry-roast semolina and chopped cashew lightly in ghee.','Add water and cook, stirring, until it thickens.','Stir in mashed banana just before serving.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0008', 60), ('ING0083', 10), ('ING0060', 6), ('ING0041', 40)) AS v(ingredient_id, quantity_g);

-- ---------------- 13-18y Breakfast (8) ----------------
WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken Keema Paratha', 'MC-01', 'Breakfast', 'flatbread', 'Non-vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook minced chicken with onion, ginger, garlic and turmeric in mustard oil until dry.','Stuff into wheat flour dough and roll into a flatbread.','Cook on a griddle with a little oil until golden on both sides.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 90), ('ING0007', 80), ('ING0038', 25), ('ING0040', 4), ('ING0039', 4), ('ING0062', 8), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Masala Egg Curry with Roti', 'MC-01', 'Breakfast', 'bowl-grain', 'Eggetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Boil eggs and halve them.','Make a base of onion, tomato, ginger, garlic, turmeric and cumin in mustard oil.','Simmer the eggs briefly in the base and serve with wheat flatbread.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 100), ('ING0007', 70), ('ING0038', 30), ('ING0025', 25), ('ING0040', 4), ('ING0039', 4), ('ING0062', 8), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer Bhurji with Paratha', 'MC-01', 'Breakfast', 'flatbread', 'Vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Crumble paneer and cook with onion, tomato, turmeric and cumin in groundnut oil.','Make a soft wheat flour flatbread on a griddle.','Serve the paneer alongside the flatbread.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 80), ('ING0007', 70), ('ING0038', 25), ('ING0025', 20), ('ING0063', 7), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Rohu Fish Cutlets', 'MC-01', 'Breakfast', 'patty', 'Non-vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Flake cooked rohu fish and mix with mashed potato, onion and coriander leaves.','Shape into small cutlets, coat lightly with wheat flour.','Shallow-fry in groundnut oil until golden.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0069', 90), ('ING0016', 50), ('ING0038', 20), ('ING0095', 3), ('ING0007', 10), ('ING0063', 8)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chana Dal and Vegetable Chilla', 'MC-01', 'Breakfast', 'pancake', 'Vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Soak and blend chana dal to a thick batter with a little water.','Stir in finely chopped onion, carrot and coriander leaves.','Cook spoonfuls on a lightly oiled griddle until set on both sides.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0005', 80), ('ING0038', 20), ('ING0018', 20), ('ING0095', 3), ('ING0063', 7)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Cheese and Vegetable Paratha', 'MC-01', 'Breakfast', 'flatbread', 'Vegetarian', 156, 216, '13–18 years', 'North India', 'Family texture', ARRAY['Grate cheese and mix with finely chopped carrot and coriander leaves.','Stuff into wheat flour dough and roll into a flatbread.','Cook on a griddle with a little ghee until golden.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0059', 60), ('ING0007', 70), ('ING0018', 20), ('ING0095', 3), ('ING0060', 6)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken and Vegetable Poha', 'MC-01', 'Breakfast', 'bowl-grain', 'Non-vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook finely diced chicken through in mustard oil with turmeric.','Add onion, carrot and green peas and cook until soft.','Rinse and add flattened rice, tossing gently until warmed through.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 80), ('ING0009', 70), ('ING0038', 25), ('ING0018', 20), ('ING0029', 20), ('ING0062', 8), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Mixed Dal and Vegetable Khichdi Bowl', 'MC-01', 'Breakfast', 'pot-khichdi', 'Vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice with moong dal and chana dal until soft.','Add carrot, green peas and turmeric and cook through.','Finish with ghee and cumin.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0001', 70), ('ING0002', 20), ('ING0005', 20), ('ING0018', 25), ('ING0029', 20), ('ING0060', 7), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

-- ---------------- 6-9y Lunch (2) ----------------
WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken Curry with Rice and Cauliflower', 'MC-03', 'Lunch', 'bowl-grain', 'Non-vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Brown chicken pieces lightly in mustard oil.','Add onion, tomato, ginger, garlic, turmeric and cumin and cook down to a base.','Add cauliflower and simmer covered until everything is cooked through.','Serve with rice.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 90), ('ING0001', 70), ('ING0031', 40), ('ING0038', 25), ('ING0025', 20), ('ING0062', 8), ('ING0040', 4), ('ING0039', 4), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer and Spinach Rice Bowl', 'MC-03', 'Lunch', 'bowl-grain', 'Vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook spinach with onion and garlic until soft.','Add cubed paneer and warm through in a little mustard oil with turmeric.','Serve over cooked rice.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 70), ('ING0026', 60), ('ING0038', 20), ('ING0001', 70), ('ING0062', 7), ('ING0039', 3), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

-- ---------------- 13-18y Lunch (2) ----------------
WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Egg and Vegetable Rice Bowl (Mild Biryani-Style)', 'MC-03', 'Lunch', 'bowl-grain', 'Eggetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Marinate boiled eggs briefly in curd, ginger and garlic.','Cook onion, carrot and green peas in ghee with turmeric and cumin.','Layer the eggs and vegetables over cooked rice and serve.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 100), ('ING0001', 90), ('ING0038', 30), ('ING0018', 25), ('ING0029', 20), ('ING0057', 20), ('ING0060', 7), ('ING0040', 4), ('ING0039', 4), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Katla Fish Curry with Rice and Vegetables', 'MC-03', 'Lunch', 'bowl-grain', 'Non-vegetarian', 156, 216, '13–18 years', 'Bangladesh', 'Family texture', ARRAY['Fry katla fish pieces lightly in mustard oil with turmeric.','Make a gravy of onion, tomato, cauliflower and cumin.','Simmer the fish in the gravy until cooked through and serve with rice.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0070', 100), ('ING0001', 90), ('ING0038', 30), ('ING0025', 25), ('ING0031', 40), ('ING0062', 9), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

-- ---------------- 10-12y Lunch (1, found by the peer session's independent recheck) ----------------
WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, meal_type, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken and Cauliflower Curry with Rice', 'MC-03', 'Lunch', 'bowl-grain', 'Non-vegetarian', 120, 155, '10–12 years', 'West Bengal / East India', 'Family texture', ARRAY['Brown chicken pieces lightly in mustard oil.','Add onion, tomato, ginger, garlic, turmeric and cumin and cook down to a base.','Add cauliflower and simmer covered until everything is cooked through.','Serve with rice.'], 'claude-authored', 'corpus-fill-2026-09-06-batch3')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 90), ('ING0001', 80), ('ING0031', 40), ('ING0038', 25), ('ING0025', 20), ('ING0062', 8), ('ING0040', 4), ('ING0039', 4), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);
