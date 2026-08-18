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

	return cp, notes, nil
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
		INSERT INTO child_profile (child_id, case_id, display_name, date_of_birth, sex,
			language_id, region_culture, cuisine_code, diet_type, vegan,
			religious_restriction, budget_band, max_prep_time_min, max_cook_time_min, created_by)
		VALUES ($1,$2,nullif($3,''),$4,nullif($5,''),nullif($6,''),nullif($7,''),nullif($8,''),
			nullif($9,''),$10,nullif($11,''),nullif($12,''),nullif($13,0),nullif($14,0),$15)
		ON CONFLICT (child_id) DO UPDATE SET
			case_id = excluded.case_id,
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
		s.ChildID, nullString(s.CaseID), s.DisplayName, s.DateOfBirth, s.Sex,
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
		SELECT child_id, coalesce(case_id,''), coalesce(display_name,''), date_of_birth,
		       coalesce(sex,''), coalesce(language_id,''), coalesce(region_culture,''),
		       coalesce(cuisine_code,''), coalesce(diet_type,''), vegan,
		       coalesce(religious_restriction,''), coalesce(budget_band,''),
		       coalesce(max_prep_time_min,0), coalesce(max_cook_time_min,0), created_by
		FROM child_profile WHERE child_id = $1`, childID).
		Scan(&s.ChildID, &s.CaseID, &s.DisplayName, &s.DateOfBirth, &s.Sex, &s.LanguageID,
			&s.RegionCulture, &s.CuisineCode, &s.DietType, &s.Vegan, &s.ReligiousRestriction,
			&s.BudgetBand, &s.MaxPrepTimeMin, &s.MaxCookTimeMin, &s.CreatedBy)
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
