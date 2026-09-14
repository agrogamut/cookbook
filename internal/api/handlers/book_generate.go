package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/profile"
)

// maxGenerateBody caps the request. Generous because the body may carry a cover photograph
// as a data URI, which base64 inflates by about a third; the image itself is checked against
// its own smaller limit in book.ParsePhoto, and this only stops a body large enough to be a
// problem before anything gets to look at it.
const maxGenerateBody = 16 << 20

// generateRequest is a whole child, supplied inline.
//
// Deliberately not a child id. Generation used to require a profile saved to the database
// first, which made an operator do two steps and created a stored record of a child for what
// is often a single consultation. The inputs are the same either way -- this is the same
// profile shape the write path validates -- so the difference is only whether it is kept.
//
// Nothing here is persisted. A child's name, birth date and photograph are the most personal
// data this system touches, and the honest default for a document generated once and handed
// over is to hold them for the length of the request.
type generateRequest struct {
	profileDTO

	// Growth measurements print in Book 1's monitoring table. Supplied per request rather
	// than read from a stored history, because there is no stored child here to have one.
	Growth []generateGrowthDTO `json:"growth,omitempty"`

	// Conditions carry the clinical flags and the special-care stop gate.
	Conditions []generateConditionDTO `json:"conditions,omitempty"`

	// PhotoDataURI is a cover portrait as a base64 data URI. Validated by book.ParsePhoto.
	PhotoDataURI string `json:"photo_data_uri,omitempty"`
	PhotoCaption string `json:"photo_caption,omitempty"`

	// ParentsPhotoDataURI is the back cover's portrait, of the parents rather than the
	// child. Validated by book.ParsePhoto, same as PhotoDataURI.
	ParentsPhotoDataURI string `json:"parents_photo_data_uri,omitempty"`
	ParentsPhotoCaption string `json:"parents_photo_caption,omitempty"`

	// PrescriptionPhotoDataURI is a photograph or scan of a prescription a doctor has
	// already written and signed on paper. Validated by book.ParsePhoto, same as
	// PhotoDataURI -- this is a real document being attached, not a diagnosis or a dose
	// this service computes. See Metadata.PrescriptionPhoto's doc comment.
	PrescriptionPhotoDataURI string `json:"prescription_photo_data_uri,omitempty"`
	PrescriptionPhotoCaption string `json:"prescription_photo_caption,omitempty"`
}

type generateGrowthDTO struct {
	MeasuredOn          string   `json:"measured_on"`
	WeightKg            *float64 `json:"weight_kg,omitempty"`
	HeightCm            *float64 `json:"height_cm,omitempty"`
	HeadCircumferenceCm *float64 `json:"head_circumference_cm,omitempty"`
	BMIForAgeZ          *float64 `json:"bmi_for_age_z,omitempty"`
	WeightForAgeZ       *float64 `json:"weight_for_age_z,omitempty"`
	HeightForAgeZ       *float64 `json:"height_for_age_z,omitempty"`
	Interpretation      string   `json:"interpretation,omitempty"`
	MeasuredBy          string   `json:"measured_by,omitempty"`
}

type generateConditionDTO struct {
	TriggerField string `json:"trigger_field"`
	FlagValue    string `json:"flag_value"`
	Class        string `json:"class,omitempty"`
	OnsetDate    string `json:"onset_date,omitempty"`
	// ExpiresAfterDays lets an acute condition age out, the same way a stored one does. An
	// acute flag with no expiry is applied as entered and reported as an omission.
	ExpiresAfterDays *int `json:"expires_after_days,omitempty"`
}

