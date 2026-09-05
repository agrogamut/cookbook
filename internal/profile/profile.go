// Package profile holds the canonical child profile: what a consultation produces and
// what a book is generated from.
//
// This is deliberately separate from models.ChildProfile, which is the engine's query
// input. The stored profile keeps date_of_birth and derives age at query time, so a book
// generated today and read in six months does not carry a stale age on every page. The
// SRS draws the same line, between an immutable profile snapshot and an engine that takes
// a query.
package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// ErrNotFound is returned by Load when no profile exists for the given child id.
var ErrNotFound = errors.New("profile not found")

// ErrInvalidProfile marks stored data the caller must correct before the engine can run
// against it -- today, only a date of birth in the future.
var ErrInvalidProfile = errors.New("invalid stored profile")

// GrowthMeasurement is one dated set of anthropometry. One child has many, and the trend
// is the clinical point.
//
// Every z-score is clinician-entered. Nothing here computes one: that would mean choosing
// a growth reference, which is a clinical decision this project has no basis to make.
type GrowthMeasurement struct {
	MeasuredOn          time.Time
	WeightKg            *float64
	HeightCm            *float64
	HeadCircumferenceCm *float64
	BMIForAgeZ          *float64
	WeightForAgeZ       *float64
	HeightForAgeZ       *float64
	Interpretation      string
	MeasuredBy          string
}

// DeclaredAllergen carries the three states the provider's own masters distinguish and
// the flat []string on models.ChildProfile cannot express.
type DeclaredAllergen struct {
	Group          string // allergen_mapping.allergen_group
	Status         string // confirmed | suspected | resolved
	Severity       string // mild | systemic, empty when unknown
	Source         string // parent_reported | clinician_documented
	LastReactionOn *time.Time
	EnteredBy      string
}

// Preference is a family-sourced ranking input. Never a filter: a picky child with eight
// dislikes would empty a hard-filtered list.
type Preference struct {
	IngredientID string
	Kind         string // like | dislike | accepted
	EnteredBy    string
}

// ClinicalCondition carries the time dimension the rest of the model lacks. An acute
// condition entered three weeks ago must stop driving a nutrition target.
type ClinicalCondition struct {
	TriggerField       string // clinical_rule_master.trigger_field
	FlagValue          string
	Class              string // acute | chronic | congenital
	OnsetDate          *time.Time
	ExpiresAfterDays   *int
	SpecialistTargetID string
	EnteredBy          string
}

// Stored is one child's full persisted profile.
type Stored struct {
	ChildID              string
	CaseID               string
	MotherName           string
	DisplayName          string
	DateOfBirth          time.Time
	Sex                  string
	LanguageID           string
	RegionCulture        string
	CuisineCode          string
	DietType             string
	Vegan                bool
	ReligiousRestriction string
	BudgetBand           string
	MaxPrepTimeMin       int
	MaxCookTimeMin       int
	CreatedBy            string

	Growth      []GrowthMeasurement
	Allergens   []DeclaredAllergen
	Preferences []Preference
	Conditions  []ClinicalCondition
}

// ageMonths returns completed months between dob and asOf, or -1 if dob is after asOf.
//
// -1 rather than 0 on purpose: a newborn is genuinely 0 months old, so returning 0 for a
// future date of birth would make a data-entry error indistinguishable from a real
// newborn, and the engine would happily rank infant purees for a child who does not exist
// yet.
func ageMonths(dob, asOf time.Time) int {
	// dob comes from Postgres date columns via pgx and is always UTC-normalized, but asOf
	// may be time.Now() from a caller, which is in the local timezone. In IST (UTC+5:30),
	// a call made in the local evening falls on the next UTC day, so comparing calendar
	// fields across two different timezones can be off by one day right at a month
	// boundary -- and NT01's 6-23 month auto-activation window makes that boundary
	// clinically meaningful. Normalize both to UTC calendar dates before comparing.
	dob = dob.UTC()
	asOf = asOf.UTC()
	if dob.After(asOf) {
		return -1
	}
	months := int(asOf.Year()-dob.Year())*12 + int(asOf.Month()) - int(dob.Month())
	if asOf.Day() < dob.Day() {
		months--
	}
	if months < 0 {
		return 0
	}
	return months
}

