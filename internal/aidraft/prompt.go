package aidraft

import (
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// modificationResponse is the JSON shape the model is constrained to for a modification note.
// One field, deliberately: nothing to invent a structure around.
type modificationResponse struct {
	Note string `json:"note"`
}

// modificationSchema forces the response down to exactly one string field. It cannot, by
// itself, enforce that the string only paraphrases the grounding text -- that instruction is
// carried in the prompt, and the blocklist check in internal/book is the guardrail that isn't
// just trusting the model to have followed it.
func modificationSchema() *genai.Schema {
	return &genai.Schema{
		Type:     genai.TypeObject,
		Required: []string{"note"},
		Properties: map[string]*genai.Schema{
			"note": {Type: genai.TypeString},
		},
	}
}

// buildModificationPrompt hands the model the provider's own text as the only source of
// clinical fact and instructs it to paraphrase, not extend. The grounding block is quoted
// verbatim from the DB row -- nothing here is written from scratch about the condition itself.
func buildModificationPrompt(req ModificationRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are rephrasing a feeding note for a children's recipe book. "+
		"Rewrite the following provider guidance into one short, warm, parent-readable "+
		"paragraph (2-4 sentences). Do not add any clinical claim, medication, dosage, "+
		"diagnosis, or fact that is not already present in the guidance below. If the "+
		"guidance is thin, keep the note short rather than inventing detail to fill space.\n\n")
	fmt.Fprintf(&b, "Recipe: %s\n", req.RecipeName)
	fmt.Fprintf(&b, "Child age: %d months\n", req.ChildAgeMonths)
	fmt.Fprintf(&b, "Clinical domain: %s\n\n", req.ClinicalDomain)
	fmt.Fprintf(&b, "Provider guidance (the ONLY source of fact you may use):\n")
	if req.BookAction != "" {
		fmt.Fprintf(&b, "- Book action: %s\n", req.BookAction)
	}
	if req.RequiredModification != "" {
		fmt.Fprintf(&b, "- Required modification: %s\n", req.RequiredModification)
	}
	return b.String()
}

// doctorApproachResponse is the JSON shape the model is constrained to for a doctor-approach
// note. One field, same reasoning as modificationResponse.
type doctorApproachResponse struct {
	Note string `json:"note"`
}

func doctorApproachSchema() *genai.Schema {
	return &genai.Schema{
		Type:     genai.TypeObject,
		Required: []string{"note"},
		Properties: map[string]*genai.Schema{
			"note": {Type: genai.TypeString},
		},
	}
}

// buildDoctorApproachPrompt hands the model the block's own provider text -- content_purpose,
// parent_facing_output -- and its cited evidence row's how_used/important_limitation as the
// only source of fact, and instructs it to paraphrase rather than extend. Severity is fixed at
// "info" by the caller, never decided here: a drafted note claiming warning-level urgency would
// be exactly the overclaim CLAUDE.md's hard rule exists to prevent, and "warning" stays reserved
// for the provider's own red-flag rows.
func buildDoctorApproachPrompt(req DoctorApproachRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are writing a short general-guidance note for a page in a children's "+
		"daily-life handbook, on the topic of when to involve a doctor. Write one short, "+
		"warm, parent-readable paragraph (2-3 sentences) suggesting when a parent might want "+
		"to mention this page's topic to their child's doctor. Do not add any clinical claim, "+
		"threshold, diagnosis, medication, or fact that is not already present in the "+
		"guidance below. Never claim urgency beyond what the guidance states. If the "+
		"guidance is thin, keep the note short and generic (e.g. \"mention this at your next "+
		"visit if you have questions\") rather than inventing detail to fill space.\n\n")
	fmt.Fprintf(&b, "Book section: %s\n", req.Section)
	if req.ContentPurpose != "" {
		fmt.Fprintf(&b, "This page's purpose: %s\n", req.ContentPurpose)
	}
	if req.ParentFacingOutput != "" {
		fmt.Fprintf(&b, "What this page shows a parent: %s\n", req.ParentFacingOutput)
	}
	fmt.Fprintf(&b, "\nCited evidence (the ONLY source of fact you may use):\n")
	if req.EvidenceAuthority != "" || req.EvidenceTopic != "" {
		fmt.Fprintf(&b, "- Source: %s, on %s\n", req.EvidenceAuthority, req.EvidenceTopic)
	}
	if req.HowUsed != "" {
		fmt.Fprintf(&b, "- How this project uses it: %s\n", req.HowUsed)
	}
	if req.ImportantLimitation != "" {
		fmt.Fprintf(&b, "- Its stated limitation: %s\n", req.ImportantLimitation)
	}
	return b.String()
}

// inventedRecipeResponse is the JSON shape a fallback recipe is constrained to.
type inventedRecipeResponse struct {
	Name         string                   `json:"name"`
	MethodSteps  []string                 `json:"method_steps"`
	Ingredients  []inventedIngredientJSON `json:"ingredients"`
	DishFormatID string                   `json:"dish_format_id"`
}

type inventedIngredientJSON struct {
	IngredientID string  `json:"ingredient_id"`
	QuantityG    float64 `json:"quantity_g"`
}

// inventedRecipeSchema constrains ingredient_id to an enum over exactly the allowed set and
// dish_format_id to an enum over the fixed drawn archetypes -- the schema is the first line of
// defense, not the only one. internal/book still re-checks every id against the same allowed
// set before printing, because a schema constrains shape, not the cross-field safety logic
// (allergen overlap, texture agreement) that has to run in Go.
func inventedRecipeSchema(allowed []AllowedIngredient, archetypes []string) *genai.Schema {
	ids := make([]string, len(allowed))
	for i, a := range allowed {
		ids[i] = a.IngredientID
	}
	return &genai.Schema{
		Type:     genai.TypeObject,
		Required: []string{"name", "method_steps", "ingredients", "dish_format_id"},
		Properties: map[string]*genai.Schema{
			"name": {Type: genai.TypeString},
			"method_steps": {
				Type:  genai.TypeArray,
				Items: &genai.Schema{Type: genai.TypeString},
			},
			"ingredients": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"ingredient_id", "quantity_g"},
					Properties: map[string]*genai.Schema{
						"ingredient_id": {Type: genai.TypeString, Enum: ids},
						"quantity_g":    {Type: genai.TypeNumber},
					},
				},
			},
			"dish_format_id": {Type: genai.TypeString, Enum: archetypes},
		},
	}
}

// buildInventedRecipePrompt states the constraints as prose too, not just as schema, because a
// schema alone doesn't explain *why* an ingredient list is short -- the model should choose
// from what's allowed rather than treat the enum as a suggestion it can wander from with a
// verbose alternative.
func buildInventedRecipePrompt(req InventedRecipeRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Invent one new recipe for a children's recipe book, for the meal "+
		"category %q. This child is %d months old, diet type %q, and needs %q texture. "+
		"Use ONLY ingredients from the allowed list below -- do not name any ingredient "+
		"absent from it, and do not substitute a similar-sounding ingredient. Choose "+
		"realistic quantities in grams. Pick the dish_format_id that best matches how this "+
		"dish is actually prepared, from the fixed list given.\n\n",
		req.MealCategory, req.ChildAgeMonths, req.DietType, req.RequiredTexture)

	fmt.Fprintf(&b, "Allowed ingredients:\n")
	for _, a := range req.AllowedIngredients {
		fmt.Fprintf(&b, "- %s (%s)\n", a.IngredientID, a.Name)
	}
	fmt.Fprintf(&b, "\nAllowed dish formats: %s\n", strings.Join(req.DishFormatArchetypes, ", "))
	return b.String()
}
