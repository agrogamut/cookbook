-- The Book 2 recipe card schema (MadamGY_Book2_JSON_Schema_V1.json) requires a
-- recipe_version on every card. recipe_master carries no version column at all: the
-- provider ships one row per recipe id with no revision history and nothing to read a
-- version from.
--
-- book.RecipeCard.RecipeVersion is populated from import_table_stat.content_hash for
-- table_name = 'recipe_master' on the latest run instead -- a real, traceable value of
-- the row as loaded (the importer's own hash asserts two imports over the same workbooks
-- reproduce it exactly), but it is not a version the provider assigned, and presenting it
-- as one would be exactly the kind of invented provenance the hard rule forbids. It is
-- schema-required scaffolding, not a provider fact, and the gap register says so rather
-- than letting the field look sourced.

INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows,
     measured_by, ui_behaviour, resolution_path)
VALUES
    ('GAP-024', 'minor', 'Book 2 assembly',
     'recipe_master', NULL,
     'recipe_master carries no version column, but the Book 2 recipe card schema requires '
     || 'recipe_version on every card. The field is populated from the import run''s '
     || 'content_hash for recipe_master, which is a real and traceable value of the row as '
     || 'loaded, not a version the provider assigned. affected_rows counts recipes with no '
     || 'provider version -- every recipe in scope, since the column does not exist.',
     0, 'importer',
     'The card shows the content hash in a monospace field labelled as an import hash, '
     || 'never presented as a provider-assigned recipe version number.',
     'Provider adds a real revision or version column to Recipe_Master. Until then this '
     || 'is derived scaffolding, not a provider fact, and every card says so.');
