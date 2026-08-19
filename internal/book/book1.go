package book

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
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

// vaccineScheduleRow is one row of book1_vaccine_schedule, read verbatim. age_min_months is
// kept as the provider's own text: most rows are numeric, but the risk-based rows ("Varies",
// "Any") are not, and forcing them to a number here would be a guess this project has no
// basis for.
type vaccineScheduleRow struct {
	ScheduleID   string
	Age          string
	AgeMinMonths string
	Vaccine      string
	DoseOrEvent  string
}

// developmentMilestoneRow is one row of book1_development_milestone, read verbatim.
type developmentMilestoneRow struct {
	MilestoneID        string
	AgeReference       string
	AgeMonths          int
	Domain             string
	ReferenceMilestone string
	ConcernOrRedFlag   string
	ActionIfConcern    string
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

	// Loaded once, up front, and filtered per-block in Go below -- the same reason the block
	// query itself carries no WHERE on age: a row a SQL clause drops never reaches anything
	// that can report it, and that exact defect has already shipped on this branch.
	vaccines, err := loadVaccineSchedule(ctx, pool)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load vaccine schedule: %w", err)
	}
	milestones, err := loadDevelopmentMilestones(ctx, pool)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load development milestones: %w", err)
	}

	// Every block is selected, and age is applied in Go rather than in SQL. An age-excluded
	// block is a block this child's book deliberately does not contain, which is a fact the
	// caller needs; a WHERE clause would drop it before anything could record it.
	rows, err := pool.Query(ctx, `
		SELECT block_id, book_order, coalesce(section, ''), coalesce(subsection, ''),
		       age_from_mo, age_to_mo
		FROM book1_content_block
		ORDER BY book_order`)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load blocks: %w", err)
	}
	defer rows.Close()

	skipped := append([]string{}, dropped...)
	for rows.Next() {
		var blockID, sectionTitle, subsection string
		var order int
		var ageFrom, ageTo *int
		if err := rows.Scan(&blockID, &order, &sectionTitle, &subsection, &ageFrom, &ageTo); err != nil {
			return Book1{}, nil, fmt.Errorf("book: scan block: %w", err)
		}
		if (ageFrom != nil && cp.AgeMonths < *ageFrom) || (ageTo != nil && cp.AgeMonths > *ageTo) {
			skipped = append(skipped, fmt.Sprintf(
				"block %s (%s) covers %s and does not apply at %d months",
				blockID, sectionTitle, ageRangeLabel(ageFrom, ageTo), cp.AgeMonths))
			continue
		}
		tmpl, ok := blockTemplate[blockID]
		if !ok {
			skipped = append(skipped, fmt.Sprintf(
				"block %s (%s) has no template mapping and was not rendered", blockID, sectionTitle))
			continue
		}

		sec := Section{
			BlockID: blockID, TemplateID: tmpl, BookOrder: order,
			Title: sectionTitle, Subtitle: subsection,
		}

		// A rendered section must carry content. Populating Rows/Cards/Callout here, per
		// template, is the other half of the block-vs-skip accounting above: a template
		// dispatch with nothing behind it is a heading over an empty table, which is the
		// defect this fix exists to close.
		switch tmpl {
		case "B1-PROFILE-01":
			sec.Rows = profileRows(b.Child)

		case "B1-VAX-01":
			var excluded int
			sec.Rows, excluded = vaccineRows(vaccines, cp.AgeMonths)
			if excluded > 0 {
				// Deliberately not prefixed "block ": the block itself IS rendered (it is
				// about to be appended to b.Sections below), so this must not be counted as
				// a block-level skip by the conservation check in
				// TestEveryBlockIsEitherRenderedOrReported -- it is a note about which rows
				// within a rendered block were left out, not about the block being absent.
				skipped = append(skipped, fmt.Sprintf(
					"%d of %d vaccination-schedule rows for block %s (%s) are not yet due "+
						"at %d months and are left off this child's tracker",
					excluded, len(vaccines), blockID, sectionTitle, cp.AgeMonths))
			}
			if len(sec.Rows) == 0 {
				skipped = append(skipped, fmt.Sprintf(
					"block %s (%s) has no vaccination-schedule rows due at %d months and "+
						"was not rendered", blockID, sectionTitle, cp.AgeMonths))
				continue
			}

		case "B1-DEV-01":
			var excluded int
			sec.Rows, excluded = milestoneRows(milestones, cp.AgeMonths)
			if excluded > 0 {
				// See the vaccination-row note above: not "block "-prefixed, for the same
				// reason -- the block is rendered, this only reports rows within it.
				skipped = append(skipped, fmt.Sprintf(
					"%d of %d development-milestone rows for block %s (%s) are ahead of "+
						"this child's current age (%d months) and are left off this page",
					excluded, len(milestones), blockID, sectionTitle, cp.AgeMonths))
			}
			if len(sec.Rows) == 0 {
				skipped = append(skipped, fmt.Sprintf(
					"block %s (%s) has no development-milestone rows at or before %d months "+
						"and was not rendered", blockID, sectionTitle, cp.AgeMonths))
				continue
			}

		case "B1-RED-01":
			callout, ok := globalRedFlagCallout(milestones)
			if !ok {
				skipped = append(skipped, fmt.Sprintf(
					"block %s (%s) has no global red-flag row in book1_development_milestone "+
						"and was not rendered", blockID, sectionTitle))
				continue
			}
			sec.Callout = &callout

		case "B1-END-01":
			// No section content: the template reads Metadata directly (book_version,
			// release_id, generation_date, review_status), which is not per-child data and
			// has nowhere else to live. See body.html's own comment on why.
		}

		b.Sections = append(b.Sections, sec)
	}
	if err := rows.Err(); err != nil {
		return Book1{}, nil, fmt.Errorf("book: block rows: %w", err)
	}

	return b, skipped, nil
}

