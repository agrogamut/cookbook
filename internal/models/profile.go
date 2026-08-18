// Package models holds types shared between the engine and the API so neither package
// has to reach into the other's internals.
package models

// ChildProfile is the operator's input to the engine. Every field is optional except
// AgeMonths; an unset field means "no preference", not "false" or "none" -- the engine
// must not treat a missing BudgetBand as "cheapest only".
type ChildProfile struct {
	AgeMonths int `json:"age_months"`

	// DietType matches recipe_master.diet_type exactly: "Vegetarian", "Non-vegetarian",
	// "Eggetarian". Empty means no diet-type filter is applied.
	DietType string `json:"diet_type,omitempty"`

	// Vegan is additional to DietType. There is no "Vegan" value in diet_type, so a
	// vegan profile must also set DietType = "Vegetarian"; the engine hard-excludes
	// recipes containing any ingredient from the seven animal-derived food groups on
	// top of the diet_type filter. See Task 4.
	Vegan bool `json:"vegan,omitempty"`

	// Allergens are allergen_mapping.allergen_group values the family has declared,
	// e.g. "Peanut", "Milk", "Egg". Hard filter, step 2, never relaxed.
	Allergens []string `json:"allergens,omitempty"`

	// SuspectedAllergens are allergen groups the family or clinician has raised but not
	// confirmed. AS-002 marks these hard_block = N, so they are a ranker input, never a
	// filter: unnecessary elimination is itself a recognised cause of faltering growth,
	// and treating every suspicion as confirmation is a different risk rather than the
	// cautious choice. See internal/engine/rank.go, applySuspectedAllergenRank.
	SuspectedAllergens []string `json:"suspected_allergens,omitempty"`

	// ClinicalFlags keys match clinical_rule_master.trigger_field exactly (e.g.
	// "Coeliac_Status", "CKD", "Eating_Disorder_Risk"). This is a deliberately open map
	// rather than a fixed struct: the trigger fields are provider data, not something
	// this codebase should hardcode as Go struct fields that drift from the workbook.
	ClinicalFlags map[string]string `json:"clinical_flags,omitempty"`

	// SpecialCareCondition is a special_care_condition_gate.condition_id (SC-DS, SC-CP,
	// SC-CHD, SC-CLP, SC-ASD, SC-ID). All six are STOP-REVIEW in the provider's
	// Special-Care master: the engine stops and names the required reviewer rather than
	// ranking recipes, because the feeding decision for these children turns on swallowing
	// assessment, feeding route and prescribed texture, none of which this project holds.
	//
	// Blocking needs no clinical sign-off because it is the conservative direction: a
	// block never puts an unsafe recipe in front of an operator. Serving a special-care
	// recipe would need sign-off, and nothing does that.
	SpecialCareCondition string `json:"special_care_condition,omitempty"`

	// ClinicalMarker is the operator's explicit nutrition-target choice when one of
	// NT02-NT12 applies (e.g. "growth_faltering", "iron_deficiency"). Empty means let
	// step 5 auto-select between NT01 (age 6-23mo) and the NT00 fallback. See Task 5.
	ClinicalMarker string `json:"clinical_marker,omitempty"`

	// RegionCulture is one of the 9 recipe_master.region_culture values. Empty means
	// use region_focus's default tier ordering (Task 6). An explicit choice here beats
	// the project's West-Bengal-first default -- see CLAUDE.md, "Region focus".
	RegionCulture string `json:"region_culture,omitempty"`

	// CuisineCode is a culture_location_master.culture_code value, must resolve via
	// cuisine_option (so it is guaranteed to have at least one recipe). Optional,
	// ranker only.
	CuisineCode string `json:"cuisine_code,omitempty"`

	MealType       string `json:"meal_type,omitempty"`       // recipe_master.meal_type
	BudgetBand     string `json:"budget_band,omitempty"`      // recipe_master.budget_band
	MaxPrepTimeMin int    `json:"max_prep_time_min,omitempty"` // 0 = no limit
	MaxCookTimeMin int    `json:"max_cook_time_min,omitempty"` // 0 = no limit

	Limit int `json:"limit,omitempty"` // 0 = use meal_category_target.default_target_recipes
}
