package book

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The four provider tables migration 0013 imported and nothing read.
//
// Between them they hold the body of twenty-two Book 1 blocks that were rendering as
// "no template mapping" omissions. The content was never missing; the code was not asking
// for it. Every field below is the provider's own text, loaded verbatim -- no field is
// trimmed to a sentence, reworded, defaulted or summarised.

// DailyLifeModule is one row of book1_daily_life_module: the reference side of an
// ideal-versus-actual page for one everyday domain (sleep, screens, teeth, toilet training).
//
// ParentActualStatus, DateOrFrequency and ParentExampleOrNote are empty on all thirteen rows
// and are not loaded. They are the columns the parent fills in, so the template renders them
// as writing lines; loading three always-empty strings to print three blanks would suggest
// the data was expected and absent, which it is not.
type DailyLifeModule struct {
	DailyLifeID string
	Domain      string
	AgeContext  string
	Reference   string
	Goal        string
	RedFlag     string
	Referral    string
	Display     string
	// AILimit is the provider's own prohibition on this row -- "Do not diagnose sleep
	// disorder", "No psychiatric diagnosis", "Texture safety rules override independence
	// goals". Every one of the thirteen rows carries one, and every one is printed on the
	// page it constrains. A limit honoured silently is a limit the next person to touch this
	// template does not know exists.
	AILimit      string
	ReviewStatus string
}

// MonitoringTemplate is one row of book1_monitoring_template: the column specification for a
// tracker grid, written by the provider rather than chosen here.
type MonitoringTemplate struct {
	TemplateID string
	Section    string
	Parameter  string
	Reference  string
	Actual     string
	DateTime   string
	Notes      string
	Alarm      string
	Review     string
	Frequency  string
}

// IllnessFeedingBlock is one row of book1_illness_feeding_block.
type IllnessFeedingBlock struct {
	IllnessBlockID    string
	Situation         string
	SupportiveMessage string
	WhatToMonitor     string
	RedFlags          string
	// EngineLimit is the load-bearing string on an illness page. Without it -- "Does not
	// replace clinical assessment" -- a page about feeding a vomiting child reads as
	// treatment advice.
	EngineLimit string
	SourceID    string
	Status      string
}

// EvidenceSource is one row of book1_evidence_source.
type EvidenceSource struct {
	SourceID  string
	Authority string
	Topic     string
	Reference string
	URL       string
	HowUsed   string
	// Limitation is the provider's important_limitation, and the reference page prints it
	// beside every citation. A source listed without its stated limitation reads as an
	// endorsement of whatever page cites it, which is the opposite of what these rows say:
	// "Use validated z-score calculation module", "Does not replace clinical assessment".
	Limitation  string
	LastChecked string
	Status      string
}

// BlockSources is every provider row that feeds a block, keyed by block id and already in
// the seed's declared order.
type BlockSources struct {
	Daily      map[string][]DailyLifeModule
	Monitoring map[string][]MonitoringTemplate
	Illness    map[string][]IllnessFeedingBlock
	Evidence   map[string][]EvidenceSource
	// AllMonitoring is every monitoring template, in section order. B1-020 (weekly dashboard)
	// and B1-031 (daily-life dashboard) summarise across domains rather than naming rows, so
	// they are resolved from this rather than from a seed that would duplicate the table.
	AllMonitoring []MonitoringTemplate
}

// evidenceBlock is the one block resolved by rule instead of by seed: B1-022 takes every
// evidence source there is. Seeding thirteen literals would mean a source the provider adds
// later is held by this project and never disclosed in the book that relies on it.
const evidenceBlock = "B1-022"

