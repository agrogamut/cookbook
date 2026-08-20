-- Which drawn mark illustrates each dish format.
--
-- The provider encodes the dish format in the recipe name -- "{Region} {Ing1} & {Ing2} {Format}"
-- -- and the vocabulary is closed: these twenty-eight formats match all 940 in-scope recipes,
-- verified in both directions by TestEveryRecipeResolvesOneFormatMark.
--
-- The mark is line art of the format, not of the dish, and the page captions it with the format
-- name so the claim is explicit. It asserts nothing the provider did not record. There is no
-- photograph here and none is coming from a dataset (GAP-025); the reasoning, including why the
-- external corpus's image-url column is not usable, is in
-- docs/superpowers/specs/2026-08-20-book-layout-and-imagery-design.md section 3.2.
--
-- Hand-written, like culture_region_map and book1_block_source, because the join is by meaning:
-- a fuzzy match would put a flatbread on a porridge.

CREATE TABLE recipe_format_mark (
  format_pattern text PRIMARY KEY,
  mark_id        text NOT NULL,
  note           text NOT NULL
);

COMMENT ON TABLE recipe_format_mark IS
    'Hand-written map from a dish-format substring in recipe_name to the drawn archetype that '
    'illustrates it. Twenty-eight formats collapse to eleven marks; both directions are pinned '
    'by TestEveryRecipeResolvesOneFormatMark.';

INSERT INTO recipe_format_mark (format_pattern, mark_id, note) VALUES
  ('Soft rice bowl',              'bowl-grain',   'served in a bowl, grain-led'),
  ('Regional rice bowl',          'bowl-grain',   'served in a bowl, grain-led'),
  ('High-protein regional bowl',  'bowl-grain',   'served in a bowl, grain-led'),
  ('Adolescent power bowl',       'bowl-grain',   'served in a bowl, grain-led'),
  ('Protein snack bowl',          'bowl-grain',   'served in a bowl, grain-led'),
  ('Balanced pulao bowl',         'bowl-grain',   'one-pot seasoned rice, served in a bowl'),
  ('Soft protein rice',           'bowl-grain',   'grain-led, served in a bowl'),
  ('Millet meal',                 'bowl-grain',   'grain-led, served in a bowl'),
  ('High-fibre meal',             'bowl-grain',   'grain-led, served in a bowl'),
  ('Quick school/college meal',   'bowl-grain',   'grain-led, served in a bowl'),
  ('Thick porridge bowl',         'bowl-porridge','spoonable, served in a bowl with a spoon'),
  ('Single-grain porridge',       'bowl-porridge','spoonable, served in a bowl with a spoon'),
  ('Savory porridge',             'bowl-porridge','spoonable, served in a bowl with a spoon'),
  ('Vegetable mash',              'dish-mash',    'mashed, served in a shallow dish'),
  ('Fruit-cereal mash',           'dish-mash',    'mashed, served in a shallow dish'),
  ('Dal-rice mash',               'dish-mash',    'mashed, served in a shallow dish'),
  ('Family-style khichdi',        'pot-khichdi',  'one-pot dal and rice, served from the pot'),
  ('Soft khichdi',                'pot-khichdi',  'one-pot dal and rice, served from the pot'),
  ('Mini cutlet/patty',           'patty',        'shaped and pan-cooked'),
  ('Mini pancake/cheela',         'pancake',      'batter cooked flat on a griddle'),
  ('Savory pancake',              'pancake',      'batter cooked flat on a griddle'),
  ('Stuffed flatbread',           'flatbread',    'filled griddle bread'),
  ('Lunchbox wrap',               'wrap',         'filled and rolled'),
  ('School tiffin roll',          'wrap',         'filled and rolled'),
  ('Soft finger bites',           'finger-bites', 'small pieces eaten by hand'),
  ('Breakfast upma/poha style',   'plate-upma',   'savoury semolina or flattened rice, on a plate'),
  ('Sports snack',                'snack',        'portable, eaten away from a table'),
  ('Sports recovery meal',        'snack',        'portable, eaten away from a table');

-- One row per recipe, resolving name to mark. A view rather than a column on recipe_master:
-- the provider's table is never modified, and this is our reading of their name.
CREATE VIEW recipe_mark AS
SELECT r.recipe_id,
       m.mark_id,
       m.format_pattern AS format_label,
       m.note           AS format_note,
       'derived'        AS value_kind
FROM recipe_master r
JOIN recipe_format_mark m ON r.recipe_name LIKE '%' || m.format_pattern || '%';
