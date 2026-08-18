package book

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/profile"
)

// blockTemplate maps a Book 1 content block to the provider's template id from
// MadamGY_PDF_Template_Contract_V1.json. Hand-written, because the workbook carries no
// template column and guessing from a block's title would put clinical content in the wrong
// visual treatment -- B1-RED-01 is the high-contrast warning panel, and a red-flag block
// rendered as an ordinary table would lose exactly the emphasis it needs.
//
// A block with no entry is not rendered, and AssembleBook1 reports it. That is the honest
// gap: an unmapped block is one nobody has decided the presentation for, and inventing a
// layout for clinical content is the same class of error as inventing its text.
var blockTemplate = map[string]string{
	"B1-001": "B1-PROFILE-01",
	"B1-009": "B1-VAX-01",
	"B1-011": "B1-DEV-01",
	"B1-012": "B1-RED-01",
	"B1-014": "B1-DEV-01",
	"B1-022": "B1-END-01",
}

// AssembleBook1 builds the Book 1 document for one child as of a given date.
//
// The second return names every block that was skipped and why, so a reviewer sees what the
// book does not contain rather than assuming the absence is deliberate.
func AssembleBook1(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time) (Book1, []string, error) {
	cp, dropped, err := s.ToChildProfile(asOf)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: derive engine input: %w", err)
	}

	b := Book1{
		Metadata: Metadata{
			Title:          "My Child's Growth, Nutrition & Development Companion",
			BookVersion:    "V1",
			GenerationDate: asOf,
			Language:       "en",
			ReviewStatus:   "Draft - Culinary/Nutrition/Clinical Review Required",
		},
		Child: ChildSummary{
			DisplayName: s.DisplayName,
			AgeMonths:   cp.AgeMonths,
			AgeLabel:    ageLabel(cp.AgeMonths),
			// "No known food allergy reported" is the prototype's own wording for the
			// empty case. An empty string here would render a blank cell that reads as an
			// unanswered question rather than a recorded negative.
			AllergyStatus: allergyStatus(cp.Allergens),
		},
	}

	// Most recent measurement only, formatted as recorded. Growth[0] is newest: profile.Load
	// orders them that way.
	if len(s.Growth) > 0 {
		g := s.Growth[0]
		b.Child.MeasuredOn = g.MeasuredOn.UTC().Format("2006-01-02")
		if g.WeightKg != nil {
			v := fmt.Sprintf("%.1f kg", *g.WeightKg)
			b.Child.WeightKg = &v
		}
		if g.HeightCm != nil {
			v := fmt.Sprintf("%.0f cm", *g.HeightCm)
			b.Child.HeightCm = &v
		}
	}

	rows, err := pool.Query(ctx, `
		SELECT block_id, book_order, coalesce(section, ''), coalesce(subsection, ''),
		       coalesce(table_or_format, ''), ai_can_draft
		FROM book1_content_block
		WHERE (age_from_mo IS NULL OR age_from_mo <= $1)
		  AND (age_to_mo IS NULL OR age_to_mo >= $1)
		ORDER BY book_order`, cp.AgeMonths)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load blocks: %w", err)
	}
	defer rows.Close()

	skipped := append([]string{}, dropped...)
	for rows.Next() {
		var blockID, section, subsection, format, aiCanDraft string
		var order int
		if err := rows.Scan(&blockID, &order, &section, &subsection, &format, &aiCanDraft); err != nil {
			return Book1{}, nil, fmt.Errorf("book: scan block: %w", err)
		}
		tmpl, ok := blockTemplate[blockID]
		if !ok {
			skipped = append(skipped, fmt.Sprintf(
				"block %s (%s) has no template mapping and was not rendered", blockID, section))
			continue
		}
		b.Sections = append(b.Sections, Section{
			BlockID: blockID, TemplateID: tmpl, BookOrder: order,
			Title: section, Subtitle: subsection,
		})
	}
	if err := rows.Err(); err != nil {
		return Book1{}, nil, fmt.Errorf("book: block rows: %w", err)
	}

	return b, skipped, nil
}

func ageLabel(months int) string {
	if months < 24 {
		return fmt.Sprintf("%d months", months)
	}
	y, m := months/12, months%12
	if m == 0 {
		return fmt.Sprintf("%d years", y)
	}
	return fmt.Sprintf("%d years %d months", y, m)
}

func allergyStatus(groups []string) string {
	if len(groups) == 0 {
		return "No known food allergy reported"
	}
	out := "Declared: "
	for i, g := range groups {
		if i > 0 {
			out += ", "
		}
		out += g
	}
	return out
}