// LoadBlockSources reads all four source tables in four queries.
//
// Four queries and not one per block: thirty-two blocks against four tables is a hundred and
// twenty-eight round trips to fetch forty-nine rows, on a path that runs once per generated
// book.
func LoadBlockSources(ctx context.Context, pool *pgxpool.Pool) (BlockSources, error) {
	src := BlockSources{
		Daily:      map[string][]DailyLifeModule{},
		Monitoring: map[string][]MonitoringTemplate{},
		Illness:    map[string][]IllnessFeedingBlock{},
		Evidence:   map[string][]EvidenceSource{},
	}

	rows, err := pool.Query(ctx, `
		SELECT s.block_id, d.dailylife_id, coalesce(d.domain, ''),
		       coalesce(d.suggested_age_context, ''), coalesce(d.readiness_or_reference, ''),
		       coalesce(d.progress_goal, ''), coalesce(d.concern_or_red_flag, ''),
		       coalesce(d.approach_doctor_or_specialist, ''), coalesce(d.book1_display, ''),
		       coalesce(d.ai_limit, ''), coalesce(d.review_status, '')
		FROM book1_block_source s
		JOIN book1_daily_life_module d ON d.dailylife_id = s.source_row_id
		WHERE s.source_table = 'book1_daily_life_module'
		ORDER BY s.block_id, s.ordinal`)
	if err != nil {
		return BlockSources{}, fmt.Errorf("book: load daily-life modules: %w", err)
	}
	for rows.Next() {
		var block string
		var m DailyLifeModule
		if err := rows.Scan(&block, &m.DailyLifeID, &m.Domain, &m.AgeContext, &m.Reference,
			&m.Goal, &m.RedFlag, &m.Referral, &m.Display, &m.AILimit, &m.ReviewStatus); err != nil {
			rows.Close()
			return BlockSources{}, fmt.Errorf("book: scan daily-life module: %w", err)
		}
		src.Daily[block] = append(src.Daily[block], m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return BlockSources{}, fmt.Errorf("book: read daily-life modules: %w", err)
	}

	rows, err = pool.Query(ctx, `
		SELECT s.block_id, t.template_id, coalesce(t.section, ''), coalesce(t.parameter, ''),
		       coalesce(t.reference_or_ideal_column, ''), coalesce(t.actual_column, ''),
		       coalesce(t.date_time, ''), coalesce(t.parent_notes, ''),
		       coalesce(t.alarm_column, ''), coalesce(t.doctor_review_column, ''),
		       coalesce(t.frequency, '')
		FROM book1_block_source s
		JOIN book1_monitoring_template t ON t.template_id = s.source_row_id
		WHERE s.source_table = 'book1_monitoring_template'
		ORDER BY s.block_id, s.ordinal`)
	if err != nil {
		return BlockSources{}, fmt.Errorf("book: load monitoring templates: %w", err)
	}
	for rows.Next() {
		var block string
		var m MonitoringTemplate
		if err := rows.Scan(&block, &m.TemplateID, &m.Section, &m.Parameter, &m.Reference,
			&m.Actual, &m.DateTime, &m.Notes, &m.Alarm, &m.Review, &m.Frequency); err != nil {
			rows.Close()
			return BlockSources{}, fmt.Errorf("book: scan monitoring template: %w", err)
		}
		src.Monitoring[block] = append(src.Monitoring[block], m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return BlockSources{}, fmt.Errorf("book: read monitoring templates: %w", err)
	}

	rows, err = pool.Query(ctx, `
		SELECT s.block_id, i.illness_block_id, coalesce(i.situation, ''),
		       coalesce(i.supportive_feeding_message, ''), coalesce(i.what_to_monitor, ''),
		       coalesce(i.red_flags_or_approach_doctor, ''), coalesce(i.book_engine_limit, ''),
		       coalesce(i.source_id, ''), coalesce(i.status, '')
		FROM book1_block_source s
		JOIN book1_illness_feeding_block i ON i.illness_block_id = s.source_row_id
		WHERE s.source_table = 'book1_illness_feeding_block'
		ORDER BY s.block_id, s.ordinal`)
	if err != nil {
		return BlockSources{}, fmt.Errorf("book: load illness blocks: %w", err)
	}
	for rows.Next() {
		var block string
		var b IllnessFeedingBlock
		if err := rows.Scan(&block, &b.IllnessBlockID, &b.Situation, &b.SupportiveMessage,
			&b.WhatToMonitor, &b.RedFlags, &b.EngineLimit, &b.SourceID, &b.Status); err != nil {
			rows.Close()
			return BlockSources{}, fmt.Errorf("book: scan illness block: %w", err)
		}
		src.Illness[block] = append(src.Illness[block], b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return BlockSources{}, fmt.Errorf("book: read illness blocks: %w", err)
	}

	// Every evidence source, by rule rather than by seed -- see evidenceBlock.
	rows, err = pool.Query(ctx, `
		SELECT source_id, coalesce(authority, ''), coalesce(topic, ''),
		       coalesce(reference, ''), coalesce(url, ''), coalesce(how_used, ''),
		       coalesce(important_limitation, ''), coalesce(last_checked, ''),
		       coalesce(status, '')
		FROM book1_evidence_source
		ORDER BY authority, source_id`)
	if err != nil {
		return BlockSources{}, fmt.Errorf("book: load evidence sources: %w", err)
	}
	for rows.Next() {
		var e EvidenceSource
		if err := rows.Scan(&e.SourceID, &e.Authority, &e.Topic, &e.Reference, &e.URL,
			&e.HowUsed, &e.Limitation, &e.LastChecked, &e.Status); err != nil {
			rows.Close()
			return BlockSources{}, fmt.Errorf("book: scan evidence source: %w", err)
		}
		src.Evidence[evidenceBlock] = append(src.Evidence[evidenceBlock], e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return BlockSources{}, fmt.Errorf("book: read evidence sources: %w", err)
	}

	rows, err = pool.Query(ctx, `
		SELECT template_id, coalesce(section, ''), coalesce(parameter, ''),
		       coalesce(reference_or_ideal_column, ''), coalesce(actual_column, ''),
		       coalesce(date_time, ''), coalesce(parent_notes, ''),
		       coalesce(alarm_column, ''), coalesce(doctor_review_column, ''),
		       coalesce(frequency, '')
		FROM book1_monitoring_template ORDER BY section, template_id`)
	if err != nil {
		return BlockSources{}, fmt.Errorf("book: load all monitoring templates: %w", err)
	}
	for rows.Next() {
		var m MonitoringTemplate
		if err := rows.Scan(&m.TemplateID, &m.Section, &m.Parameter, &m.Reference, &m.Actual,
			&m.DateTime, &m.Notes, &m.Alarm, &m.Review, &m.Frequency); err != nil {
			rows.Close()
			return BlockSources{}, fmt.Errorf("book: scan monitoring template: %w", err)
		}
		src.AllMonitoring = append(src.AllMonitoring, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return BlockSources{}, fmt.Errorf("book: read all monitoring templates: %w", err)
	}

	return src, nil
}

// AgeStage is the provider's feeding guidance for one age band, read from
// age_feeding_stage_master. Forty-three columns; these are the ones Book 1's four feeding
// blocks declare as their content.
//
// This table is the answer to "what should a parent know about feeding a child this age",
// and the provider wrote all of it. It was already loaded and already read by the engine --
// it was simply never read by the book.
type AgeStage struct {
	StageCode  string
	StageName  string
	DisplayAge string
	AgeFrom    int
	AgeTo      int

	Phase             string
	MilkContext       string
	ComplementaryFood string
	// Breastfed and non-breastfed meal frequency are separate provider columns and stay
	// separate rows on the page. Collapsing them to one number would be a value the workbook
	// does not contain, and the two genuinely differ.
	BreastfedMeals    string
	BreastfedSnacks   string
	NonBreastfedMeals string

	TextureMinimum     string
	TextureProgression string
	FingerFood         string
	FamilyFood         string
	SelfFeeding        string
	CaregiverMode      string

	ResponsiveFeeding string
	QuantityRule      string
	VarietyRule       string
	AnimalSourceRule  string
	FruitVegRule      string
	ConsistencyRule   string
	HygieneRule       string

	ChokingRisk    string
	ChokingControl string
	HardExclusion  string
	HoneyRule      string
	SaltSugar      string
	IllnessFeeding string

	EscalationTriggers string
	Book1Use           string
	EvidenceBasis      string
	ReviewStatus       string
}

const ageStageColumns = `
	stage_code, coalesce(stage_name, ''), coalesce(display_age, ''),
	age_from_months, age_to_months,
	coalesce(feeding_phase, ''), coalesce(breastfeeding_or_milk_context, ''),
	coalesce(complementary_food_status, ''), coalesce(breastfed_meal_frequency, ''),
	coalesce(breastfed_snack_frequency, ''), coalesce(nonbreastfed_meal_frequency, ''),
	coalesce(texture_minimum, ''), coalesce(texture_progression, ''),
	coalesce(finger_food_status, ''), coalesce(family_food_status, ''),
	coalesce(self_feeding_level, ''), coalesce(caregiver_feeding_mode, ''),
	coalesce(responsive_feeding_rule, ''), coalesce(quantity_progression_rule, ''),
	coalesce(food_variety_rule, ''), coalesce(animal_source_food_rule, ''),
	coalesce(fruit_vegetable_rule, ''), coalesce(meal_consistency_rule, ''),
	coalesce(utensil_hygiene_rule, ''), coalesce(choking_risk_level, ''),
	coalesce(key_choking_control, ''), coalesce(hard_exclusion_or_block, ''),
	coalesce(honey_rule, ''), coalesce(salt_sugar_approach, ''),
	coalesce(illness_feeding_rule, ''), coalesce(clinical_escalation_triggers, ''),
	coalesce(book1_use, ''), coalesce(evidence_basis, ''), coalesce(review_status, '')`

func scanAgeStage(row interface{ Scan(...any) error }) (*AgeStage, error) {
	var s AgeStage
	err := row.Scan(&s.StageCode, &s.StageName, &s.DisplayAge, &s.AgeFrom, &s.AgeTo,
		&s.Phase, &s.MilkContext, &s.ComplementaryFood, &s.BreastfedMeals,
		&s.BreastfedSnacks, &s.NonBreastfedMeals, &s.TextureMinimum, &s.TextureProgression,
		&s.FingerFood, &s.FamilyFood, &s.SelfFeeding, &s.CaregiverMode,
		&s.ResponsiveFeeding, &s.QuantityRule, &s.VarietyRule, &s.AnimalSourceRule,
		&s.FruitVegRule, &s.ConsistencyRule, &s.HygieneRule, &s.ChokingRisk,
		&s.ChokingControl, &s.HardExclusion, &s.HoneyRule, &s.SaltSugar,
		&s.IllnessFeeding, &s.EscalationTriggers, &s.Book1Use, &s.EvidenceBasis,
		&s.ReviewStatus)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// LoadAgeStages returns the child's own feeding stage and the stages either side of it.
//
// The neighbours are what B1-008's declared "comparison table" needs: a parent seeing what
// changes next is the point of that block, and both neighbours come from the same provider
// table as the current stage rather than being extrapolated from it.
//
// A child whose age matches no stage returns a nil current stage and no error. That is an
// absence the caller reports as an omission, not a failure -- though the ten shipped stages
// cover 0-228 months without a gap, which TestLoadAgeStageCoversEveryAge holds them to.
func LoadAgeStages(ctx context.Context, pool *pgxpool.Pool, ageMonths int) (current, prev, next *AgeStage, err error) {
	// Ordered by age_from_months DESC so an overlap resolves to the more specific (later)
	// stage rather than erroring. The masters declare no overlaps; this decides what happens
	// if the provider ever ships one, rather than leaving it to row order.
	row := pool.QueryRow(ctx, `SELECT `+ageStageColumns+`
		FROM age_feeding_stage_master
		WHERE age_from_months <= $1 AND age_to_months >= $1
		ORDER BY age_from_months DESC LIMIT 1`, ageMonths)
	current, err = scanAgeStage(row)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, fmt.Errorf("book: load age stage for %d months: %w", ageMonths, err)
	}

	row = pool.QueryRow(ctx, `SELECT `+ageStageColumns+`
		FROM age_feeding_stage_master WHERE age_to_months < $1
		ORDER BY age_to_months DESC LIMIT 1`, current.AgeFrom)
	if p, perr := scanAgeStage(row); perr == nil {
		prev = p
	} else if !strings.Contains(perr.Error(), "no rows") {
		return nil, nil, nil, fmt.Errorf("book: load previous age stage: %w", perr)
	}

	row = pool.QueryRow(ctx, `SELECT `+ageStageColumns+`
		FROM age_feeding_stage_master WHERE age_from_months > $1
		ORDER BY age_from_months ASC LIMIT 1`, current.AgeTo)
	if n, nerr := scanAgeStage(row); nerr == nil {
		next = n
	} else if !strings.Contains(nerr.Error(), "no rows") {
		return nil, nil, nil, fmt.Errorf("book: load next age stage: %w", nerr)
	}

	return current, prev, next, nil
}
