package db_test

import (
	"context"
	"testing"
)

// The three name-identical mappings are the only ones the provider has asserted. If this
// count grows, a ruling arrived and the documents quoting 354 need updating in the same
// commit; if it shrinks, someone deleted an assertion the provider made.
func TestMealCategoryMapHoldsOnlyAssertedMappings(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var inferred int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM meal_category_recipe_map WHERE basis <> 'provider-identical-name'`).
		Scan(&inferred); err != nil {
		t.Fatalf("basis query: %v", err)
	}
	if inferred != 0 {
		t.Fatalf("%d mappings claim a basis other than provider-identical-name. A "+
			"provider-ruling row is legitimate but must arrive in its own migration with "+
			"the ruling's date recorded, so update this test in that commit", inferred)
	}

	// Every asserted mapping must join two strings that really are identical. This is what
	// makes "the provider asserted it" true rather than a label we applied.
	var mismatched int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM meal_category_recipe_map m
		JOIN meal_category_target t ON t.meal_category_id = m.meal_category_id
		WHERE m.basis = 'provider-identical-name' AND t.meal_category <> m.meal_type`).
		Scan(&mismatched); err != nil {
		t.Fatalf("identity query: %v", err)
	}
	if mismatched != 0 {
		t.Fatalf("%d rows are marked provider-identical-name but the two strings differ; "+
			"that basis is a claim about the data, not a category to file guesses under",
			mismatched)
	}
}

// The defect this migration exists to make visible. Not an assertion that 354 is correct
// forever -- an assertion that the number is measured and reported rather than hidden.
func TestUnreachableRecipesAreCounted(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var unreachable int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM recipe_master r
		WHERE NOT EXISTS (
		    SELECT 1 FROM meal_category_recipe_map m WHERE m.meal_type = r.meal_type)`).
		Scan(&unreachable); err != nil {
		t.Fatalf("unreachable query: %v", err)
	}

	var registered int
	if err := pool.QueryRow(ctx,
		`SELECT coalesce(affected_rows, -1) FROM gap_register WHERE gap_id = 'GAP-023'`).
		Scan(&registered); err != nil {
		t.Fatalf("GAP-023: %v", err)
	}
	if registered != unreachable {
		t.Fatalf("GAP-023 reports %d unreachable recipes but the data holds %d; the gap "+
			"register is only useful if its numbers are the measured ones",
			registered, unreachable)
	}
}

// A recipe must never reach a chapter it was not mapped to. This is the property that makes
// omitting an empty chapter safe rather than lossy.
func TestMealCategoryRecipeViewOnlyServesMappedTypes(t *testing.T) {
	pool := testPool(t)
	var leaked int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM meal_category_recipe v
		WHERE NOT EXISTS (
		    SELECT 1 FROM meal_category_recipe_map m
		    WHERE m.meal_category_id = v.meal_category_id AND m.meal_type = v.meal_type)`).
		Scan(&leaked); err != nil {
		t.Fatalf("leak query: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("%d rows in meal_category_recipe are not backed by a mapping row", leaked)
	}
}
