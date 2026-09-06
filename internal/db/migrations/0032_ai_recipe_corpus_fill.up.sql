-- Extends ai_recipe (migration 0025) so it can hold batch-generated corpus-fill recipes,
-- not only the runtime per-child fallback it was built for. Agreed with the book/engine
-- session over cross-session messages 2026-09-06: reuse this table rather than touch
-- recipe_master (cmd/import's upsert-plus-sweep would delete anything there with no
-- workbook row) or add a source column to recipe_master (same reason).
--
-- age_group, region_culture and texture are declared inputs, chosen when a recipe is
-- authored -- the same status meal_category_id already has on this table, not something
-- derived from the ingredients. max_age_months is likewise stored rather than viewed,
-- computed once at insert time as the age_feeding_stage_master upper bound for the
-- declared age_group (a real provider table, the same one engine step 1 already reads),
-- matching how min_age_months already worked before this migration. The CHECK below only
-- catches min > max; it does not (and cannot, from SQL alone) verify the age_group/texture
-- pairing was actually looked up rather than typed -- that discipline lives in whatever
-- inserts the row, the same trust boundary quantity_g > 0 already relies on for the
-- ingredient table.
ALTER TABLE ai_recipe
    ADD COLUMN age_group      text,
    ADD COLUMN region_culture text,
    ADD COLUMN texture        text,
    ADD COLUMN max_age_months integer,
    ADD CONSTRAINT ai_recipe_age_band_check
        CHECK (max_age_months IS NULL OR min_age_months IS NULL OR max_age_months >= min_age_months);

COMMENT ON COLUMN ai_recipe.age_group IS
    'Declared at generation time, from recipe_master''s own age_group vocabulary (en-dash '
    'separated, e.g. "2–5 years") so it joins cleanly with recipe_master rows wherever the '
    'two are unioned. Null on runtime-fallback rows persisted before this migration.';
COMMENT ON COLUMN ai_recipe.region_culture IS
    'Declared at generation time, constrained to region_focus''s real 9-value vocabulary so '
    'recipe_ranked''s join to region_focus needs no separate code path for these rows.';
COMMENT ON COLUMN ai_recipe.texture IS
    'Declared at generation time, from recipe_master''s own texture vocabulary.';
COMMENT ON COLUMN ai_recipe.max_age_months IS
    'age_feeding_stage_master''s upper bound for the declared age_group, computed once at '
    'insert time -- never guessed independently of min_age_months.';

-- ---------------------------------------------------------------------------
-- Per-ingredient nutrition, exactly recipe_nutrition_recomputed's formula (migration 0010)
-- run against ai_recipe_ingredient instead of recipe_ingredient_mapping. Prefers the IFCT-
-- corrected value the same way; an ingredient with no IFCT match keeps the provider's
-- placeholder value, same honesty rule as the real corpus.
--
-- energy_kcal has one narrow guard on top of that, as originally written: the loaded
-- IFCT-2017 index carries energy_kcal_100g = 0 for every pure oil/fat row (T004
-- Gingelly/Sesame oil, T005 Groundnut oil, T006 Mustard oil, T013 Ghee) while their own
-- fat_g_100g is a correct 100 -- checked directly against data/external/ifct2017_index.csv,
-- a defect in the source file itself, not an import or matching bug. ingredient_nutrition_
-- corrected was deliberately left untouched at the time: the defect reaches an estimated
-- 406 real recipes, and fixing a table this many other things read is a shared-layer
-- decision, not one to make inside a fallback-recipe migration.
--
-- Migration 0035 removes this guard: 0034 (applied after this one) fixed
-- ingredient_nutrition_corrected itself with the more principled Atwater derivation, so a
-- second, narrower rule here could now only drift from it, never improve on it.
CREATE VIEW ai_recipe_nutrition AS
SELECT ar.recipe_id,
       sum(ai.quantity_g)                                              AS total_mass_g,
       sum(ai.quantity_g / 100.0 *
           CASE WHEN c.energy_kcal_100g = 0 AND c.fat_g_100g > 0
                THEN c.provider_energy_kcal_100g
                ELSE c.energy_kcal_100g
           END)                                                        AS energy_kcal,
       sum(ai.quantity_g / 100.0 * c.protein_g_100g)                   AS protein_g,
       sum(ai.quantity_g / 100.0 * c.fat_g_100g)                       AS fat_g,
       sum(ai.quantity_g / 100.0 * c.carb_g_100g)                      AS carb_g,
       sum(ai.quantity_g / 100.0 * c.fibre_g_100g)                     AS fibre_g,
       sum(ai.quantity_g / 100.0 * c.iron_mg_100g)                     AS iron_mg,
       sum(ai.quantity_g / 100.0 * c.calcium_mg_100g)                  AS calcium_mg,
       coalesce(sum(ai.quantity_g) FILTER (WHERE c.value_source = 'ifct'), 0)
           / NULLIF(sum(ai.quantity_g), 0)                             AS ingredient_coverage
