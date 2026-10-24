package casesource

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Citation struct {
	ArchiveSHA256 string `json:"archive_sha256"`
	MemberSHA256  string `json:"member_sha256"`
	Workbook      string `json:"workbook"`
	Case          int    `json:"case"`
	Sheet         string `json:"sheet"`
	Row           int    `json:"row"`
	RecipeID      string `json:"recipe_id"`
}

type Occurrence struct {
	Ordinal   int    `json:"ordinal"`
	MeaningID string `json:"meaning_id"`
	RawID     string `json:"raw_id"`
	RawName   string `json:"raw_name"`
	RawGrams  string `json:"raw_grams"`
	Grams     string `json:"grams"`
}

type Recipe struct {
	CandidateID  string            `json:"candidate_id"`
	Citation     Citation          `json:"source"`
	Fields       map[string]string `json:"fields"`
	ServingGrams string            `json:"serving_grams"`
	Ingredients  []Occurrence      `json:"ingredients,omitempty"`
}

func IsCandidate(id string) bool { return strings.HasPrefix(id, "case:") }

// Revisions remain readable after a corrected snapshot changes collection membership.
func Recipes(ctx context.Context, pool *pgxpool.Pool, ids []string) ([]Recipe, error) {
	rows, err := pool.Query(ctx, `SELECT r.candidate_id,r.archive_sha256,w.member_sha256,w.member_name,
	    r.case_number,r.sheet_name,r.row_number,r.recipe_id,r.fields,r.serving_grams::text
	    FROM case_source_recipe_revision r JOIN case_source_workbook w USING (archive_sha256,case_number)
	    WHERE r.candidate_id=ANY($1) ORDER BY r.case_number,r.recipe_id`, ids)
	if err != nil {
		return nil, fmt.Errorf("read case revisions: %w", err)
	}
	defer rows.Close()
	var out []Recipe
	for rows.Next() {
		var r Recipe
		if err := rows.Scan(&r.CandidateID, &r.Citation.ArchiveSHA256, &r.Citation.MemberSHA256, &r.Citation.Workbook,
			&r.Citation.Case, &r.Citation.Sheet, &r.Citation.Row, &r.Citation.RecipeID, &r.Fields, &r.ServingGrams); err != nil {
			return nil, fmt.Errorf("scan case revision: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read case revision rows: %w", err)
	}
	return out, nil
}

func Ingredients(ctx context.Context, pool *pgxpool.Pool, id string) ([]Occurrence, error) {
	rows, err := pool.Query(ctx, `SELECT ordinal,meaning_id,raw_id,raw_name,raw_grams,grams::text
	    FROM case_source_recipe_ingredient WHERE candidate_id=$1 ORDER BY ordinal`, id)
	if err != nil {
		return nil, fmt.Errorf("read case ingredient occurrences: %w", err)
	}
	defer rows.Close()
	var out []Occurrence
	for rows.Next() {
		var i Occurrence
		if err := rows.Scan(&i.Ordinal, &i.MeaningID, &i.RawID, &i.RawName, &i.RawGrams, &i.Grams); err != nil {
			return nil, fmt.Errorf("scan case ingredient occurrence: %w", err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read case ingredient rows: %w", err)
	}
	return out, nil
}
