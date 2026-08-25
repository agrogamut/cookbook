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
