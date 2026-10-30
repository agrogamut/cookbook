CREATE TABLE case_source_locale_validation (
    archive_sha256 text PRIMARY KEY REFERENCES case_source_archive,
    baseline_sha256 text NOT NULL REFERENCES case_source_archive,
    summary jsonb NOT NULL CHECK (jsonb_typeof(summary) = 'object'),
    CHECK (archive_sha256 <> baseline_sha256),
    CHECK (summary->>'parser_version' IS NOT DISTINCT FROM '2-bn-staging'),
    CHECK (summary->>'activation_blocked' IS NOT DISTINCT FROM 'true')
);
CREATE INDEX case_source_locale_validation_baseline ON case_source_locale_validation (baseline_sha256);

CREATE TABLE case_source_locale_sheet (
    archive_sha256 text NOT NULL REFERENCES case_source_locale_validation,
    case_number integer NOT NULL,
    sheet_name text NOT NULL CHECK (sheet_name IN ('Recipe_Master', 'Uniqueness_Audit', 'For_translation', 'Glossary')),
    xml_part text NOT NULL,
    header_row integer NOT NULL,
    raw_headers jsonb NOT NULL CHECK (jsonb_typeof(raw_headers) = 'array'),
    PRIMARY KEY (archive_sha256, case_number, sheet_name),
    FOREIGN KEY (archive_sha256, case_number) REFERENCES case_source_workbook,
    CHECK (header_row = CASE WHEN sheet_name IN ('For_translation', 'Glossary') THEN 2 ELSE 4 END)
);

CREATE TABLE case_source_locale_raw_row (
    archive_sha256 text NOT NULL,
    case_number integer NOT NULL,
    sheet_name text NOT NULL,
    row_number integer NOT NULL CHECK (row_number > 0),
    recipe_id text,
    raw_cells jsonb NOT NULL CHECK (jsonb_typeof(raw_cells) = 'array'),
    row_sha256 text NOT NULL CHECK (row_sha256 ~ '^[0-9a-f]{64}$'),
    findings jsonb NOT NULL CHECK (jsonb_typeof(findings) = 'array'),
    PRIMARY KEY (archive_sha256, case_number, sheet_name, row_number),
    UNIQUE (archive_sha256, case_number, sheet_name, row_number, recipe_id),
    FOREIGN KEY (archive_sha256, case_number, sheet_name) REFERENCES case_source_locale_sheet
);
CREATE INDEX case_source_locale_raw_recipe ON case_source_locale_raw_row (archive_sha256, case_number, recipe_id);

CREATE TABLE case_source_locale_text (
    archive_sha256 text NOT NULL,
    case_number integer NOT NULL,
    recipe_id text NOT NULL,
    field text NOT NULL CHECK (field IN ('title', 'ingredient', 'method', 'safety')),
    ingredient_ordinal integer NOT NULL CHECK (ingredient_ordinal >= 0),
    locale text NOT NULL DEFAULT 'bn' CHECK (locale = 'bn'),
    sheet_name text NOT NULL DEFAULT 'For_translation' CHECK (sheet_name = 'For_translation'),
    row_number integer NOT NULL,
    master_sheet text NOT NULL DEFAULT 'Recipe_Master' CHECK (master_sheet = 'Recipe_Master'),
    master_row integer NOT NULL,
    english_text text NOT NULL CHECK (btrim(english_text) <> ''),
    english_sha256 text NOT NULL CHECK (english_sha256 ~ '^[0-9a-f]{64}$'),
    bengali_text text NOT NULL CHECK (btrim(bengali_text) <> ''),
    raw_quantity text NOT NULL,
    source_note text NOT NULL,
    raw_ingredient_id text,
    raw_ingredient_name text,
    raw_ingredient_grams text,
    grams numeric,
    PRIMARY KEY (archive_sha256, case_number, recipe_id, field, ingredient_ordinal),
    FOREIGN KEY (archive_sha256, case_number, sheet_name, row_number, recipe_id) REFERENCES case_source_locale_raw_row (archive_sha256, case_number, sheet_name, row_number, recipe_id),
    FOREIGN KEY (archive_sha256, case_number, master_sheet, master_row, recipe_id) REFERENCES case_source_locale_raw_row (archive_sha256, case_number, sheet_name, row_number, recipe_id),
    CHECK ((field = 'ingredient' AND ingredient_ordinal > 0 AND raw_ingredient_id IS NOT NULL
        AND raw_ingredient_name IS NOT NULL AND raw_ingredient_grams IS NOT NULL
        AND grams IS NOT NULL AND grams > 0 AND grams <> 'NaN'::numeric
        AND grams = btrim(raw_ingredient_grams)::numeric AND grams = btrim(raw_quantity)::numeric)
        OR (field <> 'ingredient' AND ingredient_ordinal = 0 AND raw_quantity = ''
        AND raw_ingredient_id IS NULL AND raw_ingredient_name IS NULL AND raw_ingredient_grams IS NULL AND grams IS NULL))
);
CREATE INDEX case_source_locale_text_row ON case_source_locale_text (archive_sha256, case_number, sheet_name, row_number);
CREATE INDEX case_source_locale_text_master ON case_source_locale_text (archive_sha256, case_number, master_sheet, master_row);

CREATE TABLE case_source_locale_glossary (
    archive_sha256 text NOT NULL,
    case_number integer NOT NULL,
    sheet_name text NOT NULL DEFAULT 'Glossary' CHECK (sheet_name = 'Glossary'),
    row_number integer NOT NULL,
    english_text text NOT NULL CHECK (btrim(english_text) <> ''),
    print_site text NOT NULL CHECK (btrim(print_site) <> ''),
    bengali_text text NOT NULL CHECK (btrim(bengali_text) <> ''),
    PRIMARY KEY (archive_sha256, case_number, sheet_name, row_number),
    UNIQUE (archive_sha256, case_number, english_text, print_site),
    FOREIGN KEY (archive_sha256, case_number, sheet_name, row_number) REFERENCES case_source_locale_raw_row
);

CREATE TRIGGER case_source_locale_validation_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_locale_validation FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_locale_sheet_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_locale_sheet FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_locale_raw_row_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_locale_raw_row FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_locale_text_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_locale_text FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_locale_glossary_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_locale_glossary FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();

COMMENT ON TABLE case_source_locale_text IS 'Unreconciled source-paired Bengali text, not an active translation or canonical ingredient mapping. Exact original cell XML is retained in the archive.';
COMMENT ON TABLE case_source_locale_validation IS 'Comparison with a retained source archive, not clinical equivalence, approval or activation. No current-recipe pointers reference this staging.';
