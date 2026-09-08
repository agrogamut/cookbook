package book

import (
	"context"
	"html/template"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
)

// fakeDrafter is a test double for aidraft.Drafter. Both methods report
// aidraft.ErrDraftingUnavailable unless the matching field is set, mirroring the real
// disabled client's default behaviour so a test only has to wire up what it actually exercises.
type fakeDrafter struct {
	inventFn    func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error)
	doctorFn    func(ctx context.Context, req aidraft.DoctorApproachRequest) (aidraft.DraftedText, error)
	foodGroupFn func(ctx context.Context, req aidraft.FoodGroupPriorityRequest) (aidraft.FoodGroupPriorities, error)
}

func (f fakeDrafter) DraftModificationNote(context.Context, aidraft.ModificationRequest) (aidraft.DraftedText, error) {
	return aidraft.DraftedText{}, aidraft.ErrDraftingUnavailable
}

func (f fakeDrafter) DraftInventedRecipe(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
	if f.inventFn == nil {
		return aidraft.InventedRecipe{}, aidraft.ErrDraftingUnavailable
	}
	return f.inventFn(ctx, req)
}

func (f fakeDrafter) DraftDoctorApproachNote(ctx context.Context, req aidraft.DoctorApproachRequest) (aidraft.DraftedText, error) {
	if f.doctorFn == nil {
		return aidraft.DraftedText{}, aidraft.ErrDraftingUnavailable
	}
	return f.doctorFn(ctx, req)
}

func (f fakeDrafter) DraftFoodGroupPriorities(ctx context.Context, req aidraft.FoodGroupPriorityRequest) (aidraft.FoodGroupPriorities, error) {
	if f.foodGroupFn == nil {
		return aidraft.FoodGroupPriorities{}, aidraft.ErrDraftingUnavailable
	}
	return f.foodGroupFn(ctx, req)
}

func (f fakeDrafter) TranslateTexts(context.Context, aidraft.TranslateRequest) (aidraft.TranslatedTexts, error) {
	return aidraft.TranslatedTexts{}, aidraft.ErrDraftingUnavailable
}

// TestInventedRecipeTopUpFillsACategoryWithNoRealCandidates pins the case the fallback exists
// for: a meal category real recipes have nothing at all for is still filled, entirely by
// AI-invented recipes drawn from the child's own safe-ingredient allow-list.
//
// Exercised directly against topUpInvented with a synthetic testMealCategory, not through
// AssembleBook2: AssembleBook2 now only ever iterates the three chapters with real
// recipe_master.meal_type coverage (mainMealCategories -- Breakfast, Lunch, Dinner), so it
// never reaches a category with zero real candidates at all any more. That is the whole point
// of the mainMealCategories policy -- see its doc comment -- but the fallback mechanism this
// test actually cares about (an empty start, filled entirely by invention, validated and
// labelled the same as any other card) still exists and still needs to be pinned; it is simply
// exercised at the layer where "zero real candidates" is still a real, reachable case, rather
// than through a whole real category the DB happens to leave unmapped (GAP-023), which was
// always a slightly fragile thing to hang a test on: had the provider ever ruled on those three
// meal types, unmappedCategory's own t.Skip would have silently stopped testing this at all.
func TestInventedRecipeTopUpFillsACategoryWithNoRealCandidates(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cat := testMealCategory(t, 4)
	cleanupAIRecipes(t, pool, cat.ID)

	fake := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			if len(req.AllowedIngredients) == 0 || len(req.DishFormatArchetypes) == 0 {
				t.Fatalf("invented recipe request carries no allowed ingredients or dish formats: %+v", req)
			}
			return aidraft.InventedRecipe{
				Name:        "Test invented recipe",
				MethodSteps: []string{"Combine and serve."},
				Ingredients: []aidraft.InventedIngredientLine{
					{IngredientID: req.AllowedIngredients[0].IngredientID, QuantityG: 50},
				},
				DishFormatID: req.DishFormatArchetypes[0],
			}, nil
		},
	}

	cp := models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian"} // above inventedRecipeMinAgeMonths
	res := models.EngineResult{ActiveTarget: "NT00", TargetReason: "default"}

	cards, note := topUpInvented(ctx, pool, fake, cp, cat, "v1", res, nil)
	if len(cards) == 0 {
		t.Fatalf("category %s did not fill from an empty start even with a working Drafter; note=%q", cat.ID, note)
	}
	for _, r := range cards {
		// Source stays set on the struct -- the operator console's fact-check surface needs
		// it -- but the printed page must never carry a tell. ReviewStatus and
		// SelectionReasons read exactly like a real card's; recipe.html has no branch on
		// Source at all any more.
		if r.Source != "ai-invented" {
			t.Fatalf("recipe %s in an empty-start category has Source %q, want \"ai-invented\"", r.RecipeID, r.Source)
		}
		if r.ReviewStatus != inventedReviewStatus {
			t.Fatalf("recipe %s has ReviewStatus %q, want the same value a real card carries (%q)",
				r.RecipeID, r.ReviewStatus, inventedReviewStatus)
		}
		for _, reason := range r.SelectionReasons {
			lower := strings.ToLower(reason)
			if strings.Contains(lower, "invented") || strings.Contains(lower, "gemini") || strings.Contains(lower, "ai-") {
				t.Fatalf("recipe %s selection reason %q names the drafting mechanism", r.RecipeID, reason)
			}
		}
	}
	if len(cards) > cat.Target {
		t.Fatalf("category %s got %d recipes, more than its own target %d", cat.ID, len(cards), cat.Target)
	}

	html := renderRecipeCardHTML(t, cards[0])
	if strings.Contains(html, "ai-badge") || strings.Contains(strings.ToLower(html), "ai-invented recipe") {
		t.Fatalf("rendered recipe page for %s still carries an AI-invented disclosure badge", cards[0].RecipeID)
	}
}

