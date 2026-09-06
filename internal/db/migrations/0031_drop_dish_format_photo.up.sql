-- Drop the dish-format photography pipeline.
--
-- Migration 0026 built it: two cleanly-licensed image datasets (CC0 and CC BY 4.0), a
-- hand-written label-to-archetype map, and dish_format_photo holding the matched bytes, so a
-- recipe page could print a photograph of its dish FORMAT -- never of the specific recipe.
-- cmd/photomatch fetched and matched; internal/book resolved a photo per card.
--
-- Nothing printed it. The recipe page's own template comment records the history: the picture
-- was removed by decision, restored on 2026-09-04, and removed again the same day, because a
-- per-card image read as a repeated template tell on a book that now carries scattered
-- decorative art on every page instead. Both removals left the resolution in place -- a query
-- per recipe card, base64-encoding image bytes into RecipeCard.Photo -- feeding a JSON field
-- the console does not read and a template that does not print it.
--
-- So this is not a change of policy on photography; it is deleting the machinery for a
-- picture the book decided twice not to show. If a recipe page ever wants a photograph again,
-- 0026 is in git history with its schema and all 41 rows of its label map, and this
-- migration's own down restores them.
--
-- GAP-025 is untouched and stays open: no recipe has a commissioned per-recipe photograph,
-- and recipe_photo (migration 0021) is still the empty table one would land in. That gap was
-- always distinct from this one -- 0026's own comment says so -- and removing the format-level
-- pipeline does not close it or widen it.

-- GAP-029 described what this pipeline delivered. Its ui_behaviour already said something
-- untrue ("then prints the photo instead"), which is its own evidence that the gap outlived
-- the feature it measured.
DELETE FROM gap_register WHERE gap_id = 'GAP-029';

-- Both rows are registrations only: local_file names a manifest that no longer has a producer,
-- and sha256/rows_loaded stayed empty because the fetch was never run here. Nothing references
-- them once dish_format_photo is gone -- external_recipe, external_food_composition and
-- ingredient_nutrition_audit all point at IFCT-2017 and INDIAN-RECIPES.
DELETE FROM external_source WHERE source_key IN ('BHARAT-INDIAN-FOODS', 'FOODBD');

DROP TABLE IF EXISTS dish_format_photo;
DROP TABLE IF EXISTS photo_label_archetype_map;
