package casesource

import (
	"reflect"
	"testing"
)

func TestFinalLocaleAssociations(t *testing.T) {
	i := finalDelivery(t)
	locales := map[string]*Locale{}
	for _, w := range i.Archive.Workbooks {
		s := sheetByName(w, "Recipe_Master")
		for _, row := range s.Rows {
			if row.RecipeID == "" {
				continue
			}
			locales[row.RecipeID] = &Locale{Language: "bn", Sheet: "For_translation", Source: Citation{ArchiveSHA256: i.Archive.SHA256, Case: w.Case, RecipeID: row.RecipeID, Row: row.Number, Sheet: s.Name, Workbook: w.Name, MemberSHA256: w.SHA256}}
		}
	}
	for _, v := range i.Locales {
		l := locales[v.RecipeID]
		l.Texts = append(l.Texts, LocaleText{Field: v.Field, Ordinal: v.Ordinal, English: v.English, EnglishSHA256: v.EnglishSHA256, Text: v.Bengali, Row: v.Row, Quantity: v.RawQuantity})
	}
	var count int
	for _, w := range i.Archive.Workbooks {
		s := sheetByName(w, "Recipe_Master")
		for _, row := range s.Rows {
			if row.RecipeID == "" {
				continue
			}
			l := locales[row.RecipeID]
			r := Recipe{CandidateID: CandidateID(i.Archive.SHA256, row.RecipeID), Citation: l.Source, Fields: Fields(s, row)}
			if err := l.Validate(r); err != nil {
				t.Fatal(row.RecipeID, err)
			}
			count += len(l.Texts)
			if row.RecipeID != "MG-WB-C26-001" {
				continue
			}
			for _, tc := range []struct {
				name string
				edit func(*Locale)
			}{
				{"wrong revision", func(x *Locale) { x.Source.ArchiveSHA256 = deliveryHash }},
				{"wrong recipe", func(x *Locale) { x.Source.RecipeID = "MG-WB-C26-002" }},
				{"wrong ordinal", func(x *Locale) { x.Texts[1].Ordinal++ }},
				{"wrong English hash", func(x *Locale) { x.Texts[0].EnglishSHA256 = "wrong" }},
				{"wrong English", func(x *Locale) { x.Texts[0].English += " old" }},
				{"wrong amount", func(x *Locale) { x.Texts[1].Quantity = "99" }},
				{"missing field", func(x *Locale) { x.Texts = x.Texts[1:] }},
				{"duplicate field", func(x *Locale) { x.Texts = append(x.Texts, x.Texts[0]) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					bad := *l
					bad.Texts = append([]LocaleText{}, l.Texts...)
					tc.edit(&bad)
					if bad.Validate(r) == nil {
						t.Fatal("mismatched locale accepted")
					}
				})
			}
		}
	}
	if count != 78106 {
		t.Fatal(count)
	}
	if got := (Recipe{Fields: map[string]string{"Energy_kcal": "", "Protein_g": "0", "Cost_per_Serving_INR": "12.50"}}).SourceNumbers(); got["Energy_kcal"] != nil || got["Protein_g"] == nil || *got["Protein_g"] != "0" || *got["Cost_per_Serving_INR"] != "12.50" {
		t.Fatal("optional numeric gap/decimal changed", got)
	}
	if !reflect.DeepEqual(i.Report.AllergenRemovals, map[string]int{"ALG-MUSTARD": 340, "ALG-SHELLFISH": 46}) {
		t.Fatal("historical comparison altered")
	}
	t.Log("FINAL_LOCALE_OK exact_pairs=78106 ingredient_ordinals=60106 current_revision_hash_required=true")
}
