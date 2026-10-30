package casesource

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IntakeResult struct {
	Inserted bool         `json:"inserted"`
	Report   IntakeReport `json:"report"`
}

func ImportIntake(ctx context.Context, pool *pgxpool.Pool, data, baseline []byte) (IntakeResult, error) {
	i, err := ParseIntake(ctx, data, baseline)
	if err != nil {
		return IntakeResult{}, err
	}
	result := IntakeResult{Report: i.Report}
	if i.Report.ParserVersion == ParserVersion {
		r, err := Import(ctx, pool, data)
		result.Inserted = r.Inserted
		return result, err
	}
	// Retain the comparison's exact bytes independently of any local attachment path.
	if _, err := Import(ctx, pool, baseline); err != nil {
		return result, fmt.Errorf("retain comparison source: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin locale staging: %w", err)
	}
	defer tx.Rollback(ctx)
	insert, err := tx.Exec(ctx, `INSERT INTO case_source_archive
	    (archive_sha256,source_key,parser_version,archive_bytes) VALUES ($1,$2,$3,$4)
	    ON CONFLICT (archive_sha256) DO NOTHING`, i.Archive.SHA256, SourceKey, LocaleParserVersion, data)
	if err != nil {
		return result, fmt.Errorf("retain locale archive: %w", err)
	}
	if insert.RowsAffected() == 0 {
		var previous string
		if err := tx.QueryRow(ctx, `SELECT baseline_sha256 FROM case_source_locale_validation WHERE archive_sha256=$1`, i.Archive.SHA256).Scan(&previous); err != nil {
			return result, fmt.Errorf("read existing locale validation: %w", err)
		}
		if previous != i.Baseline.SHA256 {
			return result, fmt.Errorf("existing locale revision has a different immutable comparison source")
		}
		return result, nil
	}
	summary := i.Report
	summary.Findings = nil
	encoded, err := json.Marshal(summary)
	if err != nil {
		return result, fmt.Errorf("encode locale validation: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO case_source_locale_validation (archive_sha256,baseline_sha256,summary) VALUES ($1,$2,$3)`, i.Archive.SHA256, i.Baseline.SHA256, string(encoded)); err != nil {
		return result, fmt.Errorf("store locale validation: %w", err)
	}
	type location struct {
		caseNumber int
		sheet      string
		row        int
	}
	findings := map[location][]Finding{}
	for _, f := range i.Report.Findings {
		if f.Row > 0 {
			key := location{f.Case, f.Sheet, f.Row}
			findings[key] = append(findings[key], f)
		}
	}
	for _, w := range i.Archive.Workbooks {
		if _, err := tx.Exec(ctx, `INSERT INTO case_source_workbook (archive_sha256,case_number,member_name,member_sha256) VALUES ($1,$2,$3,$4)`, i.Archive.SHA256, w.Case, w.Name, w.SHA256); err != nil {
			return result, fmt.Errorf("store locale workbook: %w", err)
		}
		for _, s := range w.Sheets {
			headers, err := json.Marshal(s.Headers)
			if err != nil {
				return result, fmt.Errorf("encode locale headers: %w", err)
			}
			headerRow := 4
			if s.Name == "For_translation" || s.Name == "Glossary" {
				headerRow = 2
			}
			if _, err := tx.Exec(ctx, `INSERT INTO case_source_locale_sheet (archive_sha256,case_number,sheet_name,xml_part,header_row,raw_headers) VALUES ($1,$2,$3,$4,$5,$6)`, i.Archive.SHA256, w.Case, s.Name, s.Part, headerRow, string(headers)); err != nil {
				return result, fmt.Errorf("store locale sheet: %w", err)
			}
			rows := make([][]any, 0, len(s.Rows))
			for _, r := range s.Rows {
				cells, err := json.Marshal(r.Cells)
				if err != nil {
					return result, fmt.Errorf("encode locale source row: %w", err)
				}
				issues := findings[location{w.Case, s.Name, r.Number}]
				if issues == nil {
					issues = []Finding{}
				}
				encoded, err := json.Marshal(issues)
				if err != nil {
					return result, fmt.Errorf("encode source findings: %w", err)
				}
				var id any
				if r.RecipeID != "" {
					id = r.RecipeID
				}
				rows = append(rows, []any{i.Archive.SHA256, w.Case, s.Name, r.Number, id, cells, hash(cells), encoded})
			}
			if err := intakeCopy(ctx, tx, "case_source_locale_raw_row", []string{"archive_sha256", "case_number", "sheet_name", "row_number", "recipe_id", "raw_cells", "row_sha256", "findings"}, rows); err != nil {
				return result, err
			}
		}
	}
	rows := make([][]any, 0, len(i.Locales))
	for _, r := range i.Locales {
		var rawID, rawName, rawGrams, grams any
		if r.Ingredient != nil {
			rawID, rawName, rawGrams = r.Ingredient.RawID, r.Ingredient.RawName, r.Ingredient.RawGrams
			var numeric pgtype.Numeric
			if err := numeric.Scan(r.Ingredient.Grams); err != nil {
				return result, fmt.Errorf("locale decimal: %w", err)
			}
			grams = numeric
		}
		rows = append(rows, []any{i.Archive.SHA256, r.Case, r.RecipeID, r.Field, r.Ordinal, r.Row, r.MasterRow, r.English, r.EnglishSHA256, r.Bengali, r.RawQuantity, r.Note, rawID, rawName, rawGrams, grams})
	}
	if err := intakeCopy(ctx, tx, "case_source_locale_text", []string{"archive_sha256", "case_number", "recipe_id", "field", "ingredient_ordinal", "row_number", "master_row", "english_text", "english_sha256", "bengali_text", "raw_quantity", "source_note", "raw_ingredient_id", "raw_ingredient_name", "raw_ingredient_grams", "grams"}, rows); err != nil {
		return result, err
	}
	rows = make([][]any, 0, len(i.Glossary))
	for _, r := range i.Glossary {
		rows = append(rows, []any{i.Archive.SHA256, r.Case, r.Row, r.English, r.Site, r.Bengali})
	}
	if err := intakeCopy(ctx, tx, "case_source_locale_glossary", []string{"archive_sha256", "case_number", "row_number", "english_text", "print_site", "bengali_text"}, rows); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit locale staging: %w", err)
	}
	result.Inserted = true
	return result, nil
}

func intakeCopy(ctx context.Context, tx pgx.Tx, table string, columns []string, rows [][]any) error {
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("store %s: %w", table, err)
	}
	return nil
}
