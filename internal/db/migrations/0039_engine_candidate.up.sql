-- One candidate pool for the engine: the provider's corpus and the verified AI corpus.
--
-- Design and the two decisions behind it:
-- docs/superpowers/specs/2026-09-06-ai-recipe-union-design.md
--
-- Until now step 1 built its pool from `SELECT recipe_id FROM recipe_master`, so the 71 rows in
-- ai_recipe were never candidates. These two views are what the engine reads instead. They add
-- nothing to either corpus and change no stored row -- a union and a column alias, no more.
--
-- The reason this is a view rather than a UNION written into each query: step 2's allergen
-- filter is a keep-list (`WHERE r.recipe_id = ANY($1) AND NOT EXISTS (...)`), so any query left
-- pointing at recipe_master simply fails to return the AI ids handed to it, and they vanish
-- from the pool silently. One view means repointing is a mechanical, checkable edit rather than
-- eight independent chances to leave one behind.

CREATE VIEW engine_candidate AS
SELECT
    r.recipe_id,
    r.recipe_name,
    r.age_group,
    r.min_age_months,
    r.max_age_months,
    r.meal_type,
    r.texture,
    r.diet_type,
    r.allergen_tags,
    r.region_culture,
    r.budget_band,
    r.cost_per_serving_inr,
    r.prep_time_min,
    r.cook_time_min,
    r.clinical_tag,
    r.review_status,
    'provider'::text AS source
FROM recipe_master r

UNION ALL

SELECT
    a.recipe_id,
    a.title            AS recipe_name,
    a.age_group,
    a.min_age_months,
    a.max_age_months,
    a.meal_type,
    a.texture,
    a.diet_type,
    a.allergen_tags,
    a.region_culture,
    -- budget_band, prep_time_min, cook_time_min and clinical_tag are NULL on every AI row, and
    -- deliberately not derived. The three rankers that read them (budget, time, and the
    -- clinical badge) all lift a matching subset and fall back to the full ranking when that
    -- subset is empty, so a NULL costs an AI recipe a lift it has not earned and never removes
    -- it. Deriving a budget band from cost would need boundaries no table states, and a prep
    -- time for a recipe nobody has cooked would be a number with no source -- both are the
    -- invention this project's hard rule forbids, to buy a ranking nudge.
    NULL::text         AS budget_band,
    a.cost_per_serving_inr,
    NULL::integer      AS prep_time_min,
    NULL::integer      AS cook_time_min,
    NULL::text         AS clinical_tag,
    a.review_status,
    'ai'::text         AS source
FROM ai_recipe_derived a;

COMMENT ON VIEW engine_candidate IS
    'Every recipe the engine may consider, provider corpus and verified AI corpus together. '
    'source says which, and is what rank.go partitions on: an AI recipe never interleaves by '
    'score with a provider one, because recipe_target_score normalises within a band built '
    'from recipe_master''s placeholder values while ai_recipe_derived computes real nutrition '
    'from ingredient quantities, and the two scores are not the same measurement.';

COMMENT ON COLUMN engine_candidate.source IS
    'provider or ai. Never shown to a family; the printed page does not mark a recipe''s '
    'origin, by decision. It exists so the ranker can partition and so an operator can see '
    'the split before a book is signed.';

-- The ingredient side. Five columns, because that is all the engine reads: the allergen filter
-- needs ingredient_allergen_tag and ingredient_id, the vegan branch of the diet filter needs
-- food_group, dedupe needs recipe_id and ingredient_id, and the availability ranker joins
-- ingredient_master itself for region_availability rather than reading it from the mapping.
--
-- food_group was missed on the first cut of this view and the diet tests caught it -- which is
-- the half-done-union failure the design doc names, arriving exactly where it was predicted to.
-- It matters more than the others: without it the vegan exclusion cannot run at all.
CREATE VIEW engine_candidate_ingredient AS
SELECT
    m.recipe_id,
    m.ingredient_id,
    m.quantity,
    m.food_group,
    m.ingredient_allergen_tag