// renderRecipeCardHTML renders one recipe card through the real B2-RECIPE-01 template
// (templates/book2/recipe.html), the same file AssembleBook2's own printed output uses -- so
// this test is pinning what a reader actually sees, not a Go-level assertion about a field
// that never reaches the page. recipe.html defines B2-RECIPE-01 plus the two sub-templates it
// calls (B2-NUTRI-01, B2-TRACK-01) all in the one file, so parsing just it is self-contained.
func renderRecipeCardHTML(t *testing.T, card RecipeCard) string {
	t.Helper()
	tpl, err := template.New("recipe.html").Funcs(templateFuncs).ParseFS(templateFS, "templates/book2/recipe.html")
	if err != nil {
		t.Fatalf("parse recipe template: %v", err)
	}
	var buf strings.Builder
	if err := tpl.ExecuteTemplate(&buf, "B2-RECIPE-01", card); err != nil {
		t.Fatalf("render recipe card: %v", err)
	}
	return buf.String()
}

// TestInventedRecipeFallbackNeverRunsBelowMinAge pins inventedRecipeMinAgeMonths: no
// ingredient-level texture or choking-hazard rule exists anywhere in this codebase to check an
// invented recipe against, so the fallback must never even ask the model below that age. The
// fake's inventFn fails the test outright if called, rather than returning an error the caller
// might swallow -- proving the gate stops the call before it happens, not merely that a
// downstream check would have caught the response. Exercised directly against topUpInvented;
// see TestInventedRecipeTopUpFillsACategoryWithNoRealCandidates for why.
func TestInventedRecipeFallbackNeverRunsBelowMinAge(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cat := testMealCategory(t, 4)
	cleanupAIRecipes(t, pool, cat.ID)

	fake := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			t.Fatalf("DraftInventedRecipe was called for a child below inventedRecipeMinAgeMonths; "+
				"the age gate must stop this before any drafting request is made (req=%+v)", req)
			return aidraft.InventedRecipe{}, nil
		},
	}

	cp := models.ChildProfile{AgeMonths: 12, DietType: "Non-vegetarian"} // below inventedRecipeMinAgeMonths (24)
	res := models.EngineResult{ActiveTarget: "NT00", TargetReason: "default"}

	cards, note := topUpInvented(ctx, pool, fake, cp, cat, "v1", res, nil)
	if len(cards) != 0 {
		t.Fatalf("category %s rendered %d cards for a 12-month-old; the only possible source is "+
			"the invented-recipe fallback, which must not run below the age gate", cat.ID, len(cards))
	}
	if note == "" {
		t.Fatalf("category %s must report a shortfall for a child below the invented-recipe age gate, got an empty note", cat.ID)
	}
}

