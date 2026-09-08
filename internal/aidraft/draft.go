package aidraft

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/genai"
)

// DraftModificationNote asks Gemini to paraphrase one clinical_rule_master row into a short,
// parent-readable note. See buildModificationPrompt for the grounding contract: the model may
// only restate BookAction and RequiredModification, never add to them.
func (g *geminiClient) DraftModificationNote(ctx context.Context, req ModificationRequest) (DraftedText, error) {
	ctx, cancel := context.WithTimeout(ctx, perCallTimeout)
	defer cancel()
	resp, err := g.client.Models.GenerateContent(ctx, modelName, genai.Text(buildModificationPrompt(req)),
		&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   modificationSchema(),
		})
	if err != nil {
		return DraftedText{}, fmt.Errorf("%w: gemini modification note request: %v", ErrDraftingUnavailable, err)
	}

	var out modificationResponse
	if err := json.Unmarshal([]byte(resp.Text()), &out); err != nil {
		return DraftedText{}, fmt.Errorf("%w: decode modification note response: %v", ErrDraftingUnavailable, err)
	}

	return DraftedText{
		Text:             out.Note,
		Source:           "gemini",
		Model:            modelName,
		GeneratedAt:      time.Now(),
		GroundedOnRuleID: req.RuleID,
	}, nil
}

// DraftDoctorApproachNote asks Gemini to paraphrase a Book 1 block's own provider text and its
// cited evidence row into a short doctor-approach note. See buildDoctorApproachPrompt for the
// grounding contract.
func (g *geminiClient) DraftDoctorApproachNote(ctx context.Context, req DoctorApproachRequest) (DraftedText, error) {
	ctx, cancel := context.WithTimeout(ctx, perCallTimeout)
	defer cancel()
	resp, err := g.client.Models.GenerateContent(ctx, modelName, genai.Text(buildDoctorApproachPrompt(req)),
		&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   doctorApproachSchema(),
		})
	if err != nil {
		return DraftedText{}, fmt.Errorf("%w: gemini doctor-approach note request: %v", ErrDraftingUnavailable, err)
	}

	var out doctorApproachResponse
	if err := json.Unmarshal([]byte(resp.Text()), &out); err != nil {
		return DraftedText{}, fmt.Errorf("%w: decode doctor-approach note response: %v", ErrDraftingUnavailable, err)
	}

	return DraftedText{
		Text:             out.Note,
		Source:           "gemini",
		Model:            modelName,
		GeneratedAt:      time.Now(),
		GroundedOnRuleID: req.EvidenceSourceID,
	}, nil
}

// DraftFoodGroupPriorities asks Gemini to link the child's active nutrition target's real
// per-nutrient guidance to real food-group names. See buildFoodGroupPriorityPrompt for the
// grounding contract: the model may only choose from req.MacroGroups, and only for a
// nutrient whose guidance text is already fed in.
func (g *geminiClient) DraftFoodGroupPriorities(ctx context.Context, req FoodGroupPriorityRequest) (FoodGroupPriorities, error) {
	ctx, cancel := context.WithTimeout(ctx, perCallTimeout)
	defer cancel()
	resp, err := g.client.Models.GenerateContent(ctx, modelName, genai.Text(buildFoodGroupPriorityPrompt(req)),
		&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   foodGroupPrioritySchema(req.MacroGroups),
		})
	if err != nil {
		return FoodGroupPriorities{}, fmt.Errorf("%w: gemini food group priorities request: %v", ErrDraftingUnavailable, err)
	}

	var out foodGroupPriorityResponse
	if err := json.Unmarshal([]byte(resp.Text()), &out); err != nil {
		return FoodGroupPriorities{}, fmt.Errorf("%w: decode food group priorities response: %v", ErrDraftingUnavailable, err)
	}

	rows := make([]FoodGroupPriority, 0, len(out.Rows))
	for _, r := range out.Rows {
		rows = append(rows, FoodGroupPriority{Nutrient: r.Nutrient, FoodGroup: r.FoodGroup})
	}

	return FoodGroupPriorities{
		Rows:             rows,
		Source:           "gemini",
		Model:            modelName,
		GeneratedAt:      time.Now(),
		GroundedOnRuleID: req.TargetCode,
	}, nil
}

