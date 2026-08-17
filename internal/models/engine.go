package models

// StepResult records what one engine step did to the candidate pool, so the "why this
// result" panel can show every step rather than just the final list.
type StepResult struct {
	Step          int               `json:"step"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"` // "hard_filter" | "ranker" | "target" | "escalation"
	CandidatesIn  int               `json:"candidates_in"`
	CandidatesOut int               `json:"candidates_out"`
	Note          string            `json:"note,omitempty"`
	Excluded      []ExclusionReason `json:"excluded,omitempty"`
}

// ExclusionReason names one recipe a step removed and why, capped by the caller (Task 7)
// so a step that removes hundreds of recipes doesn't bloat the response -- the count in
// StepResult is always exact even when Excluded is truncated.
type ExclusionReason struct {
	RecipeID   string `json:"recipe_id"`
	RecipeName string `json:"recipe_name"`
	Reason     string `json:"reason"`
}

// RankedRecipe is one row of the final ordered result.
type RankedRecipe struct {
	RecipeID       string  `json:"recipe_id"`
	RecipeName     string  `json:"recipe_name"`
	RegionCulture  string  `json:"region_culture"`
	MealType       string  `json:"meal_type"`
	ClinicalTag    string  `json:"clinical_tag"`
	AgeGroup       string  `json:"age_group"`
	NutritionScore float64 `json:"nutrition_score"`
	RankedScore    float64 `json:"ranked_score"`
	ScoredAxes     string  `json:"scored_axes"`
	ValueKind      string  `json:"value_kind"` // always "derived"
}

// EngineResult is the full response of a search: the ordered list plus the full step
// accounting and the target that was selected and why.
type EngineResult struct {
	Recipes      []RankedRecipe `json:"recipes"`
	Steps        []StepResult   `json:"steps"`
	ActiveTarget string         `json:"active_target"`
	TargetReason string         `json:"target_reason"`
	Blocked      bool           `json:"blocked"`
	BlockReason  string         `json:"block_reason,omitempty"`

	// UnscreenedAllergens names declared allergen groups that have no tag anywhere in
	// the recipe corpus (allergen_tag_vocabulary.corpus_tag IS NULL). They excluded zero
	// recipes because nothing carries the tag, not because the filter passed. Any client
	// rendering a result set MUST render this: a result page that omits it implies a
	// screening that did not happen, which is the one failure mode this project treats
	// as dangerous rather than untidy.
	UnscreenedAllergens []string `json:"unscreened_allergens,omitempty"`
}