// ToChildProfile derives the engine's query input from the stored profile as of a given
// date. The second return names every stored fact that did not reach the query and why,
// so a caller can show the operator what was dropped rather than leaving it invisible.
func (s Stored) ToChildProfile(asOf time.Time) (models.ChildProfile, []string, error) {
	age := ageMonths(s.DateOfBirth, asOf)
	if age < 0 {
		return models.ChildProfile{}, nil, fmt.Errorf(
			"profile %s: date of birth %s is after the reference date %s: %w",
			s.ChildID, s.DateOfBirth.Format("2006-01-02"), asOf.Format("2006-01-02"), ErrInvalidProfile)
	}

	cp := models.ChildProfile{
		AgeMonths:      age,
		DietType:       s.DietType,
		Vegan:          s.Vegan,
		RegionCulture:  s.RegionCulture,
		CuisineCode:    s.CuisineCode,
		BudgetBand:     s.BudgetBand,
		MaxPrepTimeMin: s.MaxPrepTimeMin,
		MaxCookTimeMin: s.MaxCookTimeMin,
	}

	var notes []string
	for _, a := range s.Allergens {
		switch a.Status {
		case "confirmed":
			cp.Allergens = append(cp.Allergens, a.Group)
		case "suspected":
			// AS-002: hard_block = N. Ranks down, raises a review flag, never filters.
			cp.SuspectedAllergens = append(cp.SuspectedAllergens, a.Group)
			notes = append(notes, fmt.Sprintf(
				"%s is suspected, not confirmed: it ranks recipes down and raises a review flag, and does not exclude anything (AS-002)", a.Group))
		case "resolved":
			notes = append(notes, fmt.Sprintf(
				"%s is recorded as resolved and excludes nothing; it is kept in history", a.Group))
		}
	}

	for _, c := range s.Conditions {
		if c.Class == "acute" {
			live, note := acuteStatus(c, asOf)
			if note != "" {
				notes = append(notes, note)
			}
			if !live {
				continue
			}
		}
		// A condition whose trigger field is Special_Care_Condition is not a clinical
		// rule flag: it is the engine's own stop-gate input (models.ChildProfile.
		// SpecialCareCondition), read by internal/engine/special_care.go to decide
		// whether to halt generation entirely rather than rank recipes. Routing it into
		// ClinicalFlags alongside ordinary trigger fields would leave the engine unable
		// to see it at all, since specialCareGate reads SpecialCareCondition specifically
		// and the STOP-REVIEW gate would silently never fire.
		if c.TriggerField == "Special_Care_Condition" {
			cp.SpecialCareCondition = c.FlagValue
			continue
		}
		if cp.ClinicalFlags == nil {
			cp.ClinicalFlags = map[string]string{}
		}
		cp.ClinicalFlags[c.TriggerField] = c.FlagValue
	}

	return cp, notes, nil
}

// acuteStatus decides whether an acute condition still applies, and returns the note the
// caller should show either way.
//
// Two cases, and the difference matters:
//
//   - A condition past a stated window is dropped. A diarrhoea flag entered three weeks
//     ago must stop pushing NT12, or every later generation is distorted by a fact that
//     is no longer true.
//   - A condition with no stated window is KEPT and reported as possibly stale. Dropping
//     it would mean inventing an expiry the provider has not given (outstanding question
//     12), and several acute triggers escalate -- failing to apply one is the dangerous
//     direction, while applying a stale one is merely wrong in the cautious direction.
func acuteStatus(c ClinicalCondition, asOf time.Time) (live bool, note string) {
	if c.OnsetDate == nil {
		// The CHECK constraint on child_clinical_condition forbids this, so reaching it
		// means the row was written outside this package. Keep the flag and say so.
		return true, fmt.Sprintf(
			"%s is acute with no onset date, so its age cannot be checked; it is being applied as entered", c.TriggerField)
	}

	days := int(asOf.Sub(*c.OnsetDate).Hours() / 24)

	if c.ExpiresAfterDays == nil {
		return true, fmt.Sprintf(
			"%s is acute, entered %d days ago, and no expiry window is set for its class; it is still being applied and may be stale (provider question 12)",
			c.TriggerField, days)
	}

	if days > *c.ExpiresAfterDays {
		return false, fmt.Sprintf(
			"%s is acute, entered %d days ago, past its %d-day window; it no longer drives a nutrition target",
			c.TriggerField, days, *c.ExpiresAfterDays)
	}
	return true, ""
}