// TestInventedRecipeOutsideAllowedSetIsRejected pins the guardrail that actually matters: a
// model response naming an ingredient outside the child-safe allow-list must never reach a
// printed card, even though the response schema was already supposed to prevent it -- this is
// the defense-in-depth check, exercised by a fake that deliberately violates the schema's own
// contract the way a real model still might. Exercised directly against topUpInvented; see
// TestInventedRecipeTopUpFillsACategoryWithNoRealCandidates for why.
func TestInventedRecipeOutsideAllowedSetIsRejected(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cat := testMealCategory(t, 4)
	cleanupAIRecipes(t, pool, cat.ID)

	fake := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			return aidraft.InventedRecipe{
				Name:        "Unsafe invented recipe",
				MethodSteps: []string{"Combine and serve."},
				Ingredients: []aidraft.InventedIngredientLine{
					{IngredientID: "NOT-A-REAL-ALLOWED-ID", QuantityG: 50},
				},
				DishFormatID: req.DishFormatArchetypes[0],
			}, nil
		},
	}

	cp := models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian"}
	res := models.EngineResult{ActiveTarget: "NT00", TargetReason: "default"}

	cards, note := topUpInvented(ctx, pool, fake, cp, cat, "v1", res, nil)
	if len(cards) != 0 {
		t.Fatalf("category %s rendered %d cards despite every invented recipe failing "+
			"validation; a rejected response must never reach a printed card", cat.ID, len(cards))
	}
	if note == "" {
		t.Fatalf("category %s must report a shortfall when every invented recipe fails validation, got an empty note", cat.ID)
	}
}

// testMealCategory returns a mealCategory keyed to this test's own name, so a test that
// writes to ai_recipe never shares a meal_category_id with another test's rows and its
// assertions can't be contaminated by state a different test left behind -- this package's
// tests share one live database connection across the whole run rather than each getting an
// isolated transaction. t.Cleanup removes every row this category id ever accumulates,
// including across repeated runs against the same dev database.
func testMealCategory(t *testing.T, target int) mealCategory {
	t.Helper()
	cat := mealCategory{
		ID:     "MC-TEST-" + strings.ReplaceAll(t.Name(), "/", "-"),
		Name:   "Test Category",
		Target: target,
	}
	return cat
}

