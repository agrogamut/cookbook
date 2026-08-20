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

	// StartsPart marks the first rendered block of each provider part, plus the blocks named in
	// pagepolicy.go. It is the only thing that forces a page break in Book 1.
	//
	// Not derived in the template by comparing this section's Part against the previous one's:
	// a part whose every block was omitted for this child must not leave its successor
	// unmarked, and a part whose first block was omitted must still start a sheet at whichever
	// of its blocks did render. Only the final, post-omission slice knows which those are, and
	// the template never sees the blocks that were dropped.
	StartsPart bool `json:"starts_part,omitempty"`

	// Widths is one percentage per column for the block's own table, where it has one -- the
	// milestone tables of B1-011 and B1-014. Same reason as TrackerSpec.Widths.
	Widths []int `json:"widths,omitempty"`
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
	// Widths is one percentage per column, computed from what the column holds. Fixed table
	// layout gives every column the same width otherwise, and a uniform column narrower than
	// the longest word in it breaks that word. See colwidth.go.
	Widths []int `json:"widths,omitempty"`
}

// FlowRows and TailRows split a tracker's blank rows into the part that may break anywhere and
// the tail that moves whole.
//
// Chromium honours break-inside: avoid on a tbody and ignores CSS orphans and widows on table
// rows entirely, so a blank form splits anywhere: the food-diversity grid put three writing
// lines and a repeated column header alone on Book 1 page 42, with 92% of the sheet blank.
//
// Only the tail is protected, and that is the whole design. Grouping every row into fours was
// tried first and measured worse on printed sheets -- a group that will not fit in what is left
// of a page moves whole, so every grid gained up to 26mm of waste and Book 1 went from sixteen
// underfilled pages to twenty-one, losing the section pairings that breaking on parts had just
// won. Protecting the tail alone costs nothing when the form fits, because there is no break to
// resolve, and it is the only case that was ever bad: a break inside the body of a form leaves a
// usable continuation with its header repeated, while a break four rows from the end leaves a
// stub nobody can use on a sheet nobody wanted to print.
//
// Blank grids only. A prefilled grid is one row per provider row -- eighteen on the dashboards
// -- and its rows carry their own labels, so a split leaves both halves readable.
const trackerTailRows = 4

// FlowRows is the leading rows, free to break anywhere.
func (t TrackerSpec) FlowRows() []struct{} {
	if len(t.Prefilled) > 0 || t.Rows <= trackerTailRows {
		return nil
	}
	return make([]struct{}, t.Rows-trackerTailRows)
}

// TailRows is the trailing rows, which move together.
func (t TrackerSpec) TailRows() []struct{} {
	if len(t.Prefilled) > 0 || t.Rows <= 0 {
		return nil
	}
	return make([]struct{}, min(t.Rows, trackerTailRows))
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

// ContentsGroup is one heading on Book 1's contents page and the pages under it.
//
// The provider's own two levels: book1_content_block.section is the group ("Vaccination",
// "Common Illness Feeding") and .subsection is the page within it ("IAP-ACVIP 2025 schedule",
// "Post-vaccination feeding & comfort"). Several blocks share a section, which is why the
// first version's contents page listed "Vaccination" twice and "Personal Nutrition Target"
// twice with nothing to tell them apart.
type ContentsGroup struct {
	Title string   `json:"title"`
	Pages []string `json:"pages"`
}

// Contents groups the rendered sections for the contents page, in book order.
//
// Built from the sections that actually rendered, never from the block list: this book omits
// blocks deliberately -- B1-004 always, the age-inapplicable ones per child, and anything
// that resolved to no content -- and a contents entry for a page the book does not contain
// sends a reader hunting for nothing.
//
// Consecutive runs, not a map: the sections are already in book_order and grouping by key
// would reorder them. A section title that recurred after an interruption would correctly
// open a second group, because in book order it is a second place in the book.
func (b Book1) Contents() []ContentsGroup {
	var out []ContentsGroup
	for _, s := range b.Sections {
		page := s.Subtitle
		if page == "" {
			page = s.Title
		}
		if n := len(out); n > 0 && out[n-1].Title == s.Title {
			out[n-1].Pages = append(out[n-1].Pages, page)
			continue
		}
		out = append(out, ContentsGroup{Title: s.Title, Pages: []string{page}})
	}
	return out
}

// SectionCount is how many sections rendered, for the cover.
//
// A method rather than {{ len .Sections }} in the template: html/template's len fails hard on
// an absent field, and the renderer is deliberately callable with nil data (the provisional
// banner and palette tests render every book kind with no book at all, to prove the base
// template carries them). A method on the type degrades to nothing instead of erroring.
func (b Book1) SectionCount() int { return len(b.Sections) }

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

	// Number is the recipe's position in the whole book, counted across chapters from 1.
	//
	// It exists because a printed contents page here cannot carry page numbers: Chromium
	// assigns those at print time and the template has no way to learn them. A stable
	// per-book recipe number is the navigation key instead -- printed large on the card and
	// beside the entry in the contents -- so "recipe 7" finds the same page for every reader
	// of the same book. It is a position in this book, never an identifier of the recipe;
	// RecipeID is that and is printed alongside.
	Number int `json:"number"`

	// Mark is the drawn illustration of this recipe's dish format, or nil.
	//
	// A drawing, never a photograph: no photograph of any recipe in this corpus exists
	// (GAP-025) and the nearest available images are of other people's dishes under an
	// unstated licence. The full reasoning, including why the external corpus's image-url
	// column is not a source, is in marks.go.
	Mark *DishMark `json:"mark,omitempty"`

	// RegionCulture is the recipe's own Region_Culture, printed as the card's kicker. It is
	// the provider's value, not the family's stated region -- the two agree on most cards in
	// a region-matched book and a reader is entitled to see which recipe came from where.
	RegionCulture string `json:"region_culture,omitempty"`

	// Meta is the production strip: prep, cook, serving, cost band, texture. Built by
	// recipeMeta from the columns above so the template iterates one list instead of
	// branching on five nullable fields.
	Meta []RecipeMeta `json:"meta,omitempty"`

	// Nutrition is nil for a recipe with no recomputed row. See RecipeNutrition.
	Nutrition *RecipeNutrition `json:"nutrition,omitempty"`
}

