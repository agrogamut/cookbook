-- Real dish-format photography, sourced from two verified, cleanly-licensed datasets
-- (see docs/superpowers/specs/2026-08-24-recipe-photo-pipeline-design.md).
--
-- Keyed to the archetype (mark_id), not to a recipe. Two reasons, both load-bearing:
--
--   1. Matching is format-bucket only, deliberately -- the same order of claim
--      recipe_format_mark's drawn marks already make ("this is what a khichdi looks
--      like", not "this is what THIS khichdi looks like"). A per-recipe row would claim
--      more precision than the match actually has.
--   2. recipe_photo's recipe_id FK targets recipe_master only. An AI-invented recipe
--      (ai_recipe, AR-#####) has no recipe_master row, so a per-recipe table could never
--      hold its photo. Keying to mark_id covers both real and invented recipes uniformly,
--      the same way marks.go's embedded SVGs already do.
--
-- recipe_photo (migration 0021) is untouched and keeps its original, narrower meaning: a
-- real, commissioned photograph of one specific recipe. This table never writes there and
-- GAP-025 (which recipe_photo measures) does not move because of this pipeline -- see
-- GAP-029 below for what this pipeline actually delivers.
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

-- Hand-written, like recipe_format_mark and external_cuisine_region_map: both source
-- datasets have small, closed label vocabularies, so this is a real editorial judgement
-- per label, not a fuzzy string match. mark_id NULL means deliberately excluded -- the
-- label names an ingredient or a format with no honest archetype fit, not an oversight.
-- Not every one of FoodBD's 67 classes is listed: unlisted labels (raw ingredients like
-- "banana", "rice", "egg") are implicitly unmapped by absence from this table. A handful
-- of labels a reader might specifically expect to see mapped are listed explicitly with
-- NULL and a reason, so the omission reads as reviewed rather than missed.
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

-- Both datasets were checked directly, not assumed. bharat-raghunathan's dataset card
-- states CC0 (public domain, no restriction). FoodBD's Mendeley listing states CC BY 4.0.
-- local_file/sha256 start empty and are filled by cmd/photomatch -fetch on its first run,
-- the same post-hoc UPDATE pattern enrichment_run already uses for rows_loaded on the
-- existing two external_source rows -- the manifest these values describe doesn't exist
-- until the fetch actually runs.
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

-- GAP-029: what this pipeline actually delivers, distinct from GAP-025 (which stays open
-- and unmeasured by this table). severity 'minor' matches GAP-025's own severity -- see
-- migration 0023 for the current severity vocabulary.
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
