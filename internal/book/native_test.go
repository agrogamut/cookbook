package book

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/bookdocx"
)

func TestNativeModuleContract(t *testing.T) {
	for _, language := range []string{"en", "bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			s := storedProfileAged(8)
			s.DisplayName = "Synthetic child"
			s.LanguageID = language
			set := Set{AsOf: time.Now(), Book1: Book1{Metadata: Metadata{Language: language}}, Book2: Book2{Metadata: Metadata{Language: language}, MealSections: []MealSection{{Title: "Snack", Recipes: []RecipeCard{{RecipeID: "MG-WB-TEST-001", Title: "Source recipe", Number: 1, Serving: "79.6 g", ReviewStatus: "Draft - source review", Ingredients: []IngredientLine{{Name: "Cumin", Quantity: "0.1 g", SourceID: "WBING0080", SourceOrdinal: 8}, {Name: "Potable water", Quantity: "42 g", SourceID: "WBING0080", SourceOrdinal: 10}}}}}}}}
			n := NativeBindings(s, set)
			if err := LocalizeNative(context.Background(), &n, aidraft.Disabled); err != nil {
				t.Fatal(err)
			}
			for bi, req := range n.Books {
				if len(req.Modules) != []int{63, 56}[bi] {
					t.Fatalf("module count %d", len(req.Modules))
				}
				doc, err := bookdocx.Fill(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
				xml := nativeDocumentXML(t, doc.DOCX)
				if !strings.Contains(xml, "MG-WB-TEST-001") || !strings.Contains(xml, "79.6 g") {
					t.Fatal("shared snapshot identity or decimal lost")
				}
				if bi == 1 && (!strings.Contains(xml, "Cumin") || !strings.Contains(xml, "Potable water") || strings.Count(xml, "WBING0080") < 2) {
					t.Fatal("ingredient collisions lost")
				}
			}
		})
	}
}

func nativeDocumentXML(t *testing.T, b []byte) string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatal("document part absent")
	return ""
}

func TestNativeLocalizationProtectsValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transform func(string) string
		wantErr   bool
	}{
		{"preserved", func(s string) string { return "अनुवाद " + s }, false},
		{"ingredient identifier protected", func(s string) string { return strings.ReplaceAll(s, "WBING", "changed") }, false},
		{"changed quantity", func(s string) string { return strings.ReplaceAll(s, "ZXQ1QXZ", "99") }, true},
		{"inserted quantity", func(s string) string { return s + " 99 g" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := NativeSet{Books: [2]bookdocx.Request{{Book: 1, Language: "hi", Modules: map[string]bookdocx.Module{"B1-01": {Blocks: []bookdocx.Block{nativeText("Recipe MG-WB-C26-001 has 79.6 g and WBING0080")}}}}, {Book: 2, Language: "en"}}}
			err := LocalizeNative(context.Background(), &n, &translateFakeDrafter{transform: tc.transform})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error %v", err)
			}
			if err == nil {
				got := n.Books[0].Modules["B1-01"].Blocks[0].Paragraph.Runs[0].Text
				if !strings.Contains(got, "MG-WB-C26-001") || !strings.Contains(got, "79.6 g") || !strings.Contains(got, "WBING0080") {
					t.Fatal(got)
				}
			}
		})
	}
}