// DraftInventedRecipe asks Gemini for a whole fallback recipe constrained to req's allowed
// ingredients and dish formats. The response is returned exactly as decoded -- this function
// performs no safety validation. internal/book must re-check every ingredient id against
// req.AllowedIngredients, the allergen set, and the required texture before treating this as
// printable; see the package doc comment for why that split exists.
func (g *geminiClient) DraftInventedRecipe(ctx context.Context, req InventedRecipeRequest) (InventedRecipe, error) {
	ctx, cancel := context.WithTimeout(ctx, perCallTimeout)
	defer cancel()
	resp, err := g.client.Models.GenerateContent(ctx, modelName, genai.Text(buildInventedRecipePrompt(req)),
		&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   inventedRecipeSchema(req.AllowedIngredients, req.DishFormatArchetypes),
		})
	if err != nil {
		return InventedRecipe{}, fmt.Errorf("%w: gemini invented recipe request: %v", ErrDraftingUnavailable, err)
	}

	var out inventedRecipeResponse
	if err := json.Unmarshal([]byte(resp.Text()), &out); err != nil {
		return InventedRecipe{}, fmt.Errorf("%w: decode invented recipe response: %v", ErrDraftingUnavailable, err)
	}

	lines := make([]InventedIngredientLine, 0, len(out.Ingredients))
	for _, ing := range out.Ingredients {
		lines = append(lines, InventedIngredientLine{
			IngredientID: ing.IngredientID,
			QuantityG:    ing.QuantityG,
		})
	}

	return InventedRecipe{
		Name:         out.Name,
		MethodSteps:  out.MethodSteps,
		Ingredients:  lines,
		DishFormatID: out.DishFormatID,
		Model:        modelName,
		GeneratedAt:  time.Now(),
	}, nil
}

// TranslateTexts asks Gemini to carry one page's batch of already-rendered text nodes into
// req.TargetLanguage, preserving order and count. See buildTranslatePrompt for the numbering
// discipline that keeps the response aligned to the request.
//
// Retries once on failure, unlike every other method in this file. A full book's translation
// makes dozens of these calls (one per translateBatchSize text nodes, internal/book/
// translate.go), where the other Drafter methods make at most a handful per book -- so a
// transient upstream failure that would be rare enough to ignore once in a while compounds
// into a near-certain whole-book failure at this call volume. Observed directly: a real run
// against the live API hit "Error 504 ... DEADLINE_EXCEEDED" from Gemini's own backend on one
// batch out of several dozen, with every other batch succeeding -- a transient fault on
// Gemini's side, not a persistent one, since the identical request succeeded on the retry. Two
// attempts total, each under its own fresh perCallTimeout rather than sharing one deadline, so
// a slow-but-real second attempt is not punished for the first attempt's time.
func (g *geminiClient) TranslateTexts(ctx context.Context, req TranslateRequest) (TranslatedTexts, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		out, err := g.translateOnce(ctx, req)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	return TranslatedTexts{}, lastErr
}

func (g *geminiClient) translateOnce(ctx context.Context, req TranslateRequest) (TranslatedTexts, error) {
	ctx, cancel := context.WithTimeout(ctx, perCallTimeout)
	defer cancel()
	resp, err := g.client.Models.GenerateContent(ctx, translateModelName, genai.Text(buildTranslatePrompt(req)),
		&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   translateSchema(),
		})
	if err != nil {
		return TranslatedTexts{}, fmt.Errorf("%w: gemini translate request: %v", ErrDraftingUnavailable, err)
	}

	var out translateResponse
	if err := json.Unmarshal([]byte(resp.Text()), &out); err != nil {
		return TranslatedTexts{}, fmt.Errorf("%w: decode translate response: %v", ErrDraftingUnavailable, err)
	}

	return TranslatedTexts{
		Texts:       out.Texts,
		Source:      "gemini",
		Model:       translateModelName,
		GeneratedAt: time.Now(),
	}, nil
}
