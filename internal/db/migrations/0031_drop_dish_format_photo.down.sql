-- Restore the dish-format photography pipeline dropped by 0031.
--
-- The schema and every seed row below are copied verbatim from 0026 rather than rewritten, so
-- this cannot drift from the migration it undoes. Restoring the tables does not restore the
-- image bytes: dish_format_photo comes back empty, and cmd/photomatch would have to be
-- reinstated and re-run to refill it. That is the honest limit of a down migration over a
-- table whose contents came from a network fetch.

CREATE TABLE dish_format_photo (
  photo_id        bigserial PRIMARY KEY,
  mark_id         text NOT NULL CHECK (mark_id <> ''),
  media_type      text NOT NULL CHECK (media_type IN ('image/png','image/jpeg','image/webp')),
  bytes           bytea NOT NULL,
  credit          text NOT NULL CHECK (credit <> ''),
  licence         text NOT NULL CHECK (licence <> ''),
  source_dataset  text NOT NULL,
  source_row_id   text NOT NULL,
  source_label    text NOT NULL,
  added_by        text NOT NULL CHECK (added_by <> ''),
  added_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX dish_format_photo_mark_idx ON dish_format_photo (mark_id);

COMMENT ON TABLE dish_format_photo IS
    'Real photography per dish-format archetype (the same 11 marks.go archetypes), not '
    'per recipe. Written only by cmd/photomatch, never by cmd/import. See GAP-029: this '
    'is a lesser claim than a per-recipe commissioned photo, which stays GAP-025 and '
    'stays open.';

CREATE TABLE photo_label_archetype_map (
  source_dataset  text NOT NULL,
  source_label    text NOT NULL,
  mark_id         text,
  note            text NOT NULL,
  PRIMARY KEY (source_dataset, source_label)
);

COMMENT ON COLUMN photo_label_archetype_map.mark_id IS
    'NULL means deliberately excluded -- see note. Never a fuzzy match target.';

INSERT INTO photo_label_archetype_map (source_dataset, source_label, mark_id, note) VALUES
  -- bharat-raghunathan/indian-foods-dataset, all 15 classes, CC0
  ('BHARAT-INDIAN-FOODS', 'biryani',      'bowl-grain',    'rice dish, grain-led'),
  ('BHARAT-INDIAN-FOODS', 'cholebhature', 'flatbread',     'bhature is a fried flatbread; chole is the side'),
  ('BHARAT-INDIAN-FOODS', 'dabeli',       'snack',         'stuffed bun, portable snack'),
  ('BHARAT-INDIAN-FOODS', 'dal',          NULL,            'lentil side, not a served format on its own'),
  ('BHARAT-INDIAN-FOODS', 'dhokla',       'snack',         'steamed cake, eaten as a snack'),
  ('BHARAT-INDIAN-FOODS', 'dosa',         'pancake',       'batter cooked flat on a griddle'),
  ('BHARAT-INDIAN-FOODS', 'jalebi',       'snack',         'sweet snack'),
  ('BHARAT-INDIAN-FOODS', 'kathiroll',    'wrap',          'filled and rolled'),
  ('BHARAT-INDIAN-FOODS', 'kofta',        'patty',         'shaped and fried ball, closest to patty'),
  ('BHARAT-INDIAN-FOODS', 'naan',         'flatbread',     'griddle bread'),
  ('BHARAT-INDIAN-FOODS', 'pakora',       'snack',         'fried snack'),
  ('BHARAT-INDIAN-FOODS', 'paneer',       NULL,            'ingredient, not a served format'),
  ('BHARAT-INDIAN-FOODS', 'panipuri',     'snack',         'street snack'),
  ('BHARAT-INDIAN-FOODS', 'pavbhaji',     'dish-mash',     'mashed vegetable mix, served in a shallow dish'),
  ('BHARAT-INDIAN-FOODS', 'vadapav',      'snack',         'fried-fritter sandwich, portable snack'),

  -- FoodBD, selected dish-format classes out of 67 total (raw ingredients unlisted, see comment above), CC BY 4.0
  ('FOODBD', 'biriyani',       'bowl-grain',    'rice dish, grain-led'),
  ('FOODBD', 'khichuri',       'pot-khichdi',   'one-pot dal and rice'),
  ('FOODBD', 'payesh',         'bowl-porridge', 'spoonable rice pudding'),
  ('FOODBD', 'ruti',           'flatbread',     'griddle bread'),
  ('FOODBD', 'puri',           'flatbread',     'fried flatbread'),
  ('FOODBD', 'vorta',          'dish-mash',     'mashed vegetable or fish preparation'),
  ('FOODBD', 'begun-vaji',     'dish-mash',     'stir-fried vegetable side, shallow dish'),
  ('FOODBD', 'korola-vaji',    'dish-mash',     'stir-fried vegetable side, shallow dish'),
  ('FOODBD', 'kumra-vaji',     'dish-mash',     'stir-fried vegetable side, shallow dish'),
  ('FOODBD', 'chichinga-vaji', 'dish-mash',     'stir-fried vegetable side, shallow dish'),
  ('FOODBD', 'vaji',           'dish-mash',     'stir-fried vegetable side, shallow dish, generic label'),
  ('FOODBD', 'lal-shak',       'dish-mash',     'sauteed leafy green side'),
  ('FOODBD', 'shak',           'dish-mash',     'sauteed leafy green side, generic label'),
  ('FOODBD', 'kabab',          'patty',         'shaped and pan-cooked'),
  ('FOODBD', 'chop-alu',       'patty',         'shaped potato patty'),
  ('FOODBD', 'beguni',         'patty',         'shaped and fried'),
  ('FOODBD', 'peyaju',         'patty',         'shaped and fried lentil fritter'),
  ('FOODBD', 'piyaju',         'patty',         'shaped and fried lentil fritter, alternate spelling'),
  ('FOODBD', 'roll',           'wrap',          'filled and rolled'),
  ('FOODBD', 'puffed-rice',    'snack',         'portable snack'),
  ('FOODBD', 'jilapi',         'snack',         'sweet snack'),
  ('FOODBD', 'chomchom',       'snack',         'sweet snack'),
  ('FOODBD', 'sweets',         'snack',         'sweet snack, generic label'),
  ('FOODBD', 'daal',           NULL,            'lentil side, not a served format on its own'),
  ('FOODBD', 'noodles',        NULL,            'no archetype fits; not a home format this project''s recipes use'),
  ('FOODBD', 'salad',          NULL,            'raw side, not a served format');

INSERT INTO external_source
    (source_key, name, publisher, url, licence, region_scope, retrieved_on, local_file, sha256, used_for) VALUES
    ('BHARAT-INDIAN-FOODS',
     'Indian Foods Dataset',
     'bharat-raghunathan (Hugging Face), derived from Kaggle: The Massive Indian Foods Dataset',
     'https://huggingface.co/datasets/bharat-raghunathan/indian-foods-dataset',
     'CC0: Public Domain',
     'India',
     CURRENT_DATE,
     'data/external/photos/manifest.csv',
     '',
     'Representative photography per dish-format archetype (dish_format_photo). Format-level only, never claimed as a photo of any specific recipe.'),

    ('FOODBD',
     'FoodBD: A Polygon-Annotated Meal Image Dataset of Bangladeshi Cuisine with Visual and Nutritional Labels',
     'Ahmed, Haque, Huq, Ali, Masud, Naznin (Mendeley Data)',
     'https://data.mendeley.com/datasets/xh3ghf3jbg/2',
     'CC BY 4.0',
     'Bangladesh',
     CURRENT_DATE,
     'data/external/photos/manifest.csv',
     '',
     'Representative photography per dish-format archetype (dish_format_photo), restricted to single-item meal photos. Format-level only, never claimed as a photo of any specific recipe.');

INSERT INTO gap_register
  (gap_id, severity, area, source_table, source_column, description,
   affected_rows, measured_by, ui_behaviour, resolution_path, measured_at)
VALUES
  ('GAP-029', 'minor', 'book2', 'dish_format_photo', NULL,
   'No recipe has a per-recipe commissioned photograph (that remains GAP-025, unchanged). '
   'dish_format_photo instead holds representative stock photography per dish-format '
   'archetype (the same 11 marks.go archetypes), sourced from two verified, cleanly-'
   'licensed datasets and matched by format only -- the same order of claim the drawn '
   'marks already make, just photographic. A recipe whose archetype has no stored photo '
   'still prints its drawn mark.',
   11, 'importer',
   'A recipe prints the drawn mark for its dish format until that format has a stored '
   'photo, then prints the photo instead. Never a photo of the specific recipe.',
   'Commission a real per-recipe photograph (closes GAP-025 directly) or add more '
   'candidate images per archetype via cmd/photomatch -fetch (raises coverage here, does '
   'not touch GAP-025).',
   now());
