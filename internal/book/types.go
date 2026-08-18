package book

import "time"

// Metadata carries the three release footer fields the template contract names
// (book_version, release_id, generation_date). It is not the provider schema's
// book_metadata object -- see the Book1 doc comment for why.
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

// Book1 is the render model for Book 1 -- the shape the templates consume, not the
// provider's wire format.
//
// It deliberately does not conform to MadamGY_Book1_JSON_Schema_V1.json. That schema is
// strict and requires a consultation_summary carrying reviewed_by and a consultation_date,
// a release object, and a profile_snapshot_id and generation_job_id from a generation
// pipeline this project does not have. Producing a conformant document today would mean
// writing a reviewer's name and a consultation date for a review that never happened, which
// is the one claim this project must never make. Conformance arrives with the generation-job
// and release layer, not before it.
type Book1 struct {
	Metadata            Metadata     `json:"book_metadata"`
	Child               ChildSummary `json:"child_profile"`
	ConsultationSummary []Row        `json:"consultation_summary"`
	Sections            []Section    `json:"sections"`
}

// RecipeCard mirrors the recipe_card definition in MadamGY_Book2_JSON_Schema_V1.json. Its
// required fields are recipe_id, recipe_version, title, meal_category_id, age_stage_ids,
// selection_reasons, ingredients, method_steps, serving, safety and review_status.
//
// RecipeVersion has no source column: recipe_master carries no version. It is populated from
// the import run's content hash for that table, which is a real, traceable version of the
// row as loaded rather than an invented number, and GAP-024 records that the provider does
// not version recipes.
type RecipeCard struct {
	RecipeID         string   `json:"recipe_id"`
	RecipeVersion    string   `json:"recipe_version"`
	Title            string   `json:"title"`
	MealCategoryID   string   `json:"meal_category_id"`
	AgeStageIDs      []string `json:"age_stage_ids"`
	SelectionReasons []string `json:"selection_reasons"`
	NutritionTags    []string `json:"nutrition_tags,omitempty"`
	PrepTimeMinutes  *int     `json:"prep_time_minutes"`
	CookTimeMinutes  *int     `json:"cook_time_minutes"`
	CostBand         *string  `json:"cost_band"`
	Ingredients      []string `json:"ingredients"`
	MethodSteps      []string `json:"method_steps"`
	TextureServing   *string  `json:"texture_serving"`
	Serving          string   `json:"serving"`
	Safety           string   `json:"safety"`
	ReviewStatus     string   `json:"review_status"`
	// MethodIsProviderBoilerplate marks the 6-unique-texts problem (GAP-001) on the card
	// itself, so a reader is told the steps are generic rather than discovering it.
	MethodIsProviderBoilerplate bool `json:"method_is_provider_boilerplate"`
}

type MealSection struct {
	MealCategoryID    string       `json:"meal_category_id"`
	Title             string       `json:"title"`
	TargetRecipeCount int          `json:"target_recipe_count"`
	Recipes           []RecipeCard `json:"recipes"`
	SelectionNote     *string      `json:"selection_note"`
}

// Book2 mirrors MadamGY_Book2_JSON_Schema_V1.json. RotationPlan is nullable there and is
// nil here whenever the selected recipes cannot fill seven days without repetition beyond
// what the provider's diversity target allows.
type Book2 struct {
	Metadata     Metadata      `json:"book_metadata"`
	Child        ChildSummary  `json:"child_recipe_profile"`
	MealSections []MealSection `json:"meal_sections"`
	RotationPlan *RotationPlan `json:"rotation_plan"`
}

type RotationPlan struct {
	Days []RotationDay `json:"days"`
}

type RotationDay struct {
	Day   string            `json:"day"`
	Meals map[string]string `json:"meals"`
}