// loadVaccineSchedule reads all 44 rows of the IAP-ACVIP 2025 schedule. Unfiltered: age is
// applied by the caller, in Go, so an excluded row can be reported rather than silently
// dropped by a WHERE clause.
func loadVaccineSchedule(ctx context.Context, pool *pgxpool.Pool) ([]vaccineScheduleRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT schedule_id, coalesce(age, ''), coalesce(age_min_months, ''),
		       coalesce(vaccine, ''), coalesce(dose_or_event, '')
		FROM book1_vaccine_schedule
		ORDER BY schedule_id`)
	if err != nil {
		return nil, fmt.Errorf("query vaccine schedule: %w", err)
	}
	defer rows.Close()

	var out []vaccineScheduleRow
	for rows.Next() {
		var v vaccineScheduleRow
		if err := rows.Scan(&v.ScheduleID, &v.Age, &v.AgeMinMonths, &v.Vaccine, &v.DoseOrEvent); err != nil {
			return nil, fmt.Errorf("scan vaccine schedule row: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccine schedule rows: %w", err)
	}
	return out, nil
}

// loadDevelopmentMilestones reads all 33 rows of the milestone surveillance table.
// Unfiltered, for the same reason as loadVaccineSchedule.
func loadDevelopmentMilestones(ctx context.Context, pool *pgxpool.Pool) ([]developmentMilestoneRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT milestone_id, coalesce(age_reference, ''), coalesce(age_months, 0),
		       coalesce(domain, ''), coalesce(reference_milestone, ''),
		       coalesce(concern_or_red_flag, ''), coalesce(action_if_concern, '')
		FROM book1_development_milestone
		ORDER BY age_months, milestone_id`)
	if err != nil {
		return nil, fmt.Errorf("query development milestones: %w", err)
	}
	defer rows.Close()

	var out []developmentMilestoneRow
	for rows.Next() {
		var m developmentMilestoneRow
		if err := rows.Scan(&m.MilestoneID, &m.AgeReference, &m.AgeMonths, &m.Domain,
			&m.ReferenceMilestone, &m.ConcernOrRedFlag, &m.ActionIfConcern); err != nil {
			return nil, fmt.Errorf("scan development milestone row: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("development milestone rows: %w", err)
	}
	return out, nil
}

// parseAgeMonths reads book1_vaccine_schedule.age_min_months, which is text so that the
// risk-based rows can hold "Varies" and "Any" rather than a number nobody supplied. ok is
// false for exactly those rows, and the caller treats "no numeric bound" as "cannot be
// excluded", the same convention ageRangeLabel uses for a nil block bound.
func parseAgeMonths(raw string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// vaccineRows builds the B1-VAX-01 rows for one child: due-by-now doses only, oldest due
// first. A dose whose due age is still ahead of the child is excluded and counted, not
// dropped silently -- the risk-based rows ("Varies", "Any") have no numeric due age and are
// never excluded on this basis, sorted after every dated row.
func vaccineRows(vaccines []vaccineScheduleRow, ageMonths int) ([]Row, int) {
	type candidate struct {
		row     Row
		dueAt   float64
		numeric bool
	}

	var included []candidate
	excluded := 0
	for _, v := range vaccines {
		due, numeric := parseAgeMonths(v.AgeMinMonths)
		if numeric && due > float64(ageMonths) {
			excluded++
			continue
		}
		ref := v.Vaccine
		if v.DoseOrEvent != "" && !strings.Contains(ref, v.DoseOrEvent) {
			ref = ref + ", " + v.DoseOrEvent
		}
		included = append(included, candidate{
			row:     Row{Label: v.Age, Reference: ref},
			dueAt:   due,
			numeric: numeric,
		})
	}

	sort.SliceStable(included, func(i, j int) bool {
		if included[i].numeric != included[j].numeric {
			return included[i].numeric
		}
		return included[i].dueAt < included[j].dueAt
	})

	out := make([]Row, len(included))
	for i, c := range included {
		out[i] = c.row
	}
	return out, excluded
}

// milestoneRows builds the B1-DEV-01 rows for one child: milestones at or before the
// child's current age, in age order -- the surveillance record of what has already been
// reached, never a prediction of what is still ahead.
func milestoneRows(milestones []developmentMilestoneRow, ageMonths int) ([]Row, int) {
	var out []Row
	excluded := 0
	for _, m := range milestones {
		if m.AgeMonths > ageMonths {
			excluded++
			continue
		}
		out = append(out, Row{Label: m.Domain, Reference: m.ReferenceMilestone, Note: m.AgeReference})
	}
	return out, excluded
}

// globalRedFlagCallout builds B1-012's single warning panel from the one milestone row the
// provider marked domain "Global red flag" -- the age-independent regression flag, not any
// one of the 32 age-specific concern/action pairs. B1-DEV-01 already prints every
// age-appropriate milestone as a row with its own reference; this is the standing caution
// that applies regardless of which of those the child has or has not reached, which is why
// no age filtering applies here the way it does for the other two tables.
func globalRedFlagCallout(milestones []developmentMilestoneRow) (Callout, bool) {
	for _, m := range milestones {
		if m.Domain == "Global red flag" {
			return Callout{
				Severity: "warning",
				Heading:  m.ConcernOrRedFlag,
				Body:     m.ActionIfConcern,
			}, true
		}
	}
	return Callout{}, false
}

// profileRows builds B1-PROFILE-01's rows from ChildSummary, which is already populated by
// the time this runs. Label carries the fact name; Note carries the recorded value, or is
// left empty so the template's own fallback prints a writing line -- an honest gap rather
// than a fabricated one, for exactly the fields (feeding stage today) that nothing upstream
// has sourced yet.
func profileRows(c ChildSummary) []Row {
	rows := []Row{
		{Label: "Child's name", Note: c.DisplayName},
		{Label: "Age", Note: c.AgeLabel},
		{Label: "Feeding stage", Note: c.FeedingStage},
		{Label: "Allergy status", Note: c.AllergyStatus},
	}

	weight := ""
	if c.WeightKg != nil {
		weight = *c.WeightKg
		if c.MeasuredOn != "" {
			weight += " (measured " + c.MeasuredOn + ")"
		}
	}
	rows = append(rows, Row{Label: "Weight", Note: weight})

	height := ""
	if c.HeightCm != nil {
		height = *c.HeightCm
		if c.MeasuredOn != "" {
			height += " (measured " + c.MeasuredOn + ")"
		}
	}
	rows = append(rows, Row{Label: "Height", Note: height})

	return rows
}

// ageRangeLabel renders a block's applicable age window for the skip reason. An open end is
// printed as open rather than as a number, because a bound nobody set is not a bound of zero.
func ageRangeLabel(from, to *int) string {
	switch {
	case from == nil && to == nil:
		return "every age"
	case from == nil:
		return fmt.Sprintf("up to %d months", *to)
	case to == nil:
		return fmt.Sprintf("from %d months", *from)
	default:
		return fmt.Sprintf("%d-%d months", *from, *to)
	}
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