// RecipeNutrition is one recipe's per-serving figures, rebuilt from IFCT-corrected
// ingredient values by recipe_nutrition_recomputed and scaled to the serving the card
// states. Every field is derived, Formula names the computation, and Coverage says what
// fraction of the recipe's mass is backed by a measured IFCT value rather than the
// provider's group placeholder -- so a partly-verified total is never presented as measured.
//
// A pointer on the card, because a recipe with no recomputed row prints no figures at all
// rather than zeros. Zero calories is a claim; an absent panel is the truth.
type RecipeNutrition struct {
	EnergyKcal string `json:"energy_kcal"`
	ProteinG   string `json:"protein_g"`
	IronMg     string `json:"iron_mg"`
	CalciumMg  string `json:"calcium_mg"`
	// Coverage is the fraction of recipe mass on a verified IFCT value, 0..1, and
	// CoveragePct the same as a whole-number percentage for the page.
	Coverage      float64 `json:"ingredient_coverage"`
	CoveragePct   int     `json:"ingredient_coverage_pct"`
	FullyVerified bool    `json:"fully_verified"`
	// BasisG is the mass these figures are totals for: the listed ingredients, summed.
	//
	// They are deliberately NOT divided down to recipe_master.serving_size_g. On sampled
	// recipes the stated serving (308 g) exceeds the whole ingredient mass (205 g), because
	// the serving includes the water the method adds and the ingredient list does not. No
	// column documents that relationship, so scaling by serving/ingredient-mass would invent
	// one. The panel states the basis it actually has and lets the serving size stand beside
	// it as its own separate fact.
	BasisG  string `json:"basis_g"`
	Formula string `json:"formula"`
}

// RecipeMeta is the strip of production facts a cook reads before starting: how long it
// takes, what it costs, what texture it lands at. Every value is a recipe_master column that
// loadRecipeCards has always read and the card template never printed.
type RecipeMeta struct {
	Label string `json:"label"`
	Value string `json:"value"`
	// Mono marks a value set in the identity/quantity face rather than in prose.
	Mono bool `json:"mono"`
}

type MealSection struct {
	MealCategoryID    string       `json:"meal_category_id"`
	Title             string       `json:"title"`
	TargetRecipeCount int          `json:"target_recipe_count"`
	Recipes           []RecipeCard `json:"recipes"`
	// Number is the chapter's position in this book, from 1. Like RecipeCard.Number it is a
	// position rather than an identity, and it is what the chapter opener prints as its
	// display numeral.
	Number int `json:"number"`
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

// RecipeCount is the number of recipes across every chapter. A method rather than a field
// because it is a count of what is already in the struct, and a stored copy is one more thing
// that can disagree with the book it describes.
func (b Book2) RecipeCount() int {
	n := 0
	for _, s := range b.MealSections {
		n += len(s.Recipes)
	}
	return n
}

// ChapterCount is how many meal chapters rendered. A method for the same reason as
// Book1.SectionCount.
func (b Book2) ChapterCount() int { return len(b.MealSections) }

// RecipeDataVersion is the import content hash every card was built from, for the imprint
// page. It is read off the first card rather than stored separately because it is one value
// for the whole run -- see RecipeCard.RecipeVersion and GAP-024. Empty for a book with no
// recipes, and the imprint prints nothing rather than a label with a blank after it.
func (b Book2) RecipeDataVersion() string {
	for _, s := range b.MealSections {
		for _, r := range s.Recipes {
			return r.RecipeVersion
		}
	}
	return ""
}

type RotationPlan struct {
	Days []RotationDay `json:"days"`
}

type RotationDay struct {
	Day   string            `json:"day"`
	Meals map[string]string `json:"meals"`
}
