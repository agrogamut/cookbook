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
	DisplayName string `json:"display_name"`
	// DateOfBirth is printed as well as the age derived from it. The age is what the book
	// reasons with, but a parent checking the book is about their child reads the birth date,
	// and a clinician re-deriving an age needs the input rather than the result.
	DateOfBirth string `json:"date_of_birth"`
	AgeMonths   int    `json:"age_months"`
	AgeLabel    string `json:"age_label"`
	// Sex and Language are recorded on the profile and named by B1-001's own
	// personalization_inputs, so they are printed as stored. Sex never changes recipe
	// ranking -- the provider's sex_applicability is "All" on every row -- and it is on the
	// page as identity, not as an input to any selection.
	Sex      string `json:"sex,omitempty"`
	Language string `json:"language,omitempty"`
	// Photo is the cover portrait, when one was supplied. A pointer because most books have
	// none and the cover lays out differently rather than leaving a hole where one would be.
	Photo         *ChildPhoto `json:"photo,omitempty"`
	FoodPractice  string      `json:"food_practice,omitempty"`
	AllergyStatus string      `json:"allergy_status"`
	WeightKg      *string     `json:"weight_kg"`
	HeightCm      *string     `json:"height_cm"`
	MeasuredOn    string      `json:"measured_on,omitempty"`
}

// Section is one rendered block of Book 1, keyed to the provider's template id so the
// renderer picks the template the contract names rather than one this code invents.
type Section struct {
	BlockID    string `json:"block_id"`
	TemplateID string `json:"template_id"`
	BookOrder  int    `json:"book_order"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle,omitempty"`
	Rows       []Row  `json:"rows,omitempty"`
	// Growth carries B1-003's dated anthropometry table. It is a distinct shape rather than
	// more Rows because a monitoring table is one row per visit across several measured
	// columns, which Row's label/reference/note shape cannot hold.
	Growth []GrowthRow `json:"growth,omitempty"`
	// Domains carries the daily-life blocks: sleep, screens, teeth, toilet training,
	// activity, school, self-care, adolescent self-management.
	Domains []DailyDomain `json:"domains,omitempty"`
	// Illness carries the five illness-feeding situations.
	Illness []IllnessBlock `json:"illness,omitempty"`
	// Trackers are blank forms whose columns the provider declared. A block may carry
	// several: B1-013 prints one grid per monitoring parameter.
	Trackers []TrackerSpec `json:"trackers,omitempty"`
	// Stage is the child's feeding stage with the stages either side, for B1-005 to B1-008.
	Stage *StagePage `json:"stage,omitempty"`
	// Safety is the child's own allergy and choking card for B1-018.
	Safety *SafetyCard `json:"safety,omitempty"`
	// Refs is the evidence table for B1-022.
	Refs []EvidenceSource `json:"refs,omitempty"`
	// Part is the provider's own part letter (A-O) from book1_content_block. It groups the
	// contents page and places dividers; it is never printed as a label on its own, because
	// the workbook names no parts and "Part J" tells a reader nothing.
	Part string `json:"part,omitempty"`
	// Purpose is the block's own content_purpose, printed under the section heading. It is
	// the provider's sentence about why the page exists -- the closest thing to introductory
	// prose this book may carry, because they wrote it and this project did not.
	Purpose string `json:"purpose,omitempty"`
	// Covers is the block's parent_facing_output split into a list: the topics the page
	// addresses, in the provider's words.
	Covers  []string `json:"covers,omitempty"`
	Callout *Callout `json:"callout,omitempty"`
}

// DailyDomain is one daily-life module ready for the page. Every text field is the
// provider's, verbatim; an empty one renders as a writing line rather than as absent.
type DailyDomain struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	AgeContext string `json:"age_context,omitempty"`
	Reference  string `json:"reference,omitempty"`
	Goal       string `json:"goal,omitempty"`
	RedFlag    string `json:"red_flag,omitempty"`
	Referral   string `json:"referral,omitempty"`
	// AILimit is the provider's own prohibition on this row, printed on the page it
	// constrains. See DailyLifeModule.AILimit.
	AILimit string       `json:"ai_limit,omitempty"`
	Display string       `json:"display,omitempty"`
	Tracker *TrackerSpec `json:"tracker,omitempty"`
}

// IllnessBlock is one illness-feeding situation ready for the page.
type IllnessBlock struct {
	ID                string `json:"id"`
	Situation         string `json:"situation"`
	SupportiveMessage string `json:"supportive_message,omitempty"`
	WhatToMonitor     string `json:"what_to_monitor,omitempty"`
	RedFlags          string `json:"red_flags,omitempty"`
	// EngineLimit is the load-bearing string on the page. See IllnessFeedingBlock.EngineLimit.
	EngineLimit string `json:"engine_limit,omitempty"`
}