// toStored turns the request into the same profile.Stored the database path produces, so the
// engine, the stop gate and both assemblers cannot tell the two apart. A second code path
// that built the profile differently would be a second set of rules to keep in step.
func (g generateRequest) toStored() (profile.Stored, error) {
	// Checked before fromDTO, which would otherwise surface Go's own time-parse text --
	// `cannot parse "01-05-2022" as "2006"` names neither the field nor the format an
	// operator is supposed to type, and this is the one field with no workable default.
	if strings.TrimSpace(g.DateOfBirth) == "" {
		return profile.Stored{}, fmt.Errorf(
			"date_of_birth is required: every book states the child's age")
	}

	s, err := fromDTO(g.profileDTO)
	if err != nil {
		return profile.Stored{}, fmt.Errorf(
			"date_of_birth %q is not a YYYY-MM-DD date", g.DateOfBirth)
	}

	for i, m := range g.Growth {
		on, err := time.Parse(dateLayout, m.MeasuredOn)
		if err != nil {
			return profile.Stored{}, fmt.Errorf(
				"growth measurement %d: measured_on %q is not a YYYY-MM-DD date", i+1, m.MeasuredOn)
		}
		s.Growth = append(s.Growth, profile.GrowthMeasurement{
			MeasuredOn: on, WeightKg: m.WeightKg, HeightCm: m.HeightCm,
			HeadCircumferenceCm: m.HeadCircumferenceCm,
			BMIForAgeZ:          m.BMIForAgeZ, WeightForAgeZ: m.WeightForAgeZ,
			HeightForAgeZ:  m.HeightForAgeZ,
			Interpretation: m.Interpretation, MeasuredBy: m.MeasuredBy,
		})
	}
	// Newest first, matching what profile.Load returns from the database. Book 1's growth
	// table reverses it for display; anything else reading Growth[0] expects the latest.
	sortGrowthNewestFirst(s.Growth)

	for _, c := range g.Conditions {
		cc := profile.ClinicalCondition{
			TriggerField: c.TriggerField, FlagValue: c.FlagValue, Class: c.Class,
		}
		cc.ExpiresAfterDays = c.ExpiresAfterDays
		if c.OnsetDate != "" {
			on, err := time.Parse(dateLayout, c.OnsetDate)
			if err != nil {
				return profile.Stored{}, fmt.Errorf(
					"condition %s: onset_date %q is not a YYYY-MM-DD date", c.TriggerField, c.OnsetDate)
			}
			cc.OnsetDate = &on
		}
		s.Conditions = append(s.Conditions, cc)
	}
	return s, nil
}

func sortGrowthNewestFirst(ms []profile.GrowthMeasurement) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && ms[j].MeasuredOn.After(ms[j-1].MeasuredOn); j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

// decodeGenerate reads and validates the request, writing the response itself on failure.
//
// The same vocabulary check the write path applies. A profile that would be rejected as a
// saved record must be rejected as a generated book too: the failure it prevents -- a region
// the corpus does not carry, silently producing a book ranked against nothing -- is about the
// book, not about the row.
func (h *Handlers) decodeGenerate(w http.ResponseWriter, r *http.Request) (profile.Stored, *book.ChildPhoto, *book.ChildPhoto, *book.ChildPhoto, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxGenerateBody)

	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body: "+err.Error())
		return profile.Stored{}, nil, nil, nil, false
	}

	s, err := req.toStored()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return profile.Stored{}, nil, nil, nil, false
	}
	if msg, err := h.validateProfileVocabularies(r.Context(), s); err != nil {
		writeError(w, http.StatusInternalServerError, "vocabulary check failed: "+err.Error())
		return profile.Stored{}, nil, nil, nil, false
	} else if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return profile.Stored{}, nil, nil, nil, false
	}

	photo, err := book.ParsePhoto(req.PhotoDataURI, req.PhotoCaption)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return profile.Stored{}, nil, nil, nil, false
	}
	parentsPhoto, err := book.ParsePhoto(req.ParentsPhotoDataURI, req.ParentsPhotoCaption)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return profile.Stored{}, nil, nil, nil, false
	}
	prescriptionPhoto, err := book.ParsePhoto(req.PrescriptionPhotoDataURI, req.PrescriptionPhotoCaption)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return profile.Stored{}, nil, nil, nil, false
	}
	return s, photo, parentsPhoto, prescriptionPhoto, true
}

// slugOrDefault turns a child's name into something safe to put in a filename.
//
// A downloaded file's name reaches a filesystem and a Content-Disposition header, so it is
// built from an allowlist rather than by removing the characters currently known to cause
// trouble: anything that is not a letter, digit or dash becomes a dash. Non-ASCII goes too,
// which loses a Bengali name in the filename -- the book itself still carries it correctly,
// and a file that saves everywhere beats one that renders its name properly on some systems
// and fails to save on others.
func slugOrDefault(name, fallback string) string {
	var b strings.Builder
	lastDash := true // leading dashes never start the slug
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return fallback
	}
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	return out
}

