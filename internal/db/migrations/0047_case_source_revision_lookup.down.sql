CREATE OR REPLACE VIEW case_source_recipe_ingredient AS
SELECT r.candidate_id, i.ordinal, m.meaning_id,
       i.raw_id, i.raw_name, i.raw_grams, i.grams,
       i.archive_sha256, i.case_number, i.sheet_name, i.row_number
FROM case_source_recipe_revision r
JOIN case_source_ingredient i USING (archive_sha256, case_number, sheet_name, row_number)
JOIN case_source_ingredient_meaning m
    ON m.source_id = btrim(i.raw_id) AND m.source_name = btrim(i.raw_name);
