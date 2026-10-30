package book

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/bookdocx"
	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
)

func finalLocaleCard() RecipeCard {
	citation := casesource.Citation{ArchiveSHA256: casesource.FinalArchiveSHA256, RecipeID: "MG-WB-C26-001", Case: 26, Row: 5, Sheet: "Recipe_Master"}
	f := map[string]string{"Recipe_Name": "Source recipe", "Ingredient_Names": "Cumin;Potable water", "Ingredient_Quantities_g": "0.1; 42", "Preparation_Method_FULL": "Measure cumin (0.1 g). Add water (42 g). Do not add milk.", "Safety_Rule": "Do not add milk.", "Review_Status": "Draft source status"}
	l := &casesource.Locale{Language: "bn", Source: citation, Sheet: "For_translation"}
	for _, v := range []struct {
		field, english, text, quantity string
		ordinal                        int
	}{
		{"title", f["Recipe_Name"], "উৎস রেসিপি", "", 0},
		{"ingredient", "Cumin", "জিরা", "0.1", 1},
		{"ingredient", "Potable water", "পানীয় জল", "42", 2},
		{"method", f["Preparation_Method_FULL"], "জিরা (0.1 g) মাপুন। জল (42 g) দিন। দুধ দেবেন না।", "", 0},
		{"safety", f["Safety_Rule"], "দুধ দেবেন না।", "", 0},
	} {
		l.Texts = append(l.Texts, casesource.LocaleText{Field: v.field, Ordinal: v.ordinal, English: v.english, EnglishSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(v.english))), Text: v.text, Quantity: v.quantity, Row: len(l.Texts) + 3})
	}
	return RecipeCard{SourceCandidateID: casesource.CandidateID(citation.ArchiveSHA256, citation.RecipeID), SourceRevision: &citation, SourceLocale: l, SourceFields: f, RecipeID: citation.RecipeID, Title: f["Recipe_Name"], Number: 1, MethodSteps: splitMethodSteps(f["Preparation_Method_FULL"]), Safety: f["Safety_Rule"], Ingredients: []IngredientLine{{Name: "Cumin", SourceID: "ING0093", SourceOrdinal: 1, Quantity: "0.1 g"}, {Name: "Potable water", SourceID: "WBING0080", SourceOrdinal: 2, Quantity: "42 g"}}}
}

func TestFinalSourceMethodFieldFidelity(t *testing.T) {
	baseline, err := os.ReadFile("../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../data/case-source/" + casesource.FinalArchiveSHA256 + ".zip")
	if err != nil {
		t.Fatal(err)
	}
	i, err := casesource.ParseIntake(context.Background(), data, baseline)
	if err != nil {
		t.Fatal(err)
	}
	var count, quantities int
	for _, w := range i.Archive.Workbooks {
		for _, s := range w.Sheets {
			if s.Name != "Recipe_Master" {
				continue
			}
			for _, row := range s.Rows {
				if row.RecipeID == "" {
					continue
				}
				f := casesource.Fields(s, row)
				raw := f["Preparation_Method_FULL"]
				want, markers := methodCorpusOracle(raw)
				got := splitMethodSteps(raw)
				if !reflect.DeepEqual(got, want) {
					t.Fatal("source method characters or order changed", row.RecipeID)
				}
				if len(markers) > 0 {
					var restored strings.Builder
					for j, m := range markers {
						restored.WriteString(m)
						restored.WriteString(got[j])
					}
					if restored.String() != raw {
						t.Fatal("source not reconstructed", row.RecipeID)
					}
				}
				blocks := nativeRecipe(RecipeCard{RecipeID: row.RecipeID, MethodSteps: got}, "", 45)
				for j, step := range got {
					if blocks[j+1].Paragraph.Runs[0].Text != step {
						t.Fatal("method binding changed", row.RecipeID)
					}
				}
				if !reflect.DeepEqual(parenthesizedQuantity.FindAllString(raw, -1), parenthesizedQuantity.FindAllString(strings.Join(got, ""), -1)) {
					t.Fatal("method quantity occurrence changed", row.RecipeID)
				}
				quantities += len(parenthesizedQuantity.FindAllString(raw, -1))
				count++
			}
		}
	}
	if count != 6000 {
		t.Fatal(count)
	}
	t.Logf("FINAL_METHOD_OK raw_cells=%d complete_text_order=true parenthesized_quantities=%d", count, quantities)
}

