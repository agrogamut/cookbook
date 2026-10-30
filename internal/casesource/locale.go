package casesource

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LocaleText struct {
	Field         string `json:"field"`
	Ordinal       int    `json:"ingredient_ordinal"`
	English       string `json:"english"`
	EnglishSHA256 string `json:"english_sha256"`
	Text          string `json:"text"`
	Row           int    `json:"translation_row"`
	Quantity      string `json:"raw_quantity"`
	Presentation  string `json:"presentation,omitempty"`
}

func (t LocaleText) Presented() string {
	if t.Presentation != "" {
		return t.Presentation
	}
	return t.Text
}

type Locale struct {
	Language string       `json:"language"`
	Source   Citation     `json:"source"`
	Sheet    string       `json:"translation_sheet"`
	Texts    []LocaleText `json:"texts"`
}

func Bengali(ctx context.Context, pool *pgxpool.Pool, r Recipe) (*Locale, error) {
	if !AcceptedArchive(r.Citation.ArchiveSHA256) {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `SELECT l.field,l.ingredient_ordinal,l.english_text,l.english_sha256,l.bengali_text,l.row_number,l.raw_quantity
	    FROM case_source_locale_text l JOIN case_source_recipe_revision r
	      ON r.archive_sha256=l.archive_sha256 AND r.case_number=l.case_number AND r.recipe_id=l.recipe_id AND r.row_number=l.master_row
	    JOIN case_source_recipe_current c USING(candidate_id)
	    WHERE r.candidate_id=$1 ORDER BY l.row_number`, r.CandidateID)
	if err != nil {
		return nil, fmt.Errorf("read revision locale: %w", err)
	}
	defer rows.Close()
	out := &Locale{Language: "bn", Source: r.Citation, Sheet: "For_translation"}
	for rows.Next() {
		var t LocaleText
		if err := rows.Scan(&t.Field, &t.Ordinal, &t.English, &t.EnglishSHA256, &t.Text, &t.Row, &t.Quantity); err != nil {
			return nil, fmt.Errorf("scan revision locale: %w", err)
		}
		out.Texts = append(out.Texts, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Texts) == 0 {
		return nil, nil
	}
	if err := out.Validate(r); err != nil {
		return nil, err
	}
	for i := range out.Texts {
		t := &out.Texts[i]
		if t.Field == "method" {
			if text := r.PresentationText("Preparation_Method_FULL", "bn", t.Text); text != t.Text {
				t.Presentation = text
			}
		}
	}
	return out, nil
}

func (l Locale) Validate(r Recipe) error {
	if l.Language != "bn" || !AcceptedArchive(r.Citation.ArchiveSHA256) || l.Source != r.Citation ||
		r.CandidateID != CandidateID(r.Citation.ArchiveSHA256, r.Citation.RecipeID) {
		return fmt.Errorf("locale revision identity differs")
	}
	names := strings.Split(r.Fields["Ingredient_Names"], ";")
	quantities := strings.Split(r.Fields["Ingredient_Quantities_g"], ";")
	seen := map[string]int{}
	for _, t := range l.Texts {
		seen[t.Field]++
		want := ""
		switch t.Field {
		case "title":
			want = r.Fields["Recipe_Name"]
		case "method":
			want = r.Fields["Preparation_Method_FULL"]
		case "safety":
			want = r.Fields["Safety_Rule"]
		case "ingredient":
			if t.Ordinal != seen[t.Field] || t.Ordinal > len(names) || t.Ordinal > len(quantities) {
				return fmt.Errorf("locale ingredient ordinal differs")
			}
			want = strings.TrimSpace(names[t.Ordinal-1])
			if strings.TrimSpace(t.Quantity) != strings.TrimSpace(quantities[t.Ordinal-1]) {
				return fmt.Errorf("locale quantity differs")
			}
		default:
			return fmt.Errorf("unknown locale field %q", t.Field)
		}
		if t.Field != "ingredient" && (t.Ordinal != 0 || seen[t.Field] != 1) {
			return fmt.Errorf("locale field identity differs")
		}
		if want == "" || t.English != want || t.EnglishSHA256 != hash([]byte(want)) || strings.TrimSpace(t.Text) == "" {
			return fmt.Errorf("locale source pair differs for %s/%s/%d", r.Citation.RecipeID, t.Field, t.Ordinal)
		}
		if t.Presentation != "" && (t.Field != "method" || t.Presentation != r.PresentationText("Preparation_Method_FULL", "bn", t.Text)) {
			return fmt.Errorf("locale presentation differs from scoped amendment")
		}
	}
	if seen["title"] != 1 || seen["method"] != 1 || seen["safety"] != 1 || seen["ingredient"] != len(names) {
		return fmt.Errorf("incomplete active source locale")
	}
	return nil
}

func (r Recipe) SourceNumbers() map[string]*string {
	out := map[string]*string{}
	for _, key := range []string{"Energy_kcal", "Protein_g", "Carb_g", "Fat_g", "Fibre_g", "Iron_mg", "Calcium_mg", "Cost_per_Serving_INR"} {
		value := r.Fields[key]
		if strings.TrimSpace(value) == "" {
			out[key] = nil
		} else {
			out[key] = &value
		}
	}
	return out
}
