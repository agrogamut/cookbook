-- Drop the unified candidate pool, returning the engine to recipe_master alone.
--
-- The 71 ai_recipe rows stay in their own tables and simply stop being candidates, which is
-- where they were before 0039. No recipe data is lost by this revert.

DROP VIEW IF EXISTS engine_ranked;
DROP VIEW IF EXISTS ai_recipe_target_score;
DROP VIEW IF EXISTS ai_recipe_nutrition_normalised;
DROP VIEW IF EXISTS engine_candidate_ingredient;
DROP VIEW IF EXISTS engine_candidate;