func TestFinalSourceBookCards(t *testing.T) {
	pool := sourceBookPool(t)
	ctx := context.Background()
	baseline, err := os.ReadFile("../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../data/case-source/" + casesource.FinalArchiveSHA256 + ".zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := casesource.ImportIntake(ctx, pool, data, baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := casesource.Activate(ctx, pool, casesource.FinalArchiveSHA256); err != nil {
		t.Fatal(err)
	}
	var ids []string
	rows, err := pool.Query(ctx, `SELECT candidate_id FROM case_source_recipe_current WHERE case_number=36 ORDER BY recipe_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	cards, gaps, err := loadSourceCards(ctx, pool, ids, "CS:Snack", models.ChildProfile{AgeMonths: 36})
	if err != nil || len(cards) == 0 {
		t.Fatal("source cards", len(cards), gaps, err)
	}
	var selected []RecipeCard
	for _, id := range ids {
		if r, ok := cards[id]; ok {
			r.Number = len(selected) + 1
			selected = append(selected, r)
			if len(r.SourceFields) != 38 || r.SourceLocale == nil || r.Nutrition != nil {
				t.Fatal("source locale/gap missing")
			}
		}
	}
	b := Book2{Metadata: Metadata{Language: "bn"}, MealSections: []MealSection{{Title: "Snack", Recipes: selected}}}
	n := NativeBindings(storedProfileAged(36), Set{AsOf: time.Now(), Book1: Book1{Metadata: Metadata{Language: "bn"}}, Book2: b})
	if err := LocalizeNative(ctx, &n, aidraft.Disabled); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(n.Books[1])
	for _, r := range selected {
		for _, v := range r.SourceLocale.Texts {
			b, _ := json.Marshal(v.Text)
			if !strings.Contains(string(encoded), string(b[1:len(b)-1])) {
				t.Fatal("actual supplied Bengali field missing", r.RecipeID, v.Field)
			}
		}
	}
	var retiredID string
	if err := pool.QueryRow(ctx, `SELECT recipe_id FROM recipe_ingredient_mapping WHERE ingredient_id='ING0075' ORDER BY recipe_id LIMIT 1`).Scan(&retiredID); err != nil {
		t.Fatal(err)
	}
	legacy, omissions, err := loadRecipeCards(ctx, pool, []string{retiredID}, "MC-01", "test", nil, nil, nil, nil, models.ChildProfile{AgeMonths: 36}, models.EngineResult{}, aidraft.Disabled, nil)
	if err != nil || len(legacy) != 0 || len(omissions) == 0 {
		t.Fatal("direct retired card loader bypass", err)
	}
	safe, err := engine.SafeIngredients(ctx, pool, models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatal(err)
	}
	allowed, names := map[string]bool{}, map[string]string{}
	for _, v := range safe {
		allowed[v.IngredientID] = true
		names[v.IngredientID] = v.Name
	}
	archetypes := MarkIDs()
	unsupported := aidraft.InventedRecipe{Name: "Synthetic retired tofu control", MethodSteps: []string{"Synthetic test only."}, DishFormatID: archetypes[0], Ingredients: []aidraft.InventedIngredientLine{{IngredientID: "ING0075", QuantityG: 1}}}
	if err := validateInventedRecipe(unsupported, allowed, archetypes); err == nil {
		t.Fatal("fresh fallback accepted tofu")
	}
	cat := mealCategory{ID: "MC-01", Name: "Breakfast", Target: 1}
	stored, err := persistInventedRecipe(ctx, pool, cat, models.ChildProfile{AgeMonths: 36, DietType: "Vegetarian"}, unsupported)
	if err != nil {
		t.Fatal(err)
	}
	excluded := map[string]bool{}
	storedRows, err := pool.Query(ctx, `SELECT recipe_id FROM ai_recipe WHERE recipe_id<>$1`, stored)
	if err != nil {
		t.Fatal(err)
	}
	for storedRows.Next() {
		var id string
		if err := storedRows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		excluded[id] = true
	}
	storedRows.Close()
	if err := storedRows.Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := findStoredInventedCard(ctx, pool, cat, "test", models.EngineResult{}, models.ChildProfile{AgeMonths: 36}, allowed, names, archetypes, excluded); err != nil || ok {
		t.Fatal("stored tofu fallback reentered", err)
	}
	t.Logf("FINAL_BOOK_OK source_cards=%d supplied_fields_preserved=true direct_tofu_guard=true", len(selected))
}

func TestFinalSourceNativeBengaliPreference(t *testing.T) {
	card := finalLocaleCard()
	for _, language := range []string{"en", "bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			s := storedProfileAged(9)
			s.LanguageID = language
			set := Set{AsOf: time.Now(), Book1: Book1{Metadata: Metadata{Language: language}}, Book2: Book2{Metadata: Metadata{Language: language}, MealSections: []MealSection{{Title: "Snack", Recipes: []RecipeCard{card}}}}}
			before, _ := json.Marshal(set)
			n := NativeBindings(s, set)
			if err := LocalizeNative(context.Background(), &n, aidraft.Disabled); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(set)
			if string(before) != string(after) {
				t.Fatal("shared assembled source changed")
			}
			encoded, _ := json.Marshal(n.Books)
			if language == "bn" {
				for _, x := range card.SourceLocale.Texts {
					if !strings.Contains(string(encoded), x.Text) {
						t.Fatal("supplied text absent", x.Field)
					}
				}
				if len(n.SourceLocales) != 1 {
					t.Fatal("source provenance absent")
				}
			} else if strings.Contains(string(encoded), card.SourceLocale.Texts[0].Text) || len(n.SourceLocales) != 0 {
				t.Fatal("Bengali attached to another language")
			}
		})
	}
}

func TestFinalSourceNoRedundantTranslation(t *testing.T) {
	r := finalLocaleCard()
	l := r.SourceLocale
	n := NativeSet{SourceLocales: map[string]*casesource.Locale{r.RecipeID: l}, Books: [2]bookdocx.Request{{Book: 1, Language: "bn", Modules: map[string]bookdocx.Module{"B1-03": {Blocks: []bookdocx.Block{nativeText(l.Texts[0].Text), nativeText(l.Texts[3].Text)}}}}, {Book: 2, Language: "en"}}}
	d := &translateFakeDrafter{transform: func(text string) string { t.Error("supplied source entered translation", text); return text }}
	if err := LocalizeNative(context.Background(), &n, d); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&d.calls) != 0 {
		t.Fatal("redundant source translation")
	}
	n.Books[0].Modules["B1-03"] = bookdocx.Module{Blocks: []bookdocx.Block{nativeText("Recipe: " + l.Texts[0].Text + " [" + r.RecipeID + "]"), nativeTable([]string{r.RecipeID, r.Title, "Clinical_Tag", "General"}), nativeTable([]string{"Review_Status", "Draft source status"})}}
	d = &translateFakeDrafter{transform: func(text string) string {
		for _, v := range l.Texts {
			if strings.Contains(text, v.Text) {
				t.Error("authoritative text sent unprotected", v.Field)
			}
		}
		return "অনুবাদ " + text
	}}
	if err := LocalizeNative(context.Background(), &n, d); err != nil {
		t.Fatal(err)
	}
	b := n.Books[0].Modules["B1-03"].Blocks
	if !strings.Contains(b[0].Paragraph.Runs[0].Text, l.Texts[0].Text) || b[1].Table.Rows[0][1] != l.Texts[0].Text || b[2].Table.Rows[0][1] != "Draft source status" {
		t.Fatal("source title/raw status changed")
	}
	bad := *l
	bad.Source.ArchiveSHA256 = strings.Repeat("0", 64)
	r.SourceLocale = &bad
	_, locales := nativeSourceLanguage(Book2{Metadata: Metadata{Language: "bn"}, MealSections: []MealSection{{Recipes: []RecipeCard{r}}}})
	if len(locales) != 0 {
		t.Fatal("historical locale attached by recipe ID")
	}
}
