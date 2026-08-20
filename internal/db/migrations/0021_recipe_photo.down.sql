-- Put GAP-025 back to a seeded number before the table it counts goes away. gapMeasures in
-- internal/importer/gaps.go queries recipe_photo, so a database rolled back past this point with
-- the register still saying 'importer' would fail its next import on a missing table.
UPDATE gap_register
SET measured_by = 'seed', affected_rows = (SELECT count(*) FROM recipe_master)
WHERE gap_id = 'GAP-025';

DROP TABLE IF EXISTS recipe_photo;
