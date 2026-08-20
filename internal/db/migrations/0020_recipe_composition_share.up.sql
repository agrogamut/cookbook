-- What a recipe is made of, by ingredient mass, grouped.
--
-- Formula, derived and recorded here as the hard rule requires:
--
--   share(macro_group) = sum(quantity where food_group -> macro_group) / sum(quantity)
--
-- over recipe_ingredient_mapping, joined to ingredient_master for the food group. The source
-- rows are the mapping rows themselves; basis_g is their total, so a reader can reproduce it.
-- recipe_ingredient_mapping.quantity is paired with a unit column, and every one of the 3116
-- in-scope mapping rows carries unit = 'g' -- verified directly against the loaded data before
-- writing this view. The view still filters on unit = 'g' rather than assuming it, so a future
-- provider release that adds a non-gram row is excluded from the mass total instead of being
-- silently summed into it as though it were grams.
--
-- This is a mass share, not a nutrition claim. It says nothing about energy, protein or
-- adequacy, and it is deliberately not drawn against a reference intake -- this project carries
-- no reference intakes and would have to invent them.

-- 21 food groups appear on in-scope recipes (checked against the loaded data); 7 macro groups
-- is what a band can distinguish at 26mm. Hand-written, like recipe_format_mark and
-- culture_region_map, because the grouping is a judgement call, not a lookup. 'Unmapped' is a
-- real bucket the view falls back to via COALESCE, not a silent drop, for any food_group this
-- table does not name -- none of the 21 in use today falls there, and TestCompositionSharesSumToOneAndCarryTheUnmapped
-- pins the mechanism rather than the current absence of a case.
-- NOT PRINTED IN BOOK 2, and that is a decision rather than an oversight.
--
-- The band this view was built for was drawn, measured and removed. A recipe page already ends
-- at about 86% of the text block, and a bar plus a seven-group key added roughly 20mm however it
-- was placed -- full width below the method, or stacked in the ingredients column -- which
-- pushed eleven of twelve recipes' trackers onto sheets carrying nothing else. One recipe on one
-- page is the oldest rule in cookbook layout and it is worth more than a second chart on a page
-- that already carries a nutrition panel with an IFCT coverage meter.
--
-- The view stays because the data is right and the place for it is a screen, not a sheet: the
-- operator console is a dense internal devtool where derived values with their formula and basis
-- belong, and this one carries both. Nothing reads it today.

CREATE TABLE food_group_macro (
  food_group  text PRIMARY KEY,
  macro_group text NOT NULL,
  note        text NOT NULL DEFAULT ''
);

COMMENT ON TABLE food_group_macro IS
    'Hand-written collapse of ingredient_master.food_group (21 values in use) to the 7 macro '
    'groups the composition band on a recipe page can distinguish at 26mm.';

INSERT INTO food_group_macro (food_group, macro_group, note) VALUES
  ('Cereal',           'Grain',              ''),
  ('Millet',            'Grain',              ''),
  ('Pseudocereal',      'Grain',              ''),
  ('Cereal-like seed',  'Grain',              ''),
  ('Pulse',             'Pulse & legume',     ''),
  ('Pulse product',     'Pulse & legume',     ''),
  ('Legume',            'Pulse & legume',     ''),
  ('Legume/Vegetable',  'Pulse & legume',     ''),
  ('Nut/Legume',        'Pulse & legume',     ''),
  ('Soy product',       'Pulse & legume',     ''),
  ('Vegetable',         'Vegetable',          ''),
  ('Leafy vegetable',   'Vegetable',          ''),
  ('Root vegetable',    'Vegetable',          ''),
  ('Tuber',             'Vegetable',
   'grouped with vegetables for mass share only -- the nutrition ranker still excludes tuber from its fruit/veg measure, and the two measures answer different questions'),
  ('Fruit',             'Fruit',              ''),
  ('Dried fruit',       'Fruit',              ''),
  ('Fruit/Fat source',  'Fruit',              ''),
  ('Dairy',             'Dairy',              ''),
  ('Fish',              'Animal protein',     ''),
  ('Animal protein',    'Animal protein',     ''),
  ('Fat',               'Fat, seed & spice',  '');

CREATE VIEW recipe_composition_share AS
WITH m AS (
  SELECT rim.recipe_id,
         COALESCE(fgm.macro_group, 'Unmapped') AS macro_group,
         sum(rim.quantity) AS grams
  FROM recipe_ingredient_mapping rim
  JOIN ingredient_master im USING (ingredient_id)
  LEFT JOIN food_group_macro fgm ON fgm.food_group = im.food_group
  WHERE rim.unit = 'g'
  GROUP BY 1, 2
), t AS (
  SELECT recipe_id, sum(grams) AS basis_g FROM m GROUP BY 1
)
SELECT m.recipe_id,
       m.macro_group,
       round(m.grams / t.basis_g, 4) AS share,
       m.grams,
       t.basis_g,
       'derived' AS value_kind,
       'share = sum(quantity) per macro group / sum(quantity), quantities in grams; source rows are recipe_ingredient_mapping'
         AS formula
FROM m JOIN t USING (recipe_id);

COMMENT ON VIEW recipe_composition_share IS
    'What a recipe is made of, by ingredient mass, grouped by food_group_macro. Every mapping '
    'row is counted, including a food_group with no entry in food_group_macro (as "Unmapped"), '
    'so a recipe''s shares always sum to 1 and no ingredient mass is renormalised away.';
