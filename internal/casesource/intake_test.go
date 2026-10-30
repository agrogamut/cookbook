package casesource

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

const bengaliDeliveryHash = "4efae52ed55f1b2fa6bf91df5ef00ac0c7801680c54f75d093366665d7689ef3"

func bengaliDelivery(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("CASE_SOURCE_BENGALI_ARCHIVE")
	if path == "" {
		path = "../../data/case-source/" + FinalArchiveSHA256 + ".zip"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash(b) != bengaliDeliveryHash {
		t.Fatal("Bengali delivery bytes differ from the independently audited attachment")
	}
	return b
}

func TestIntakeDelivery(t *testing.T) {
	b, original := bengaliDelivery(t), delivery(t)
	i, err := ParseIntake(context.Background(), b, original)
	if err != nil {
		t.Fatal(err)
	}
	r := i.Report
	if r.Workbooks != 60 || r.Recipes != 6000 || r.Audits != 6000 || r.Occurrences != 60106 || r.Translations != 78106 || r.Glossary != 2420 || r.ExpandedBytes != 138135088 {
		t.Fatalf("delivery totals: %+v", r)
	}
	if !r.ActivationBlocked || r.ParserVersion != LocaleParserVersion || r.BaselineSHA256 != deliveryHash || r.ArchiveSHA256 != bengaliDeliveryHash {
		t.Fatal("version/provenance contract differs")
	}
	if r.FindingCounts["allergen_associations_removed"] != 376 || r.AllergenRemovals["ALG-MUSTARD"] != 340 || r.AllergenRemovals["ALG-SHELLFISH"] != 46 || len(r.AllergenRemovals) != 2 || r.FindingCounts["rule_fingerprint_changed"] != 6000 || r.FieldChanges["Safety_Rule"] != 1400 || r.FieldChanges["Review_Status"] != 0 || r.FieldChanges["AI_Adaptation_Rule"] != 0 {
		t.Fatalf("validation findings: counts=%v field changes=%v removals=%v", r.FindingCounts, r.FieldChanges, r.AllergenRemovals)
	}
	oldRows := map[string]Row{}
	for _, w := range i.Baseline.Workbooks {
		for _, row := range sheetByName(w, "Recipe_Master").Rows {
			if row.RecipeID != "" {
				oldRows[row.RecipeID] = row
			}
		}
	}
	files, err := packageFiles(b, 128)
	if err != nil {
		t.Fatal(err)
	}
	var quantities, tofu, milk, c26 int
	for _, w := range i.Archive.Workbooks {
		member, err := readMember(files[w.Name])
		if err != nil || hash(member) != w.SHA256 {
			t.Fatalf("member bytes: %s %v", w.Name, err)
		}
		for _, row := range sheetByName(w, "Recipe_Master").Rows {
			if row.RecipeID == "" {
				continue
			}
			old, ok := oldRows[row.RecipeID]
			if !ok || len(old.Ingredients) != len(row.Ingredients) {
				t.Fatal("logical recipe/occurrence mismatch", row.RecipeID)
			}
			for n, ingredient := range row.Ingredients {
				if ingredient.RawGrams != old.Ingredients[n].RawGrams || ingredient.Ordinal != n+1 {
					t.Fatal("raw quantity/ordinal changed", row.RecipeID)
				}
				quantities++
				if strings.TrimSpace(ingredient.RawID) == "ING0075" {
					tofu++
					fields := Fields(sheetByName(w, "Recipe_Master"), row)
					tags, _ := comparisonAllergens(fields["Allergen_Tags"])
					if !tags["ALG-SOY"] {
						t.Fatal("tofu soy association absent")
					}
					if tags["ALG-MILK"] {
						milk++
					}
				}
			}
		}
	}
	for _, row := range i.Locales {
		if row.EnglishSHA256 != hash([]byte(row.English)) || row.Row <= 2 || row.MasterRow <= 4 {
			t.Fatal("locale provenance incomplete")
		}
		if row.RecipeID == "MG-WB-C26-001" && row.Field == "ingredient" {
			if row.Ordinal == 8 {
				if row.Ingredient.RawName != "Cumin" || row.Ingredient.RawID != "ING0093" || row.RawQuantity != "0.1" {
					t.Fatalf("C26 cumin: %+v", row)
				}
				c26++
			}
			if row.Ordinal == 10 {
				if row.Ingredient.RawName != "Potable water" || row.Ingredient.RawID != "WBING0080" || row.RawQuantity != "42" {
					t.Fatalf("C26 water: %+v", row)
				}
				c26++
			}
		}
	}
	if quantities != 60106 || tofu != 287 || milk != 0 || c26 != 2 {
		t.Fatalf("occurrence/tofu controls: %d %d %d %d", quantities, tofu, milk, c26)
	}
	if _, err := Parse(context.Background(), b); err == nil {
		t.Fatal("legacy parser accepted v2")
	}
	t.Logf("INTAKE_FIDELITY_OK workbooks=%d recipes=%d occurrences=%d translations=%d glossary=%d", r.Workbooks, r.Recipes, r.Occurrences, r.Translations, r.Glossary)
	t.Logf("INTAKE_BLOCKERS_OK removed_recipes=%d mustard=%d shellfish=%d safety_cells=%d rule_fingerprints=%d", r.FindingCounts["allergen_associations_removed"], r.AllergenRemovals["ALG-MUSTARD"], r.AllergenRemovals["ALG-SHELLFISH"], r.FieldChanges["Safety_Rule"], r.FindingCounts["rule_fingerprint_changed"])
}

func intakeFixture(t *testing.T, edit func(*excelize.File)) ([]byte, []byte) {
	t.Helper()
	base := fixtureWorkbook(t, func(f *excelize.File) {
		for cell, text := range map[string]string{"B5": "Source title", "H5": "Breakfast", "O5": "1) Measure 0.125 g. 2) Add 12.50 g water.", "AG5": "Source safety", "AB5": "ALG-SOY;ALG-MUSTARD"} {
			setCell(t, f, "Recipe_Master", cell, text)
		}
	})
	f, err := excelize.OpenReader(bytes.NewReader(base))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	setCell(t, f, "Recipe_Master", "AL4", "Nutrient_Gap")
	setCell(t, f, "Recipe_Master", "AB5", "Soy")
	for sheet, headers := range map[string][]string{"For_translation": {"Recipe_ID", "Meal_Type", "Field", "English", "Quantity_g", "Bengali", "Note"}, "Glossary": {"English", "Where it prints", "Bengali"}} {
		if _, err := f.NewSheet(sheet); err != nil {
			t.Fatal(err)
		}
		for col, value := range headers {
			ref, _ := excelize.CoordinatesToCellName(col+1, 2)
			setCell(t, f, sheet, ref, value)
		}
	}
	rows := [][]string{{"title", "Source title", ""}, {"ingredient", "Cumin", "0.125"}, {"ingredient", "Potable water", "12.50"}, {"method", "1) Measure 0.125 g. 2) Add 12.50 g water.", ""}, {"safety", "Source safety", ""}}
	for n, row := range rows {
		values := []string{"MG-WB-C26-001", "Breakfast", row[0], row[1], row[2], "বাংলা", ""}
		for col, value := range values {
			ref, _ := excelize.CoordinatesToCellName(col+1, n+3)
			setCell(t, f, "For_translation", ref, value)
		}
	}
	for col, value := range []string{"Prep", "Recipe timing label", "প্রস্তুতি"} {
		ref, _ := excelize.CoordinatesToCellName(col+1, 3)
		setCell(t, f, "Glossary", ref, value)
	}
	if edit != nil {
		edit(f)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return zipBytes(t, zipEntry{name: "MadamGY_WB_Case_26_Bengali.xlsx", data: buf.Bytes()}), zipBytes(t, zipEntry{name: fixtureMember, data: base})
}

func TestIntakeContracts(t *testing.T) {
	b, baseline := intakeFixture(t, nil)
	i, err := ParseIntake(context.Background(), b, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(i.Locales) != 5 || len(i.Glossary) != 1 || i.Locales[2].Ingredient.RawGrams != " 12.50" || i.Report.AllergenRemovals["ALG-MUSTARD"] != 1 {
		t.Fatalf("fixture associations: %+v", i)
	}
	old, err := Parse(context.Background(), baseline)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := ParseIntake(context.Background(), baseline, nil)
	if err != nil || !reflect.DeepEqual(old, legacy.Archive) || legacy.Report.ActivationBlocked {
		t.Fatal("old format changed", err)
	}
	t.Run("legacy Bengali filename", func(t *testing.T) {
		files, err := packageFiles(baseline, 128)
		if err != nil {
			t.Fatal(err)
		}
		member, err := readMember(files[fixtureMember])
		if err != nil {
			t.Fatal(err)
		}
		legacyName := zipBytes(t, zipEntry{name: "MadamGY_WB_Case_26_Bengali.xlsx", data: member})
		parsed, err := ParseIntake(context.Background(), legacyName, nil)
		if err != nil || parsed.Report.ParserVersion != ParserVersion {
			t.Fatal("a filename alone changed the two-sheet contract", err)
		}
	})
	if _, err := ParseIntake(context.Background(), b, nil); err == nil {
		t.Fatal("missing comparison source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseIntake(ctx, b, baseline); err == nil {
		t.Fatal("cancelled intake succeeded")
	}
	for _, tc := range []struct {
		name, sheet, cell, value string
	}{
		{"missing target", "For_translation", "F3", ""},
		{"wrong English", "For_translation", "D3", "Different title"},
		{"unknown field", "For_translation", "C3", "clinical"},
		{"missing field", "For_translation", "C7", "method"},
		{"unknown recipe", "For_translation", "A3", "MG-WB-C26-999"},
		{"wrong meal", "For_translation", "B3", "Dinner"},
		{"quantity changed", "For_translation", "E4", "0.126"},
		{"occurrence reordered", "For_translation", "D4", "Potable water"},
		{"extra quantity", "For_translation", "E3", "1"},
		{"header drift", "For_translation", "D2", "Source"},
		{"master drift", "Recipe_Master", "AL4", "New fact"},
		{"missing glossary site", "Glossary", "B3", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad, original := intakeFixture(t, func(f *excelize.File) { setCell(t, f, tc.sheet, tc.cell, tc.value) })
			if _, err := ParseIntake(context.Background(), bad, original); err == nil {
				t.Fatal("invalid source association accepted")
			}
		})
	}
	t.Run("formula", func(t *testing.T) {
		bad, original := intakeFixture(t, func(f *excelize.File) {
			if err := f.SetCellFormula("For_translation", "E4", "1/8"); err != nil {
				t.Fatal(err)
			}
		})
		if _, err := ParseIntake(context.Background(), bad, original); err == nil {
			t.Fatal("formula accepted")
		}
	})
	files, _ := packageFiles(b, 128)
	member, _ := readMember(files["MadamGY_WB_Case_26_Bengali.xlsx"])
	remaining := uint64(1)
	if _, err := parseWorkbookFormat(member, 26, &remaining, true); err == nil {
		t.Fatal("expanded limit ignored")
	}
	for _, entries := range [][]zipEntry{
		{{name: "../MadamGY_WB_Case_26_Bengali.xlsx", data: member}},
		{{name: "MadamGY_WB_Case_26_Bengali.xlsx", data: member}, {name: "MadamGY_WB_Case_26_Bengali.xlsx", data: member}},
		{{name: "MadamGY_WB_Case_26_Bengali.xlsx", data: member}, {name: fixtureMember, data: member}},
	} {
		if _, err := ParseIntake(context.Background(), zipBytes(t, entries...), baseline); err == nil {
			t.Fatal("unsafe, repeated or mixed member accepted")
		}
	}
	encoded, err := json.Marshal(i.Report)
	if err != nil || !bytes.Contains(encoded, []byte(`"activation_blocked":true`)) {
		t.Fatal("activation blocker missing from report")
	}
	t.Log("INTAKE_BLOCKERS_OK synthetic loss computed independently of delivery totals")
}
