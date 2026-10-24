package casesource

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const CandidateSource = "case_source"

type Activation struct {
	ArchiveSHA256  string
	RevisionsAdded int64
	CurrentChanged int64
}

func CandidateID(archive, recipe string) string { return "case:" + archive + ":" + recipe }

func MeaningID(id, name string) string {
	b, _ := json.Marshal([]string{SourceKey, strings.Trim(id, " "), strings.Trim(name, " ")})
	return "case-ingredient:" + hash(b)
}

func Fields(s Sheet, r Row) map[string]string {
	values := rowValues(r)
	out := make(map[string]string, len(s.Headers))
	for i, header := range s.Headers {
		if i < len(values) {
			out[header] = values[i]
		} else {
			out[header] = ""
		}
	}
	return out
}

func Activate(ctx context.Context, pool *pgxpool.Pool, archiveSHA256 string) (Activation, error) {
	result := Activation{ArchiveSHA256: archiveSHA256}
	var data []byte
	if err := pool.QueryRow(ctx, `SELECT archive_bytes FROM case_source_archive WHERE archive_sha256=$1`, archiveSHA256).Scan(&data); err != nil {
		return result, fmt.Errorf("read retained source for activation: %w", err)
	}
	a, err := Parse(ctx, data)
	if err != nil {
		return result, fmt.Errorf("validate retained source: %w", err)
	}
	if a.SHA256 != archiveSHA256 {
		return result, fmt.Errorf("retained archive hash mismatch")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin activation: %w", err)
	}
	defer tx.Rollback(ctx)
	// Serialize current-pointer updates across overlapping snapshots.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(460060)`); err != nil {
		return result, fmt.Errorf("lock activation: %w", err)
	}
	meanings := make(map[string]bool)
	for _, w := range a.Workbooks {
		for _, s := range w.Sheets {
			if s.Name != "Recipe_Master" {
				continue
			}
			for _, r := range s.Rows {
				if r.RecipeID == "" {
					continue
				}
				fields := Fields(s, r)
				for _, key := range []string{"Min_Age_Months", "Max_Age_Months", "Prep_Time_Min", "Cook_Time_Min"} {
					n, err := strconv.Atoi(fields[key])
					if err != nil || n < 0 {
						return result, fmt.Errorf("case %d row %d: invalid %s", w.Case, r.Number, key)
					}
				}
				if !decimal.MatchString(fields["Serving_Size_g"]) {
					return result, fmt.Errorf("case %d row %d: invalid serving decimal", w.Case, r.Number)
				}
				encoded, err := json.Marshal(fields)
				if err != nil {
					return result, fmt.Errorf("encode recipe fields: %w", err)
				}
				id := CandidateID(a.SHA256, r.RecipeID)
				insert, err := tx.Exec(ctx, `INSERT INTO case_source_recipe_revision
				    (candidate_id,archive_sha256,case_number,row_number,recipe_id,fields,serving_grams)
				    VALUES ($1,$2,$3,$4,$5,$6,$7::numeric) ON CONFLICT (candidate_id) DO NOTHING`,
					id, a.SHA256, w.Case, r.Number, r.RecipeID, string(encoded), fields["Serving_Size_g"])
				if err != nil {
					return result, fmt.Errorf("activate %s: %w", r.RecipeID, err)
				}
				result.RevisionsAdded += insert.RowsAffected()
				for _, ingredient := range r.Ingredients {
					meaning := MeaningID(ingredient.RawID, ingredient.RawName)
					if meanings[meaning] {
						continue
					}
					_, err := tx.Exec(ctx, `INSERT INTO case_source_ingredient_meaning (meaning_id,source_id,source_name)
					    VALUES ($1,$2,$3) ON CONFLICT (meaning_id) DO NOTHING`, meaning, strings.Trim(ingredient.RawID, " "), strings.Trim(ingredient.RawName, " "))
					if err != nil {
						return result, fmt.Errorf("store ingredient meaning: %w", err)
					}
					meanings[meaning] = true
				}
				updated, err := tx.Exec(ctx, `INSERT INTO case_source_recipe_current (case_number,recipe_id,candidate_id) VALUES ($1,$2,$3)
				    ON CONFLICT (case_number,recipe_id) DO UPDATE SET candidate_id=excluded.candidate_id
				    WHERE case_source_recipe_current.candidate_id IS DISTINCT FROM excluded.candidate_id`, w.Case, r.RecipeID, id)
				if err != nil {
					return result, fmt.Errorf("select current revision: %w", err)
				}
				result.CurrentChanged += updated.RowsAffected()
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit activation: %w", err)
	}
	return result, nil
}
