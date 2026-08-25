// Package aidraft wraps Gemini-based live drafting for content the provider's data does not
// carry: per-recipe clinical modification notes grounded in real provider text, and a
// last-resort invented recipe when a meal chapter falls short of its target after real corpus
// recipes are exhausted.
//
// Nothing here writes to the database, and nothing here is a source of truth for a data value.
// Every safety check on what a draft is allowed to contain -- ingredient allow-listing,
// allergen overlap, age/texture agreement -- lives in the caller (internal/book), not here.
// This package's job is to produce a candidate; internal/book's job is to decide whether that
// candidate is safe to print.
package aidraft

import (
	"context"
	"errors"
	"time"
)

// ErrDraftingUnavailable means no drafting can happen right now -- GEMINI_API_KEY is unset, or
// the underlying API call itself failed. Callers treat this the same as "AI declined to help":
// the existing corpus-only / omission-reporting path runs unchanged, nothing blocks on it.
var ErrDraftingUnavailable = errors.New("aidraft: drafting unavailable")

// Drafter is the seam every caller in internal/book depends on, so tests can supply a fake
// instead of making a real network call.
type Drafter interface {
	DraftModificationNote(ctx context.Context, req ModificationRequest) (DraftedText, error)
	DraftInventedRecipe(ctx context.Context, req InventedRecipeRequest) (InventedRecipe, error)
	DraftDoctorApproachNote(ctx context.Context, req DoctorApproachRequest) (DraftedText, error)
	DraftFoodGroupPriorities(ctx context.Context, req FoodGroupPriorityRequest) (FoodGroupPriorities, error)
}

// DraftedText is one piece of AI-drafted prose plus a provenance record -- the same "every
// value carries its source" instinct CLAUDE.md applies to derived data values, carried over to
// generated prose even though prose isn't a derived value in the schema sense.
type DraftedText struct {
	Text        string    `json:"text"`
	Source      string    `json:"source"` // "gemini" -- static, non-AI text never reaches this package at all
	Model       string    `json:"model,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
	// GroundedOnRuleID names the provider row this note paraphrases: clinical_rule_master.rule_id
	// for a recipe modification note, book1_evidence_source.source_id for a Book 1 doctor-approach
	// note. One field rather than two, because both are the same claim -- "here is the row a
	// human can check this sentence against" -- against a different master depending on caller.
	GroundedOnRuleID string `json:"grounded_on_rule_id,omitempty"`
}

// DoctorApproachRequest grounds a Book 1 doctor-approach/red-flag note in the block's own
// provider text plus its cited evidence row. Nothing here is per-child: these are general
// guidance blocks (child profile, growth record, feeding-stage pages, monitoring dashboards),
// not a condition-specific note the way ModificationRequest is, so unlike that request there is
// no child age or recipe name to thread through.
type DoctorApproachRequest struct {
	BlockID             string
	Section             string
	ContentPurpose      string // book1_content_block.content_purpose, verbatim
	ParentFacingOutput  string // book1_content_block.parent_facing_output, verbatim
	EvidenceSourceID    string
	EvidenceAuthority   string
	EvidenceTopic       string
	HowUsed             string // book1_evidence_source.how_used, verbatim
	ImportantLimitation string // book1_evidence_source.important_limitation, verbatim
}

// ModificationRequest grounds one drafted note in the provider's own clinical_rule_master
// text. BookAction and RequiredModification are copied verbatim from the DB row; the prompt is
// instructed to paraphrase only what is here and never add a claim absent from these two
// fields.
type ModificationRequest struct {
	RuleID               string
	ClinicalDomain       string
	BookAction           string
	RequiredModification string
	RecipeName           string
	ChildAgeMonths       int
}

// AllowedIngredient is one row of the child-safe ingredient set an invented recipe may draw
// from. The caller builds this from the same allergy/diet/texture filtering the engine already
// applies to real recipes -- never a fresh, unaudited list built just for this feature.
type AllowedIngredient struct {
	IngredientID string
	Name         string
}

// InventedRecipeRequest is the full allow-list a fallback recipe is constrained to. Every
// field narrows what the model may return; nothing here widens it.
type InventedRecipeRequest struct {
	MealCategory         string
	ChildAgeMonths       int
	DietType             string
	RequiredTexture      string
	AllowedIngredients   []AllowedIngredient
	DishFormatArchetypes []string // the fixed archetype ids internal/book/marks.go already draws
}

// InventedIngredientLine is one ingredient the model chose, by id. The caller must reject any
// id absent from the request's AllowedIngredients before this ever reaches a page -- the
// response schema constrains this at the API level, but that is not treated as sufficient on
// its own (see internal/book's validation).
type InventedIngredientLine struct {
	IngredientID string
	QuantityG    float64
}

// InventedRecipe is a fallback recipe the model drafted whole. Every field is unvalidated
// model output until internal/book runs it through its own guardrails. This package never
// marks a recipe safe -- it only returns what the model said.
type InventedRecipe struct {
	Name         string
	MethodSteps  []string
	Ingredients  []InventedIngredientLine
	DishFormatID string
	Model        string
	GeneratedAt  time.Time
}

// FoodGroupPriorityRequest grounds a Book 1 "Food Groups and Nutrient Priorities" page in two
// real sources: the child's active nutrition_target_master row's own *_action text (already
// loaded for the Active Nutrition Target page) and the real food_group_macro vocabulary. No
// nutrient-to-food-group mapping exists anywhere in this schema, so the model is asked to
// synthesize one from these two real sources rather than this project hand-writing a rule
// table it has no nutrition-science basis for -- see the caller's own doc comment
// (foodGroupPrioritySection in book1.go) for the full reasoning. MacroGroups is the complete,
// closed vocabulary the model may choose from; it may name nothing outside it.
type FoodGroupPriorityRequest struct {
	TargetCode  string
	TargetName  string
	Actions     map[string]string // action column name -> its real text, e.g. "iron_action" -> "High priority"
	MacroGroups []string          // real food_group_macro.macro_group values, the closed vocabulary
}

// FoodGroupPriority is one nutrient's food-group recommendation, exactly as the model
// returned it -- internal/book must still reject any FoodGroup value outside the real
// MacroGroups list before this reaches a page (see validateFoodGroupPriorities).
type FoodGroupPriority struct {
	Nutrient  string // the action column's plain-English name, e.g. "Iron"
	FoodGroup string // must be one of req.MacroGroups
}

// FoodGroupPriorities is the whole drafted page: one row per nutrient the model chose to
// cover (it may skip a nutrient with no clear food-group link rather than force one), plus
// the same provenance every DraftedText carries.
type FoodGroupPriorities struct {
	Rows        []FoodGroupPriority
	Source      string
	Model       string
	GeneratedAt time.Time
	// GroundedOnRuleID names the nutrition_target_master row this page paraphrases, the same
	// field DraftedText uses for the same purpose against a different master.
	GroundedOnRuleID string
}
