-- Engine step 5, as a ranker.
--
-- This is the fix for filter collapse. Stacking hard filters on the provider's
-- single-valued tag columns empties the result set; ranking cannot. Recipes are ordered
-- by fitness against the active nutrition target instead of being excluded for lacking
-- a tag, so a query always returns something and the user sees why it is ordered as it
-- is. Clinical_Tag stays a display badge and is never filtered on.
--
-- The weights are the provider's, from Nutrition_Target_Master.Recipe_Score_*. Nothing
-- here invents a threshold or a percentile.

-- Fruit and vegetable presence per recipe. Counted from the mapping table only.
--
-- "Fruit or vegetable" means a food_group whose name contains "fruit" or "vegetable":
-- Vegetable, Leafy vegetable, Root vegetable, Fermented vegetable, Legume/Vegetable,
-- Fruit, Dried fruit, Fruit/Fat source, Fruit/Starch. Tuber is deliberately excluded --
-- potato and similar are starchy staples, and counting them would inflate the score of
-- a recipe that contains no actual fruit or vegetable serving.
CREATE VIEW recipe_fruitveg AS
SELECT r.recipe_id,
       count(*) FILTER (WHERE m.food_group ~* '(fruit|vegetable)') AS fruitveg_ingredients,
       count(*)                                                    AS total_ingredients,
       CASE WHEN count(*) = 0 THEN 0
            ELSE count(*) FILTER (WHERE m.food_group ~* '(fruit|vegetable)')::numeric / count(*)
       END AS fruitveg_share
FROM recipe_master r
JOIN recipe_ingredient_mapping m ON m.recipe_id = r.recipe_id
GROUP BY r.recipe_id;

-- Diversity and fruit/veg share, min-max normalised within the age band on the same
-- rule as recipe_nutrition_normalised, so every axis fed to the ranker is on 0-1.
CREATE VIEW recipe_composition_normalised AS
WITH base AS (
    SELECT r.recipe_id,
           r.age_group,
           d.distinct_food_groups::numeric AS diversity,
           v.fruitveg_share
    FROM recipe_master r
    JOIN recipe_diversity d ON d.recipe_id = r.recipe_id
    JOIN recipe_fruitveg  v ON v.recipe_id = r.recipe_id
), band AS (
    SELECT age_group,
           min(diversity)      AS d_lo, max(diversity)      AS d_hi,
           min(fruitveg_share) AS v_lo, max(fruitveg_share) AS v_hi
    FROM base GROUP BY age_group
)
SELECT b.recipe_id,
       b.age_group,
       CASE WHEN n.d_hi = n.d_lo THEN 0 ELSE (b.diversity      - n.d_lo) / (n.d_hi - n.d_lo) END AS norm_diversity,
       CASE WHEN n.v_hi = n.v_lo THEN 0 ELSE (b.fruitveg_share - n.v_lo) / (n.v_hi - n.v_lo) END AS norm_fruitveg
FROM base b JOIN band n ON n.age_group = b.age_group;

-- The ranker itself: every recipe scored against every nutrition target.
--
--     score = sum(weight_axis * normalised_axis) / sum(weight_axis)
--
-- Seven of the provider's ten Recipe_Score_* axes are scored. The other three are
-- omitted from BOTH the numerator and the denominator, deliberately and visibly:
--
--   Recipe_Score_Low_UPF         no ultra-processed flag exists on any provider table,
--                                so there is nothing to measure. Not estimated.
--   Recipe_Score_Texture_Safety  all 13 targets weight it 5 and texture is already a
--                                hard filter at engine step 1, so it is constant across
--                                every candidate and cannot change an ordering.
--   Recipe_Score_Culture         handled separately at engine step 7 by region_focus
--                                and the cuisine-cluster overlap ranker, so folding it
--                                in here would count culture twice.
--
-- scored_axes records which axes actually contributed, so a score is reproducible from
-- the row rather than taken on trust.
CREATE VIEW recipe_target_score AS
SELECT r.recipe_id,
       t.target_code,
       r.age_group,
       round((
             t.recipe_score_energy    * n.norm_energy
           + t.recipe_score_protein   * n.norm_protein
           + t.recipe_score_iron      * n.norm_iron
           + t.recipe_score_calcium   * n.norm_calcium
           + t.recipe_score_fruitveg  * c.norm_fruitveg
           + t.recipe_score_diversity * c.norm_diversity
           + t.recipe_score_cost      * n.norm_cost_inverted
       ) / NULLIF(
             t.recipe_score_energy + t.recipe_score_protein + t.recipe_score_iron
           + t.recipe_score_calcium + t.recipe_score_fruitveg + t.recipe_score_diversity
           + t.recipe_score_cost, 0), 6) AS score,
       'energy,protein,iron,calcium,fruitveg,diversity,cost'::text AS scored_axes,
       'derived'::text AS value_kind
FROM recipe_master r
JOIN recipe_nutrition_normalised   n ON n.recipe_id = r.recipe_id
JOIN recipe_composition_normalised c ON c.recipe_id = r.recipe_id
CROSS JOIN nutrition_target_master t;

COMMENT ON VIEW recipe_target_score IS
    'Engine step 5. Weights are the provider''s Recipe_Score_* columns; axes are min-max '
    'normalised within the recipe age band. Every score is derived, never a clinician''s '
    'judgement, and must be labelled as such wherever it is shown.';

-- Default ordering when the user has expressed no region preference: nutrition fitness
-- first, project region focus as a tie shaper. rank_weight only ever multiplies, so an
-- out-of-scope recipe can be outranked but never removed.
--
-- When the user DOES pick a region, the caller must rank on recipe_target_score and
-- apply their choice instead. The project's focus tiers are our default, not an
-- override of what the user actually asked for: someone who selects Nepal should get
-- Nepali recipes at the top, not Bengali ones.
CREATE VIEW recipe_ranked AS
SELECT s.recipe_id,
       s.target_code,
       r.region_culture,
       f.focus_tier,
       s.score                     AS nutrition_score,
       round(s.score * f.rank_weight, 6) AS ranked_score,
       s.scored_axes,
       'derived'::text AS value_kind
FROM recipe_target_score s
JOIN recipe_master r ON r.recipe_id = s.recipe_id
JOIN region_focus  f ON f.region_culture = r.region_culture;
