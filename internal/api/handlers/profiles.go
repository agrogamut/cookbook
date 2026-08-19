package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/profile"
)

// dateLayout is the wire format for every date this resource carries. Dates, not
// timestamps: a date of birth has no time of day, and RFC3339 would invite a timezone into
// a value that has none. profile's own age derivation normalises to UTC for the same
// reason -- in IST a local evening falls on the next UTC day, which at a month boundary
// changes a derived age, and NT01's 6-23 month window makes that boundary clinically
// meaningful.
const dateLayout = "2006-01-02"

type profileAllergenDTO struct {
	Group          string `json:"group"`
	Status         string `json:"status"`
	Severity       string `json:"severity,omitempty"`
	Source         string `json:"source,omitempty"`
	LastReactionOn string `json:"last_reaction_on,omitempty"`
	EnteredBy      string `json:"entered_by,omitempty"`
}

type profileDTO struct {
	ChildID              string               `json:"child_id"`
	CaseID               string               `json:"case_id,omitempty"`
	DisplayName          string               `json:"display_name,omitempty"`
	DateOfBirth          string               `json:"date_of_birth"`
	Sex                  string               `json:"sex,omitempty"`
	LanguageID           string               `json:"language_id,omitempty"`
	RegionCulture        string               `json:"region_culture,omitempty"`
	CuisineCode          string               `json:"cuisine_code,omitempty"`
	DietType             string               `json:"diet_type,omitempty"`
	Vegan                bool                 `json:"vegan,omitempty"`
	ReligiousRestriction string               `json:"religious_restriction,omitempty"`
	BudgetBand           string               `json:"budget_band,omitempty"`
	MaxPrepTimeMin       int                  `json:"max_prep_time_min,omitempty"`
	MaxCookTimeMin       int                  `json:"max_cook_time_min,omitempty"`
	CreatedBy            string               `json:"created_by,omitempty"`
	Allergens            []profileAllergenDTO `json:"allergens"`
}

func toDTO(s profile.Stored) profileDTO {
	d := profileDTO{
		ChildID: s.ChildID, CaseID: s.CaseID, DisplayName: s.DisplayName,
		DateOfBirth: s.DateOfBirth.UTC().Format(dateLayout),
		Sex:         s.Sex, LanguageID: s.LanguageID, RegionCulture: s.RegionCulture,
		CuisineCode: s.CuisineCode, DietType: s.DietType, Vegan: s.Vegan,
		ReligiousRestriction: s.ReligiousRestriction, BudgetBand: s.BudgetBand,
		MaxPrepTimeMin: s.MaxPrepTimeMin, MaxCookTimeMin: s.MaxCookTimeMin,
		CreatedBy: s.CreatedBy,
		// Never nil: a nil slice marshals to null, and a client rendering a list should
		// not need a null check to show "no allergens declared".
		Allergens: []profileAllergenDTO{},
	}
	for _, a := range s.Allergens {
		dto := profileAllergenDTO{
			Group: a.Group, Status: a.Status, Severity: a.Severity,
			Source: a.Source, EnteredBy: a.EnteredBy,
		}
		if a.LastReactionOn != nil {
			dto.LastReactionOn = a.LastReactionOn.UTC().Format(dateLayout)
		}
		d.Allergens = append(d.Allergens, dto)
	}
	return d
}

func fromDTO(d profileDTO) (profile.Stored, error) {
	dob, err := time.Parse(dateLayout, d.DateOfBirth)
	if err != nil {
		return profile.Stored{}, err
	}
	s := profile.Stored{
		ChildID: d.ChildID, CaseID: d.CaseID, DisplayName: d.DisplayName,
		DateOfBirth: dob, Sex: d.Sex, LanguageID: d.LanguageID,
		RegionCulture: d.RegionCulture, CuisineCode: d.CuisineCode,
		DietType: d.DietType, Vegan: d.Vegan,
		ReligiousRestriction: d.ReligiousRestriction, BudgetBand: d.BudgetBand,
		MaxPrepTimeMin: d.MaxPrepTimeMin, MaxCookTimeMin: d.MaxCookTimeMin,
		CreatedBy: d.CreatedBy,
	}
	for _, a := range d.Allergens {
		da := profile.DeclaredAllergen{
			Group: a.Group, Status: a.Status, Severity: a.Severity,
			Source: a.Source, EnteredBy: a.EnteredBy,
		}
		if a.LastReactionOn != "" {
			t, err := time.Parse(dateLayout, a.LastReactionOn)
			if err != nil {
				return profile.Stored{}, err
			}
			da.LastReactionOn = &t
		}
		s.Allergens = append(s.Allergens, da)
	}
	return s, nil
}