func TestNativeSourceSnapshot(t *testing.T) {
	pool := sourceBookPool(t)
	ctx := context.Background()
	s := storedProfileAged(8)
	set, err := AssembleSet(ctx, pool, s, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	n := NativeBindings(s, set)
	if err := NativeIndices(ctx, pool, set, &n); err != nil {
		t.Fatal(err)
	}
	for _, request := range n.Books {
		if _, err := bookdocx.Fill(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	for _, meal := range set.Book2.MealSections {
		for _, r := range meal.Recipes {
			for _, key := range []string{"B1-03"} {
				found := false
				for _, cell := range nativeTextCells(n.Books[0].Modules[key].Blocks) {
					if strings.HasPrefix(*cell, r.RecipeID+" | ") {
						found = true
					}
				}
				if !found {
					t.Fatal(fmt.Sprintf("%s missing %s", key, r.RecipeID))
				}
			}
		}
	}
	selected := map[string]int{}
	for _, meal := range set.Book2.MealSections {
		for _, recipe := range meal.Recipes {
			selected[recipe.RecipeID]++
		}
	}
	full := map[string]int{}
	for key, module := range n.Books[1].Modules {
		for _, entry := range module.Entries {
			if key < "B2-09" || key > "B2-14" {
				t.Fatalf("full recipe duplicated in %s", key)
			}
			full[entry.ID]++
		}
	}
	if len(selected) != len(full) {
		t.Fatalf("selected=%d full entries=%d", len(selected), len(full))
	}
	for id, count := range selected {
		if count != 1 || full[id] != 1 {
			t.Fatalf("recipe %s selected=%d full entries=%d", id, count, full[id])
		}
	}
	t.Logf("selected recipes=%d unique full cards=%d", len(selected), len(full))
}

func TestNativeLocalizationBatchesKeepOrder(t *testing.T) {
	var blocks []bookdocx.Block
	for i := 0; i < 70; i++ {
		blocks = append(blocks, nativeText(fmt.Sprintf("Recipe MG-WB-C12-%03d contains WBING0080 at %d.5 g", i, i)))
	}
	n := NativeSet{Books: [2]bookdocx.Request{{Book: 1, Language: "bn", Modules: map[string]bookdocx.Module{"B1-01": {Blocks: blocks}}}, {Book: 2, Language: "en"}}}
	fake := &translateFakeDrafter{transform: func(s string) string { return "অনুবাদ " + s }}
	if err := LocalizeNative(context.Background(), &n, fake); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&fake.calls) != 3 {
		t.Fatalf("unexpected batch count %d", fake.calls)
	}
	for i, b := range n.Books[0].Modules["B1-01"].Blocks {
		if b.Paragraph.Runs[0].Text != "অনুবাদ "+blocks[i].Paragraph.Runs[0].Text {
			t.Fatalf("batch reordered or changed protected values at %d", i)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := LocalizeNative(ctx, &n, fake); err == nil {
		t.Fatal("cancelled translation succeeded")
	}
}

func TestNativeLocalizationOrdinaryCompounds(t *testing.T) {
	compounds := []string{"age-appropriate", "age-safe", "vitamin-C", "low-medium", "cross-contact", "batch-cooked", "selected-recipe", "clinical-history"}
	for _, language := range []string{"bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			blocks := []bookdocx.Block{nativeText(strings.Join(compounds, " "))}
			for _, word := range compounds {
				blocks = append(blocks, nativeText(word))
			}
			n := NativeSet{Books: [2]bookdocx.Request{{Book: 1, Language: language, Modules: map[string]bookdocx.Module{"B1-01": {Blocks: blocks}}}, {Language: "en"}}}
			d := &translateFakeDrafter{transform: strings.ToUpper}
			if err := LocalizeNative(context.Background(), &n, d); err != nil {
				t.Fatal(err)
			}
			for i, block := range n.Books[0].Modules["B1-01"].Blocks {
				if got, want := block.Paragraph.Runs[0].Text, strings.ToUpper(blocks[i].Paragraph.Runs[0].Text); got != want {
					t.Errorf("ordinary prose skipped translation: %q, want %q", got, want)
				}
			}
		})
	}
}

func TestNativeLocalizationExactSourceFacts(t *testing.T) {
	facts := []string{"MG-WB-C26-001", "MG-R-00042", "WBING0070", "WBING0080", "ING0001", "B1-NUTRITION-01", "B2-56", "CLD-04", "CLR-007", "AF01", "NT02", "WHO-CF-2023", "Book1", "Book2", "v1", "2026-10-24", "9adf9451-e1fa-417d-8d43-e10af0d0c8a9", "3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2", "0.005 g", "79.6 g", ".005 mg", "-0.25 kg", "+1.25 cm", "120 mL", "0.5 µg", "12 min", "62.5%", "https://example.invalid/source-01"}
	for _, language := range []string{"bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			original := "Use age-safe preparation: " + strings.Join(facts, " | ")
			raw := "Clinical_Tag: age-safe source wording; Review_Status: Draft; Data_Quality: Provisional; audit PASS"
			n := NativeSet{Books: [2]bookdocx.Request{{Language: language, Modules: map[string]bookdocx.Module{"B1-01": {Blocks: []bookdocx.Block{nativeText(original), nativeText(raw)}}}}, {Language: "en"}}}
			if err := LocalizeNative(context.Background(), &n, &translateFakeDrafter{transform: func(s string) string {
				for _, fact := range facts {
					if strings.Contains(s, fact) {
						t.Errorf("source fact exposed to translation: %q", fact)
					}
				}
				return strings.ToUpper(s)
			}}); err != nil {
				t.Fatal(err)
			}
			got := n.Books[0].Modules["B1-01"].Blocks
			if got[0].Paragraph.Runs[0].Text != "USE AGE-SAFE PREPARATION: "+strings.Join(facts, " | ") {
				t.Fatal(got[0].Paragraph.Runs[0].Text)
			}
			if got[1].Paragraph.Runs[0].Text != raw {
				t.Fatal("explicit raw source/status field changed")
			}
		})
	}
}

func TestNativeLocalizationSourceFilenames(t *testing.T) {
	file := "MadamGY_WB_Case_26_CLR_001_100_Recipes.xlsx"
	text := "Workbook " + file + "; Recipe_Master; ingredient_master.quantity_g; ALG-MILK; WHO-GROWTH; IAP-DEVELOPMENT; MG-INTERNAL; GENERIC-GAS; ____________________; age-safe recipe MG-WB-C26-001"
	n := NativeSet{Books: [2]bookdocx.Request{{Language: "bn", Modules: map[string]bookdocx.Module{"B2-56": {Blocks: []bookdocx.Block{nativeText(text)}}}}, {Language: "en"}}}
	if err := LocalizeNative(context.Background(), &n, &translateFakeDrafter{transform: strings.ToUpper}); err != nil {
		t.Fatal(err)
	}
	if got := n.Books[0].Modules["B2-56"].Blocks[0].Paragraph.Runs[0].Text; got != "WORKBOOK "+file+"; Recipe_Master; ingredient_master.quantity_g; ALG-MILK; WHO-GROWTH; IAP-DEVELOPMENT; MG-INTERNAL; GENERIC-GAS; ____________________; AGE-SAFE RECIPE MG-WB-C26-001" {
		t.Fatal(got)
	}
}
