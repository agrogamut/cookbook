package aidraft

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/genai"
)

// modelName is pinned rather than left to the API's own default. "Whatever the latest model
// is" silently changing what reaches a printed book is exactly the kind of drift this project
// avoids everywhere else (recipe_master_version, import content hashes); bumping this is a
// deliberate, reviewable edit instead.
//
// Pro rather than Flash: both spend real thinking tokens on every call regardless of prompt
// simplicity (measured 500-900 thinking tokens, 12-60s per call, on a two-sentence paraphrase
// task), so Flash's usual speed advantage does not hold for this workload -- and gemini-2.5-pro
// itself now 404s with "no longer available to new users... use models/gemini-3.1-pro-preview",
// which is the API's own recommended replacement.
const modelName = "gemini-3.1-pro-preview"

// translateModelName is deliberately a different, cheaper-quota model than modelName, used
// only by TranslateTexts. Measured on a live Tier 1 key (2026-09-08): Gemini 3.1 Pro sits at
// 243/250 of its daily request quota after generating one Bengali book, while Gemini 3.6 Flash
// on the same project sat at 51/10,000 -- a whole-book Bengali translation alone makes dozens
// of calls (one per translateBatchSize text nodes, internal/book/translate.go), which is a
// request-count shape none of this package's other methods share (at most a dozen or so per
// book). Tier 1 does not raise a preview model's own daily ceiling the way it raises a GA
// model's, so Pro's 250/day is a platform floor here, not something a paid tier lifts.
//
// The tradeoff this const's sibling doc comment above warns about (Flash spending real
// thinking tokens regardless of prompt simplicity, no speed win over Pro) is accepted here on
// purpose: translation is exactly the "simple restatement" shape that tradeoff describes, and
// 40x the daily request headroom matters more for this specific caller than shaving seconds
// off any one call does.
const translateModelName = "gemini-3.6-flash"

// perCallTimeout bounds a single Gemini request independently of whatever deadline the caller's
// ctx already carries. Measured directly against the real API: every drafting call this package
// makes -- including DraftInventedRecipe's large-schema request, the slowest -- completes well
// under a minute in the normal case (10-60s observed). But live testing with a real key also hit
// a single call that sat in IO wait for 5+ minutes with no response and no error, silently
// consuming the entire print-route budget (internal/api/router.go's printTimeout) before that
// route's own timeout ever fired -- turning one stalled upstream call into a hard failure for a
// whole two-book request that had otherwise fully succeeded. context.WithTimeout(ctx,
// perCallTimeout) takes the *earlier* of this bound and the caller's own deadline, so it only
// ever tightens the ceiling, never loosens it beyond what the caller already allowed.
const perCallTimeout = 75 * time.Second

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

func (disabledClient) TranslateTexts(context.Context, TranslateRequest) (TranslatedTexts, error) {
	return TranslatedTexts{}, ErrDraftingUnavailable
}

// geminiClient is the real implementation, defined in draft.go.
type geminiClient struct {
	client *genai.Client
}