// profileVocabularies names each write-validated field and the query that yields the values
// the corpus actually recognises for it.
//
// Read from the database, never from a list written here. Every one of these vocabularies
// lives in a provider table that a re-import can change, and a hardcoded copy would drift
// from the corpus silently -- the accepted values would still be listed in an error message
// long after the workbook stopped using them. For the same reason none of these becomes a
// CHECK constraint: a migration-time constraint would fight the importer.
//
// region_focus rather than recipe_master.region_culture, because region_focus is the single
// place scope is decided (CLAUDE.md, "Region focus"); cuisine_option rather than the culture
// master, because the view is already gated on count(*) > 0 and the 8 orphan culture codes
// with zero recipes must not be accepted any more than a typo should be.
var profileVocabularies = []struct {
	field string
	value func(profile.Stored) string
	query string
}{
	{
		field: "diet_type",
		value: func(s profile.Stored) string { return s.DietType },
		// Capitalisation is part of the value: engine.dietPermits is keyed on the
		// provider's own casing, so "vegetarian" is not "Vegetarian" and used to survive
		// the write only to fail as a 500 at generation time, far from its cause.
		query: `SELECT DISTINCT diet_type FROM recipe_master WHERE diet_type <> '' ORDER BY 1`,
	},
	{
		field: "budget_band",
		value: func(s profile.Stored) string { return s.BudgetBand },
		query: `SELECT DISTINCT budget_band FROM recipe_master WHERE budget_band <> '' ORDER BY 1`,
	},
	{
		field: "region_culture",
		value: func(s profile.Stored) string { return s.RegionCulture },
		query: `SELECT region_culture FROM region_focus ORDER BY 1`,
	},
	{
		field: "cuisine_code",
		value: func(s profile.Stored) string { return s.CuisineCode },
		query: `SELECT culture_code FROM cuisine_option ORDER BY 1`,
	},
}

// sexValues mirrors child_profile's CHECK constraint in migration 0014. Kept beside the
// validator rather than read from the database because a CHECK is not a provider vocabulary:
// it cannot drift with a re-import, and there is no table to query for it.
var sexValues = []string{"male", "female", "other"}

// allergenStatusValues, allergenSeverityValues and allergenSourceValues mirror
// child_allergen's CHECK constraints in migration 0014, for the same reason as sexValues.
//
// status carries real weight rather than being a label: "confirmed" filters recipes out,
// "suspected" ranks them down and raises a review flag, and "resolved" does neither. An
// operator who mistypes it is asking for a different safety behaviour than they get, so it
// is rejected at the write rather than silently coerced.
var (
	allergenStatusValues   = []string{"confirmed", "suspected", "resolved"}
	allergenSeverityValues = []string{"mild", "systemic"}
	allergenSourceValues   = []string{"parent_reported", "clinician_documented"}
)

// validateProfileVocabularies returns a message naming the first field whose non-empty value
// the corpus does not recognise, and the values it does.
//
// An empty value stays legal: an operator who has not asked for a region is not the same as
// one who asked for a region that does not exist. A wrong one is rejected at the write
// rather than at generation, because the two failures it otherwise produces are both bad in
// their own way -- diet_type explodes as a 500 hours later, and region_culture and
// cuisine_code do not fail at all, they quietly produce a book ranked against nothing with
// no omission reported.
func (h *Handlers) validateProfileVocabularies(ctx context.Context, s profile.Stored) (string, error) {
	// sex is the one write-validated field whose vocabulary is not a corpus table. It is a
	// CHECK constraint in migration 0014 (`sex IN ('male', 'female', 'other')`), so the
	// constraint is the authority and this mirrors it rather than querying for it. Without
	// this, an operator entering "F" gets a 500 from a constraint violation instead of being
	// told the three values -- the same failure mode the vocabularies above exist to close.
	if s.Sex != "" && !slices.Contains(sexValues, s.Sex) {
		return fmt.Sprintf("sex %q is not a recognised value; accepted values are: %s",
			s.Sex, strings.Join(sexValues, ", ")), nil
	}

	for _, a := range s.Allergens {
		if !slices.Contains(allergenStatusValues, a.Status) {
			return fmt.Sprintf("allergen %q status %q is not a recognised value; accepted "+
				"values are: %s", a.Group, a.Status,
				strings.Join(allergenStatusValues, ", ")), nil
		}
		if a.Severity != "" && !slices.Contains(allergenSeverityValues, a.Severity) {
			return fmt.Sprintf("allergen %q severity %q is not a recognised value; accepted "+
				"values are: %s", a.Group, a.Severity,
				strings.Join(allergenSeverityValues, ", ")), nil
		}
		if !slices.Contains(allergenSourceValues, a.Source) {
			return fmt.Sprintf("allergen %q source %q is not a recognised value; accepted "+
				"values are: %s", a.Group, a.Source,
				strings.Join(allergenSourceValues, ", ")), nil
		}
	}

	for _, v := range profileVocabularies {
		value := v.value(s)
		if value == "" {
			continue
		}
		accepted, err := h.vocabulary(ctx, v.query)
		if err != nil {
			return "", fmt.Errorf("read accepted %s values: %w", v.field, err)
		}
		if slices.Contains(accepted, value) {
			continue
		}
		return fmt.Sprintf("%s %q is not a value this corpus carries; accepted values are: %s",
			v.field, value, strings.Join(accepted, ", ")), nil
	}
	return "", nil
}