// Save upserts the profile and replaces its child rows in one transaction.
//
// Child rows are deleted and reinserted rather than individually reconciled: a profile is
// small, the write is transactional, and the alternative is a per-row diff that can leave
// a stale measurement behind. This is the same upsert-and-sweep contract the workbook
// importer holds itself to, at a much smaller scale.
func Save(ctx context.Context, pool *pgxpool.Pool, s Stored) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("profile: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO child_profile (child_id, case_id, mother_name, display_name, date_of_birth,
			sex, language_id, region_culture, cuisine_code, diet_type, vegan,
			religious_restriction, budget_band, max_prep_time_min, max_cook_time_min, created_by)
		VALUES ($1,$2,nullif($3,''),nullif($4,''),$5,nullif($6,''),nullif($7,''),nullif($8,''),
			nullif($9,''),nullif($10,''),$11,nullif($12,''),nullif($13,''),nullif($14,0),
			nullif($15,0),$16)
		ON CONFLICT (child_id) DO UPDATE SET
			case_id = excluded.case_id,
			mother_name = excluded.mother_name,
			display_name = excluded.display_name,
			date_of_birth = excluded.date_of_birth,
			sex = excluded.sex,
			language_id = excluded.language_id,
			region_culture = excluded.region_culture,
			cuisine_code = excluded.cuisine_code,
			diet_type = excluded.diet_type,
			vegan = excluded.vegan,
			religious_restriction = excluded.religious_restriction,
			budget_band = excluded.budget_band,
			max_prep_time_min = excluded.max_prep_time_min,
			max_cook_time_min = excluded.max_cook_time_min,
			updated_by = excluded.created_by,
			updated_at = now()`,
		s.ChildID, nullString(s.CaseID), s.MotherName, s.DisplayName, s.DateOfBirth, s.Sex,
		s.LanguageID, s.RegionCulture, s.CuisineCode, s.DietType, s.Vegan,
		s.ReligiousRestriction, s.BudgetBand, s.MaxPrepTimeMin, s.MaxCookTimeMin, s.CreatedBy)
	if err != nil {
		return fmt.Errorf("profile: upsert %s: %w", s.ChildID, err)
	}

	for _, table := range []string{
		"child_growth_measurement", "child_allergen", "child_preference", "child_clinical_condition",
	} {
		// Table names come from this literal list, never from input.
		if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE child_id = $1", s.ChildID); err != nil {
			return fmt.Errorf("profile: clear %s for %s: %w", table, s.ChildID, err)
		}
	}

	for _, g := range s.Growth {
		_, err = tx.Exec(ctx, `
			INSERT INTO child_growth_measurement (child_id, measured_on, weight_kg, height_cm,
				head_circumference_cm, bmi_for_age_z, weight_for_age_z, height_for_age_z,
				interpretation, measured_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,nullif($9,''),$10)`,
			s.ChildID, g.MeasuredOn, g.WeightKg, g.HeightCm, g.HeadCircumferenceCm,
			g.BMIForAgeZ, g.WeightForAgeZ, g.HeightForAgeZ, g.Interpretation, g.MeasuredBy)
		if err != nil {
			return fmt.Errorf("profile: insert growth %s for %s: %w",
				g.MeasuredOn.Format("2006-01-02"), s.ChildID, err)
		}
	}

	for _, a := range s.Allergens {
		_, err = tx.Exec(ctx, `
			INSERT INTO child_allergen (child_id, allergen_group, status, severity, source,
				last_reaction_on, entered_by)
			VALUES ($1,$2,$3,nullif($4,''),$5,$6,$7)`,
			s.ChildID, a.Group, a.Status, a.Severity, defaultSource(a.Source),
			a.LastReactionOn, defaultActor(a.EnteredBy, s.CreatedBy))
		if err != nil {
			return fmt.Errorf("profile: insert allergen %s for %s: %w", a.Group, s.ChildID, err)
		}
	}

	for _, p := range s.Preferences {
		_, err = tx.Exec(ctx, `
			INSERT INTO child_preference (child_id, ingredient_id, kind, entered_by)
			VALUES ($1,$2,$3,$4)`,
			s.ChildID, p.IngredientID, p.Kind, defaultActor(p.EnteredBy, s.CreatedBy))
		if err != nil {
			return fmt.Errorf("profile: insert preference %s for %s: %w", p.IngredientID, s.ChildID, err)
		}
	}

	for _, c := range s.Conditions {
		_, err = tx.Exec(ctx, `
			INSERT INTO child_clinical_condition (child_id, trigger_field, flag_value, class,
				onset_date, expires_after_days, specialist_target_id, entered_by)
			VALUES ($1,$2,$3,$4,$5,$6,nullif($7,''),$8)`,
			s.ChildID, c.TriggerField, c.FlagValue, c.Class, c.OnsetDate,
			c.ExpiresAfterDays, c.SpecialistTargetID, defaultActor(c.EnteredBy, s.CreatedBy))
		if err != nil {
			return fmt.Errorf("profile: insert condition %s for %s: %w", c.TriggerField, s.ChildID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("profile: commit %s: %w", s.ChildID, err)
	}
	return nil
}

// Load reads one profile and all its child rows. Growth measurements come back newest
// first, so a caller reading Growth[0] gets the current measurement.
func Load(ctx context.Context, pool *pgxpool.Pool, childID string) (Stored, error) {
	var s Stored
	err := pool.QueryRow(ctx, `
		SELECT child_id, coalesce(case_id,''), coalesce(mother_name,''),
		       coalesce(display_name,''), date_of_birth,
		       coalesce(sex,''), coalesce(language_id,''), coalesce(region_culture,''),
		       coalesce(cuisine_code,''), coalesce(diet_type,''), vegan,
		       coalesce(religious_restriction,''), coalesce(budget_band,''),
		       coalesce(max_prep_time_min,0), coalesce(max_cook_time_min,0), created_by
		FROM child_profile WHERE child_id = $1`, childID).
		Scan(&s.ChildID, &s.CaseID, &s.MotherName, &s.DisplayName, &s.DateOfBirth, &s.Sex,
			&s.LanguageID, &s.RegionCulture, &s.CuisineCode, &s.DietType, &s.Vegan,
			&s.ReligiousRestriction, &s.BudgetBand, &s.MaxPrepTimeMin, &s.MaxCookTimeMin,
			&s.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stored{}, fmt.Errorf("profile %s: %w", childID, ErrNotFound)
	}
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load %s: %w", childID, err)
	}

	growthRows, err := pool.Query(ctx, `
		SELECT measured_on, weight_kg, height_cm, head_circumference_cm,
		       bmi_for_age_z, weight_for_age_z, height_for_age_z,
		       coalesce(interpretation,''), measured_by
		FROM child_growth_measurement WHERE child_id = $1 ORDER BY measured_on DESC`, childID)
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load growth %s: %w", childID, err)
	}
	defer growthRows.Close()
	for growthRows.Next() {
		var g GrowthMeasurement
		if err := growthRows.Scan(&g.MeasuredOn, &g.WeightKg, &g.HeightCm, &g.HeadCircumferenceCm,
			&g.BMIForAgeZ, &g.WeightForAgeZ, &g.HeightForAgeZ, &g.Interpretation, &g.MeasuredBy); err != nil {
			return Stored{}, fmt.Errorf("profile: scan growth %s: %w", childID, err)
		}
		s.Growth = append(s.Growth, g)
	}
	if err := growthRows.Err(); err != nil {
		return Stored{}, fmt.Errorf("profile: growth rows %s: %w", childID, err)
	}

	allergenRows, err := pool.Query(ctx, `
		SELECT allergen_group, status, coalesce(severity,''), source, last_reaction_on, entered_by
		FROM child_allergen WHERE child_id = $1 ORDER BY allergen_group`, childID)
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load allergens %s: %w", childID, err)
	}
	defer allergenRows.Close()
	for allergenRows.Next() {
		var a DeclaredAllergen
		if err := allergenRows.Scan(&a.Group, &a.Status, &a.Severity, &a.Source,
			&a.LastReactionOn, &a.EnteredBy); err != nil {
			return Stored{}, fmt.Errorf("profile: scan allergen %s: %w", childID, err)
		}
		s.Allergens = append(s.Allergens, a)
	}
	if err := allergenRows.Err(); err != nil {
		return Stored{}, fmt.Errorf("profile: allergen rows %s: %w", childID, err)
	}

	prefRows, err := pool.Query(ctx, `
		SELECT ingredient_id, kind, entered_by
		FROM child_preference WHERE child_id = $1 ORDER BY kind, ingredient_id`, childID)
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load preferences %s: %w", childID, err)
	}
	defer prefRows.Close()
	for prefRows.Next() {
		var p Preference
		if err := prefRows.Scan(&p.IngredientID, &p.Kind, &p.EnteredBy); err != nil {
			return Stored{}, fmt.Errorf("profile: scan preference %s: %w", childID, err)
		}
		s.Preferences = append(s.Preferences, p)
	}
	if err := prefRows.Err(); err != nil {
		return Stored{}, fmt.Errorf("profile: preference rows %s: %w", childID, err)
	}

	condRows, err := pool.Query(ctx, `
		SELECT trigger_field, flag_value, class, onset_date, expires_after_days,
		       coalesce(specialist_target_id,''), entered_by
		FROM child_clinical_condition WHERE child_id = $1 ORDER BY trigger_field`, childID)
	if err != nil {
		return Stored{}, fmt.Errorf("profile: load conditions %s: %w", childID, err)
	}
	defer condRows.Close()
	for condRows.Next() {
		var c ClinicalCondition
		if err := condRows.Scan(&c.TriggerField, &c.FlagValue, &c.Class, &c.OnsetDate,
			&c.ExpiresAfterDays, &c.SpecialistTargetID, &c.EnteredBy); err != nil {
			return Stored{}, fmt.Errorf("profile: scan condition %s: %w", childID, err)
		}
		s.Conditions = append(s.Conditions, c)
	}
	if err := condRows.Err(); err != nil {
		return Stored{}, fmt.Errorf("profile: condition rows %s: %w", childID, err)
	}

	return s, nil
}

// MatchCandidate is one possible existing profile surfaced to an operator, never applied
// automatically. Deliberately narrower than Stored -- a candidate is something to look at
// and decide about, not a profile ready to use as-is.
type MatchCandidate struct {
	ChildID     string
	CaseID      string
	DisplayName string
	DateOfBirth time.Time
	LastTouched time.Time
}

// FindMatches looks for an existing child_profile row that might be the same child as the
// one described by caseID/displayName/motherName/dateOfBirth, matched on exact identity
// only -- never a similarity score. A wrong match in a pediatric feeding-safety context
// (attaching one child's allergy or growth history to another) is a worse failure than
// finding nothing, so this deliberately returns zero rows rather than a "close enough" one.
//
// Two independent match conditions, either of which surfaces a row:
//   - an exact, non-empty case_id match -- the strongest signal when the operator has one
//   - an exact date_of_birth match together with a case-insensitive, whitespace-trimmed
//     match on BOTH display_name and mother_name -- name+dob alone is a real coincidence
//     (twins, common names), and mother_name is what makes this fallback trustworthy
//     without depending on case_id, which nothing prompts an operator for today
//
// Called with no case_id and no complete display_name+motherName+dateOfBirth triple, this
// returns no rows rather than every profile in the table.
func FindMatches(ctx context.Context, pool *pgxpool.Pool, caseID, displayName, motherName string, dateOfBirth time.Time) ([]MatchCandidate, error) {
	caseID = strings.TrimSpace(caseID)
	displayName = strings.TrimSpace(displayName)
	motherName = strings.TrimSpace(motherName)
	if caseID == "" && (displayName == "" || motherName == "" || dateOfBirth.IsZero()) {
		return nil, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT child_id, coalesce(case_id,''), coalesce(display_name,''), date_of_birth,
		       coalesce(updated_at, created_at)
		FROM child_profile
		WHERE (nullif($1,'') IS NOT NULL AND case_id = $1)
		   OR (nullif($2,'') IS NOT NULL AND nullif($3,'') IS NOT NULL AND $4::date IS NOT NULL
		       AND lower(trim(display_name)) = lower(trim($2))
		       AND lower(trim(mother_name)) = lower(trim($3))
		       AND date_of_birth = $4)
		ORDER BY coalesce(updated_at, created_at) DESC`,
		caseID, displayName, motherName, nullDate(dateOfBirth))
	if err != nil {
		return nil, fmt.Errorf("profile: find matches: %w", err)
	}
	defer rows.Close()

	var out []MatchCandidate
	for rows.Next() {
		var m MatchCandidate
		if err := rows.Scan(&m.ChildID, &m.CaseID, &m.DisplayName, &m.DateOfBirth, &m.LastTouched); err != nil {
			return nil, fmt.Errorf("profile: scan match: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("profile: match rows: %w", err)
	}
	return out, nil
}

// nullDate returns nil for a zero time.Time so an absent date of birth reaches Postgres as
// SQL NULL rather than as 0001-01-01, which would otherwise be a real (wrong) date to
// compare against instead of "no date supplied".
func nullDate(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// defaultSource keeps the CHECK constraint satisfiable without silently upgrading the
// trustworthiness of a claim: an unstated source is parent_reported, the weaker of the
// two, never clinician_documented.
func defaultSource(s string) string {
	if s == "" {
		return "parent_reported"
	}
	return s
}

func defaultActor(actor, fallback string) string {
	if actor == "" {
		return fallback
	}
	return actor
}
