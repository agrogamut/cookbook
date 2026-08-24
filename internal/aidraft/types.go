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
}

// DraftedText is one piece of AI-drafted prose plus a provenance record -- the same "every
// value carries its source" instinct CLAUDE.md applies to derived data values, carried over to
// generated prose even though prose isn't a derived value in the schema sense.
type DraftedText struct {
	Text             string    `json:"text"`
	Source           string    `json:"source"` // "gemini" -- static, non-AI text never reaches this package at all
	Model            string    `json:"model,omitempty"`
	GeneratedAt      time.Time `json:"generated_at"`
	GroundedOnRuleID string    `json:"grounded_on_rule_id,omitempty"` // clinical_rule_master.rule_id this note paraphrases
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