func cleanupAIRecipes(t *testing.T, pool *pgxpool.Pool, catID string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM ai_recipe WHERE meal_category_id = $1`, catID); err != nil {
			t.Logf("cleanup ai_recipe rows for %s: %v", catID, err)
		}
	})
}

// TestGenerateInventedCardPersistsAndIsReused pins the two halves of persistence together: a
// freshly drafted recipe is written to ai_recipe/ai_recipe_ingredient before
// generateInventedCard returns, and findStoredInventedCard -- which never even holds a
// reference to a Drafter -- finds and re-clears that same row afterward. That
// findStoredInventedCard's signature carries no Drafter is itself the proof reuse never calls
// Gemini: there is nothing in that function able to.
func TestGenerateInventedCardPersistsAndIsReused(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cat := testMealCategory(t, 1)
	cleanupAIRecipes(t, pool, cat.ID)

	cp := models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian"}
	res := models.EngineResult{ActiveTarget: "NT00", TargetReason: "default"}

	safe, err := engine.SafeIngredients(ctx, pool, cp)
	if err != nil {
		t.Fatalf("SafeIngredients: %v", err)
	}
	if len(safe) == 0 {
		t.Skip("no safe ingredients on the current dataset for an unrestricted child")
	}
	allowedIDs := make(map[string]bool, len(safe))
	nameByID := make(map[string]string, len(safe))
	for _, s := range safe {
		allowedIDs[s.IngredientID] = true
		nameByID[s.IngredientID] = s.Name
	}
	archetypes := MarkIDs()

	fake := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			return aidraft.InventedRecipe{
				Name:        "Test invented recipe for persistence",
				MethodSteps: []string{"Combine and serve."},
				Ingredients: []aidraft.InventedIngredientLine{
					{IngredientID: safe[0].IngredientID, QuantityG: 40},
				},
				DishFormatID: archetypes[0],
				Model:        "test-model",
			}, nil
		},
	}

	card, err := generateInventedCard(ctx, pool, fake, cp, cat, "v1", res, safe, allowedIDs, nameByID, archetypes)
	if err != nil {
		t.Fatalf("generateInventedCard: %v", err)
	}
	if !strings.HasPrefix(card.RecipeID, "AR-") {
		t.Fatalf("card.RecipeID = %q, want the AR-##### namespace", card.RecipeID)
	}
	if card.Source != "ai-invented" {
		t.Fatalf("card.Source = %q, want \"ai-invented\"", card.Source)
	}

	var storedCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM ai_recipe_ingredient WHERE recipe_id = $1`, card.RecipeID,
	).Scan(&storedCount); err != nil {
		t.Fatalf("count ai_recipe_ingredient: %v", err)
	}
	if storedCount != 1 {
		t.Fatalf("ai_recipe_ingredient rows for %s = %d, want 1", card.RecipeID, storedCount)
	}

	found, ok, err := findStoredInventedCard(ctx, pool, cat, "v1", res, cp, allowedIDs, nameByID, archetypes, map[string]bool{})
	if err != nil {
		t.Fatalf("findStoredInventedCard: %v", err)
	}
	if !ok {
		t.Fatalf("findStoredInventedCard did not find the recipe just persisted")
	}
	if found.RecipeID != card.RecipeID {
		t.Fatalf("findStoredInventedCard returned %s, want the persisted row %s", found.RecipeID, card.RecipeID)
	}
}

