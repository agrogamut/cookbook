package casesource

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func finalDelivery(t *testing.T) *Intake {
	t.Helper()
	i, err := ParseIntake(context.Background(), bengaliDelivery(t), delivery(t))
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestFinalSourceContract(t *testing.T) {
	i := finalDelivery(t)
	if !AcceptedArchive(i.Archive.SHA256) || AcceptedArchive(i.Baseline.SHA256) || len(finalContract.Rules) != 6000 {
		t.Fatal("accepted source identity differs")
	}
	files := map[string]string{}
	books := map[string]*excelize.File{}
	for _, ref := range finalContract.Ingredients {
		if _, ok := files[ref.File]; !ok {
			b, err := os.ReadFile("../../data/provider/" + ref.File)
			if err != nil {
				t.Fatal(err)
			}
			files[ref.File] = hash(b)
			book, err := excelize.OpenFile("../../data/provider/" + ref.File)
			if err != nil {
				t.Fatal(err)
			}
			books[ref.File] = book
			t.Cleanup(func() {
				if err := book.Close(); err != nil {
					t.Error(err)
				}
			})
		}
		if files[ref.File] != ref.SHA256 || ref.Row < 4 || ref.Fields["Ingredient_ID"] != ref.SourceID || ref.Fields["English_Name"] != ref.SourceName {
			t.Fatal("ingredient reference identity differs", ref.SourceID)
		}
		book := books[ref.File]
		rows, err := book.GetRows(ref.Sheet, excelize.Options{RawCellValue: true})
		if err != nil || len(rows) < ref.Row {
			t.Fatal("reference row absent", err)
		}
		for key, value := range ref.Fields {
			found := false
			for col, header := range rows[3] {
				if header != key {
					continue
				}
				cell, _ := excelize.CoordinatesToCellName(col+1, ref.Row)
				got, err := book.GetCellValue(ref.Sheet, cell, excelize.Options{RawCellValue: true})
				if err != nil || got != value {
					t.Fatal("reference cell differs", ref.SourceID, key, cell, err)
				}
				found = true
			}
			if !found {
				t.Fatal("reference header absent", key)
			}
		}
	}
	var recipes, quantities, addedRecipes, mustard, shellfish, tofu, mixed, mixedEligible, nullEnergy int
	holds := map[string]int{}
	for _, w := range i.Archive.Workbooks {
		s := sheetByName(w, "Recipe_Master")
		for _, row := range s.Rows {
			if row.RecipeID == "" {
				continue
			}
			f := Fields(s, row)
			original := make(map[string]string, len(f))
			for k, v := range f {
				original[k] = v
			}
			p, err := finalPolicy(f, row.Ingredients)
			if err != nil {
				t.Fatal(row.RecipeID, err)
			}
			if !reflect.DeepEqual(f, original) || len(f) != 38 {
				t.Fatal("raw fields modified")
			}
			declared, unknown := comparisonAllergens(f["Allergen_Tags"])
			if len(unknown) != 0 {
				t.Fatal("unexpected final label", unknown)
			}
			added := false
			for _, code := range p.AllergenCodes {
				if !declared[code] {
					added = true
					switch code {
					case "ALG-MUSTARD":
						mustard++
					case "ALG-SHELLFISH":
						shellfish++
					default:
						t.Fatal("unexpected derived association", row.RecipeID, code)
					}
				}
				age, _ := strconv.Atoi(f["Min_Age_Months"])
				r := Recipe{Citation: Citation{ArchiveSHA256: i.Archive.SHA256, Case: w.Case, RecipeID: row.RecipeID}, Fields: f, Policy: &p}
				found := false
				for _, gap := range Assess(r, Input{AgeMonths: age, AllergenCodes: []string{code}}) {
					found = found || gap.Code == "allergen_excluded"
				}
				if !found {
					t.Fatal("derived allergen bypass", row.RecipeID, code)
				}
			}
			if added {
				addedRecipes++
			}
			for _, h := range p.Holds {
				holds[h.Code]++
				if h.Code == "whole_soy_reformulation_missing" {
					tofu++
				}
			}
			for _, gap := range p.DataGaps {
				if gap.Code == "soy_form_ambiguous" {
					mixed++
					age, _ := strconv.Atoi(f["Min_Age_Months"])
					r := Recipe{Citation: Citation{ArchiveSHA256: i.Archive.SHA256, Case: w.Case, RecipeID: row.RecipeID}, Fields: f, Policy: &p}
					if len(Assess(r, Input{AgeMonths: age})) == 0 {
						mixedEligible++
					}
				}
			}
			if (Recipe{Fields: f}).SourceNumbers()["Energy_kcal"] == nil {
				nullEnergy++
			}
			quantities += len(row.Ingredients)
			recipes++
		}
	}
	if recipes != 6000 || quantities != 60106 || addedRecipes != 376 || mustard != 340 || shellfish != 46 || tofu != 287 || mixed != 389 || mixedEligible == 0 || nullEnergy == 0 {
		t.Fatalf("counts recipes=%d quantities=%d corrected=%d mustard=%d shellfish=%d tofu=%d mixed=%d usable_mixed=%d nullenergy=%d", recipes, quantities, addedRecipes, mustard, shellfish, tofu, mixed, mixedEligible, nullEnergy)
	}
	t.Logf("FINAL_CONTRACT_OK recipes=%d quantities=%d corrected=%d mustard=%d shellfish=%d tofu_holds=%d existing_mixed=%d usable_mixed=%d null_energy=%d holds=%v", recipes, quantities, addedRecipes, mustard, shellfish, tofu, mixed, mixedEligible, nullEnergy, holds)
}

func TestFinalPolicyRejectsUnmappedSource(t *testing.T) {
	i := finalDelivery(t)
	s := sheetByName(i.Archive.Workbooks[0], "Recipe_Master")
	for _, row := range s.Rows {
		if row.RecipeID == "" {
			continue
		}
		f := Fields(s, row)
		f["Safety_Rule"] += " changed"
		if _, err := finalPolicy(f, row.Ingredients); err == nil {
			t.Fatal("changed rule accepted by pinned mapping")
		}
		break
	}
	if AcceptedArchive(strings.Repeat("0", 64)) {
		t.Fatal("unrecognized delivery accepted")
	}
}

func TestFinalSourceLabelAmendments(t *testing.T) {
	i := finalDelivery(t)
	before, _ := json.Marshal(i)
	count, methodEN, methodBN, useEN := 0, 0, 0, 0
	locales := map[string][]LocaleRow{}
	for _, l := range i.Locales {
		locales[l.RecipeID] = append(locales[l.RecipeID], l)
	}
	for _, w := range i.Archive.Workbooks {
		s := sheetByName(w, "Recipe_Master")
		for _, row := range s.Rows {
			if row.RecipeID == "" {
				continue
			}
			f := Fields(s, row)
			p, err := finalPolicy(f, row.Ingredients)
			if err != nil {
				t.Fatal(err)
			}
			for _, hold := range p.Holds {
				if hold.Code == "source_method_form_conflict" {
					t.Errorf("%s held only for stale dish-family name", row.RecipeID)
				}
			}
			r := Recipe{CandidateID: CandidateID(FinalArchiveSHA256, row.RecipeID), Citation: Citation{ArchiveSHA256: FinalArchiveSHA256, MemberSHA256: w.SHA256, Workbook: w.Name, Case: w.Case, RecipeID: row.RecipeID, Sheet: s.Name, Row: row.Number}, Fields: f, Policy: &p}
			if r.PresentationText("Safety_Rule", "en", f["Safety_Rule"]) != f["Safety_Rule"] {
				t.Fatal("warning changed")
			}
			for _, a := range p.TextAmendments {
				raw := f[a.Field]
				if a.Language == "bn" {
					for _, l := range locales[row.RecipeID] {
						if l.Field == "method" {
							raw = l.Bengali
							if a.Cell != fmt.Sprintf("F%d", l.Row) {
								t.Fatal("target cell differs")
							}
						}
					}
				} else {
					prefix := "O"
					if a.Field == "Book2_Use" {
						prefix = "AJ"
					}
					if a.Cell != fmt.Sprintf("%s%d", prefix, row.Number) {
						t.Fatal("master cell differs")
					}
				}
				if hash([]byte(raw)) != a.RawSHA256 {
					t.Fatal("raw cell differs", row.RecipeID, a.Field)
				}
				got := r.PresentationText(a.Field, a.Language, raw)
				want := strings.Replace(raw, a.Before, a.After, 1)
				if raw == got || got != want || hash([]byte(got)) != a.DisplaySHA256 || strings.Count(raw, a.Before) != 1 {
					t.Fatal("span differs", row.RecipeID, a.Field, a.Language)
				}
				// Exact outside-span equality preserves negations, duplicate phrases and numeric tokens.
				if raw[:a.ByteOffset] != got[:a.ByteOffset] || raw[a.ByteOffset+len(a.Before):] != got[a.ByteOffset+len(a.After):] {
					t.Fatal("outside span changed")
				}
				nounFound := false
				for _, l := range locales[row.RecipeID] {
					if l.Field == "ingredient" && a.NounSourceCell == fmt.Sprintf("For_translation!F%d", l.Row) && l.English == "Soybean/soy chunks" && hash([]byte(l.Bengali)) == a.NounSHA256 && l.Bengali == "সয়াবিন" {
						nounFound = true
					}
				}
				if !nounFound {
					t.Fatal("supplied noun not linked")
				}
				for _, kind := range []string{"archive", "member", "candidate", "raw"} {
					bad := r
					text := raw
					switch kind {
					case "archive":
						bad.Citation.ArchiveSHA256 = deliveryHash
					case "member":
						bad.Citation.MemberSHA256 = strings.Repeat("0", 64)
					case "candidate":
						bad.CandidateID += "x"
					case "raw":
						text += " Changed."
					}
					if bad.PresentationText(a.Field, a.Language, text) != text {
						t.Fatal("unmatched source amended", kind)
					}
				}
				count++
				if a.Language == "bn" {
					methodBN++
				} else if a.Field == "Book2_Use" {
					useEN++
				} else {
					methodEN++
				}
			}
			if len(p.TextAmendments) == 0 && r.PresentationText("Preparation_Method_FULL", "en", f["Preparation_Method_FULL"]) != f["Preparation_Method_FULL"] {
				t.Fatal("unlisted recipe changed")
			}
		}
	}
	after, _ := json.Marshal(i)
	if string(before) != string(after) {
		t.Fatal("raw source mutated")
	}
	if count != 40 || methodEN != 9 || methodBN != 9 || useEN != 22 {
		t.Fatal(count, methodEN, methodBN, useEN)
	}
	t.Logf("FINAL_LABEL_AMENDMENTS_OK English_methods=%d Bengali_methods=%d Book2_Use=%d raw_and_outside_span_exact=true", methodEN, methodBN, useEN)
}