// BookGenerate runs one generation from inline inputs and returns both books.
//
// The console's primary action. No child id, no saved profile, no second step.
func (h *Handlers) BookGenerate(w http.ResponseWriter, r *http.Request) {
	s, photo, parentsPhoto, prescriptionPhoto, ok := h.decodeGenerate(w, r)
	if !ok {
		return
	}
	resp, _, ok := h.renderSetWithPhotos(w, r, s, photo, parentsPhoto, prescriptionPhoto)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// BookGenerateOne runs one generation from inline inputs and prints a single book.
//
// The set is still assembled whole -- both books come from one profile read at one instant --
// but only the requested half is printed. An operator who wants Book 2 alone should not wait
// for Book 1's print.
func (h *Handlers) BookGenerateOne(w http.ResponseWriter, r *http.Request) {
	which := chi.URLParam(r, "book")
	if which != "book1" && which != "book2" {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown book %q: must be book1 or book2", which))
		return
	}
	s, photo, parentsPhoto, prescriptionPhoto, ok := h.decodeGenerate(w, r)
	if !ok {
		return
	}
	resp, set, ok := h.renderSetWithPhotos(w, r, s, photo, parentsPhoto, prescriptionPhoto)
	if !ok {
		return
	}

	htmlDoc, meta := resp.Book1, set.Book1.Metadata
	if which == "book2" {
		htmlDoc, meta = resp.Book2, set.Book2.Metadata
	}

	pdf, err := book.PrintPDF(r.Context(), []byte(htmlDoc), meta)
	if err != nil {
		if errors.Is(err, book.ErrChromiumUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "pdf renderer unavailable: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "pdf render failed for "+which+": "+err.Error())
		return
	}

	name := slugOrDefault(s.DisplayName, "book")
	w.Header().Set(bookOmissionHeader, joinOmissions(omissionsFor(which, resp)))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", name+"-"+which+".pdf"))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

// omissionsFor is what one book leaves out: its own omissions plus the profile-level ones,
// which hold for both. A single download must not lose the profile facts just because it
// carries one book -- a suspected allergen is a fact about the child either way.
func omissionsFor(which string, resp bookSetResponse) []string {
	out := append([]string{}, resp.ProfileOmissions...)
	if which == "book1" {
		return append(out, resp.Book1Omissions...)
	}
	return append(out, resp.Book2Omissions...)
}

// BookGenerateZip runs one generation from inline inputs and returns both printed PDFs.
func (h *Handlers) BookGenerateZip(w http.ResponseWriter, r *http.Request) {
	s, photo, parentsPhoto, prescriptionPhoto, ok := h.decodeGenerate(w, r)
	if !ok {
		return
	}
	resp, set, ok := h.renderSetWithPhotos(w, r, s, photo, parentsPhoto, prescriptionPhoto)
	if !ok {
		return
	}

	// A generated child has no id to name the file after, so the child's own name is used,
	// slugged. Falls back to "books" rather than producing a file called "-books.zip".
	h.writeBookZip(w, r, slugOrDefault(s.DisplayName, "books"), resp, set)
}

// printedBook is one book's print result inside a generate.printed response. Exactly one of
// PDF and Error is set. Go's JSON encoder writes a []byte as base64, which is what the
// console decodes.
type printedBook struct {
	PDF   []byte      `json:"pdf,omitempty"`
	Error *printError `json:"error,omitempty"`
}

// printError keeps the split the single-book route makes with its status codes, now that
// both prints share one 200: "unavailable" is a missing browser, an install problem the HTML
// preview stands in for; "print-failed" is a browser that was there and failed, which may be
// about the document and must reach the operator as an error.
type printError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type printedSetResponse struct {
	bookSetResponse
	Book1PDF printedBook `json:"book1_print"`
	Book2PDF printedBook `json:"book2_print"`
}

// BookGeneratePrinted runs one generation and returns both books' HTML and both printed PDFs.
//
// The console's action. It used to call /generate and then generate/book1.pdf and
// generate/book2.pdf, and each of those three requests assembled the whole set again -- every
// Gemini drafting and translation call, three times per click, for one book set. Against a
// per-day request quota on the drafting model that tripled what a click cost. One request,
// one assembly, two prints of the same bytes also means the PDF on screen and the HTML beside
// it can no longer come from two different drafting runs.
//
// A print failure does not fail the response. The set assembled, and its HTML genuinely
// works as a preview, so each book's print result is reported next to it and the caller
// decides; that is the same per-book independence the console had when the prints were two
// settled requests. Assembly failure is still a 500, since then there is nothing to show.
func (h *Handlers) BookGeneratePrinted(w http.ResponseWriter, r *http.Request) {
	s, photo, parentsPhoto, prescriptionPhoto, ok := h.decodeGenerate(w, r)
	if !ok {
		return
	}
	resp, set, ok := h.renderSetWithPhotos(w, r, s, photo, parentsPhoto, prescriptionPhoto)
	if !ok {
		return
	}

	out := printedSetResponse{bookSetResponse: resp}
	out.Book1PDF = printOne(r, "book1", resp.Book1, set.Book1.Metadata)
	out.Book2PDF = printOne(r, "book2", resp.Book2, set.Book2.Metadata)
	writeJSON(w, http.StatusOK, out)
}

func printOne(r *http.Request, which, htmlDoc string, meta book.Metadata) printedBook {
	pdf, err := book.PrintPDF(r.Context(), []byte(htmlDoc), meta)
	switch {
	case err == nil:
		return printedBook{PDF: pdf}
	case errors.Is(err, book.ErrChromiumUnavailable):
		return printedBook{Error: &printError{Kind: "unavailable",
			Message: "pdf renderer unavailable: " + err.Error()}}
	default:
		return printedBook{Error: &printError{Kind: "print-failed",
			Message: "pdf render failed for " + which + ": " + err.Error()}}
	}
}
