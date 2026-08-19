package book

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
	"github.com/madamgy/recipie/internal/profile"
)

// blockTemplate maps a Book 1 content block to the provider's template id from
// MadamGY_PDF_Template_Contract_V1.json. Hand-written, because the workbook carries no
// template column and guessing from a block's title would put clinical content in the wrong
// visual treatment -- B1-RED-01 is the high-contrast warning panel, and a red-flag block
// rendered as an ordinary table would lose exactly the emphasis it needs.
//
// A block with no entry is not rendered, and AssembleBook1 reports it.
//
// For most of this branch's life that was true of twenty-five of the thirty-two blocks, and
// the omission message said they had "no template mapping" -- accurate about the code and
// wrong about the data. The provider had declared each block's format in table_or_format, its
// columns in writable_fields and its monitoring axis in monitoring_fields, and had shipped
// the body content in four separate tables that nothing queried. Reading those columns as the
// page's layout is following the provider's declaration; it is not the invented layout this
// comment was written to forbid. What stays forbidden is unchanged: drafting the guidance
// prose those pages do not carry text for. See docs/book1-block-sources.md.
var blockTemplate = map[string]string{
	"B1-001": "B1-PROFILE-01",
	"B1-002": "B1-TRACKER-01",
	// B1-003 is the provider's "Writable monitoring table" of dated anthropometry, and its
	// declared inputs -- DOB, sex, weight, length/height, BMI and HC where age-appropriate --
	// are exactly what child_growth_measurement stores.
	//
	// B1-004 (growth trend interpretation) is deliberately NOT mapped alongside it, and is the
	// only block in the workbook that stays unmapped. Its declared input is a
	// "z-score/percentile engine", which this project does not have: interpreting a trend
	// means computing against the WHO reference tables, and a trend stated without them would
	// be a clinical finding with no source. It stays an omission.
	"B1-003": "B1-GROWTH-01",
	"B1-005": "B1-STAGE-01",
	"B1-006": "B1-STAGE-01",
	"B1-007": "B1-STAGE-01",
	"B1-008": "B1-STAGE-01",
	"B1-009": "B1-VAX-01",
	"B1-010": "B1-TRACKER-01",
	"B1-011": "B1-DEV-01",
	"B1-012": "B1-RED-01",
	"B1-013": "B1-TRACKER-01",
	"B1-014": "B1-DEV-01",
	"B1-015": "B1-ILLNESS-01",
	"B1-016": "B1-ILLNESS-01",
	"B1-017": "B1-TRACKER-01",
	"B1-018": "B1-SAFETY-01",
	"B1-019": "B1-TRACKER-01",
	"B1-020": "B1-TRACKER-01",
	"B1-021": "B1-TRACKER-01",
	"B1-022": "B1-REFS-01",
	"B1-023": "B1-DAILY-01",
	"B1-024": "B1-DAILY-01",
	"B1-025": "B1-DAILY-01",
	"B1-026": "B1-DAILY-01",
	"B1-027": "B1-DAILY-01",
	"B1-028": "B1-DAILY-01",
	"B1-029": "B1-DAILY-01",
	"B1-030": "B1-DAILY-01",
	"B1-031": "B1-TRACKER-01",
	"B1-032": "B1-DAILY-01",
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
//
// The special-care stop gate is consulted before anything is assembled, and returns
// ErrBlocked exactly as AssembleBook2 does. Book 1 runs no engine of its own -- it carries
// no recipe to filter -- which is precisely how a child with a STOP-REVIEW diagnosis got a
// full book of general-population milestone tables in their own name with no mention of the
// clinician's stop. The provider's rule is a stop on generation, not a recipe filter, so the
// gate has to sit here too. Blocking needs no clinical sign-off; issuing the document does.
func AssembleBook1(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time) (Book1, []string, error) {
	cp, dropped, err := s.ToChildProfile(asOf)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: derive engine input: %w", err)
	}

	blocked, reason, err := engine.SpecialCareBlock(ctx, pool, cp)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: special-care gate: %w", err)
	}
	if blocked {
		return Book1{}, nil, fmt.Errorf("%w: %s", ErrBlocked, reason)
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
			DateOfBirth: s.DateOfBirth.UTC().Format("2006-01-02"),
			AgeMonths:   cp.AgeMonths,
			AgeLabel:    ageLabel(cp.AgeMonths),
			// As stored. There is no language master to resolve an id against, and
			// inventing a display name for one would be a value with no source.
			Sex:      s.Sex,
			Language: s.LanguageID,
			// Both lists, never only the confirmed one -- see allergyStatus.
			AllergyStatus: allergyStatus(cp.Allergens, cp.SuspectedAllergens),
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
	// Loaded once. Four queries for four tables, not one per block: thirty-two blocks against
	// four source tables would be a hundred and twenty-eight round trips to fetch forty-nine
	// rows, on a path that runs once per generated book.
	src, err := LoadBlockSources(ctx, pool)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load block sources: %w", err)
	}
	stageNow, stagePrev, stageNext, err := LoadAgeStages(ctx, pool, cp.AgeMonths)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load age stage: %w", err)
	}
	safety, err := loadSafetyCard(ctx, pool, s, cp)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load safety card: %w", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT block_id, book_order, coalesce(section, ''), coalesce(subsection, ''),
		       age_from_mo, age_to_mo, coalesce(part, ''), coalesce(content_purpose, ''),
		       coalesce(parent_facing_output, ''), coalesce(writable_fields, '')
		FROM book1_content_block
		ORDER BY book_order`)
	if err != nil {
		return Book1{}, nil, fmt.Errorf("book: load blocks: %w", err)
	}
	defer rows.Close()

	skipped := append([]string{}, dropped...)
	for rows.Next() {
		var blockID, sectionTitle, subsection, part, purpose, facing, writable string
		var order int
		var ageFrom, ageTo *int
		if err := rows.Scan(&blockID, &order, &sectionTitle, &subsection, &ageFrom, &ageTo,
			&part, &purpose, &facing, &writable); err != nil {
			return Book1{}, nil, fmt.Errorf("book: scan block: %w", err)
		}
		if (ageFrom != nil && cp.AgeMonths < *ageFrom) || (ageTo != nil && cp.AgeMonths > *ageTo) {
			skipped = append(skipped, omissionBlock+fmt.Sprintf(
				"%s (%s) covers %s and does not apply at %d months",
				blockID, sectionTitle, ageRangeLabel(ageFrom, ageTo), cp.AgeMonths))
			continue
		}
		tmpl, ok := blockTemplate[blockID]
		if !ok {
			skipped = append(skipped, omissionBlock+fmt.Sprintf(
				"%s (%s) has no template mapping and was not rendered", blockID, sectionTitle))
			continue
		}

		sec := Section{
			BlockID: blockID, TemplateID: tmpl, BookOrder: order,
			Title: sectionTitle, Subtitle: subsection, Part: part,
			// The block's own content_purpose and parent_facing_output. They are the
			// provider's sentences about why the page exists and what it covers -- the only
			// introductory prose these pages carry, and it is theirs rather than drafted here.
			Purpose: purpose,
			Covers:  splitDeclared(facing),
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
				// Deliberately unmarked: the block itself IS rendered (it is about to be
				// appended to b.Sections below), so this must not carry omissionBlock and
				// must not be counted as a block-level skip by the conservation check in
				// TestEveryBlockIsEitherRenderedOrReported -- it is a note about which rows
				// within a rendered block were left out, not about the block being absent.
				skipped = append(skipped, fmt.Sprintf(
					"%d of %d vaccination-schedule rows for block %s (%s) are not yet due "+
						"at %d months and are left off this child's tracker",
					excluded, len(vaccines), blockID, sectionTitle, cp.AgeMonths))
			}
			if len(sec.Rows) == 0 {
				skipped = append(skipped, omissionBlock+fmt.Sprintf(
					"%s (%s) has no vaccination-schedule rows due at %d months and "+
						"was not rendered", blockID, sectionTitle, cp.AgeMonths))
				continue
			}

		case "B1-DEV-01":
			var excluded int
			// Two blocks share this template and take different slices of the same table.
			// See currentCheckpointRows.
			if blockID == "B1-011" {
				var checkpoint string
				sec.Rows, checkpoint, excluded = currentCheckpointRows(milestones, cp.AgeMonths)
				if checkpoint != "" {
					sec.Subtitle = subsection + " - " + checkpoint + " checkpoint"
				}
			} else {
				sec.Rows, excluded = milestoneRows(milestones, cp.AgeMonths)
			}
			if excluded > 0 {
				// See the vaccination-row note above: unmarked for the same reason -- the
				// block is rendered, this only reports rows within it.
				skipped = append(skipped, fmt.Sprintf(
					"%d of %d development-milestone rows for block %s (%s) are ahead of "+
						"this child's current age (%d months) and are left off this page",
					excluded, len(milestones), blockID, sectionTitle, cp.AgeMonths))
			}
			if len(sec.Rows) == 0 {
				skipped = append(skipped, omissionBlock+fmt.Sprintf(
					"%s (%s) has no development-milestone rows at or before %d months "+
						"and was not rendered", blockID, sectionTitle, cp.AgeMonths))
				continue
			}

		case "B1-RED-01":
			callout, ok := globalRedFlagCallout(milestones)
			if !ok {
				skipped = append(skipped, omissionBlock+fmt.Sprintf(
					"%s (%s) has no global red-flag row in book1_development_milestone "+
						"and was not rendered", blockID, sectionTitle))
				continue
			}
			sec.Callout = &callout

		case "B1-GROWTH-01":
			sec.Growth = growthRows(s.Growth)
			if len(sec.Growth) == 0 {
				skipped = append(skipped, omissionBlock+fmt.Sprintf(
					"%s (%s) has no recorded growth measurements for this child and was "+
						"not rendered", blockID, sectionTitle))
				continue
			}

		case "B1-DAILY-01":
			sec.Domains, sec.Trackers = dailyDomains(src.Daily[blockID], src.Monitoring[blockID])

		case "B1-ILLNESS-01":
			for _, i := range src.Illness[blockID] {
				sec.Illness = append(sec.Illness, IllnessBlock{
					ID: i.IllnessBlockID, Situation: i.Situation,
					SupportiveMessage: i.SupportiveMessage, WhatToMonitor: i.WhatToMonitor,
					RedFlags: i.RedFlags, EngineLimit: i.EngineLimit,
				})
			}
			// B1-016 is the "supportive table" beside the situations and seeds a monitoring
			// template rather than illness rows. It renders the grid under the same heading.
			for _, m := range src.Monitoring[blockID] {
				sec.Trackers = append(sec.Trackers, trackerFromMonitoring(m))
			}

		case "B1-STAGE-01":
			sec.Stage = &StagePage{
				Current: stageNow, Prev: stagePrev, Next: stageNext,
				Facet: stageFacet[blockID],
			}
			if stageNow == nil {
				skipped = append(skipped, omissionBlock+fmt.Sprintf(
					"%s (%s) has no feeding stage at %d months: age_feeding_stage_master "+
						"ends at 216 months (eighteen years) and this block declares a wider "+
						"range. Recorded as GAP-027.", blockID, sectionTitle, cp.AgeMonths))
				continue
			}

		case "B1-SAFETY-01":
			sec.Safety = safety
			sec.Safety.ReactionLog = trackerFromWritable("Reaction record", writable, "As needed")

		case "B1-REFS-01":
			sec.Refs = src.Evidence[blockID]
			if len(sec.Refs) == 0 {
				skipped = append(skipped, omissionBlock+fmt.Sprintf(
					"%s (%s) has no rows in book1_evidence_source and was not rendered",
					blockID, sectionTitle))
				continue
			}

		case "B1-TRACKER-01":
			sec.Trackers = trackerGrids(blockID, writable, sectionTitle, src, cp.AgeMonths)

		case "B1-END-01":
			// No section content: the template reads Metadata directly (book_version,
			// release_id, generation_date, review_status), which is not per-child data and
			// has nowhere else to live. See body.html's own comment on why.
		}

		// The last guard before a section is appended, and the one conservation accounting
		// cannot supply. "Rendered plus reported equals total" stayed true for the whole time
		// this book was printing headings over empty areas: a section counted as rendered and
		// a section that carries something are different claims. B1-END-01 is exempt because
		// its content lives on Metadata rather than on the Section.
		if tmpl != "B1-END-01" && !SectionHasContent(sec) {
			skipped = append(skipped, omissionBlock+fmt.Sprintf(
				"%s (%s) resolved to no content for this child and was not rendered",
				blockID, sectionTitle))
			continue
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

// currentCheckpointRows is B1-011's half of the milestone table: the checkpoint the child is
// at, not the whole history behind them.
//
// B1-011 and B1-014 both map to B1-DEV-01 and both called milestoneRows, so they printed the
// identical table twice in one book -- forty-odd rows, then the same forty-odd rows. The
// workbook distinguishes them plainly: B1-011 is "Ideal/reference vs actual table" over 0-72
// months, B1-014 is "Detailed comparison table" over 0-216. Surveillance is about where the
// child is now; the detailed table is the reference chart behind it.
//
// The checkpoint is the latest age_reference at or before the child's age. Every milestone
// sharing that reference prints, so a domain with nothing at this checkpoint is absent rather
// than back-filled from an earlier one -- which would state a skill as current when the
// provider recorded it for a younger age.
func currentCheckpointRows(milestones []developmentMilestoneRow, ageMonths int) ([]Row, string, int) {
	checkpoint := -1
	for _, m := range milestones {
		if m.AgeMonths <= ageMonths && m.AgeMonths > checkpoint {
			checkpoint = m.AgeMonths
		}
	}
	if checkpoint < 0 {
		return nil, "", len(milestones)
	}

	var out []Row
	var label string
	excluded := 0
	for _, m := range milestones {
		if m.AgeMonths != checkpoint {
			excluded++
			continue
		}
		if label == "" {
			label = m.AgeReference
		}
		out = append(out, Row{Label: m.Domain, Reference: m.ReferenceMilestone, Note: m.AgeReference})
	}
	return out, label, excluded
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
// than a fabricated one, for a measurement nothing upstream has recorded.
//
// Feeding stage is deliberately not a row. Nothing populated it, so it printed a permanent
// blank line on every book -- and unlike weight or height, it is not an observation for a
// parent to write in: it is a fact the corpus can derive, from age against
// age_feeding_stage. A blank line invites someone to hand-enter a value the system should
// already know. It returns as a row when that join is built, not before.
// growthRows turns the child's stored measurements into B1-003's monitoring table, oldest
// first so the page reads as a trend down the column rather than backwards.
//
// profile.Load returns them newest first, which is right for "what is this child's current
// weight" and wrong for a monitoring table, so they are reversed here rather than the loader
// being changed under the other callers that depend on its order.
//
// A measurement absent from a visit formats to empty and the template draws a writing line.
// Nothing is carried forward from an earlier visit to fill it: a weight from three months ago
// printed on today's row would read as today's weight.
func growthRows(ms []profile.GrowthMeasurement) []GrowthRow {
	rows := make([]GrowthRow, 0, len(ms))
	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		r := GrowthRow{
			MeasuredOn:     m.MeasuredOn.UTC().Format("2006-01-02"),
			Interpretation: m.Interpretation,
			MeasuredBy:     m.MeasuredBy,
		}
		if m.WeightKg != nil {
			r.WeightKg = fmt.Sprintf("%.1f kg", *m.WeightKg)
		}
		if m.HeightCm != nil {
			r.HeightCm = fmt.Sprintf("%.1f cm", *m.HeightCm)
		}
		if m.HeadCircumferenceCm != nil {
			r.HeadCircumCm = fmt.Sprintf("%.1f cm", *m.HeadCircumferenceCm)
		}
		r.ZScores = formatZScores(m)
		rows = append(rows, r)
	}
	return rows
}

// formatZScores renders whichever z-scores a clinician recorded, labelled so a reader knows
// which is which, and nothing at all when none were recorded.
//
// Only recorded values appear. A missing z-score is not shown as a dash or a zero, both of
// which read as a measurement, and none is ever computed here -- see GrowthRow.ZScores.
func formatZScores(m profile.GrowthMeasurement) string {
	var parts []string
	for _, z := range []struct {
		label string
		value *float64
	}{
		{"weight-for-age", m.WeightForAgeZ},
		{"height-for-age", m.HeightForAgeZ},
		{"BMI-for-age", m.BMIForAgeZ},
	} {
		if z.value != nil {
			parts = append(parts, fmt.Sprintf("%s %+.1f", z.label, *z.value))
		}
	}
	return strings.Join(parts, "; ")
}

func profileRows(c ChildSummary) []Row {
	rows := []Row{
		{Label: "Child's name", Note: c.DisplayName},
		{Label: "Date of birth", Note: c.DateOfBirth},
		{Label: "Age", Note: c.AgeLabel},
		{Label: "Sex", Note: c.Sex},
		{Label: "Language", Note: c.Language},
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

// allergyStatus renders the allergy line both books print on the child's profile page. One
// function, called by both assemblers, so the two books can never disagree about a child's
// allergy status.
//
// It takes both lists because internal/profile splits them: only a confirmed allergen
// reaches models.ChildProfile.Allergens and is filtered on, while a suspected one goes to
// SuspectedAllergens and, per the provider's AS-002 (hard_block = N), ranks recipes down
// without excluding any. A page that printed only the confirmed list would tell a parent
// with a clinician-recorded suspected peanut allergy "No known food allergy reported" while
// the book still carried peanut recipes, which is the confident-wrong output CLAUDE.md names
// as the dangerous failure. So the suspected groups are named, and the line states what the
// engine actually did with them -- the recorded status and AS-002's own consequence, not a
// clinical sentence composed here.
//
// A resolved allergen is deliberately absent from this line: it excludes nothing and is
// history rather than current status. ToChildProfile already emits a note saying so, and
// that note reaches both books' omission list.
func allergyStatus(confirmed, suspected []string) string {
	var parts []string
	if len(confirmed) > 0 {
		parts = append(parts, "Confirmed: "+strings.Join(confirmed, ", "))
	}
	if len(suspected) > 0 {
		parts = append(parts, "Suspected, not confirmed: "+strings.Join(suspected, ", ")+
			". A suspected allergen ranks recipes down and raises a review flag, and does "+
			"not exclude anything (AS-002), so recipes containing it are not filtered out "+
			"of this book")
	}
	if len(parts) == 0 {
		// The prototype's own wording for the empty case, and correct only when both lists
		// are empty. An empty string here would render a blank cell that reads as an
		// unanswered question rather than a recorded negative.
		return "No known food allergy reported"
	}
	return strings.Join(parts, ". ")
}

// dailyDomains pairs each daily-life module with the tracker grid its block seeds.
//
// The pairing is positional only when the counts agree; otherwise every module gets every
// grid the block seeded. B1-030 is the case that forces this: it carries two modules (school
// and behaviour) and two monitoring templates, and they line up, while B1-023 carries two
// modules against one shared tracker. Guessing a correspondence where the seed declares none
// would put a behaviour grid under a school heading.
func dailyDomains(mods []DailyLifeModule, mons []MonitoringTemplate) ([]DailyDomain, []TrackerSpec) {
	out := make([]DailyDomain, 0, len(mods))
	for _, m := range mods {
		out = append(out, DailyDomain{
			ID: m.DailyLifeID, Domain: m.Domain, AgeContext: m.AgeContext,
			Reference: m.Reference, Goal: m.Goal, RedFlag: m.RedFlag,
			Referral: m.Referral, AILimit: m.AILimit, Display: m.Display,
		})
	}

	// One tracker per domain only when the counts line up. B1-030 is the case that works that
	// way: two modules (school, behaviour) and two monitoring templates, one each.
	if len(mons) == len(mods) {
		for i := range out {
			t := trackerFromMonitoring(mons[i])
			// The module's own book1_display names the form -- "Sleep diary", "Dental
			// tracker" -- which is what a parent looking for it on the page reads. The
			// monitoring template's section/parameter title is an internal label.
			if out[i].Display != "" {
				t.Title = out[i].Display
			}
			// The two masters say the same thing in two columns, and printing both put two
			// warning boxes on one page saying the same words twice: the dental domain's
			// "Dental pain, swelling, trauma, visible caries concern" against its tracker's
			// "Pain/swelling/trauma/caries concern". The domain's is the fuller sentence, so
			// it is the one that prints; suppressing the shorter copy is not dropping
			// provider content, it is declining to print the same content twice. Same for the
			// reviewer line against the domain's own referral.
			if out[i].RedFlag != "" {
				t.Alarm = ""
			}
			if out[i].Referral != "" {
				t.Review = ""
			}
			out[i].Tracker = &t
		}
		return out, nil
	}

	// Otherwise the block's trackers belong to the block, not to any one domain, and print
	// once after all of them.
	//
	// B1-023 is why. It carries two modules -- toilet-training readiness and daytime progress
	// -- against one shared PM-TOIL-01, and attaching that one tracker to each domain printed
	// the identical seven-column grid twice on consecutive pages. Two copies of one form is
	// not two forms; it is the same page turning up again, which is exactly what a reader
	// calls filler.
	var shared []TrackerSpec
	for _, m := range mons {
		shared = append(shared, trackerFromMonitoring(m))
	}
	return out, shared
}

// dashboardBlocks are the two blocks that summarise across every domain rather than naming
// source rows: B1-020's weekly dashboard and B1-031's daily-life dashboard. They carry no seed
// rows, because listing every monitoring template against them would be a second copy of that
// table which drifts the moment the provider edits one.
var dashboardBlocks = map[string]bool{"B1-020": true, "B1-031": true}

// trackerGrids builds the forms for a B1-TRACKER-01 block.
//
// Three sources in priority order, and the order matters. A monitoring template is the
// provider's full column specification -- reference, actual, date, notes, alarm, reviewer --
// so it wins wherever the seed names one. A block's own writable_fields is the fallback for
// the blocks that declare a form and have no template behind them (B1-002's goal table,
// B1-010's reaction log). A block with neither returns nothing and is reported as an omission
// by the caller's SectionHasContent guard.
func trackerGrids(blockID, writable, title string, src BlockSources, ageMonths int) []TrackerSpec {
	if dashboardBlocks[blockID] {
		return dashboardGrids(src.AllMonitoring, ageMonths)
	}
	var out []TrackerSpec
	for _, m := range src.Monitoring[blockID] {
		out = append(out, trackerFromMonitoring(m))
	}
	if len(out) == 0 {
		if t := trackerFromWritable(title, writable, ""); t != nil {
			out = append(out, *t)
		}
	}
	return out
}

// dashboardGrids builds the summary dashboards from every monitoring template.
//
// One row per parameter rather than one grid per parameter: a dashboard is the overview page,
// and eighteen separate seven-row grids is eighteen pages of the same thing the per-domain
// blocks already print. The columns are the provider's own reference and actual column names
// taken from the first template, because every row shares the shape even where the wording
// differs -- and where it differs, the parameter's own reference text prints in its row.
func dashboardGrids(all []MonitoringTemplate, ageMonths int) []TrackerSpec {
	if len(all) == 0 {
		return nil
	}
	// One row per monitoring parameter, with the area and the provider's reference already
	// printed. Only the columns the parent fills -- actual, date, note -- are writing lines.
	//
	// The alternative, and the first attempt, was eighteen blank rows under five headings.
	// It conserved perfectly, tested green, and was useless on paper: a form that does not
	// say what belongs on each line is not a form. Every filled cell here is a provider
	// string, so nothing is gained by leaving it out except the reader's ability to use it.
	rows := make([][]string, 0, len(all))
	for _, m := range all {
		area := m.Section
		if m.Parameter != "" && m.Parameter != m.Section {
			area = m.Section + ": " + m.Parameter
		}
		rows = append(rows, []string{area, m.Reference})
	}
	return []TrackerSpec{{
		Title:     "Every area, one page",
		Frequency: "At each review",
		Columns:   []string{"Area", "Reference", "Actual", "Date", "Note"},
		Prefilled: rows,
		Review:    "Bring this page to the follow-up visit.",
	}}
}

// loadSafetyCard builds B1-018 from the child's own record, not from the corpus.
//
// Confirmed and suspected stay in separate lists all the way to the page: they carry
// different instructions, and a merged list loses which is which.
//
// Choking rules are NOT filtered by age, deliberately. age_band on choking_texture_safety is
// prose -- "<5 years", "All pediatric ages", "Young child" -- and turning that into a numeric
// comparison means deciding what "young child" means, which is a clinical boundary this
// project has no source for and would be inventing. The whole table prints with age_band as
// its own visible column, so a reader sees which rule applies to whom and the judgement stays
// with the person holding the book. It is eight rows; there is no cost to printing them all.
func loadSafetyCard(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, cp models.ChildProfile) (*SafetyCard, error) {
	card := &SafetyCard{
		Confirmed: append([]string{}, cp.Allergens...),
		Suspected: append([]string{}, cp.SuspectedAllergens...),
	}

	// Column names are choking_texture_safety's own: food_or_form, risk_form, default_action,
	// age_band. safe_modification is deliberately not read -- it is the provider's adaptation
	// text and belongs to the recipe engine's substitution path, not to a safety card whose
	// whole job is to state the rule without qualifying it.
	rows, err := pool.Query(ctx, `
		SELECT coalesce(food_or_form, ''), coalesce(risk_form, ''),
		       coalesce(default_action, ''), coalesce(age_band, '')
		FROM choking_texture_safety
		ORDER BY hazard_id`)
	if err != nil {
		return nil, fmt.Errorf("query choking rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c ChokingRule
		if err := rows.Scan(&c.Food, &c.Risk, &c.Rule, &c.AgeFor); err != nil {
			return nil, fmt.Errorf("scan choking rule: %w", err)
		}
		card.Choking = append(card.Choking, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("choking rules: %w", err)
	}

	return card, nil
}

// stageFacet says which slice of age_feeding_stage_master each feeding block prints.
//
// Four blocks share one template, and without this they shared one page: the same twenty-row
// table, four times consecutively, which is what "generated filler" looks like from a reader's
// side even when every row is the provider's own text. The split is the workbook's, taken from
// each block's declared table_or_format and parent_facing_output rather than chosen here:
//
//	B1-005 "Daily target table"                -> what and how much, across a day
//	B1-006 "Meal schedule table"               -> when meals happen, and their texture
//	B1-007 "Age-specific guidance + checklist" -> how the caregiver feeds, not what
//	B1-008 "Comparison table"                  -> only the before/after stages
//
// The safety callouts (choking, hard exclusions, escalation triggers) print on the target
// page alone. Repeating a warning on four consecutive pages is how a warning stops being read.
var stageFacet = map[string]string{
	"B1-005": "target",
	"B1-006": "schedule",
	"B1-007": "approach",
	"B1-008": "comparison",
}