func (h *Handlers) vocabulary(ctx context.Context, query string) ([]string, error) {
	rows, err := h.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// PutProfile creates or replaces one child's stored profile. profile.Save is an upsert,
// so PUT rather than POST: the same body sent twice leaves the same state.
//
// This endpoint writes only the fields the DTO carries. Growth measurements, preferences
// and clinical conditions are part of profile.Stored and are deliberately not settable
// here -- profile.Save clears and rewrites those child tables from the struct it is given,
// so accepting a partial body for them would silently delete a child's growth history on
// every profile edit. They need their own endpoints, which this does not add.
func (h *Handlers) PutProfile(w http.ResponseWriter, r *http.Request) {
	childID := chi.URLParam(r, "childID")
	var d profileDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "malformed profile body: "+err.Error())
		return
	}
	// The path is authoritative. A body naming a different child would otherwise write to
	// one id while the caller believes it wrote to another.
	if d.ChildID != "" && d.ChildID != childID {
		writeError(w, http.StatusBadRequest, "child_id in the body does not match the path")
		return
	}
	d.ChildID = childID

	s, err := fromDTO(d)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid profile field: "+err.Error())
		return
	}
	// Validated here rather than at generation time. A value the corpus does not carry is
	// a typo, and a typo caught at the write names the field that caused it; the same typo
	// caught later is either a 500 nobody can trace back to this request or, worse, a
	// successful book ranked against a region that does not exist.
	badField, err := h.validateProfileVocabularies(r.Context(), s)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile validation failed: "+err.Error())
		return
	}
	if badField != "" {
		writeError(w, http.StatusBadRequest, badField)
		return
	}
	if err := profile.Save(r.Context(), h.pool, s); err != nil {
		writeError(w, http.StatusInternalServerError, "profile save failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDTO(s))
}

// GetProfile returns the stored profile verbatim, without deriving anything from it.
func (h *Handlers) GetProfile(w http.ResponseWriter, r *http.Request) {
	s, err := profile.Load(r.Context(), h.pool, chi.URLParam(r, "childID"))
	if errors.Is(err, profile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no profile for that child id")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile load failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDTO(s))
}

// GetProfileEngineInput returns what the engine would actually receive for this child
// today, alongside every stored fact that did not survive the conversion.
//
// The dropped list is the point of the endpoint. A stored profile holds more than the
// engine query can express -- growth trends, allergen severity, expired acute conditions --
// and silently discarding those would make the console show a profile richer than the
// search it produced. Naming what was dropped is the honest-gap rule applied to a
// conversion rather than to missing data.
func (h *Handlers) GetProfileEngineInput(w http.ResponseWriter, r *http.Request) {
	s, err := profile.Load(r.Context(), h.pool, chi.URLParam(r, "childID"))
	if errors.Is(err, profile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no profile for that child id")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile load failed: "+err.Error())
		return
	}

	asOf := time.Now().UTC()
	cp, dropped, err := s.ToChildProfile(asOf)
	if errors.Is(err, profile.ErrInvalidProfile) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "engine input derivation failed: "+err.Error())
		return
	}
	if dropped == nil {
		dropped = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile": cp,
		"dropped": dropped,
		// Age is derived from this date, so the same profile read next month yields a
		// different query. Returning it makes the response reproducible.
		"as_of": asOf.Format(dateLayout),
	})
}
