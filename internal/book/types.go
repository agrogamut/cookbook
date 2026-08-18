package book

import "time"

// Metadata is the book_metadata object both provider schemas require, plus the three
// release footer fields the template contract names (book_version, release_id,
// generation_date).
type Metadata struct {
	Title          string    `json:"title"`
	BookVersion    string    `json:"book_version"`
	ReleaseID      string    `json:"release_id"`
	GenerationDate time.Time `json:"generation_date"`
	Language       string    `json:"language"`
	// ReviewStatus is the provider's own value, carried verbatim onto the page. It is a
	// string rather than a bool because "Draft - Culinary/Nutrition/Clinical Review
	// Required" says more than false does.
	ReviewStatus string `json:"review_status"`
}

// ChildSummary is the personalization the provider's prototype actually relies on: the
// child's own recorded values, printed next to the approved reference. Every field is a
// stored measurement or a stored declaration. Nothing here is computed except AgeMonths,
// which is derived from date of birth by internal/profile.
type ChildSummary struct {
	DisplayName   string  `json:"display_name"`
	AgeMonths     int     `json:"age_months"`
	AgeLabel      string  `json:"age_label"`
	FeedingStage  string  `json:"feeding_stage,omitempty"`
	FoodPractice  string  `json:"food_practice,omitempty"`
	AllergyStatus string  `json:"allergy_status"`
	WeightKg      *string `json:"weight_kg"`
	HeightCm      *string `json:"height_cm"`
	MeasuredOn    string  `json:"measured_on,omitempty"`
}

// Section is one rendered block of Book 1, keyed to the provider's template id so the
// renderer picks the template the contract names rather than one this code invents.
type Section struct {
	BlockID    string   `json:"block_id"`
	TemplateID string   `json:"template_id"`
	BookOrder  int      `json:"book_order"`
	Title      string   `json:"title"`
	Subtitle   string   `json:"subtitle,omitempty"`
	Rows       []Row    `json:"rows,omitempty"`
	Cards      []Card   `json:"cards,omitempty"`
	Callout    *Callout `json:"callout,omitempty"`
}

// Row is the comparison shape B1-COMPARE-01 draws and that pages 3, 5 and 6 of the Book 1
// prototype all share: reference, the child's actual, and what to do next. Actual is a
// pointer because an unrecorded measurement renders as a writing line, never as a zero.
type Row struct {
	Label     string  `json:"label"`
	Reference string  `json:"reference"`
	Actual    *string `json:"actual"`
	Note      string  `json:"note,omitempty"`
}

type Card struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

// Severity is "info" or "warning". The contract sets clinical_warning_visibility to high and
// asks that colour never be the only carrier of meaning, so the template prints the severity
// as a word as well as a colour.
type Callout struct {
	Severity string `json:"severity"`
	Heading  string `json:"heading"`
	Body     string `json:"body"`
}

// Book1 mirrors MadamGY_Book1_JSON_Schema_V1.json.
type Book1 struct {
	Metadata            Metadata     `json:"book_metadata"`
	Child               ChildSummary `json:"child_profile"`
	ConsultationSummary []Row        `json:"consultation_summary"`
	Sections            []Section    `json:"sections"`
}
