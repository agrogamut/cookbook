// Package importer loads the provider workbooks into Postgres.
//
// Two properties matter more than speed:
//
//   - Idempotent. Running the importer twice over unchanged workbooks must leave an
//     identical database. Rows are upserted on their primary key and rows no longer
//     present in the workbook are deleted, so there is no drift and no duplicate.
//   - Non-destructive. Provider columns are written verbatim, including Review_Status,
//     Data_Quality and Validation_Required. Nothing is normalised, corrected, scored or
//     marked approved on the way in. Cleaning happens in the annotation layer, in its
//     own tables, where it is visible.
//
// A blank cell becomes NULL. It is never replaced with a default, a zero or a
// plausible-looking value.
package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/madamgy/recipie/internal/xlsx"
)

// Result is what one table's import did.
type Result struct {
	Table           string
	SourceFile      string
	SourceSheet     string
	HeaderRow       int
	RowsRead        int
	RowsWritten     int
	RowsSkipped     int // out of project scope; see region_focus
	RowsDeleted     int
	ContentHash     string
	UnmappedHeaders []string // workbook columns with no matching table column
}

type column struct {
	name     string
	dataType string
	nullable bool
}

// Run imports every spec inside a single transaction. Either the whole workbook set
// lands or none of it does; a half-imported database is worse than no database.
func Run(ctx context.Context, pool *pgxpool.Pool, xlsxDir string) ([]Result, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("importer: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	var runID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO import_run (source_dir) VALUES ($1) RETURNING run_id`, xlsxDir,
	).Scan(&runID); err != nil {
		return nil, fmt.Errorf("importer: open run: %w", err)
	}

	scope, err := inScopeRegions(ctx, tx)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(Specs))
	keys := make(map[string][]string, len(Specs))

	// Pass 1: upsert parent-first so foreign keys always resolve.
	for _, spec := range Specs {
		res, pks, err := upsertOne(ctx, tx, xlsxDir, spec, scope)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
		keys[spec.Table] = pks
	}

	// Pass 2: sweep rows that are no longer in the workbooks, children first.
	for i := len(Specs) - 1; i >= 0; i-- {
		spec := Specs[i]
		n, err := deleteMissing(ctx, tx, spec, keys[spec.Table])
		if err != nil {
			return nil, err
		}
		for j := range results {
			if results[j].Table == spec.Table {
				results[j].RowsDeleted = n
			}
		}
	}

	for _, r := range results {
		if _, err := tx.Exec(ctx,
			`INSERT INTO import_table_stat
			     (run_id, table_name, source_file, source_sheet, header_row,
			      rows_read, rows_written, rows_skipped, content_hash)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			runID, r.Table, r.SourceFile, r.SourceSheet, r.HeaderRow,
			r.RowsRead, r.RowsWritten, r.RowsSkipped, r.ContentHash,
		); err != nil {
			return nil, fmt.Errorf("importer: record stat for %s: %w", r.Table, err)
		}
	}

	if err := measureGaps(ctx, tx); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE import_run SET finished_at = now(), ok = true WHERE run_id = $1`, runID,
	); err != nil {
		return nil, fmt.Errorf("importer: close run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("importer: commit: %w", err)
	}
	return results, nil
}

// inScopeRegions reads the project scope from region_focus. A region absent from that
// table is not imported, so the scope lives in one reviewable seed rather than being
// scattered through the loader as hard-coded region names.
func inScopeRegions(ctx context.Context, tx pgx.Tx) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT region_culture FROM region_focus`)
	if err != nil {
		return nil, fmt.Errorf("importer: read region scope: %w", err)
	}
	defer rows.Close()

	scope := map[string]bool{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, fmt.Errorf("importer: read region scope: %w", err)
		}
		scope[r] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("importer: read region scope: %w", err)
	}
	if len(scope) == 0 {
		return nil, fmt.Errorf("importer: region_focus is empty; every row would be out of scope")
	}
	return scope, nil
}

func upsertOne(ctx context.Context, tx pgx.Tx, xlsxDir string, spec TableSpec, scope map[string]bool) (Result, []string, error) {
	res := Result{
		Table: spec.Table, SourceFile: spec.File, SourceSheet: spec.Sheet, HeaderRow: spec.HeaderRow,
	}

	sheet, err := xlsx.Load(filepath.Join(xlsxDir, spec.File), spec.Sheet, spec.HeaderRow)
	if err != nil {
		return res, nil, fmt.Errorf("importer: %s: %w", spec.Table, err)
	}
	if len(sheet.Columns) == 0 || sheet.Columns[0] != spec.FirstCol {
		got := "<none>"
		if len(sheet.Columns) > 0 {
			got = sheet.Columns[0]
		}
		return res, nil, fmt.Errorf(
			"importer: %s: expected first header %q on row %d of sheet %q but found %q; "+
				"the provider may have re-laid out the workbook",
			spec.Table, spec.FirstCol, spec.HeaderRow, spec.Sheet, got)
	}
	res.RowsRead = len(sheet.Rows)

	cols, err := tableColumns(ctx, tx, spec.Table)
	if err != nil {
		return res, nil, err
	}

	// Match workbook headers to table columns. Anything unmatched in either direction
	// is surfaced: a dropped column is a silent data loss and this project does not
	// allow silent anything.
	type binding struct {
		col      column
		sheetIdx int
	}
	var bound []binding
	seen := map[string]bool{}
	for i, h := range sheet.Columns {
		c, ok := cols[h]
		if !ok {
			res.UnmappedHeaders = append(res.UnmappedHeaders, h)
			continue
		}
		if seen[h] {
			return res, nil, fmt.Errorf("importer: %s: duplicate header %q in sheet %q", spec.Table, h, spec.Sheet)
		}
		seen[h] = true
		bound = append(bound, binding{col: c, sheetIdx: i})
	}
	for name, c := range cols {
		if !c.nullable && !seen[name] {
			return res, nil, fmt.Errorf(
				"importer: %s: column %q is NOT NULL but sheet %q has no matching header",
				spec.Table, name, spec.Sheet)
		}
	}
	sort.Slice(bound, func(i, j int) bool { return bound[i].col.name < bound[j].col.name })

	names := make([]string, len(bound))
	placeholders := make([]string, len(bound))
	updates := make([]string, 0, len(bound))
	pkSet := map[string]bool{}
	for _, k := range spec.PrimaryKey {
		pkSet[k] = true
	}
	for i, b := range bound {
		names[i] = pgx.Identifier{b.col.name}.Sanitize()
		placeholders[i] = "$" + strconv.Itoa(i+1)
		if !pkSet[b.col.name] {
			updates = append(updates, fmt.Sprintf("%s = EXCLUDED.%s", names[i], names[i]))
		}
	}
	conflict := make([]string, len(spec.PrimaryKey))
	for i, k := range spec.PrimaryKey {
		conflict[i] = pgx.Identifier{k}.Sanitize()
	}

	action := "DO NOTHING"
	if len(updates) > 0 {
		action = "DO UPDATE SET " + strings.Join(updates, ", ")
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) %s",
		pgx.Identifier{spec.Table}.Sanitize(),
		strings.Join(names, ", "),
		strings.Join(placeholders, ", "),
		strings.Join(conflict, ", "),
		action)

	pkIdx := make([]int, len(spec.PrimaryKey))
	for i, k := range spec.PrimaryKey {
		pkIdx[i] = sheet.Index(k)
		if pkIdx[i] < 0 {
			return res, nil, fmt.Errorf("importer: %s: primary key column %q not found in sheet %q", spec.Table, k, spec.Sheet)
		}
	}

	scopeIdx := -1
	if spec.ScopeColumn != "" {
		scopeIdx = sheet.Index(spec.ScopeColumn)
		if scopeIdx < 0 {
			return res, nil, fmt.Errorf("importer: %s: scope column %q not found in sheet %q",
				spec.Table, spec.ScopeColumn, spec.Sheet)
		}
	}

	batch := &pgx.Batch{}
	pks := make([]string, 0, len(sheet.Rows))
	hashLines := make([]string, 0, len(sheet.Rows))

	for rowNo, row := range sheet.Rows {
		if scopeIdx >= 0 && !scope[strings.TrimSpace(row[scopeIdx])] {
			res.RowsSkipped++
			continue
		}
		args := make([]any, len(bound))
		parts := make([]string, len(bound))
		for i, b := range bound {
			v, err := convert(row[b.sheetIdx], b.col)
			if err != nil {
				return res, nil, fmt.Errorf("importer: %s row %d column %q: %w",
					spec.Table, rowNo+spec.HeaderRow+1, b.col.name, err)
			}
			args[i] = v
			parts[i] = b.col.name + "=" + fmt.Sprint(v)
		}
		key := make([]string, len(pkIdx))
		for i, idx := range pkIdx {
			key[i] = row[idx]
		}
		pks = append(pks, strings.Join(key, "\x1f"))
		hashLines = append(hashLines, strings.Join(parts, "\x1e"))
		batch.Queue(stmt, args...)
	}

	br := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			br.Close() //nolint:errcheck // the batch error is the one that matters
			return res, nil, fmt.Errorf("importer: %s row %d: %w", spec.Table, i+spec.HeaderRow+1, err)
		}
	}
	if err := br.Close(); err != nil {
		return res, nil, fmt.Errorf("importer: %s: close batch: %w", spec.Table, err)
	}
	res.RowsWritten = len(pks)

	// Hash the sorted, normalised row values so the same workbooks always yield the
	// same digest regardless of sheet row order.
	sort.Strings(hashLines)
	sum := sha256.Sum256([]byte(strings.Join(hashLines, "\n")))
	res.ContentHash = hex.EncodeToString(sum[:])

	return res, pks, nil
}

func deleteMissing(ctx context.Context, tx pgx.Tx, spec TableSpec, pks []string) (int, error) {
	if len(spec.PrimaryKey) != 1 {
		return 0, fmt.Errorf("importer: %s: composite primary keys are not supported by the sweep", spec.Table)
	}
	tag, err := tx.Exec(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE %s::text <> ALL($1::text[])",
		pgx.Identifier{spec.Table}.Sanitize(),
		pgx.Identifier{spec.PrimaryKey[0]}.Sanitize()), pks)
	if err != nil {
		return 0, fmt.Errorf("importer: %s: sweep removed rows: %w", spec.Table, err)
	}
	return int(tag.RowsAffected()), nil
}

func tableColumns(ctx context.Context, tx pgx.Tx, table string) (map[string]column, error) {
	rows, err := tx.Query(ctx, `
		SELECT column_name, data_type, is_nullable = 'YES'
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, fmt.Errorf("importer: describe %s: %w", table, err)
	}
	defer rows.Close()

	out := map[string]column{}
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.dataType, &c.nullable); err != nil {
			return nil, fmt.Errorf("importer: describe %s: %w", table, err)
		}
		out[c.name] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("importer: describe %s: %w", table, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("importer: table %s does not exist; run migrations first", table)
	}
	return out, nil
}

// convert turns a raw cell string into a typed value. An empty cell is NULL, never a
// zero or a default: a missing number and a number that happens to be zero are
// different facts and the schema keeps them different.
func convert(raw string, c column) (any, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	switch c.dataType {
	case "integer", "bigint", "smallint":
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, nil
		}
		// Excel stores whole numbers as floats often enough to be worth handling,
		// but only when the value really is whole.
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("value %q is not an integer", s)
		}
		if f != math.Trunc(f) {
			return nil, fmt.Errorf("value %q has a fractional part but the column is an integer", s)
		}
		return int64(f), nil
	case "numeric", "real", "double precision":
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("value %q is not a number", s)
		}
		return f, nil
	case "boolean":
		switch strings.ToUpper(s) {
		case "Y", "YES", "TRUE", "1":
			return true, nil
		case "N", "NO", "FALSE", "0":
			return false, nil
		}
		return nil, fmt.Errorf("value %q is not a boolean", s)
	default:
		return s, nil
	}
}
