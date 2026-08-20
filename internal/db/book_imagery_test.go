package db_test

import (
	"context"
	"testing"
)

// Every in-scope recipe resolves to exactly one dish-format mark, and every seeded format
// matches at least one recipe. Both directions matter: a seed that matches nothing is dead
// weight, and a recipe that matches nothing prints a page with a hole where the illustration
// goes.
func TestEveryRecipeResolvesOneFormatMark(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var unmatched int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM recipe_master r
		WHERE NOT EXISTS (SELECT 1 FROM recipe_format_mark m
		                  WHERE r.recipe_name LIKE '%' || m.format_pattern || '%')`,
	).Scan(&unmatched); err != nil {
		t.Fatalf("count unmatched: %v", err)
	}
	if unmatched != 0 {
		t.Errorf("%d recipes match no dish format", unmatched)
	}

	var ambiguous int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT r.recipe_id FROM recipe_master r
			JOIN recipe_format_mark m ON r.recipe_name LIKE '%' || m.format_pattern || '%'
			GROUP BY 1 HAVING count(*) > 1) x`,
	).Scan(&ambiguous); err != nil {
		t.Fatalf("count ambiguous: %v", err)
	}
	if ambiguous != 0 {
		t.Errorf("%d recipes match more than one dish format; one seeded pattern is a substring of another", ambiguous)
	}

	var dead int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM recipe_format_mark m
		WHERE NOT EXISTS (SELECT 1 FROM recipe_master r
		                  WHERE r.recipe_name LIKE '%' || m.format_pattern || '%')`,
	).Scan(&dead); err != nil {
		t.Fatalf("count dead: %v", err)
	}
	if dead != 0 {
		t.Errorf("%d seeded formats match no recipe", dead)
	}

	// recipe_mark is the view a template reads from; confirm it actually resolves every
	// in-scope recipe rather than only the underlying table joining cleanly.
	var recipes, marked int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_master`).Scan(&recipes); err != nil {
		t.Fatalf("count recipes: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT recipe_id) FROM recipe_mark`).Scan(&marked); err != nil {
		t.Fatalf("count recipe_mark: %v", err)
	}
	if marked != recipes {
		t.Errorf("recipe_mark resolves %d of %d recipes", marked, recipes)
	}
}

// Every recipe's composition shares sum to 1, and an ingredient whose food group is not in
// food_group_macro is carried as its own "Unmapped" share rather than renormalised away. A band
// that silently drops part of the dish's mass is a band that says the dish is something it is
// not.
func TestCompositionSharesSumToOneAndCarryTheUnmapped(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var offSum int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT recipe_id, sum(share) AS total FROM recipe_composition_share
			GROUP BY recipe_id
			HAVING abs(sum(share) - 1) > 0.01) x`,
	).Scan(&offSum); err != nil {
		t.Fatalf("count off-sum recipes: %v", err)
	}
	if offSum != 0 {
		t.Errorf("%d recipes have composition shares that do not sum to 1", offSum)
	}

	// basis_g on every row for a recipe must equal the recipe's total mapped gram mass --
	// not just the mass of the groups that happen to be in food_group_macro. That is the
	// mechanism that would carry an unmapped food group as its own share instead of
	// quietly excluding it from both the numerator and the denominator.
	var basisMismatch int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT DISTINCT s.recipe_id, s.basis_g
			FROM recipe_composition_share s) basis
		JOIN (
			SELECT recipe_id, sum(quantity) AS actual_total
			FROM recipe_ingredient_mapping
			WHERE unit = 'g'
			GROUP BY recipe_id) actual USING (recipe_id)
		WHERE abs(basis.basis_g - actual.actual_total) > 0.01`,
	).Scan(&basisMismatch); err != nil {
		t.Fatalf("count basis mismatches: %v", err)
	}
	if basisMismatch != 0 {
		t.Errorf("%d recipes have a composition basis_g that does not equal their total mapped mass; "+
			"an unmapped food group's mass is being dropped instead of carried as Unmapped", basisMismatch)
	}

	// The 21 food groups actually used by in-scope recipes are all named in
	// food_group_macro today, so no recipe should currently show an Unmapped share. That
	// is a fact about the current data, not a promise the mechanism depends on -- if a
	// future provider release adds an ingredient in an unmapped food group, its mass must
	// still appear as its own "Unmapped" segment rather than vanish.
	var unmappedGroups int
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT im.food_group)
		FROM recipe_ingredient_mapping rim
		JOIN ingredient_master im USING (ingredient_id)
		LEFT JOIN food_group_macro fgm ON fgm.food_group = im.food_group
		WHERE fgm.food_group IS NULL`,
	).Scan(&unmappedGroups); err != nil {
		t.Fatalf("count unmapped food groups: %v", err)
	}
	if unmappedGroups != 0 {
		t.Logf("%d food groups in use are not named in food_group_macro and fall to Unmapped", unmappedGroups)
	}
}

// The photograph table exists and is empty, and GAP-025's affected_rows -- 940, seeded in
// migration 0018 -- is checked against a live count rather than trusted as a static number, so a
// photograph added later without the internal/importer/gaps.go wiring the migration comment
// describes is caught here instead of silently going stale.
func TestRecipePhotographsAreCountedNotAsserted(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var photos, recipes int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_photo`).Scan(&photos); err != nil {
		t.Fatalf("count recipe_photo: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipe_master`).Scan(&recipes); err != nil {
		t.Fatalf("count recipe_master: %v", err)
	}

	var gapCount *int
	if err := pool.QueryRow(ctx,
		`SELECT affected_rows FROM gap_register WHERE gap_id = 'GAP-025'`,
	).Scan(&gapCount); err != nil {
		t.Fatalf("read GAP-025: %v", err)
	}
	if gapCount == nil {
		t.Fatal("GAP-025 has a NULL affected_rows")
	}

	if *gapCount != recipes-photos {
		t.Errorf("GAP-025 says %d recipes lack a photograph; a live count says %d of %d do",
			*gapCount, recipes-photos, recipes)
	}
}
