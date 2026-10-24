package casesource

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Result struct {
	ArchiveSHA256 string
	Inserted      bool
	Workbooks     int
	Recipes       int
	Audits        int
	Occurrences   int
}

func Import(ctx context.Context, pool *pgxpool.Pool, data []byte) (Result, error) {
	a, err := Parse(ctx, data)
	if err != nil {
		return Result{}, err
	}
	result := Result{ArchiveSHA256: a.SHA256, Workbooks: len(a.Workbooks)}
	for _, w := range a.Workbooks {
		for _, s := range w.Sheets {
			for _, r := range s.Rows {
				if r.RecipeID != "" {
					if s.Name == "Recipe_Master" {
						result.Recipes++
					} else {
						result.Audits++
					}
				}
				result.Occurrences += len(r.Ingredients)
			}
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin case intake: %w", err)
	}
	defer tx.Rollback(ctx) // Rollback after commit is a no-op.
	insert, err := tx.Exec(ctx, `INSERT INTO case_source_archive
	    (archive_sha256, source_key, parser_version, archive_bytes) VALUES ($1,$2,$3,$4)
	    ON CONFLICT (archive_sha256) DO NOTHING`, a.SHA256, SourceKey, ParserVersion, data)
	if err != nil {
		return Result{}, fmt.Errorf("store case archive: %w", err)
	}
	if insert.RowsAffected() == 0 {
		return result, nil
	}
	for _, w := range a.Workbooks {
		if _, err := tx.Exec(ctx, `INSERT INTO case_source_workbook
		    (archive_sha256, case_number, member_name, member_sha256) VALUES ($1,$2,$3,$4)`,
			a.SHA256, w.Case, w.Name, w.SHA256); err != nil {
			return Result{}, fmt.Errorf("store workbook %s: %w", w.Name, err)
		}
		for _, s := range w.Sheets {
			headers, err := json.Marshal(s.Headers)
			if err != nil {
				return Result{}, fmt.Errorf("encode headers: %w", err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO case_source_sheet
			    (archive_sha256, case_number, sheet_name, xml_part, header_row, raw_headers)
			    VALUES ($1,$2,$3,$4,4,$5)`, a.SHA256, w.Case, s.Name, s.Part, string(headers)); err != nil {
				return Result{}, fmt.Errorf("store sheet %s: %w", s.Name, err)
			}
			rows := make([][]any, 0, len(s.Rows))
			var occurrences [][]any
			for _, r := range s.Rows {
				cells, err := json.Marshal(r.Cells)
				if err != nil {
					return Result{}, fmt.Errorf("encode row %d: %w", r.Number, err)
				}
				var id any
				if r.RecipeID != "" {
					id = r.RecipeID
				}
				rows = append(rows, []any{a.SHA256, w.Case, s.Name, r.Number, id, cells, hash(cells)})
				for _, i := range r.Ingredients {
					var grams pgtype.Numeric
					if err := grams.Scan(i.Grams); err != nil {
						return Result{}, fmt.Errorf("parse decimal grams: %w", err)
					}
					occurrences = append(occurrences, []any{a.SHA256, w.Case, s.Name, r.Number, i.Ordinal, i.RawID, i.RawName, i.RawGrams, grams})
				}
			}
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{"case_source_row"},
				[]string{"archive_sha256", "case_number", "sheet_name", "row_number", "recipe_id", "raw_cells", "row_sha256"}, pgx.CopyFromRows(rows)); err != nil {
				return Result{}, fmt.Errorf("store source rows: %w", err)
			}
			if len(occurrences) > 0 {
				if _, err := tx.CopyFrom(ctx, pgx.Identifier{"case_source_ingredient"},
					[]string{"archive_sha256", "case_number", "sheet_name", "row_number", "ordinal", "raw_id", "raw_name", "raw_grams", "grams"}, pgx.CopyFromRows(occurrences)); err != nil {
					return Result{}, fmt.Errorf("store ingredient occurrences: %w", err)
				}
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit case intake: %w", err)
	}
	result.Inserted = true
	return result, nil
}
