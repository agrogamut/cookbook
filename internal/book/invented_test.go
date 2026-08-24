package book

import (
	"context"
	"html/template"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
	"github.com/madamgy/recipie/internal/profile"
)

// fakeDrafter is a test double for aidraft.Drafter. Both methods report
// aidraft.ErrDraftingUnavailable unless the matching field is set, mirroring the real
// disabled client's default behaviour so a test only has to wire up what it actually exercises.
type fakeDrafter struct {
	inventFn func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error)
	doctorFn func(ctx context.Context, req aidraft.DoctorApproachRequest) (aidraft.DraftedText, error)
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

// unmappedCategory returns one meal_category_target row (GAP-023: a category with zero rows
// in the meal_category_recipe view) whose own target parses to a positive number -- the exact
// shape the invented-recipe fallback exists for: real recipes have nothing to offer at all.
func unmappedCategory(t *testing.T, pool *pgxpool.Pool) (id, name string, target int) {
	t.Helper()
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT t.meal_category_id, t.meal_category, coalesce(t.default_target_recipes, '')
		FROM meal_category_target t
		WHERE NOT EXISTS (
		    SELECT 1 FROM meal_category_recipe m WHERE m.meal_category_id = t.meal_category_id)`)
	if err != nil {
		t.Fatalf("query unmapped categories: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var i, n, targetText string
		if err := rows.Scan(&i, &n, &targetText); err != nil {
			t.Fatalf("scan unmapped category: %v", err)
		}
		if tgt, err := strconv.Atoi(strings.TrimSpace(targetText)); err == nil && tgt > 0 {
			return i, n, tgt
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("unmapped category rows: %v", err)
	}
	t.Skip("no unmapped meal category with a positive target on the current dataset")
	return "", "", 0
}

// TestInventedRecipeToppUpFillsAnUnmappedCategory pins the case the fallback exists for: a
// meal category real recipes have nothing at all for (GAP-023) still renders, filled entirely
// by AI-invented recipes drawn from the child's own safe-ingredient allow-list.
func TestInventedRecipeTopUpFillsAnUnmappedCategory(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	catID, catName, target := unmappedCategory(t, pool)

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

	s := profile.Stored{
		ChildID:     "BOOK-TEST-AIDRAFT-001",
		DateOfBirth: time.Now().AddDate(-3, 0, 0), // 36 months, above inventedRecipeMinAgeMonths
		DietType:    "Non-vegetarian",
	}
	b, skipped, err := AssembleBook2(ctx, pool, s, time.Now(), WithDrafter(fake))
	if err != nil {
		t.Fatalf("AssembleBook2: %v", err)
	}

	var got *MealSection
	for i := range b.MealSections {
		if b.MealSections[i].MealCategoryID == catID {
			got = &b.MealSections[i]
		}
	}
	if got == nil {
		t.Fatalf("category %s (%s) did not render even with a working Drafter; skipped=%v", catID, catName, skipped)
	}
	if len(got.Recipes) == 0 {
		t.Fatalf("category %s rendered with zero recipes", catID)
	}
	for _, r := range got.Recipes {
		// Source stays set on the struct -- the operator console's fact-check surface needs
		// it -- but the printed page must never carry a tell. ReviewStatus and
		// SelectionReasons read exactly like a real card's; recipe.html has no branch on
		// Source at all any more.
		if r.Source != "ai-invented" {
			t.Fatalf("recipe %s in an unmapped category has Source %q, want \"ai-invented\"", r.RecipeID, r.Source)
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
	if len(got.Recipes) > target {
		t.Fatalf("category %s got %d recipes, more than its own target %d", catID, len(got.Recipes), target)
	}

	html := renderRecipeCardHTML(t, got.Recipes[0])
	if strings.Contains(html, "ai-badge") || strings.Contains(strings.ToLower(html), "ai-invented recipe") {
		t.Fatalf("rendered recipe page for %s still carries an AI-invented disclosure badge", got.Recipes[0].RecipeID)
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
// downstream check would have caught the response.
func TestInventedRecipeFallbackNeverRunsBelowMinAge(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	catID, catName, _ := unmappedCategory(t, pool)

	fake := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			t.Fatalf("DraftInventedRecipe was called for a child below inventedRecipeMinAgeMonths; "+
				"the age gate must stop this before any drafting request is made (req=%+v)", req)
			return aidraft.InventedRecipe{}, nil
		},
	}

	s := profile.Stored{
		ChildID:     "BOOK-TEST-AIDRAFT-003",
		DateOfBirth: time.Now().AddDate(0, -12, 0), // 12 months, below inventedRecipeMinAgeMonths (24)
		DietType:    "Non-vegetarian",
	}
	b, skipped, err := AssembleBook2(ctx, pool, s, time.Now(), WithDrafter(fake))
	if err != nil {
		t.Fatalf("AssembleBook2: %v", err)
	}

	for _, sec := range b.MealSections {
		if sec.MealCategoryID == catID {
			t.Fatalf("category %s (%s) rendered for a 12-month-old from an unmapped category; "+
				"the only possible source is the invented-recipe fallback, which must not run "+
				"below the age gate", catID, catName)
		}
	}

	found := false
	for _, sk := range skipped {
		if strings.HasPrefix(sk, omissionMealCategory) && strings.Contains(sk, catID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("category %s must still be reported as an omission for a child below the "+
			"invented-recipe age gate, got skipped=%v", catID, skipped)
	}
}

// TestInventedRecipeOutsideAllowedSetIsRejected pins the guardrail that actually matters: a
// model response naming an ingredient outside the child-safe allow-list must never reach a
// printed card, even though the response schema was already supposed to prevent it -- this is
// the defense-in-depth check, exercised by a fake that deliberately violates the schema's own
// contract the way a real model still might.
//
// The category's own ai_recipe rows are cleared first. Reuse-before-regenerate means a
// category with a *good* stored recipe from an earlier test can still render even when this
// test's own fake keeps producing bad output -- correct behaviour for the feature, but it
// would make this specific test about the fake's rejected response rather than about
// anything the store already held, so the store is emptied before this test asks its own
// question.
func TestInventedRecipeOutsideAllowedSetIsRejected(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	catID, catName, _ := unmappedCategory(t, pool)
	if _, err := pool.Exec(ctx, `DELETE FROM ai_recipe WHERE meal_category_id = $1`, catID); err != nil {
		t.Fatalf("clear ai_recipe for %s: %v", catID, err)
	}

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

	s := profile.Stored{
		ChildID:     "BOOK-TEST-AIDRAFT-002",
		DateOfBirth: time.Now().AddDate(-3, 0, 0),
		DietType:    "Non-vegetarian",
	}
	b, skipped, err := AssembleBook2(ctx, pool, s, time.Now(), WithDrafter(fake))
	if err != nil {
		t.Fatalf("AssembleBook2: %v", err)
	}

	for _, sec := range b.MealSections {
		if sec.MealCategoryID == catID {
			t.Fatalf("category %s (%s) rendered despite every invented recipe failing "+
				"validation; a rejected response must never reach a printed card", catID, catName)
		}
	}

	found := false
	for _, sk := range skipped {
		if strings.HasPrefix(sk, omissionMealCategory) && strings.Contains(sk, catID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("category %s must still be reported as an omission when every invented "+
			"recipe fails validation, got skipped=%v", catID, skipped)
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

// TestInventedRecipeAssembleBook2ReusesAcrossChildren exercises reuse through the public
// AssembleBook2 entry point rather than the package-private helpers directly: two children in
// a row, the second given a drafter that fails the test outright if Gemini drafting is ever
// invoked. Whatever filled the store first -- this test's own first child, or another test's
// leftover row for the same GAP-023 category, since this package's tests share one live
// database -- the second child succeeding proves the chapter was filled from ai_recipe alone.
func TestInventedRecipeAssembleBook2ReusesAcrossChildren(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	catID, catName, _ := unmappedCategory(t, pool)

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
	profileA := profile.Stored{
		ChildID:     "BOOK-TEST-AIDRAFT-REUSE-A",
		DateOfBirth: time.Now().AddDate(-3, 0, 0),
		DietType:    "Non-vegetarian",
	}
	if _, _, err := AssembleBook2(ctx, pool, profileA, time.Now(), WithDrafter(working)); err != nil {
		t.Fatalf("AssembleBook2 (child A, seeding the store): %v", err)
	}

	mustNotDraft := fakeDrafter{
		inventFn: func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error) {
			t.Fatalf("DraftInventedRecipe was called for child B; a compatible stored recipe "+
				"should have been reused instead (req=%+v)", req)
			return aidraft.InventedRecipe{}, nil
		},
	}
	profileB := profile.Stored{
		ChildID:     "BOOK-TEST-AIDRAFT-REUSE-B",
		DateOfBirth: time.Now().AddDate(-3, 0, 0),
		DietType:    "Non-vegetarian",
	}
	b, skipped, err := AssembleBook2(ctx, pool, profileB, time.Now(), WithDrafter(mustNotDraft))
	if err != nil {
		t.Fatalf("AssembleBook2 (child B): %v", err)
	}

	var got *MealSection
	for i := range b.MealSections {
		if b.MealSections[i].MealCategoryID == catID {
			got = &b.MealSections[i]
		}
	}
	if got == nil {
		t.Fatalf("category %s (%s) did not render for child B from stored recipes alone; skipped=%v",
			catID, catName, skipped)
	}
	if len(got.Recipes) == 0 {
		t.Fatalf("category %s rendered with zero recipes for child B", catID)
	}
}
