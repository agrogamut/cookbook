package book

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/profile"
)

// fakeDrafter is a test double for aidraft.Drafter. Both methods report
// aidraft.ErrDraftingUnavailable unless the matching field is set, mirroring the real
// disabled client's default behaviour so a test only has to wire up what it actually exercises.
type fakeDrafter struct {
	inventFn func(ctx context.Context, req aidraft.InventedRecipeRequest) (aidraft.InventedRecipe, error)
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
		if r.Source != "ai-invented" {
			t.Fatalf("recipe %s in an unmapped category has Source %q, want \"ai-invented\"", r.RecipeID, r.Source)
		}
	}
	if len(got.Recipes) > target {
		t.Fatalf("category %s got %d recipes, more than its own target %d", catID, len(got.Recipes), target)
	}
}

// TestInventedRecipeOutsideAllowedSetIsRejected pins the guardrail that actually matters: a
// model response naming an ingredient outside the child-safe allow-list must never reach a
// printed card, even though the response schema was already supposed to prevent it -- this is
// the defense-in-depth check, exercised by a fake that deliberately violates the schema's own
// contract the way a real model still might.
func TestInventedRecipeOutsideAllowedSetIsRejected(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	catID, catName, _ := unmappedCategory(t, pool)

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