// TrackerSpec is a blank form: the provider's declared columns and a row count.
//
// The columns come from the block's own writable_fields, or from a monitoring template's
// reference/actual/date/notes columns. Reading those as the form's headers is following the
// provider's layout declaration, not inventing a layout -- which is the distinction that lets
// a blank tracker print under the no-generated-prose rule while drafted advice may not.
type TrackerSpec struct {
	Title     string   `json:"title,omitempty"`
	Parameter string   `json:"parameter,omitempty"`
	Reference string   `json:"reference,omitempty"`
	Frequency string   `json:"frequency,omitempty"`
	Columns   []string `json:"columns"`
	Rows      int      `json:"rows"`
	// Prefilled is rows whose leading cells the provider already supplies, used by the two
	// dashboard blocks. Each inner slice holds the filled cells in column order; the columns
	// beyond it render as writing lines.
	//
	// It exists because the first dashboard was eighteen identical rows of blank lines under
	// five headings -- a form with no indication of what to write on which line, when
	// book1_monitoring_template names the area and the reference for every one of those rows.
	// Rows is ignored when this is set: the row count is the data.
	Prefilled [][]string `json:"prefilled,omitempty"`
	// Alarm and Review are the provider's alarm_column and doctor_review_column: what makes
	// an entry concerning, and who reviews it. They print under the grid rather than as
	// columns, because a parent reading a filled row needs the threshold beside it, not an
	// empty box to tick.
	Alarm  string `json:"alarm,omitempty"`
	Review string `json:"review,omitempty"`
}

// StagePage is the provider's feeding guidance for the child's age, with the stages either
// side so B1-008's declared comparison table can show what changes next.
type StagePage struct {
	Current *AgeStage `json:"current"`
	Prev    *AgeStage `json:"prev,omitempty"`
	Next    *AgeStage `json:"next,omitempty"`
	// Facet is which of the four feeding blocks this page is, and it exists because the first
	// version did not have it: B1-005 through B1-008 all mapped to one template and printed
	// the same twenty-row stage table four times in a row. The workbook gives each of them a
	// different table_or_format -- "Daily target table", "Meal schedule table", "Age-specific
	// guidance + checklist", "Comparison table" -- so each takes the columns its own block
	// declares and no more. See stageFacet.
	Facet string `json:"facet"`
}

// SafetyCard is the child's own exclusions, printed so they are impossible to miss.
//
// Confirmed and Suspected are separate lists and must stay separate: a parent reading one
// merged list cannot tell which exclusions are diagnosed and which are being ruled out. The
// card explains exclusions and never offers a way to undo one -- steps 1 and 2 of the engine
// are hard filters with no override, and a printed page does not get to soften that.
type SafetyCard struct {
	Confirmed []string      `json:"confirmed"`
	Suspected []string      `json:"suspected,omitempty"`
	Rules     []Row         `json:"rules,omitempty"`
	Choking   []ChokingRule `json:"choking,omitempty"`
	// ReactionLog is the writable half B1-018 declares: "Reaction history/update".
	ReactionLog *TrackerSpec `json:"reaction_log,omitempty"`
}

// ChokingRule is one row of choking_texture_safety that applies at the child's age.
type ChokingRule struct {
	Food   string `json:"food"`
	Risk   string `json:"risk,omitempty"`
	Rule   string `json:"rule"`
	AgeFor string `json:"age_for,omitempty"`
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
// GrowthRow is one dated anthropometry record, printed exactly as a clinician recorded it.
//
// Every field is a formatted string, and empty means "not recorded at this visit" -- a real
// measurement never formats to empty, so the template can render a writing line for the gap
// without a second nil flag. A visit that weighed a child but did not measure head
// circumference must print a line in that column, never a zero.
//
// ZScores holds what a clinician entered, never anything computed here. Deriving a z-score
// needs the WHO reference tables and the growth-trend engine that B1-004 names and this
// project does not have, so an unrecorded z-score stays blank rather than being calculated.
type GrowthRow struct {
	MeasuredOn     string `json:"measured_on"`
	WeightKg       string `json:"weight_kg,omitempty"`
	HeightCm       string `json:"height_cm,omitempty"`
	HeadCircumCm   string `json:"head_circumference_cm,omitempty"`
	ZScores        string `json:"z_scores,omitempty"`
	Interpretation string `json:"interpretation,omitempty"`
	MeasuredBy     string `json:"measured_by,omitempty"`
}

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
	Metadata Metadata     `json:"book_metadata"`
	Child    ChildSummary `json:"child_profile"`
	Sections []Section    `json:"sections"`
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
