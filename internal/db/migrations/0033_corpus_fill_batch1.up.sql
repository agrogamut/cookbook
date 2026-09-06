-- First batch of the recipe-corpus fill (2026-09-06): 34 recipes into ai_recipe /
-- ai_recipe_ingredient, per the user's request for more egg, non-vegetarian, paneer and
-- variety (fruit/dairy, no-cook) recipes, plus the corpus-thinness gap the book/engine
-- session measured separately (age x meal_type cells below the 25-recipe chapter target).
--
-- What is authored here, per recipe: the name, the method_steps prose, the choice and
-- combination of ingredients, and each ingredient's quantity_g -- culinary judgement, not a
-- data value, per the 2026-08-24 amendment. Every ingredient_id is a real ingredient_master
-- row. Nothing else is set here: nutrition, allergen_tags, diet_type and cost are read back
-- from ai_recipe_derived (migration 0032), computed live from these ingredient rows, never
-- typed by hand. min_age_months / max_age_months are age_feeding_stage_master's own bounds
-- for the declared age_group (all four bands used here are 24 months and up, so no infant
-- texture/choking judgement is made by this batch).
--
-- 10 Eggetarian, 10 Non-vegetarian, 8 Vegetarian (paneer-centred), 6 variety/no-cook.
-- Weighted West Bengal / East India and Bangladesh per the project's own region priority;
-- a handful use North/South India for direct recipe-type variety (paratha, upma) where a
-- Bengali analogue would be a stretch.
--
-- Verified before this migration was written, against a throwaway database (not the shared
-- dev instance): every recipe's derived nutrition passes the same 4/4/9 macro-consistency
-- check TestDataIntegrity applies to the real corpus, every ingredient's own min_age_months
-- is at or below its recipe's, every declared diet_type matches what the ingredients
-- actually require, and the stored diet_type never disagrees with ai_recipe_derived's own
-- live computation. Two real data defects were found and fixed as part of writing this
-- batch (see migrations 0030 and 0032's own comments): IFCT-2017's zero-energy oil rows,
-- and a bad energy value on IFCT's Chicken-leg row.

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Bengali-Style Egg Bhurji with Rice', 'MC-03', 'bowl-grain', 'Eggetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Boil rice until soft.','Scramble egg in a pan with a little mustard oil, turmeric and chopped onion and tomato.','Mix scrambled egg through the rice and serve warm.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0001', 60), ('ING0038', 30), ('ING0025', 20), ('ING0062', 5), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Boiled Egg and Vegetable Khichdi', 'MC-06', 'pot-khichdi', 'Eggetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice and moong dal together with carrot and pumpkin until soft.','Stir in ghee and turmeric.','Top with a boiled, mashed egg before serving.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0001', 40), ('ING0002', 20), ('ING0018', 30), ('ING0019', 30), ('ING0060', 5), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Egg Paratha Roll', 'MC-04', 'wrap', 'Eggetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Make a soft dough with wheat flour and roll into a flatbread.','Cook on a griddle with a little mustard oil.','Beat egg with chopped onion and coriander leaves, pour over the half-cooked flatbread and fold in.','Cook through on both sides and roll up.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0007', 60), ('ING0038', 20), ('ING0095', 5), ('ING0062', 8)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Steamed Egg and Paneer Bhapa', 'MC-03', 'dish-mash', 'Eggetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Whisk egg with crumbled paneer, curd and a little mustard oil and turmeric.','Steam in a covered dish for 15-20 minutes until set.','Cut into pieces and serve.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0058', 40), ('ING0057', 20), ('ING0062', 5), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Egg Curry with Potato (Dimer Dalna)', 'MC-06', 'bowl-grain', 'Eggetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Boil eggs and potato separately.','Make a base of onion, tomato, ginger and garlic in mustard oil with turmeric and cumin.','Add the boiled potato and eggs to the base and simmer briefly.','Serve with rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 100), ('ING0016', 60), ('ING0038', 30), ('ING0025', 20), ('ING0062', 8), ('ING0040', 3), ('ING0039', 3), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Egg Fried Rice with Vegetables', 'MC-03', 'bowl-grain', 'Eggetarian', 120, 155, '10–12 years', 'West Bengal / East India', 'Family texture', ARRAY['Scramble egg in groundnut oil and set aside.','Stir-fry carrot, cabbage and green peas briefly.','Add cooked rice and the scrambled egg back in and toss together.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0001', 80), ('ING0018', 30), ('ING0032', 30), ('ING0029', 20), ('ING0063', 6)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Masala Omelette Wrap', 'MC-01', 'wrap', 'Eggetarian', 120, 155, '10–12 years', 'North India', 'Family texture', ARRAY['Make a soft flatbread from wheat flour.','Beat egg with chopped onion, tomato and coriander leaves and cook into an omelette in groundnut oil.','Roll the omelette inside the flatbread.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 100), ('ING0007', 50), ('ING0038', 20), ('ING0025', 15), ('ING0095', 3), ('ING0063', 5)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Egg Tarka Dal with Rice', 'MC-06', 'bowl-grain', 'Eggetarian', 120, 155, '10–12 years', 'Bangladesh', 'Family texture', ARRAY['Cook masoor dal until soft.','Temper with onion, garlic and mustard oil, add turmeric.','Boil an egg, halve it and serve on top of the dal with rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0003', 40), ('ING0001', 60), ('ING0038', 20), ('ING0039', 3), ('ING0062', 6), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Egg and Spinach Upma', 'MC-01', 'plate-upma', 'Eggetarian', 156, 216, '13–18 years', 'South India', 'Family texture', ARRAY['Dry-roast semolina lightly.','Saute onion and spinach in groundnut oil with cumin.','Add water and the semolina, stirring until it thickens.','Fold in a scrambled egg to finish.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 50), ('ING0008', 60), ('ING0026', 30), ('ING0038', 20), ('ING0063', 6), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Baked Egg and Potato Cutlets', 'MC-05', 'patty', 'Eggetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Mash boiled potato with chopped boiled egg, onion and coriander leaves.','Shape into small cutlets, coat lightly with wheat flour.','Bake or shallow-fry in groundnut oil until golden.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0066', 100), ('ING0016', 60), ('ING0038', 15), ('ING0095', 3), ('ING0007', 10), ('ING0063', 8)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Bengali Fish Curry (Rui Macher Jhol)', 'MC-03', 'bowl-grain', 'Non-vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Lightly fry rohu fish pieces in mustard oil.','Make a thin gravy with potato, tomato, ginger, turmeric and cumin.','Simmer the fish in the gravy until cooked through.','Serve with soft rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0069', 60), ('ING0016', 40), ('ING0025', 20), ('ING0062', 8), ('ING0094', 1), ('ING0093', 1), ('ING0040', 3)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken and Vegetable Khichuri', 'MC-06', 'pot-khichdi', 'Non-vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice and moong dal with diced chicken, carrot and pumpkin until soft.','Finish with ghee, ginger and turmeric.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 60), ('ING0001', 40), ('ING0002', 20), ('ING0018', 20), ('ING0019', 20), ('ING0060', 5), ('ING0040', 3), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Steamed Bhetki in Mustard (Mild Shorshe Bhetki)', 'MC-03', 'dish-mash', 'Non-vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Marinate bhetki fillets with a little mustard oil, turmeric and curd.','Steam in a covered dish for 15 minutes until the fish flakes easily.','Serve with rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0161', 70), ('ING0062', 6), ('ING0094', 1), ('ING0057', 10)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken Curry with Potato (Murgir Jhol)', 'MC-06', 'bowl-grain', 'Non-vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Brown chicken pieces lightly in mustard oil.','Add onion, tomato, ginger and garlic and cook down to a base.','Add potato and turmeric and cumin, simmer covered until the chicken and potato are cooked through.','Serve with rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 90), ('ING0016', 60), ('ING0038', 30), ('ING0025', 20), ('ING0062', 8), ('ING0040', 4), ('ING0039', 4), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Katla Fish and Rice Bowl', 'MC-03', 'bowl-grain', 'Non-vegetarian', 60, 119, '6–9 years', 'Bangladesh', 'Family texture', ARRAY['Fry katla fish pieces lightly in mustard oil with turmeric.','Saute onion until soft and combine with the fish.','Serve over cooked rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0070', 70), ('ING0001', 70), ('ING0038', 20), ('ING0094', 1), ('ING0062', 6)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Baked Chicken Cutlets', 'MC-05', 'patty', 'Non-vegetarian', 120, 155, '10–12 years', 'West Bengal / East India', 'Family texture', ARRAY['Mince or finely chop cooked chicken and mix with mashed potato, onion and coriander leaves.','Shape into small cutlets, coat lightly with wheat flour.','Bake or shallow-fry in groundnut oil until golden.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 80), ('ING0016', 40), ('ING0038', 15), ('ING0095', 3), ('ING0007', 10), ('ING0063', 8)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Pomfret Fry with Lemon', 'MC-06', 'finger-bites', 'Non-vegetarian', 120, 155, '10–12 years', 'West Bengal / East India', 'Family texture', ARRAY['Marinate pomfret with a little mustard oil, turmeric and lemon juice.','Shallow-fry until golden and cooked through.','Serve with a wedge of lemon.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0162', 90), ('ING0062', 8), ('ING0094', 1), ('ING0055', 5)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken and Rice Bowl (Mild Biryani-Style)', 'MC-03', 'bowl-grain', 'Non-vegetarian', 120, 155, '10–12 years', 'West Bengal / East India', 'Family texture', ARRAY['Marinate chicken in curd, ginger and garlic.','Cook the chicken through in ghee with onion, turmeric and cumin.','Layer over cooked rice and serve.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 90), ('ING0001', 80), ('ING0038', 30), ('ING0057', 20), ('ING0060', 6), ('ING0040', 4), ('ING0039', 4), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Rohu Fish Curry with Cauliflower', 'MC-06', 'bowl-grain', 'Non-vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Lightly fry rohu fish pieces in mustard oil.','Make a gravy of onion, potato, cauliflower, turmeric and cumin.','Simmer the fish in the gravy until cooked through.','Serve with rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0069', 100), ('ING0031', 50), ('ING0016', 40), ('ING0038', 30), ('ING0062', 10), ('ING0094', 1), ('ING0093', 1), ('ING0040', 4)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Chicken and Egg Fried Rice', 'MC-03', 'bowl-grain', 'Non-vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Stir-fry diced chicken in groundnut oil until cooked through.','Push aside, scramble in egg, then stir-fry carrot and cabbage.','Add cooked rice and toss everything together.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0067', 70), ('ING0066', 50), ('ING0001', 90), ('ING0018', 30), ('ING0032', 30), ('ING0063', 8)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer and Vegetable Khichdi', 'MC-06', 'pot-khichdi', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice and moong dal with carrot and pumpkin until soft.','Stir in cubed paneer and ghee and turmeric, warming through gently.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 50), ('ING0001', 40), ('ING0002', 20), ('ING0018', 20), ('ING0019', 20), ('ING0060', 5), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Mild Palak Paneer', 'MC-03', 'dish-mash', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook spinach with onion and garlic until soft, then mash to a smooth puree.','Add cubed paneer and warm through in a little mustard oil with turmeric.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 60), ('ING0026', 60), ('ING0038', 20), ('ING0062', 6), ('ING0039', 3), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer Bhurji Wrap', 'MC-01', 'wrap', 'Vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Make a soft flatbread from wheat flour.','Crumble paneer and cook with onion, tomato, turmeric and cumin in groundnut oil.','Fill the flatbread with the paneer mixture and roll up.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 60), ('ING0007', 60), ('ING0038', 20), ('ING0025', 20), ('ING0063', 6), ('ING0094', 1), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Matar Paneer (Peas and Paneer Curry)', 'MC-03', 'bowl-grain', 'Vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Make a base of onion, tomato and ginger in mustard oil with cumin.','Add green peas and cubed paneer, simmer until the peas are tender.','Serve with rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 70), ('ING0029', 40), ('ING0025', 30), ('ING0038', 20), ('ING0001', 60), ('ING0062', 6), ('ING0040', 3), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Baked Paneer Cutlets', 'MC-05', 'patty', 'Vegetarian', 120, 155, '10–12 years', 'West Bengal / East India', 'Family texture', ARRAY['Mash paneer with boiled potato and coriander leaves.','Shape into small cutlets, coat lightly with wheat flour.','Bake or shallow-fry in groundnut oil until golden.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 80), ('ING0016', 40), ('ING0095', 3), ('ING0007', 10), ('ING0063', 8)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer and Vegetable Pulao', 'MC-03', 'bowl-grain', 'Vegetarian', 120, 155, '10–12 years', 'West Bengal / East India', 'Family texture', ARRAY['Cook rice with cumin and turmeric.','Saute cubed paneer, carrot and green peas in ghee.','Fold the vegetables and paneer through the rice.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 70), ('ING0001', 80), ('ING0018', 30), ('ING0029', 20), ('ING0060', 6), ('ING0093', 1), ('ING0094', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Shahi Paneer (Creamy Tomato Paneer)', 'MC-06', 'bowl-grain', 'Vegetarian', 156, 216, '13–18 years', 'North India', 'Family texture', ARRAY['Make a base of onion, tomato, ginger and garlic in ghee.','Add milk and simmer to a smooth gravy.','Add cubed paneer and cumin, simmer briefly and serve.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 90), ('ING0025', 60), ('ING0038', 30), ('ING0056', 30), ('ING0060', 8), ('ING0040', 4), ('ING0039', 4), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Paneer Paratha', 'MC-04', 'flatbread', 'Vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Mix crumbled paneer with coriander leaves and cumin.','Stuff into wheat flour dough and roll into a flatbread.','Cook on a griddle with a little groundnut oil until golden on both sides.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 70), ('ING0007', 70), ('ING0095', 3), ('ING0063', 8), ('ING0093', 1)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Mixed Fruit Bowl', 'MC-05', 'snack', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Wash and dice banana, apple and mango.','Mix together in a bowl and serve fresh.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0041', 40), ('ING0042', 40), ('ING0044', 40)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Fruit and Yogurt Bowl', 'MC-05', 'snack', 'Vegetarian', 24, 59, '2–5 years', 'West Bengal / East India', 'Family texture', ARRAY['Dice banana and apple.','Stir into fresh curd and serve immediately.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0057', 80), ('ING0041', 30), ('ING0042', 30)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Fruit, Nut and Yogurt Bowl', 'MC-05', 'snack', 'Vegetarian', 60, 119, '6–9 years', 'West Bengal / East India', 'Family texture', ARRAY['Dice banana and apple and stir into curd.','Top with chopped almond and raisins.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0057', 100), ('ING0041', 30), ('ING0042', 30), ('ING0082', 10), ('ING0089', 10)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Date and Nut Energy Bites (No-Cook)', 'MC-05', 'snack', 'Vegetarian', 120, 155, '10–12 years', 'West Bengal / East India', 'Family texture', ARRAY['Pit and finely chop the dates.','Mix with finely chopped cashew, almond and walnut.','Press into small bite-sized pieces and chill before serving.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0090', 50), ('ING0083', 20), ('ING0082', 15), ('ING0084', 15)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Mango and Curd Bowl', 'MC-05', 'snack', 'Vegetarian', 120, 155, '10–12 years', 'Bangladesh', 'Family texture', ARRAY['Dice ripe mango.','Stir into fresh curd and serve chilled.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0057', 100), ('ING0044', 50)) AS v(ingredient_id, quantity_g);

WITH new_recipe AS (
  INSERT INTO ai_recipe (title, meal_category_id, dish_format_id, diet_type, min_age_months, max_age_months, age_group, region_culture, texture, method_steps, model, prompt_version)
  VALUES ('Fruit and Paneer Bowl', 'MC-05', 'snack', 'Vegetarian', 156, 216, '13–18 years', 'West Bengal / East India', 'Family texture', ARRAY['Cube fresh paneer.','Toss with diced apple, banana and raisins and serve chilled.'], 'claude-authored', 'corpus-fill-2026-09-06')
  RETURNING recipe_id
)
INSERT INTO ai_recipe_ingredient (recipe_id, ingredient_id, quantity_g)
SELECT recipe_id, v.ingredient_id, v.quantity_g FROM new_recipe, (VALUES ('ING0058', 50), ('ING0042', 40), ('ING0041', 40), ('ING0089', 10)) AS v(ingredient_id, quantity_g);