FROM ai_recipe ar
JOIN ai_recipe_ingredient ai ON ai.recipe_id = ar.recipe_id
JOIN ingredient_nutrition_corrected c ON c.ingredient_id = ai.ingredient_id
GROUP BY ar.recipe_id;

COMMENT ON VIEW ai_recipe_nutrition IS
    'Nutrition for a generated recipe, computed the same way recipe_nutrition_recomputed '
    'computes it for a real one. Never hand-typed for any ai_recipe row.';

-- ---------------------------------------------------------------------------
-- Allergen union, including ingredient_allergen_override (migration 0024). Kept in the
-- provider's own "Tag;Tag" format and the ILIKE-substring convention the engine's hard
-- filters already use (steps_hard.go, safe_ingredients.go) so a generated recipe is
-- checked by the identical string match a provider recipe is, not a second
-- implementation of the same rule.
-- ---------------------------------------------------------------------------
CREATE VIEW ai_recipe_allergen AS
SELECT ar.recipe_id,
       coalesce(
           nullif(string_agg(DISTINCT tag.allergen, '; ') FILTER (WHERE tag.allergen IS NOT NULL), ''),
           'None identified in starter tagging'
       ) AS allergen_tags
FROM ai_recipe ar
JOIN ai_recipe_ingredient ai ON ai.recipe_id = ar.recipe_id
JOIN ingredient_master m ON m.ingredient_id = ai.ingredient_id
LEFT JOIN LATERAL (
    SELECT trim(t) AS allergen
    FROM unnest(string_to_array(m.allergen_tags, ';')) AS t
    WHERE trim(t) <> 'None identified in starter tagging'
    UNION
    SELECT o.allergen_group
    FROM ingredient_allergen_override o
    WHERE o.ingredient_id = m.ingredient_id
) tag ON true
GROUP BY ar.recipe_id;

COMMENT ON VIEW ai_recipe_allergen IS
    'Union of every ingredient''s own allergen_tags plus ingredient_allergen_override, '
    'never typed by hand on a generated recipe.';

-- ---------------------------------------------------------------------------
-- Diet type: the strictest ingredient present. Vegan and Vegetarian ingredients both
-- leave a recipe Vegetarian -- recipe_master itself carries no "Vegan" recipes (3 real
-- diet_type values, not 4), so a recipe built only from vegan-labelled ingredients is
-- still honestly described as Vegetarian, matching the provider's own vocabulary rather
-- than introducing a label the real corpus never uses.
-- ---------------------------------------------------------------------------
CREATE VIEW ai_recipe_diet AS
SELECT ar.recipe_id,
       CASE
           WHEN bool_or(m.diet_type = 'Non-vegetarian') THEN 'Non-vegetarian'
           WHEN bool_or(m.diet_type = 'Eggetarian')     THEN 'Eggetarian'
           ELSE 'Vegetarian'
       END AS diet_type
FROM ai_recipe ar
JOIN ai_recipe_ingredient ai ON ai.recipe_id = ar.recipe_id
JOIN ingredient_master m ON m.ingredient_id = ai.ingredient_id
GROUP BY ar.recipe_id;

