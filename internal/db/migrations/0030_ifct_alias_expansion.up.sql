-- Nine more used ingredients matched to real IFCT-2017 food codes, closing part of the
-- 37-ingredient gap left after migration 0009 (37 of the 94 ingredients a recipe actually
-- uses had no alias and no auto-exact match). Every code below was looked up directly in
-- the IFCT-2017 index (data/external/ifct2017_index.csv) and cross-checked against the
-- already-loaded external_food_composition values before being written here -- same
-- hand-check discipline as the original 94, run again against the ingredients migration
-- 0009 didn't reach.
--
-- Pearl spot/Karimeen was found by searching the provider's own regional name rather than
-- an English generic term -- IFCT lists it as "Karimeen" outright, an identity match, not
-- a variety judgement.
--
-- Chicken deliberately does NOT use N001 "Chicken, poultry, leg, skinless", despite that
-- being the commonest curry cut: N001's own enerc field (1605 kJ / 383.6 kcal) does not
-- match its own protein and fat (19.44g protein + 12.64g fat -> 191.5 kcal by the 4/4/9
-- rule this project already checks recipe-level totals against) -- almost exactly double.
-- N002 (thigh) and N003 (breast), the other two skinless cuts in the same file, both match
-- their own macros within 1%, so this is a single bad row in the source file, not a unit or
-- parsing problem on the import side. N003 (breast, skinless) is used instead: internally
-- consistent, and an equally defensible "generic chicken" reference cut.
--
-- Genuinely absent from IFCT-2017 and left unaliased (checked directly against the index,
-- not assumed from migration 0009's list): Oats, Bamboo rice, Chhena, Curd/Yogurt, Kachki
-- small fish, Mola fish, Bhetki, Punti fish, Trout, Seabuckthorn berry, Foxtail millet,
-- Jhangora/Barnyard millet Himalayan, Buckwheat/Kuttu, Turnip, Tofu, Chayote/Squash,
-- Pointed gourd (parwal/potol), Teasel gourd/Kakrol.

INSERT INTO ingredient_ifct_alias (ingredient_english_name, food_code, match_note, exactness) VALUES
    ('Chicken',             'N003', 'IFCT separates by cut; breast, skinless is used because leg (N001) fails its own 4/4/9 macro check -- see note above.', 'closest-variety'),
    ('Carrot',              'F002', 'IFCT separates by colour; orange is the commercially dominant variety.', 'closest-variety'),
    ('Tomato',              'D076', 'IFCT separates ripe/green and hybrid/local; ripe, local is the everyday cooking tomato.', 'closest-variety'),
    ('Pumpkin',             'D066', 'IFCT separates by shape and colour; orange, round is the common kumro/kaddu.', 'closest-variety'),
    ('Banana',              'E012', 'IFCT separates by cultivar; robusta is the most widely sold commercial variety.', 'closest-variety'),
    ('Dates',               'E017', 'IFCT separates dry/processed forms; dry, pale brown is the common whole date.', 'closest-variety'),
    ('Raisins',             'E057', 'IFCT separates by colour; dried, black (kismis) is the common Indian raisin.', 'closest-variety'),
    ('Lemon/Lime',          'E033', 'Provider combines two distinct citrus fruits into one row; IFCT lists them separately and they are not nutritionally identical. Lemon, juice used as the reference -- a coarser judgement call than the other closest-variety rows above.', 'closest-variety'),
    ('Pearl spot/Karimeen', 'P026', 'IFCT lists it by the regional name Karimeen directly.', 'same-food');
