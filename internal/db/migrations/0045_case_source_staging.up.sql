CREATE TABLE case_source_archive (
    archive_sha256 text PRIMARY KEY CHECK (archive_sha256 ~ '^[0-9a-f]{64}$'),
    source_key text NOT NULL CHECK (source_key = 'madamgy-wb-cases'),
    parser_version text NOT NULL,
    archive_bytes bytea NOT NULL,
    imported_at timestamptz NOT NULL DEFAULT now(),
    CHECK (encode(sha256(archive_bytes), 'hex') = archive_sha256)
);

CREATE TABLE case_source_workbook (
    archive_sha256 text NOT NULL REFERENCES case_source_archive,
    case_number integer NOT NULL CHECK (case_number BETWEEN 1 AND 99),
    member_name text NOT NULL,
    member_sha256 text NOT NULL CHECK (member_sha256 ~ '^[0-9a-f]{64}$'),
    PRIMARY KEY (archive_sha256, case_number),
    UNIQUE (archive_sha256, member_name)
);

CREATE TABLE case_source_sheet (
    archive_sha256 text NOT NULL,
    case_number integer NOT NULL,
    sheet_name text NOT NULL CHECK (sheet_name IN ('Recipe_Master', 'Uniqueness_Audit')),
    xml_part text NOT NULL,
    header_row integer NOT NULL CHECK (header_row = 4),
    raw_headers jsonb NOT NULL CHECK (jsonb_typeof(raw_headers) = 'array'),
    PRIMARY KEY (archive_sha256, case_number, sheet_name),
    FOREIGN KEY (archive_sha256, case_number)
        REFERENCES case_source_workbook
);

CREATE TABLE case_source_row (
    archive_sha256 text NOT NULL,
    case_number integer NOT NULL,
    sheet_name text NOT NULL,
    row_number integer NOT NULL CHECK (row_number > 0),
    recipe_id text,
    raw_cells jsonb NOT NULL CHECK (jsonb_typeof(raw_cells) = 'array'),
    row_sha256 text NOT NULL CHECK (row_sha256 ~ '^[0-9a-f]{64}$'),
    PRIMARY KEY (archive_sha256, case_number, sheet_name, row_number),
    UNIQUE (archive_sha256, case_number, sheet_name, recipe_id),
    FOREIGN KEY (archive_sha256, case_number, sheet_name)
        REFERENCES case_source_sheet
);

CREATE INDEX case_source_row_logical_recipe
    ON case_source_row (case_number, recipe_id, archive_sha256)
    WHERE recipe_id IS NOT NULL;

CREATE TABLE case_source_ingredient (
    archive_sha256 text NOT NULL,
    case_number integer NOT NULL,
    sheet_name text NOT NULL DEFAULT 'Recipe_Master' CHECK (sheet_name = 'Recipe_Master'),
    row_number integer NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal > 0),
    raw_id text NOT NULL,
    raw_name text NOT NULL,
    raw_grams text NOT NULL,
    grams numeric NOT NULL CHECK (grams > 0 AND grams <> 'NaN'::numeric),
    PRIMARY KEY (archive_sha256, case_number, sheet_name, row_number, ordinal),
    FOREIGN KEY (archive_sha256, case_number, sheet_name, row_number)
        REFERENCES case_source_row,
    CHECK (grams = btrim(raw_grams)::numeric)
);

CREATE FUNCTION reject_case_source_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'case source snapshots are immutable; import corrected bytes as a new revision';
END;
$$;

CREATE TRIGGER case_source_archive_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_archive
    FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_workbook_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_workbook
    FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_sheet_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_sheet
    FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_row_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_row
    FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();
CREATE TRIGGER case_source_ingredient_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON case_source_ingredient
    FOR EACH STATEMENT EXECUTE FUNCTION reject_case_source_mutation();

COMMENT ON TABLE case_source_archive IS
    'Raw source staging only. Archive bytes are retained independently of attachment paths. No approval or candidate eligibility is implied.';
COMMENT ON COLUMN case_source_row.row_sha256 IS
    'SHA-256 of the ordered Cell JSON encoded by the recorded parser version. Archive/member hashes identify original source bytes.';
COMMENT ON TABLE case_source_ingredient IS
    'Positional source occurrences, never canonical ingredient identities. Raw tokens retain semicolon-list whitespace; grams is their exact decimal parse.';
