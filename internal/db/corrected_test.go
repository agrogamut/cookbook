package db_test

import (
	"context"
	"testing"
)

// The corrected nutrition layer has to satisfy two things at once: it must actually
// correct, and it must not touch what the provider shipped.
var correctedViolations = []violation{
	{
		name: "every alias points at a real IFCT food",
		why:  "an alias to a missing food code would silently leave the ingredient uncorrected",
		query: `SELECT a.ingredient_english_name FROM ingredient_ifct_alias a
		        LEFT JOIN external_food_composition f USING (food_code) WHERE f.food_code IS NULL`,
	},
	{
		name: "every alias names a real ingredient",
		why:  "an alias for a name that does not exist is a typo, and the ingredient it meant stays uncorrected",
		query: `SELECT a.ingredient_english_name FROM ingredient_ifct_alias a
		        LEFT JOIN ingredient_master i ON i.english_name = a.ingredient_english_name
		        WHERE i.ingredient_id IS NULL`,
	},
	{
		name: "the provider ingredient table is never modified",
		why:  "correction happens in a view; the provider's own numbers stay exactly as shipped",
		query: `SELECT c.ingredient_id FROM ingredient_nutrition_corrected c
		        JOIN ingredient_master i USING (ingredient_id)
		        WHERE c.provider_energy_kcal_100g IS DISTINCT FROM i.energy_kcal_100g
		           OR c.provider_iron_mg_100g     IS DISTINCT FROM i.iron_mg_100g`,
	},
	{
		name: "an IFCT-sourced row carries its food code",
		why:  "a corrected value without a source cannot be traced back",
		query: `SELECT ingredient_id FROM ingredient_nutrition_corrected
		        WHERE value_source = 'ifct' AND (ifct_food_code IS NULL OR ifct_match_exactness IS NULL)`,
	},
	{
		name: "a provider-sourced row claims no verification",
		why:  "an unverified placeholder must never be presented as checked",
		query: `SELECT ingredient_id FROM ingredient_nutrition_corrected
		        WHERE value_source = 'provider' AND verified`,
	},
	{
		name: "recomputed coverage is a fraction",
		why:  "coverage outside 0 to 1 means the mass accounting is wrong",
		query: `SELECT recipe_id FROM recipe_nutrition_recomputed
		        WHERE ingredient_coverage < 0 OR ingredient_coverage > 1`,
	},
	{
		name: "a fully verified recipe has full coverage",
		why:  "the boolean and the fraction must agree or one of them is lying",
		query: `SELECT recipe_id FROM recipe_nutrition_recomputed
		        WHERE fully_verified AND ingredient_coverage < 1`,
	},
	{
		name: "recomputed energy is plausible",
		why:  "a recipe cannot exceed 9 kcal per gram, which is pure fat",
		query: `SELECT recipe_id FROM recipe_nutrition_recomputed
		        WHERE energy_kcal < 0 OR energy_kcal > total_mass_g * 9`,
	},
}

func TestCorrectedNutrition(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var loaded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM external_food_composition`).Scan(&loaded); err != nil {
		t.Fatalf("probe composition: %v", err)
	}
	if loaded == 0 {
		t.Skip("no composition data loaded; run cmd/enrich first")
	}

	for _, v := range correctedViolations {
		t.Run(v.name, func(t *testing.T) {
			rows, err := pool.Query(ctx, v.query)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			defer rows.Close()

			var offenders []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					t.Fatalf("scan: %v", err)
				}
				offenders = append(offenders, id)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate: %v", err)
			}
			if len(offenders) > 0 {
				shown := offenders
				if len(shown) > 10 {
					shown = shown[:10]
				}
				t.Errorf("%d rows violate this invariant (%s); first offenders: %v",
					len(offenders), v.why, shown)
			}
		})
	}
}

// TestBrinjalIsNotEgg pins the specific defect this layer was built for.
//
// In the provider workbook, Brinjal/Eggplant carries byte-identical nutrition to Egg,
// Duck egg and Quail egg: 143 kcal and 12.6 g protein per 100 g, against IFCT's 27 kcal
// and 1.8 g. The cause looks like a name lookup matching "egg" inside "Eggplant". It
// affects every recipe containing brinjal, and it makes those recipes look like they
// carry animal protein.
//
// The provider row is left exactly as shipped. The corrected view is what the
// application reads, and this asserts the correction is actually applied.
func TestBrinjalIsNotEgg(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var providerEnergy, correctedEnergy, correctedProtein float64
	var source, foodCode string
	err := pool.QueryRow(ctx, `
		SELECT provider_energy_kcal_100g, energy_kcal_100g, protein_g_100g,
		       value_source, coalesce(ifct_food_code, '')
		FROM ingredient_nutrition_corrected
		WHERE english_name = 'Brinjal/Eggplant'`).
		Scan(&providerEnergy, &correctedEnergy, &correctedProtein, &source, &foodCode)
	if err != nil {
		t.Fatalf("read brinjal: %v", err)
	}

	if providerEnergy != 143 {
		t.Errorf("provider brinjal energy is %v, expected the shipped 143 kcal; "+
			"either the workbook changed or the provider column was modified", providerEnergy)
	}
	if source != "ifct" {
		t.Fatalf("brinjal is still on the provider placeholder (source %q); the alias is not applied", source)
	}
	if correctedEnergy > 40 {
		t.Errorf("corrected brinjal energy is %v kcal/100g; IFCT gives 27, so this still looks like egg", correctedEnergy)
	}
	if correctedProtein > 3 {
		t.Errorf("corrected brinjal protein is %v g/100g; IFCT gives 1.8, so this still looks like egg", correctedProtein)
	}

	// The three egg rows must be untouched: they were always correct.
	var eggEnergy float64
	if err := pool.QueryRow(ctx,
		`SELECT energy_kcal_100g FROM ingredient_nutrition_corrected WHERE english_name = 'Egg'`,
	).Scan(&eggEnergy); err != nil {
		t.Fatalf("read egg: %v", err)
	}
	if eggEnergy < 100 {
		t.Errorf("egg energy is %v kcal/100g, which is too low; the correction overreached", eggEnergy)
	}
}

// TestNutritionPlaceholdersAreStillDetectable keeps the original finding measurable. If
// a future provider release ships real per-ingredient values, this fails and the whole
// corrected layer should be revisited rather than left running unnecessarily.
func TestNutritionPlaceholdersAreStillDetectable(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var ingredients, valueSets int
	err := pool.QueryRow(ctx, `
		SELECT count(*),
		       count(DISTINCT (energy_kcal_100g, protein_g_100g, iron_mg_100g, calcium_mg_100g))
		FROM ingredient_master`).Scan(&ingredients, &valueSets)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	t.Logf("%d ingredients carry %d distinct nutrition value sets", ingredients, valueSets)
	if valueSets > ingredients/2 {
		t.Errorf("the provider now ships %d value sets for %d ingredients, which no longer looks "+
			"like group-level placeholders; revisit whether the corrected layer is still needed",
			valueSets, ingredients)
	}
}
