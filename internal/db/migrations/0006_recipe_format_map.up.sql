-- Dish format matching.
--
-- Ingredient overlap alone cannot identify a dish. Measured on this data: at every
-- threshold that produced a usable number of matches, roughly nine in ten pairs were
-- wrong -- a rice-and-peanut infant mash matched to peanut bhel chaat, a sweet potato
-- mash to stuffed bitter gourd, a lemon rice porridge to paneer biryani. All of those
-- share ingredients. None of them shares a preparation.
--
-- The provider encodes the dish format in the recipe name ("... Dal-rice mash",
-- "... Mini cutlet/patty", "... Stuffed flatbread"). This table says which external
-- recipe names are a valid source of method text for each format.
--
-- A format with no row here gets no suggestion, ever. That is most of the infant range:
-- purees, mashes, porridges and finger bites have no counterpart in a corpus of adult
-- home cooking, and the honest output is the provider's text alone. Writing preparation
-- steps for an infant puree from an adult curry is exactly the failure the hard rule
-- exists to prevent.

CREATE TABLE external_format_map (
    format_pattern text PRIMARY KEY,   -- matched case-insensitively against recipe_name
    name_keywords  text[] NOT NULL,    -- external recipe name must contain at least one
    note           text NOT NULL
);

-- Dish names that are never a valid method source for a pediatric recipe, whatever the
-- format keyword says. Deep frying is the whole of this list: it is not an appropriate
-- preparation for infant or toddler food, and several fried snacks share a name stem
-- with a format we do accept (vada against adai, bonda against a steamed cutlet).
CREATE TABLE external_method_exclusion (
    keyword text PRIMARY KEY,
    reason  text NOT NULL
);

INSERT INTO external_method_exclusion (keyword, reason) VALUES
    ('deep fried', 'Deep frying is not an appropriate preparation for pediatric food.'),
    ('deep-fried', 'Deep frying is not an appropriate preparation for pediatric food.'),
    ('vada',       'Deep-fried lentil snack.'),
    ('vadai',      'Deep-fried lentil snack.'),
    ('bonda',      'Deep-fried potato snack.'),
    ('pakora',     'Deep-fried fritter.'),
    ('pakoda',     'Deep-fried fritter.'),
    ('bhajji',     'Deep-fried fritter.'),
    ('fritter',    'Deep-fried.'),
    ('murukku',    'Deep-fried savoury.'),
    ('chakli',     'Deep-fried savoury.'),
    ('samosa',     'Deep-fried pastry.'),
    ('puri',       'Deep-fried bread.'),
    ('poori',      'Deep-fried bread.'),
    ('kachori',    'Deep-fried stuffed bread.'),
    ('chips',      'Deep-fried.'),
    ('namkeen',    'Deep-fried packaged-style snack.');

COMMENT ON TABLE external_format_map IS
    'Which external dish names may supply method text for each provider dish format. '
    'Hand-written and deliberately conservative: an unmapped format yields no suggestion.';

INSERT INTO external_format_map (format_pattern, name_keywords, note) VALUES
    ('khichdi',
     ARRAY['khichdi', 'khichadi', 'khichuri', 'kichadi'],
     'Same dish under several spellings across regions.'),

    ('dal-rice mash',
     ARRAY['khichdi', 'khichadi', 'khichuri', 'kichadi'],
     'A dal-and-rice mash is khichdi cooked softer. The method transfers; the texture note comes from the age master, not from here.'),

    ('cheela|savory pancake',
     ARRAY['cheela', 'chilla', 'chila', 'pudla', 'pesarattu', 'adai'],
     'Batter pancakes of pulse or grain flour.'),

    ('cutlet|patty',
     ARRAY['cutlet', 'tikki', 'patty'],
     'Shaped, pan-fried or steamed bites. Vada and bonda are excluded: both are deep-fried.'),

    ('stuffed flatbread',
     ARRAY['paratha', 'parantha', 'thepla', 'puran poli', 'stuffed roti'],
     'Filled griddle breads.'),

    ('upma|poha',
     ARRAY['upma', 'poha', 'pohe', 'uppma', 'sevai'],
     'Savoury semolina and flattened-rice breakfasts.'),

    ('pulao bowl',
     ARRAY['pulao', 'pulav', 'pilaf', 'bath'],
     'One-pot seasoned rice. Biryani is excluded: it is a layered, spiced adult dish, not a pulao.'),

    ('lunchbox wrap|tiffin roll',
     ARRAY['roll', 'wrap', 'frankie', 'kathi'],
     'Filled rolls suitable for a lunchbox.'),

    ('idli|dosa',
     ARRAY['idli', 'dosa', 'dosai', 'uttapam', 'uthappam'],
     'Fermented batter formats.'),

    ('soup',
     ARRAY['soup', 'rasam', 'shorba'],
     'Thin, spoonable preparations.');