FROM recipe_ingredient_mapping m

UNION ALL

SELECT
    ai.recipe_id,
    ai.ingredient_id,
    ai.quantity_g AS quantity,
    im.food_group,
    -- ai_recipe_ingredient carries no allergen column, so the tag comes from ingredient_master
    -- by ingredient_id. That is stronger than the corpus's own arrangement rather than weaker:
    -- recipe_ingredient_mapping.ingredient_allergen_tag is a denormalised copy the provider
    -- shipped and can drift from ingredient_master, while this cannot.
    im.allergen_tags AS ingredient_allergen_tag
FROM ai_recipe_ingredient ai
JOIN ingredient_master im ON im.ingredient_id = ai.ingredient_id;

COMMENT ON VIEW engine_candidate_ingredient IS
    'Recipe-to-ingredient rows for both corpora, carrying the four columns the engine reads. '
    'The allergen hard filter runs over this, so an AI recipe is screened by exactly the same '
    'query as a provider one rather than by a second implementation beside it.';

-- Scoring the AI partition.
--
-- recipe_target_score reads recipe_nutrition_normalised, which is keyed on recipe_master, so
-- an AI recipe has no score at all. Under the partition it only ever has to be ordered against
-- other AI recipes, so this normalises within the AI corpus alone and is deliberately NOT
-- commensurable with the provider one. That is the whole reason the partition is cheap: were
-- the two required to be comparable, the placeholder problem would come straight back.
--
-- Same axes and same min-max-within-age-band shape as recipe_nutrition_normalised, so the two
-- can be read the same way even though their numbers may not be compared.
CREATE VIEW ai_recipe_nutrition_normalised AS
WITH band AS (
    SELECT
        d.recipe_id,
        d.age_group,
        d.energy_kcal, d.protein_g, d.iron_mg, d.calcium_mg, d.fibre_g,
        d.cost_per_serving_inr,
        d.distinct_food_groups::numeric AS diversity,
        d.fruitveg_share
    FROM ai_recipe_derived d
)
SELECT
    recipe_id,
    age_group,
    -- NULLIF guards a band holding one recipe, or several with identical values: max = min
    -- makes the denominator zero, and 0.5 is the honest answer there rather than an error or a
    -- silent 1.0. A single-recipe band has no spread to place anything within.
    coalesce((energy_kcal - min(energy_kcal) OVER w)
             / NULLIF(max(energy_kcal) OVER w - min(energy_kcal) OVER w, 0), 0.5) AS energy_n,
    coalesce((protein_g - min(protein_g) OVER w)
             / NULLIF(max(protein_g) OVER w - min(protein_g) OVER w, 0), 0.5) AS protein_n,
    coalesce((iron_mg - min(iron_mg) OVER w)
             / NULLIF(max(iron_mg) OVER w - min(iron_mg) OVER w, 0), 0.5) AS iron_n,
    coalesce((calcium_mg - min(calcium_mg) OVER w)
             / NULLIF(max(calcium_mg) OVER w - min(calcium_mg) OVER w, 0), 0.5) AS calcium_n,
    coalesce((fibre_g - min(fibre_g) OVER w)
             / NULLIF(max(fibre_g) OVER w - min(fibre_g) OVER w, 0), 0.5) AS fibre_n,
    -- Inverted, exactly as recipe_nutrition_normalised does it: Recipe_Score_Cost weights
    -- budget-friendliness, so cheaper scores higher.
    coalesce(1 - (cost_per_serving_inr - min(cost_per_serving_inr) OVER w)
                 / NULLIF(max(cost_per_serving_inr) OVER w
                          - min(cost_per_serving_inr) OVER w, 0), 0.5) AS cost_n,
    coalesce((diversity - min(diversity) OVER w)
             / NULLIF(max(diversity) OVER w - min(diversity) OVER w, 0), 0.5) AS diversity_n,
    coalesce((fruitveg_share - min(fruitveg_share) OVER w)
             / NULLIF(max(fruitveg_share) OVER w - min(fruitveg_share) OVER w, 0), 0.5)
        AS fruitveg_n
