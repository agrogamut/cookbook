ALTER TABLE case_source_row ADD CONSTRAINT case_source_row_identity
    UNIQUE (archive_sha256, case_number, sheet_name, row_number, recipe_id);

CREATE TABLE case_source_recipe_revision (
    candidate_id text PRIMARY KEY,
    archive_sha256 text NOT NULL,
    case_number integer NOT NULL,
    sheet_name text NOT NULL DEFAULT 'Recipe_Master' CHECK (sheet_name = 'Recipe_Master'),
    row_number integer NOT NULL,
    recipe_id text NOT NULL,
    fields jsonb NOT NULL CHECK (jsonb_typeof(fields) = 'object'),
    serving_grams numeric NOT NULL CHECK (serving_grams > 0 AND serving_grams <> 'NaN'::numeric),
    UNIQUE (case_number, recipe_id, candidate_id),
    UNIQUE (archive_sha256, case_number, recipe_id),
    FOREIGN KEY (archive_sha256, case_number, sheet_name, row_number, recipe_id)
        REFERENCES case_source_row (archive_sha256, case_number, sheet_name, row_number, recipe_id),
    CHECK (candidate_id = 'case:' || archive_sha256 || ':' || recipe_id),
    CHECK (recipe_id = fields->>'Recipe_ID'),
    CHECK (serving_grams = (fields->>'Serving_Size_g')::numeric)
);

CREATE TABLE case_source_recipe_current (
    case_number integer NOT NULL,
    recipe_id text NOT NULL,
    candidate_id text NOT NULL UNIQUE,
    PRIMARY KEY (case_number, recipe_id),
    FOREIGN KEY (case_number, recipe_id, candidate_id)
        REFERENCES case_source_recipe_revision (case_number, recipe_id, candidate_id)
);

CREATE TABLE case_source_ingredient_meaning (
    meaning_id text PRIMARY KEY,
    source_id text NOT NULL,
    source_name text NOT NULL,
    UNIQUE (source_id, source_name)
);

CREATE TRIGGER case_source_recipe_revision_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_recipe_revision
    FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_ingredient_meaning_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_ingredient_meaning
    FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();

CREATE VIEW case_source_active_recipe AS
SELECT r.* FROM case_source_recipe_current c
JOIN case_source_recipe_revision r USING (candidate_id, case_number, recipe_id);

CREATE VIEW case_source_recipe_ingredient AS
SELECT r.candidate_id, i.ordinal, m.meaning_id,
       i.raw_id, i.raw_name, i.raw_grams, i.grams,
       i.archive_sha256, i.case_number, i.sheet_name, i.row_number
FROM case_source_recipe_revision r
JOIN case_source_ingredient i USING (archive_sha256, case_number, sheet_name, row_number)
JOIN case_source_ingredient_meaning m
    ON m.source_id = btrim(i.raw_id) AND m.source_name = btrim(i.raw_name);

ALTER VIEW engine_candidate RENAME TO engine_candidate_existing;
CREATE VIEW engine_candidate AS
SELECT * FROM engine_candidate_existing
UNION ALL
SELECT candidate_id, fields->>'Recipe_Name', fields->>'Age_Group',
       (fields->>'Min_Age_Months')::integer, (fields->>'Max_Age_Months')::integer,
       fields->>'Meal_Type', fields->>'Texture',
       CASE WHEN lower(fields->>'Diet_Type') = 'non-vegetarian' THEN 'Non-vegetarian'
            ELSE fields->>'Diet_Type' END,
       fields->>'Allergen_Tags', fields->>'Region_Culture', fields->>'Budget_Band',
       NULL::numeric, (fields->>'Prep_Time_Min')::integer, (fields->>'Cook_Time_Min')::integer,
       fields->>'Clinical_Tag', fields->>'Review_Status', 'case_source'::text
FROM case_source_active_recipe;

ALTER VIEW engine_candidate_ingredient RENAME TO engine_candidate_ingredient_existing;
CREATE VIEW engine_candidate_ingredient AS
SELECT * FROM engine_candidate_ingredient_existing
UNION ALL
SELECT i.candidate_id, i.meaning_id, i.grams, NULL::text, NULL::text
FROM case_source_recipe_ingredient i
JOIN case_source_recipe_current c ON c.candidate_id = i.candidate_id;

COMMENT ON TABLE case_source_recipe_revision IS
    'Immutable source projection. Activation is collection membership, not clinical clearance. All original fields remain verbatim; estimates are not verified nutrition.';
COMMENT ON TABLE case_source_ingredient_meaning IS
    'Exact trimmed source ID and name pair, evidenced by original ordinal occurrences. No cross-corpus ingredient, nutrient or allergen equivalence is implied.';
