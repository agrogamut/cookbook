package book

import "time"

// Omission scope markers. An assembler reports two different things through one skip slice:
// a whole unit of the book that is absent, and a note about rows left out of a unit that did
// render. The conservation checks count only the first kind, and they match on these
// constants rather than on the message's opening words -- so the human-readable remainder
// stays free to be rewritten without silently breaking the accounting.
const (
	omissionBlock        = "[block] "
	omissionMealCategory = "[meal category] "
)

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
	Callout    *Callout `json:"callout,omitempty"`
}

// Row is one line of a Book 1 table: what the block is about, the approved reference for it,
// and the personalized note built from the child's own record. The child's observed value is
// deliberately absent -- every block that ships today prints a writing line for it, because
// this project observed no measurement and a pre-filled column would read as a finding. The
// field returns with the growth-comparison page that has a verified value to put in it.
type Row struct {
	Label     string `json:"label"`
	Reference string `json:"reference"`
	Note      string `json:"note,omitempty"`
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
	Sections            []Section    `json:"sections"`
}

// IngredientLine is one ingredient on a recipe card. Bengali is separate from Name rather
// than concatenated into one string so the template can wrap it in a --font-indic span
// without parsing text back out of it. It is empty whenever ingredient_master carries no
// Bengali name for that ingredient_id -- never a transliteration, which this project has no
// verified source for and would be an invented value.
type IngredientLine struct {
	Name     string `json:"name"`
	Bengali  string `json:"bengali_name,omitempty"`
	Quantity string `json:"quantity,omitempty"`
}

// RecipeCard mirrors the recipe_card definition in MadamGY_Book2_JSON_Schema_V1.json. Its
// required fields are recipe_id, recipe_version, title, meal_category_id, age_stage_ids,
// selection_reasons, ingredients, method_steps, serving, safety and review_status.
// Ingredients is a simplified shape next to the schema's object (ingredient_id, display_name,
// quantity, unit, household_measure...): this project has no household-measure or unit data
// to put in the missing fields, so it renders what it actually has rather than a
// schema-conformant object with invented fields.
//
// RecipeVersion has no source column: recipe_master carries no version. It is populated from
// the import run's content hash for that table, which is a real, traceable version of the
// row as loaded rather than an invented number, and GAP-024 records that the provider does
// not version recipes.
type RecipeCard struct {
	RecipeID         string           `json:"recipe_id"`
	RecipeVersion    string           `json:"recipe_version"`
	Title            string           `json:"title"`
	MealCategoryID   string           `json:"meal_category_id"`
	AgeStageIDs      []string         `json:"age_stage_ids"`
	SelectionReasons []string         `json:"selection_reasons"`
	NutritionTags    []string         `json:"nutrition_tags,omitempty"`
	PrepTimeMinutes  *int             `json:"prep_time_minutes"`
	CookTimeMinutes  *int             `json:"cook_time_minutes"`
	CostBand         *string          `json:"cost_band"`
	Ingredients      []IngredientLine `json:"ingredients"`
	MethodSteps      []string         `json:"method_steps"`
	TextureServing   *string          `json:"texture_serving"`
	Serving          string           `json:"serving"`
	Safety           string           `json:"safety"`
	ReviewStatus     string           `json:"review_status"`
	// MethodIsProviderBoilerplate marks the 6-unique-texts problem (GAP-001) on the card
	// itself, so a reader is told the steps are generic rather than discovering it.
	MethodIsProviderBoilerplate bool `json:"method_is_provider_boilerplate"`
}

type MealSection struct {
	MealCategoryID    string       `json:"meal_category_id"`
	Title             string       `json:"title"`
	TargetRecipeCount int          `json:"target_recipe_count"`
	Recipes           []RecipeCard `json:"recipes"`
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
