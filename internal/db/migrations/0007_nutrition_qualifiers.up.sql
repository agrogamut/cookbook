-- Food-form qualifiers.
--
-- The composition table names foods precisely: "Groundnut oil", "Pumpkin leaves,
-- tender", "Rice flakes". The provider names them plainly: "Peanut", "Pumpkin", "Rice".
-- Matching on name tokens alone accepted all three of those pairs, because the provider
-- name is fully contained in the composition name -- and each one is a different food
-- with different nutrition. Peanut is not groundnut oil.
--
-- A qualifier listed here changes what the food IS, not merely which variety it is. If
-- the composition name carries one and the provider name does not, the two are different
-- foods and the match is rejected. Variety qualifiers ("Onion, big", "Carrot, orange",
-- "Potato, brown skin") are deliberately absent: those are the same food.

CREATE TABLE food_form_qualifier (
    qualifier text PRIMARY KEY,
    reason    text NOT NULL
);

COMMENT ON TABLE food_form_qualifier IS
    'Tokens that change what a food is. Present in the composition name but absent from '
    'the provider name means the two rows are different foods, not a loose match.';

INSERT INTO food_form_qualifier (qualifier, reason) VALUES
    ('oil',     'An oil pressed from a food is not that food.'),
    ('leaf',    'Leaves are a different part of the plant from the fruit or root.'),
    ('leave',   'Plural form of leaf after singularisation.'),
    ('flake',   'Flaked and processed, not the whole grain.'),
    ('flour',   'Milled, with different energy density and micronutrients.'),
    ('powder',  'Dried and milled.'),
    ('dry',     'Dried, so water is removed and everything per 100 g is concentrated.'),
    ('dried',   'Dried, so water is removed and everything per 100 g is concentrated.'),
    ('juice',   'Pressed liquid, not the whole food.'),
    ('pickle',  'Preserved in salt and oil.'),
    ('papad',   'A different prepared product.'),
    ('fried',   'Cooked in fat, which changes the fat and energy figures.'),
    ('roasted', 'Roasting changes moisture and therefore every per-100 g figure.'),
    ('boiled',  'Cooking changes moisture and therefore every per-100 g figure.'),
    ('sprout',  'Sprouting changes the composition.'),
    ('seed',    'Seed rather than flesh.'),
    ('skin',    'Skin or peel rather than flesh.'),
    ('stem',    'Stem rather than flesh.'),
    ('flower',  'Flower rather than fruit or root.'),
    ('milk',    'Extracted milk, such as coconut milk, is not the whole food.'),
    ('cake',    'Pressed residue after oil extraction.'),
    ('bran',    'Bran is the outer layer, not the grain.'),
    ('germ',    'Germ is one part of the grain.'),
    ('husk',    'Husk is inedible fibre.'),
    ('puffed',  'Puffed grain is processed and has a different density.'),
    ('parched', 'Parched grain is processed.'),
    ('mushroom','A distinct food that shares a word with several others ("chicken mushroom").'),
    ('tender',  'Tender greens of a plant are not its fruit or root.'),
    ('liver',   'Organ meat, not muscle meat.'),
    ('giblet',  'Organ meat, not muscle meat.'),
    ('kidney',  'Organ meat when applied to an animal food.'),
    ('blood',   'Not muscle meat.');
