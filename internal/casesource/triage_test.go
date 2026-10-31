package casesource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

var triageSource struct {
	once sync.Once
	i    *Intake
	err  error
}

func triageDelivery(t *testing.T) *Intake {
	t.Helper()
	triageSource.once.Do(func() {
		triageSource.i, triageSource.err = ParseIntake(context.Background(), bengaliDelivery(t), delivery(t))
	})
	if triageSource.err != nil {
		t.Fatal(triageSource.err)
	}
	return triageSource.i
}

func TestTriageRealSource(t *testing.T) {
	i := triageDelivery(t)
	r, err := Triage(context.Background(), i, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.AcceptedFinalSource || r.ActivationPermitted || r.SupplierApprovalVerified || r.CorrectionInput != "not_supplied" || r.ProviderPending != nil || r.CombinedPending != nil {
		t.Fatal("report inferred acceptance or complete provider coverage", r.ProviderCoverage)
	}
	byID := map[string]TriageRecipe{}
	for _, row := range r.Recipes {
		if _, duplicate := byID[row.Source.RecipeID]; duplicate {
			t.Fatal("duplicate report recipe")
		}
		byID[row.Source.RecipeID] = row
	}
	var recipes, pending, allMissing, general, zeroes, dairy, mixed int
	for _, w := range i.Archive.Workbooks {
		s := sheetByName(w, "Recipe_Master")
		for _, row := range s.Rows {
			if row.RecipeID == "" {
				continue
			}
			recipes++
			f := Fields(s, row)
			got, ok := byID[row.RecipeID]
			cells, _ := json.Marshal(row.Cells)
			if !ok || got.RawRowSHA256 == nil || *got.RawRowSHA256 != hash(cells) || got.Source.MemberSHA256 != w.SHA256 || got.Source.Row != row.Number || got.RevisionID != CandidateID(i.Archive.SHA256, row.RecipeID) {
				t.Fatal("source locator or raw-row binding lost", row.RecipeID)
			}
			missing := 0
			for field := range nutrientUnits {
				v, exists := got.Current[field]
				if !exists || (f[field] == "") != (v == nil) || (v != nil && *v != f[field]) {
					t.Fatal("null or source number changed", row.RecipeID, field)
				}
				if f[field] == "" {
					missing++
				}
				if f[field] == "0" {
					zeroes++
				}
			}
			var reportedMissing, reportedGeneral int
			for _, finding := range got.Findings {
				if finding.Code == "nutrition_missing" {
					reportedMissing++
				}
				if finding.Code == "clinical_general_source_value" {
					reportedGeneral++
				}
			}
			if missing != reportedMissing || (f["Clinical_Tag"] == "General") != (reportedGeneral == 1) {
				t.Fatal("source gap classification differs", row.RecipeID)
			}
			if missing == len(nutrientUnits) {
				allMissing++
			}
			general += reportedGeneral
			tofu, soy, milk := false, false, false
			for j, ing := range row.Ingredients {
				if len(got.Ingredients) != len(row.Ingredients) || got.Ingredients[j].Ordinal != ing.Ordinal || got.Ingredients[j].RawID != ing.RawID || got.Ingredients[j].RawName != ing.RawName || got.Ingredients[j].RawGrams != ing.RawGrams {
					t.Fatal("ingredient occurrence changed", row.RecipeID)
				}
				id, name := strings.TrimSpace(ing.RawID), strings.TrimSpace(ing.RawName)
				tofu = tofu || (id == "ING0075" && name == "Tofu")
				soy = soy || (id == "ING0074" && name == "Soybean/soy chunks")
				ref := finalContract.Ingredients[id+"|"+name]
				milk = milk || slices.Contains(strings.Split(ref.Fields["Allergen_Tags"], ";"), "Milk")
			}
			if tofu != hasHold(got.Holds, "whole_soy_reformulation_missing") {
				t.Fatal("pending reformulation does not follow ingredient occurrence", row.RecipeID)
			}
			if tofu {
				pending++
			}
			if soy {
				mixed++
				if !hasHold(got.DataGaps, "soy_form_ambiguous") {
					t.Fatal("mixed-form source gap lost", row.RecipeID)
				}
			}
			if milk {
				dairy++
				if !slices.Contains(got.AllergenCodes, "ALG-MILK") {
					t.Fatal("present dairy control lost", row.RecipeID)
				}
			}
		}
	}
	if len(r.Recipes) != recipes || r.CasePending != pending || pending == 0 || allMissing == 0 || general == 0 || dairy == 0 || mixed == 0 {
		t.Fatalf("report/source controls differ: report=%d source=%d pending=%d/%d null=%d general=%d zero=%d dairy=%d mixed=%d", len(r.Recipes), recipes, r.CasePending, pending, allMissing, general, zeroes, dairy, mixed)
	}
	copyIntake, copyArchive := *i, *i.Archive
	copyArchive.Workbooks = append([]Workbook(nil), i.Archive.Workbooks...)
	slices.Reverse(copyArchive.Workbooks)
	copyIntake.Archive = &copyArchive
	reordered, err := Triage(context.Background(), &copyIntake, nil)
	first, _ := json.Marshal(r)
	second, _ := json.Marshal(reordered)
	if err != nil || string(first) != string(second) {
		t.Fatal("triage depends on workbook iteration order", err)
	}
	t.Logf("TRIAGE_SOURCE_OK recipes=%d pending_case=%d all_nutrition_null=%d general=%d zero_cells=%d dairy_recipes=%d mixed_soy=%d", recipes, pending, allMissing, general, zeroes, dairy, mixed)
}

func TestTriageProviderSupplement(t *testing.T) {
	if _, err := providerTriage([]byte(`{"tofu":{"provider_records":[]}}`)); err == nil {
		t.Fatal("unbound provider records accepted")
	}
	path := os.Getenv("CASE_SOURCE_PROVIDER_TRIAGE_REPORT")
	if path == "" {
		t.Skip("exact external intake observation not supplied; no provider completion claim")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Triage(context.Background(), triageDelivery(t), b)
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Tofu struct {
			Rows []struct {
				ID        string `json:"recipe_id"`
				Allergens string `json:"allergen_tags"`
				IDs       string `json:"ingredient_ids"`
				Names     string `json:"ingredient_names"`
				Grams     string `json:"raw_grams"`
			} `json:"provider_records"`
		} `json:"tofu"`
	}
	if err := json.Unmarshal(b, &evidence); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{}
	for n, row := range evidence.Tofu.Rows {
		want[row.ID] = n
	}
	seen, dairy := 0, 0
	for _, row := range r.Recipes {
		if row.SourceKind != "provider_observation" {
			continue
		}
		n, exists := want[row.Source.RecipeID]
		if !exists || row.RawRowSHA256 != nil || row.RevisionID != "" || row.Source.MemberSHA256 == "" || row.EvidenceRecordSHA256 == "" || !hasHold(row.Holds, "whole_soy_reformulation_missing") {
			t.Fatal("provider observation invented a revision or lost provenance", row.Source.RecipeID)
		}
		source := evidence.Tofu.Rows[n]
		ids, names, grams := strings.Split(source.IDs, ";"), strings.Split(source.Names, ";"), strings.Split(source.Grams, ";")
		if row.Current["Allergen_Tags"] == nil || *row.Current["Allergen_Tags"] != source.Allergens || len(row.Ingredients) != len(ids) {
			t.Fatal("provider source allergens or occurrences changed", row.Source.RecipeID)
		}
		for j, ing := range row.Ingredients {
			if ing.Ordinal != j+1 || ing.RawID != ids[j] || ing.RawName != names[j] || ing.RawGrams != grams[j] {
				t.Fatal("provider ingredient occurrence changed", row.Source.RecipeID)
			}
		}
		if slices.Contains(strings.Split(source.Allergens, ";"), "Milk") {
			dairy++
		}
		seen++
		delete(want, row.Source.RecipeID)
	}
	if len(want) != 0 || dairy == 0 || r.ProviderPending == nil || *r.ProviderPending != seen || r.CombinedPending == nil || *r.CombinedPending != r.CasePending+seen {
		t.Fatal("provider scope/combined count differs")
	}
	b[len(b)-1] ^= 1
	if _, err := providerTriage(b); err == nil {
		t.Fatal("changed provider observation accepted")
	}
	t.Logf("TRIAGE_PROVIDER_OK case=%d provider=%d combined=%d provider_milk_controls=%d", r.CasePending, seen, *r.CombinedPending, dairy)
}

func TestTriageCancellationAndMissingInput(t *testing.T) {
	if _, err := Triage(context.Background(), nil, nil); err == nil {
		t.Fatal("missing intake accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Triage(ctx, triageDelivery(t), nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestTriageValueStates(t *testing.T) {
	for _, value := range []string{"", "0", "0.0", "General"} {
		got := sourceValue(value)
		if (value == "") != (got == nil) || (got != nil && *got != value) {
			t.Fatal("source null, zero or literal applicability changed", value)
		}
	}
}

// These in-memory candidate rows are unit fixtures, never supplied corrections or approvals.
func correctionFixture(t *testing.T) (*Intake, *Intake, CorrectionManifest) {
	t.Helper()
	base := triageDelivery(t)
	candidate, archive := *base, *base.Archive
	archive.Workbooks = append([]Workbook(nil), archive.Workbooks...)
	archive.Workbooks[0].Sheets = append([]Sheet(nil), archive.Workbooks[0].Sheets...)
	for n, s := range archive.Workbooks[0].Sheets {
		if s.Name == "Recipe_Master" {
			archive.Workbooks[0].Sheets[n].Rows = append([]Row(nil), s.Rows...)
		}
	}
	candidate.Archive = &archive
	candidate.Locales = append([]LocaleRow(nil), base.Locales...)
	setCorrectionCell(t, &candidate, "Energy_kcal", "0")
	m := CorrectionManifest{Version: "source-correction-v1", BaseArchiveSHA256: base.Archive.SHA256,
		Corrections: []DeclaredCorrection{{Fields: map[string]string{"Energy_kcal": "kcal"}, Evidence: CorrectionEvidence{
			DocumentSHA256: hash([]byte("unit fixture only")), DocumentReference: "unit fixture", ReviewReference: "unverified fixture reference",
			NutritionBasis: "per_recipe", NutritionSource: "fixture inputs, not a nutrition claim", CookedYieldGrams: "100"}}}}
	bindCorrectionFixture(t, base, &candidate, &m)
	return base, &candidate, m
}

func firstCorrectionRow(t *testing.T, i *Intake) (int, int) {
	t.Helper()
	for sn, s := range i.Archive.Workbooks[0].Sheets {
		if s.Name == "Recipe_Master" {
			for rn, r := range s.Rows {
				if r.RecipeID != "" {
					return sn, rn
				}
			}
		}
	}
	t.Fatal("source fixture has no recipe")
	return 0, 0
}

func setCorrectionCell(t *testing.T, i *Intake, field, value string) {
	t.Helper()
	sn, rn := firstCorrectionRow(t, i)
	s := &i.Archive.Workbooks[0].Sheets[sn]
	r := &s.Rows[rn]
	r.Cells = append([]Cell(nil), r.Cells...)
	col := slices.Index(s.Headers, field) + 1
	for n, c := range r.Cells {
		cn, _ := column(c.Ref, r.Number)
		if cn == col {
			r.Cells[n].Value, r.Cells[n].RawValue = value, value
			return
		}
	}
	t.Fatal("fixture cell absent", field)
}

func bindCorrectionFixture(t *testing.T, base, candidate *Intake, m *CorrectionManifest) {
	t.Helper()
	sn, rn := firstCorrectionRow(t, candidate)
	a, b := candidate.Archive.Workbooks[0], base.Archive.Workbooks[0]
	c := &m.Corrections[0]
	c.RecipeID = a.Sheets[sn].Rows[rn].RecipeID
	c.BaseMemberSHA256, c.BaseRowSHA256 = b.SHA256, rawRowHash(b.Sheets[sn].Rows[rn])
	c.CandidateRowSHA256 = rawRowHash(a.Sheets[sn].Rows[rn])
	candidate.Archive.Workbooks[0].SHA256 = hash([]byte("fixture member:" + c.CandidateRowSHA256))
	c.CandidateMemberSHA256 = candidate.Archive.Workbooks[0].SHA256
	candidate.Archive.SHA256 = hash([]byte("fixture archive:" + c.CandidateMemberSHA256))
	candidate.Report.ArchiveSHA256 = candidate.Archive.SHA256
	m.CandidateArchiveSHA256 = candidate.Archive.SHA256
}

func TestCorrectionManifestBindings(t *testing.T) {
	base, candidate, manifest := correctionFixture(t)
	b, _ := json.Marshal(manifest)
	result, err := ValidateCorrections(context.Background(), base, candidate, b)
	if err != nil || !result.StructurallyValid || result.ActivationPermitted || result.ClinicalApprovalVerified || result.SupplierApprovalVerified {
		t.Fatal("valid structure must not grant approval", result, err)
	}
	before, _ := intakeRows(base)
	after, _ := intakeRows(candidate)
	id := manifest.Corrections[0].RecipeID
	if before[id].fields["Energy_kcal"] == "0" || after[id].fields["Energy_kcal"] != "0" {
		t.Fatal("fixture mutated base or erased explicit zero")
	}
	for _, tc := range []struct {
		name string
		edit func(*Intake, *CorrectionManifest)
	}{
		{"unknown recipe", func(_ *Intake, m *CorrectionManifest) { m.Corrections[0].RecipeID = "missing" }},
		{"old archive", func(_ *Intake, m *CorrectionManifest) { m.BaseArchiveSHA256 = deliveryHash }},
		{"wrong candidate", func(_ *Intake, m *CorrectionManifest) { m.CandidateArchiveSHA256 = deliveryHash }},
		{"stale row", func(_ *Intake, m *CorrectionManifest) { m.Corrections[0].BaseRowSHA256 = strings.Repeat("0", 64) }},
		{"stale member", func(_ *Intake, m *CorrectionManifest) {
			m.Corrections[0].CandidateMemberSHA256 = strings.Repeat("0", 64)
		}},
		{"wrong units", func(_ *Intake, m *CorrectionManifest) { m.Corrections[0].Fields["Energy_kcal"] = "g" }},
		{"missing review reference", func(_ *Intake, m *CorrectionManifest) { m.Corrections[0].Evidence.ReviewReference = "" }},
		{"missing yield", func(_ *Intake, m *CorrectionManifest) { m.Corrections[0].Evidence.CookedYieldGrams = "" }},
		{"unknown basis", func(_ *Intake, m *CorrectionManifest) { m.Corrections[0].Evidence.NutritionBasis = "guessed" }},
		{"missing nutrient", func(i *Intake, m *CorrectionManifest) {
			setCorrectionCell(t, i, "Protein_g", "")
			m.Corrections[0].Fields["Protein_g"] = "g"
			bindCorrectionFixture(t, base, i, m)
		}},
		{"negative nutrient", func(i *Intake, m *CorrectionManifest) {
			setCorrectionCell(t, i, "Energy_kcal", "-1")
			bindCorrectionFixture(t, base, i, m)
		}},
		{"undeclared field", func(i *Intake, m *CorrectionManifest) {
			setCorrectionCell(t, i, "Clinical_Tag", "fixture")
			bindCorrectionFixture(t, base, i, m)
		}},
		{"unsupported replacement", func(i *Intake, m *CorrectionManifest) {
			setCorrectionCell(t, i, "Ingredient_Names", "fixture replacement")
			m.Corrections[0].Fields["Ingredient_Names"] = "text"
			bindCorrectionFixture(t, base, i, m)
		}},
		{"duplicate recipe", func(_ *Intake, m *CorrectionManifest) { m.Corrections = append(m.Corrections, m.Corrections[0]) }},
		{"missing translations", func(i *Intake, _ *CorrectionManifest) { i.Locales = nil }},
		{"wrong ordinal", func(i *Intake, _ *CorrectionManifest) { i.Locales[0].Ordinal = 999 }},
		{"wrong English hash", func(i *Intake, _ *CorrectionManifest) { i.Locales[0].EnglishSHA256 = strings.Repeat("0", 64) }},
		{"translation-only change", func(i *Intake, _ *CorrectionManifest) { i.Locales[0].Bengali += " fixture" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, c, m := correctionFixture(t)
			tc.edit(c, &m)
			data, _ := json.Marshal(m)
			if _, err := ValidateCorrections(context.Background(), b, c, data); err == nil {
				t.Fatal("invalid correction accepted")
			}
		})
	}
	t.Log("CORRECTION_BINDINGS_OK structure only; invalid source, units, replacements and locale associations rejected")
}

func TestCorrectionManifestTranslation(t *testing.T) {
	base, candidate, m := correctionFixture(t)
	sn, rn := firstCorrectionRow(t, candidate)
	f := Fields(candidate.Archive.Workbooks[0].Sheets[sn], candidate.Archive.Workbooks[0].Sheets[sn].Rows[rn])
	text := f["Safety_Rule"] + " [unit fixture]"
	setCorrectionCell(t, candidate, "Safety_Rule", text)
	m.Corrections[0].Fields["Safety_Rule"] = "text"
	m.Corrections[0].Evidence.ClinicalSource = "unit fixture, not clinical clearance"
	bindCorrectionFixture(t, base, candidate, &m)
	for n, l := range candidate.Locales {
		if l.RecipeID == m.Corrections[0].RecipeID && l.Field == "safety" {
			candidate.Locales[n].English, candidate.Locales[n].EnglishSHA256 = text, hash([]byte(text))
			data, _ := json.Marshal(m)
			if _, err := ValidateCorrections(context.Background(), base, candidate, data); err == nil {
				t.Fatal("untranslated English correction accepted")
			}
			candidate.Locales[n].Bengali += " পরীক্ষা"
			m.Corrections[0].Translations = []CorrectionTranslation{{Field: "safety", EnglishSHA256: hash([]byte(text)), BengaliSHA256: hash([]byte(candidate.Locales[n].Bengali))}}
			data, _ = json.Marshal(m)
			if _, err := ValidateCorrections(context.Background(), base, candidate, data); err != nil {
				t.Fatal("new source-bound translation rejected", err)
			}
			candidate.Locales[n].Note += " [unit fixture]"
			if _, err := ValidateCorrections(context.Background(), base, candidate, data); err == nil || !strings.Contains(err.Error(), "unsupported translation note change") {
				t.Fatal("changed English bypassed undeclared note rejection", err)
			}
			return
		}
	}
	t.Fatal("real source has no safety translation")
}

func TestCorrectionManifestGlossaryPositions(t *testing.T) {
	base, candidate, manifest := correctionFixture(t)
	candidate.Glossary = append([]GlossaryRow(nil), base.Glossary...)
	slices.Reverse(candidate.Glossary)
	for n := range candidate.Glossary {
		candidate.Glossary[n].Row += 100
	}
	data, _ := json.Marshal(manifest)
	if _, err := ValidateCorrections(context.Background(), base, candidate, data); err != nil {
		t.Fatal("unchanged glossary semantics rejected after row reordering", err)
	}
	candidate.Glossary = candidate.Glossary[1:]
	if _, err := ValidateCorrections(context.Background(), base, candidate, data); err == nil || !strings.Contains(err.Error(), "unsupported glossary change") {
		t.Fatal("glossary omission accepted", err)
	}
}

func TestCorrectionManifestMalformed(t *testing.T) {
	for _, data := range []string{"", "{", "null", `{"version":"x","version":"y"}`, `{"unknown":true}`, `{} {}`, strings.Repeat(" ", MaxCorrectionBytes+1)} {
		var m CorrectionManifest
		err := strictJSON([]byte(data), &m)
		if data == "null" {
			_, err = ValidateCorrections(context.Background(), nil, nil, []byte(data))
		}
		if err == nil {
			t.Fatal("malformed manifest accepted", fmt.Sprintf("%.50s", data))
		}
	}
	if !reflect.DeepEqual(nutrientUnits, map[string]string{"Energy_kcal": "kcal", "Protein_g": "g", "Carb_g": "g", "Fat_g": "g", "Fibre_g": "g", "Iron_mg": "mg", "Calcium_mg": "mg"}) {
		t.Fatal("published units changed")
	}
}
