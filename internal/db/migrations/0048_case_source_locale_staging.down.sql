DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM case_source_locale_validation) THEN
        RAISE EXCEPTION 'Cannot remove populated immutable locale staging; retain migration 0048 and its source history';
    END IF;
END;
$$;
DROP TABLE case_source_locale_glossary;
DROP TABLE case_source_locale_text;
DROP TABLE case_source_locale_raw_row;
DROP TABLE case_source_locale_sheet;
DROP TABLE case_source_locale_validation;