// TestFindStoredInventedCardSkipsRowUnsafeForThisChild pins the safety half of reuse: a stored
// row is re-validated against *this* call's own allow-list every time, never assumed safe
// because it printed for an earlier child. It uses ingredient_allergen_override's own seeded
// row (ING0063, Groundnut oil, overridden as Peanut) rather than a made-up exclusion, so the
// test is pinned to a real, documented safety correction instead of a fixture that could drift
// from what SafeIngredients actually excludes.
func TestFindStoredInventedCardSkipsRowUnsafeForThisChild(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cat := testMealCategory(t, 1)
	cleanupAIRecipes(t, pool, cat.ID)

	const groundnutOil = "ING0063"
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM ingredient_master WHERE ingredient_id = $1)`, groundnutOil,
	).Scan(&exists); err != nil {
		t.Fatalf("check %s exists: %v", groundnutOil, err)
	}
	if !exists {
		t.Skip("ING0063 (Groundnut oil) not present on the current dataset")
	}

	res := models.EngineResult{ActiveTarget: "NT00", TargetReason: "default"}

	// Generated for a child with no declared allergy, so Groundnut oil clears
	// SafeIngredients and can be persisted at all.
	permissive := models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian"}
	permissiveSafe, err := engine.SafeIngredients(ctx, pool, permissive)
	if err != nil {
		t.Fatalf("SafeIngredients (permissive): %v", err)
	}
	permissiveAllowed := make(map[string]bool, len(permissiveSafe))
	for _, s := range permissiveSafe {
		permissiveAllowed[s.IngredientID] = true
	}
	if !permissiveAllowed[groundnutOil] {
		t.Skip("ING0063 is not in the allow-list for an unrestricted child on the current dataset")
	}
	archetypes := MarkIDs()

	fake := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			return aidraft.InventedRecipe{
				Name:         "Test invented recipe with groundnut oil",
				MethodSteps:  []string{"Combine and serve."},
				Ingredients:  []aidraft.InventedIngredientLine{{IngredientID: groundnutOil, QuantityG: 5}},
				DishFormatID: archetypes[0],
				Model:        "test-model",
			}, nil
		},
	}
	nameByID := map[string]string{groundnutOil: "Groundnut oil"}
	if _, err := generateInventedCard(ctx, pool, fake, permissive, cat, "v1", res,
		permissiveSafe, permissiveAllowed, nameByID, archetypes); err != nil {
		t.Fatalf("generateInventedCard (permissive): %v", err)
	}

	// A child with a confirmed Peanut allergy: ingredient_allergen_override excludes Groundnut
	// oil from their own SafeIngredients call, so the row just stored must not clear it.
	peanutAllergic := models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian", Allergens: []string{"Peanut"}}
	peanutSafe, err := engine.SafeIngredients(ctx, pool, peanutAllergic)
	if err != nil {
		t.Fatalf("SafeIngredients (peanut-allergic): %v", err)
	}
	peanutAllowed := make(map[string]bool, len(peanutSafe))
	peanutNames := make(map[string]string, len(peanutSafe))
	for _, s := range peanutSafe {
		peanutAllowed[s.IngredientID] = true
		peanutNames[s.IngredientID] = s.Name
	}
	if peanutAllowed[groundnutOil] {
		t.Fatalf("ingredient_allergen_override did not exclude %s for a Peanut-allergic child; "+
			"this test's fixture assumption no longer holds", groundnutOil)
	}

	_, ok, err := findStoredInventedCard(ctx, pool, cat, "v1", res, peanutAllergic, peanutAllowed, peanutNames, archetypes, map[string]bool{})
	if err != nil {
		t.Fatalf("findStoredInventedCard: %v", err)
	}
	if ok {
		t.Fatalf("findStoredInventedCard served a Groundnut-oil recipe to a Peanut-allergic child")
	}
}

// TestInventedRecipeAssembleBook2ReusesAcrossChildren exercises reuse across two children in a
// row, the second given a drafter that fails the test outright if Gemini drafting is ever
// invoked -- the second child succeeding proves the chapter was filled from ai_recipe alone.
//
// Exercised directly against topUpInvented, not through AssembleBook2's public entry point (the
// shape this test used before mainMealCategories was added): AssembleBook2 now only ever
// iterates the three chapters with real recipe_master.meal_type coverage, so it never reaches a
// category with zero real candidates any more, and this test's whole premise needs one. The
// reuse mechanism itself -- findStoredInventedCard, ai_recipe, ai_recipe_ingredient -- is
// exactly the same code either way, still exercised with real DB round trips; only the entry
// point moved to the layer where "zero real candidates" is still a reachable case. See
// TestInventedRecipeTopUpFillsACategoryWithNoRealCandidates for the fuller version of this
// reasoning.
func TestInventedRecipeAssembleBook2ReusesAcrossChildren(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cat := testMealCategory(t, 4)
	cleanupAIRecipes(t, pool, cat.ID)
	res := models.EngineResult{ActiveTarget: "NT00", TargetReason: "default"}

	working := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			return aidraft.InventedRecipe{
				Name:        "Test invented recipe (seed)",
				MethodSteps: []string{"Combine and serve."},
				Ingredients: []aidraft.InventedIngredientLine{
					{IngredientID: req.AllowedIngredients[0].IngredientID, QuantityG: 50},
				},
				DishFormatID: req.DishFormatArchetypes[0],
				Model:        "test-model",
			}, nil
		},
	}
	childA := models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian"}
	if cards, _ := topUpInvented(ctx, pool, working, childA, cat, "v1", res, nil); len(cards) == 0 {
		t.Fatalf("topUpInvented (child A, seeding the store) filled zero cards")
	}

	mustNotDraft := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			t.Fatalf("DraftInventedRecipe was called for child B; a compatible stored recipe "+
				"should have been reused instead (req=%+v)", req)
			return aidraft.InventedRecipe{}, nil
		},
	}
	childB := models.ChildProfile{AgeMonths: 36, DietType: "Non-vegetarian"}
	cards, note := topUpInvented(ctx, pool, mustNotDraft, childB, cat, "v1", res, nil)
	if len(cards) == 0 {
		t.Fatalf("category %s did not fill for child B from stored recipes alone; note=%q", cat.ID, note)
	}
}