-- ---------------------------------------------------------------------------
-- Cost: ingredient_master.cost_level is a real provider price range ("₹35–350/kg"), not a
-- category label, so a midpoint-of-range times quantity is a documented computation over a
-- real source, not a guess. Four ingredients (Egg, Duck egg, Quail egg, Brinjal/Eggplant)
-- are priced "/piece" rather than by mass; converting a piece price to a per-gram one needs
-- a weight-per-egg this project has no verified source for, so their mass is excluded from
-- the cost sum rather than priced off an invented conversion -- visible as a lower
-- cost_coverage on any recipe using them, the same honesty pattern ingredient_coverage
-- already uses for nutrition. cost_per_serving_inr is therefore a partial sum on such a
-- recipe, not a wrong total.
-- ---------------------------------------------------------------------------
CREATE VIEW ai_recipe_cost AS
WITH priced AS (
    SELECT ai.recipe_id,
           ai.quantity_g,
           (regexp_match(m.cost_level, '₹([\d,]+)–([\d,]+)/(.+)$'))[1] AS low_str,
           (regexp_match(m.cost_level, '₹([\d,]+)–([\d,]+)/(.+)$'))[2] AS high_str,
           (regexp_match(m.cost_level, '₹([\d,]+)–([\d,]+)/(.+)$'))[3] AS unit
    FROM ai_recipe_ingredient ai
    JOIN ingredient_master m ON m.ingredient_id = ai.ingredient_id
), per_gram AS (
    SELECT recipe_id,
           quantity_g,
           CASE WHEN unit = 'piece' THEN NULL
                ELSE (replace(low_str, ',', '')::numeric + replace(high_str, ',', '')::numeric)
                     / 2.0
                     / CASE WHEN unit IN ('kg', 'kg/L') THEN 1000.0
                            WHEN unit = 'L'             THEN 1000.0  -- 1 mL ~= 1 g, same
                                                                      -- approximation this
                                                                      -- schema already makes
                                                                      -- by pricing every
                                                                      -- liquid ingredient's
                                                                      -- quantity in grams
                            ELSE 1.0                                 -- already per gram
                       END
           END AS inr_per_g
    FROM priced
)
SELECT recipe_id,
       sum(quantity_g * inr_per_g)                                    AS cost_per_serving_inr,
       coalesce(sum(quantity_g) FILTER (WHERE inr_per_g IS NOT NULL), 0)
           / NULLIF(sum(quantity_g), 0)                                AS cost_coverage
FROM per_gram
GROUP BY recipe_id;

-- ---------------------------------------------------------------------------
-- Diversity and fruit/veg share, same definition as recipe_diversity / recipe_fruitveg
-- (migration 0002/0003) run against ai_recipe_ingredient. Tuber excluded from fruit/veg for
-- the identical reason recipe_fruitveg excludes it: potato-family starches would inflate
-- the score of a recipe with no actual fruit or vegetable serving.
-- ---------------------------------------------------------------------------
CREATE VIEW ai_recipe_diversity AS
SELECT ai.recipe_id,
       count(DISTINCT m.food_group)    AS distinct_food_groups,
       count(DISTINCT ai.ingredient_id) AS distinct_ingredients
FROM ai_recipe_ingredient ai
JOIN ingredient_master m ON m.ingredient_id = ai.ingredient_id
GROUP BY ai.recipe_id;

CREATE VIEW ai_recipe_fruitveg AS
SELECT ai.recipe_id,
       count(*) FILTER (WHERE m.food_group ~* '(fruit|vegetable)') AS fruitveg_ingredients,
       count(*)                                                    AS total_ingredients,
       CASE WHEN count(*) = 0 THEN 0
            ELSE count(*) FILTER (WHERE m.food_group ~* '(fruit|vegetable)')::numeric / count(*)
       END AS fruitveg_share
FROM ai_recipe_ingredient ai
JOIN ingredient_master m ON m.ingredient_id = ai.ingredient_id
GROUP BY ai.recipe_id;

-- ---------------------------------------------------------------------------
-- One row per generated recipe, shaped to match recipe_master's own per-serving columns so
-- recipe_nutrition_normalised / recipe_composition_normalised can UNION this in and band a
-- generated recipe against real ones in the same age group, rather than scoring it in
-- isolation. That union is engine-side work (claimed separately, cross-session message
-- 2026-09-06) -- this view is the contract it reads, not the union itself.
-- ---------------------------------------------------------------------------
CREATE VIEW ai_recipe_derived AS
SELECT ar.recipe_id,
       ar.title,
       ar.meal_category_id,
       ar.age_group,
       ar.region_culture,
       ar.texture,
       ar.min_age_months,
       ar.max_age_months,
       -- ai_recipe has no review_status column -- the real corpus's own verbatim string is
       -- applied at card-build time (inventedReviewStatus, internal/book/invented.go), never
       -- stored here, so there is exactly one place that string can drift from the real
       -- corpus's Review_Status rather than two.
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

COMMENT ON VIEW ai_recipe_derived IS
    'Every non-authored field on a generated recipe, computed live from ai_recipe_ingredient '
    'joined to ingredient_master / ingredient_nutrition_corrected -- nutrition, allergen_tags, '
    'diet_type and cost can never drift from the ingredient data the way a stored, copied '
    'column could. review_status is stored on ai_recipe itself and must equal the real '
    'corpus''s own verbatim string (see inventedReviewStatus in internal/book/invented.go); '
    'a generated recipe never claims a better review state than a provider one.';
