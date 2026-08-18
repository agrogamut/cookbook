package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
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
