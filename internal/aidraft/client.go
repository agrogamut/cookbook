package aidraft

import (
	"context"
	"fmt"

	"google.golang.org/genai"
)

// modelName is pinned rather than left to the API's own default. "Whatever the latest model
// is" silently changing what reaches a printed book is exactly the kind of drift this project
// avoids everywhere else (recipe_master_version, import content hashes); bumping this is a
// deliberate, reviewable edit instead.
const modelName = "gemini-3.6-flash"

// NewClient returns a Drafter backed by the real Gemini API when apiKey is non-empty, or a
// disabledClient that always reports ErrDraftingUnavailable when it is empty. Callers never
// branch on whether the key was set -- they always have a Drafter, and it always behaves
// safely when there is nothing to call.
func NewClient(ctx context.Context, apiKey string) (Drafter, error) {
	if apiKey == "" {
		return disabledClient{}, nil
	}
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("aidraft: create gemini client: %w", err)
	}
	return &geminiClient{client: c}, nil
}

// Disabled is a Drafter that always reports ErrDraftingUnavailable. It is what NewClient("")
// returns, and it is exported so a caller that has not wired a real client through yet (an
// optional constructor parameter, a test default) has a safe zero-effort value to reach for
// instead of a nil interface every call site would have to guard.
var Disabled Drafter = disabledClient{}

// disabledClient is what every caller gets when GEMINI_API_KEY is unset. It exists so
// internal/book never has to nil-check a Drafter before calling it -- an absent key looks
// exactly like an unavailable API, and both are handled by the same ErrDraftingUnavailable
// branch.
type disabledClient struct{}

func (disabledClient) DraftModificationNote(context.Context, ModificationRequest) (DraftedText, error) {
	return DraftedText{}, ErrDraftingUnavailable
}

func (disabledClient) DraftInventedRecipe(context.Context, InventedRecipeRequest) (InventedRecipe, error) {
	return InventedRecipe{}, ErrDraftingUnavailable
}

func (disabledClient) DraftDoctorApproachNote(context.Context, DoctorApproachRequest) (DraftedText, error) {
	return DraftedText{}, ErrDraftingUnavailable
}

func (disabledClient) DraftFoodGroupPriorities(context.Context, FoodGroupPriorityRequest) (FoodGroupPriorities, error) {
	return FoodGroupPriorities{}, ErrDraftingUnavailable
}

// geminiClient is the real implementation, defined in draft.go.
type geminiClient struct {
	client *genai.Client
}