FROM band
WINDOW w AS (PARTITION BY age_group);

COMMENT ON VIEW ai_recipe_nutrition_normalised IS
    'Per-axis min-max normalisation within an age band, over the AI corpus only. Deliberately '
    'not comparable with recipe_nutrition_normalised: that one bands on recipe_master''s '
    'provider placeholder values, this one on nutrition computed from real ingredient '
    'quantities. AI recipes partition below provider recipes rather than interleaving, so the '
    'two never have to be compared.';

-- The AI half of recipe_target_score: same seven scored axes, same provider-authored weights,
-- same formula. Only the normalisation underneath differs.
CREATE VIEW ai_recipe_target_score AS
SELECT
    n.recipe_id,
    t.target_code,
    (t.recipe_score_energy    * n.energy_n
   + t.recipe_score_protein   * n.protein_n
   + t.recipe_score_iron      * n.iron_n
   + t.recipe_score_calcium   * n.calcium_n
   + t.recipe_score_fruitveg  * n.fruitveg_n
   + t.recipe_score_diversity * n.diversity_n
   + t.recipe_score_cost      * n.cost_n)
  / NULLIF(t.recipe_score_energy + t.recipe_score_protein + t.recipe_score_iron
         + t.recipe_score_calcium + t.recipe_score_fruitveg + t.recipe_score_diversity
         + t.recipe_score_cost, 0) AS score,
    'energy,protein,iron,calcium,fruitveg,diversity,cost'::text AS scored_axes,
    'derived'::text AS value_kind
FROM ai_recipe_nutrition_normalised n
CROSS JOIN nutrition_target_master t;

COMMENT ON VIEW ai_recipe_target_score IS
    'recipe_target_score''s counterpart for the AI corpus: the same seven scored axes and the '
    'same provider-authored NT00-NT12 weights, over ai_recipe_nutrition_normalised. The three '
    'unscored axes are omitted from numerator and denominator here for the same reasons they '
    'are there -- no ultra-processed flag exists, texture is constant across candidates, and '
    'culture is handled at step 7.';

-- One ranked view over both corpora, mirroring recipe_ranked exactly: nutrition_score times
-- the region rank_weight, which only ever multiplies, so region reorders and never removes.
--
-- carries `source` so target.go can order on it without a second query. The AI half joins
-- ai_recipe_target_score in place of recipe_target_score and engine_candidate in place of
-- recipe_master; everything else is the same expression.
CREATE VIEW engine_ranked AS
SELECT s.recipe_id, s.target_code, c.region_culture, f.focus_tier,
       s.score AS nutrition_score,
       round(s.score * f.rank_weight, 6) AS ranked_score,
       s.scored_axes, 'derived'::text AS value_kind,
       'provider'::text AS source
FROM recipe_target_score s
JOIN engine_candidate c ON c.recipe_id = s.recipe_id AND c.source = 'provider'
JOIN region_focus f ON f.region_culture = c.region_culture

UNION ALL

SELECT s.recipe_id, s.target_code, c.region_culture, f.focus_tier,
       s.score AS nutrition_score,
       round(s.score * f.rank_weight, 6) AS ranked_score,
       s.scored_axes, 'derived'::text AS value_kind,
       'ai'::text AS source
FROM ai_recipe_target_score s
JOIN engine_candidate c ON c.recipe_id = s.recipe_id AND c.source = 'ai'
JOIN region_focus f ON f.region_culture = c.region_culture;

COMMENT ON VIEW engine_ranked IS
    'recipe_ranked extended over both corpora. ranked_score is comparable WITHIN a source and '
    'not across one: the provider half normalises against recipe_master''s placeholder values, '
    'the AI half against nutrition computed from real ingredient quantities. rank.go partitions '
    'on source for exactly that reason and never sorts the two together on score alone.';
