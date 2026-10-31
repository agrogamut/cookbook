package book

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/bookdocx"
)

func NativeIndices(ctx context.Context, pool *pgxpool.Pool, set Set, n *NativeSet) error {
	byID := map[string]RecipeCard{}
	var providerIDs []string
	for _, meal := range set.Book2.MealSections {
		for _, r := range meal.Recipes {
			byID[r.RecipeID] = r
			if r.SourceRevision == nil {
				providerIDs = append(providerIDs, r.RecipeID)
			}
		}
	}
	groups := map[int][][]string{}
	add := func(module int, id, field, value string) {
		groups[module] = append(groups[module], []string{id, byID[id].Title, field, value})
	}
	rows, err := pool.Query(ctx, `SELECT DISTINCT r.recipe_id,i.ingredient_id,i.english_name,i.food_group
		FROM recipe_master r CROSS JOIN LATERAL unnest(string_to_array(r.ingredient_ids,';')) occurrence(id)
		JOIN ingredient_master i ON i.ingredient_id=btrim(occurrence.id)
		WHERE r.recipe_id=ANY($1) ORDER BY r.recipe_id,i.ingredient_id`, providerIDs)
	if err != nil {
		return fmt.Errorf("native ingredient indices: %w", err)
	}
	for rows.Next() {
		var id, ingredient, name, group string
		if err := rows.Scan(&id, &ingredient, &name, &group); err != nil {
			rows.Close()
			return err
		}
		module := 0
		switch group {
		case "Cereal", "Cereal product", "Millet", "Millet product", "Pseudocereal", "Cereal-like grain", "Cereal-like seed", "Flour", "Fermented cereal mix":
			module = 16
		case "Pulse", "Pulse product", "Legume", "Soy product":
			module = 17
		case "Vegetable", "Leafy vegetable", "Root vegetable", "Fruit", "Dried fruit", "Tuber":
			module = 18
		case "Dairy":
			module = 19
		case "Fish", "Dried fish", "Fish product":
			module = 21
		}
		if group == "Animal protein" {
			switch name {
			case "Egg", "Duck egg", "Quail egg":
				module = 20
			case "Chicken", "Mutton/Goat meat":
				module = 22
			}
		}
		if module != 0 {
			add(module, id, "ingredient_master "+ingredient, name+"; food_group="+group)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, meal := range set.Book2.MealSections {
		for _, r := range meal.Recipes {
			growth, clinical := r.SourceFields["Growth_Target"], r.SourceFields["Clinical_Tag"]
			for _, tag := range r.NutritionTags {
				if strings.HasPrefix(tag, "Growth target: ") {
					growth = strings.TrimPrefix(tag, "Growth target: ")
				}
				if strings.HasPrefix(tag, "Clinical tag: ") {
					clinical = strings.TrimPrefix(tag, "Clinical tag: ")
				}
			}
			switch growth {
			case "Balanced growth support", "Catch-up nutrition support", "Energy adequacy", "Healthy weight maintenance":
				add(23, r.RecipeID, "Growth_Target", growth)
			case "Protein adequacy":
				add(24, r.RecipeID, "Growth_Target", growth)
			case "Iron adequacy":
				add(25, r.RecipeID, "Growth_Target", growth)
			case "Calcium adequacy":
				add(26, r.RecipeID, "Growth_Target", growth)
			case "Fibre adequacy":
				add(27, r.RecipeID, "Growth_Target", growth)
			}
			switch clinical {
			case "Iron-deficiency-support option":
				add(25, r.RecipeID, "Clinical_Tag", clinical)
			case "Constipation-support option":
				add(31, r.RecipeID, "Clinical_Tag", clinical)
			case "Fever/recovery option", "Fever/recovery/low appetite":
				add(29, r.RecipeID, "Clinical_Tag", clinical)
			case "Diarrhoea recovery option", "Vomiting recovery option":
				add(30, r.RecipeID, "Clinical_Tag", clinical)
			}
			if r.SourceRevision != nil {
				switch strings.TrimSpace(strings.Split(clinical, " | ")[0]) {
				case "Growth", "Growth/Diet Pattern":
					add(23, r.RecipeID, "Clinical_Tag", clinical)
				case "Iron/Anemia":
					add(25, r.RecipeID, "Clinical_Tag", clinical)
				case "Bone Health":
					add(26, r.RecipeID, "Clinical_Tag", clinical)
				case "Constipation":
					add(27, r.RecipeID, "Clinical_Tag", clinical)
					add(31, r.RecipeID, "Clinical_Tag", clinical)
				case "Acute Diarrhoea", "Vomiting/Poor Intake":
					add(30, r.RecipeID, "Clinical_Tag", clinical)
				}
			}
		}
	}
	for module, values := range groups {
		key := fmt.Sprintf("B2-%02d", module)
		blocks := []bookdocx.Block{nativeText("Index of recipes already selected for this child; categories retain the named source field and are not nutrient measurements or treatment prescriptions."), nativeTable(append([][]string{{"Original recipe ID", "Recipe", "Source field", "Source value"}}, values...)...)}
		if module >= 16 && module <= 22 {
			blocks = append(blocks, nativeText("Source case ingredients have no verified food-group crosswalk and are not assigned to these groups by numeric ingredient suffix."))
		}
		n.Books[1].Modules[key] = bookdocx.Module{Blocks: blocks}
		n.nativeOwnIndex(key, blocks, values)
	}
	return nil
}
