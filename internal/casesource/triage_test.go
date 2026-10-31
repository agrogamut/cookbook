package casesource

import (
	"bytes"
	"path/filepath"
	"strconv"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xuri/excelize/v2"
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
	r, err := Triage(context.Background(), i, "")
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
	reordered, err := Triage(context.Background(), &copyIntake, "")
	first, _ := json.Marshal(r)
	second, _ := json.Marshal(reordered)
	if err != nil || string(first) != string(second) {
		t.Fatal("triage depends on workbook iteration order", err)
	}
	t.Logf("TRIAGE_SOURCE_OK recipes=%d pending_case=%d all_nutrition_null=%d general=%d zero_cells=%d dairy_recipes=%d mixed_soy=%d", recipes, pending, allMissing, general, zeroes, dairy, mixed)
}

func providerTestSheets(t *testing.T) []providerSheet {
	t.Helper()
	var out []providerSheet
	for _, spec := range providerWorkbookSpecs() {
		s, err := readProviderWorkbook(context.Background(), "../../data/provider", spec)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestProviderSourceTuples(t *testing.T) {
	rows, evidence, err := providerTriage(context.Background(), "../../data/provider")
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string][][]string{}
	for _, spec := range providerWorkbookSpecs() {
		book, err := excelize.OpenFile(filepath.Join("../../data/provider", spec.Workbook))
		if err != nil {
			t.Fatal(err)
		}
		raw[spec.Workbook], err = book.GetRows(spec.Sheet, excelize.Options{RawCellValue: true})
		closeErr := book.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
	}
	specs := providerWorkbookSpecs()
	master := map[string][]string{}
	for _, row := range raw[specs[2].Workbook][4:] {
		master[row[0]] = row
	}
	want, wantDairy := map[string]int{}, map[string]bool{}
	var sourceIn, sourceOut, tofu, mixed int
	for n, row := range raw[specs[0].Workbook][4:] {
		if row[2] != "India" && row[2] != "Bangladesh" {
			sourceOut++
			continue
		}
		sourceIn++
		ids := strings.Split(row[11], ";")
		isTofu, isMixed := slices.Contains(ids, "ING0075"), slices.Contains(ids, "ING0074")
		if !isTofu && !isMixed {
			continue
		}
		want[row[0]] = n + 5
		if isTofu {
			tofu++
			for _, id := range ids {
				if slices.Contains(strings.Split(master[id][19], ";"), "Milk") {
					wantDairy[row[0]] = true
				}
			}
		}
		if isMixed {
			mixed++
		}
	}
	tuples := map[string][]string{}
	for _, row := range raw[specs[1].Workbook][4:] {
		tuples[row[1]+"/"+row[3]] = row
	}
	assertEvidence := func(e ProviderRowEvidence) {
		t.Helper()
		source := raw[e.Source.Workbook]
		if e.Source.Row < 5 || e.Source.Row > len(source) || !validDigest(e.Source.MemberSHA256) {
			t.Fatal("invalid source locator", e.Source)
		}
		header, row := source[3], source[e.Source.Row-1]
		padded := make([]string, len(header))
		copy(padded, row)
		b, _ := json.Marshal(padded)
		if e.RawRowSHA256 != hash(b) {
			t.Fatal("raw row hash changed", e.Source)
		}
		for field, cell := range e.Cells {
			col, n, err := excelize.CellNameToCoordinates(cell.Ref)
			if err != nil || n != e.Source.Row || col < 1 || col > len(header) || header[col-1] != field || padded[col-1] != cell.Value {
				t.Fatal("cell evidence differs from original workbook", field, cell)
			}
		}
	}
	gotTofu, gotMixed, gotDairy := 0, 0, map[string]bool{}
	for _, row := range rows {
		id := row.Source.RecipeID
		n, exists := want[id]
		if !exists || row.Source.Row != n || row.RevisionID != "" || row.RawRowSHA256 == nil || row.ProviderEvidence == nil {
			t.Fatal("provider scope or identity differs", id)
		}
		original := raw[specs[0].Workbook][n-1]
		ids, names, grams := strings.Split(original[11], ";"), strings.Split(original[12], ";"), strings.Split(original[13], ";")
		if len(row.Ingredients) != len(ids) || len(row.ProviderEvidence.Ingredients) != len(ids) ||
			*row.Current["Allergen_Tags"] != original[27] || *row.Current["Review_Status"] != original[36] ||
			*row.RawRowSHA256 != row.ProviderEvidence.Recipe.RawRowSHA256 {
			t.Fatal("source flags or ingredient lengths differ", id)
		}
		assertEvidence(row.ProviderEvidence.Recipe)
		for j, item := range row.Ingredients {
			if item.Ordinal != j+1 || item.RawID != ids[j] || item.RawName != names[j] || item.RawGrams != grams[j] {
				t.Fatal("raw occurrence differs", id, j)
			}
			e := row.ProviderEvidence.Ingredients[j]
			mapping := tuples[id+"/"+strconv.Itoa(j+1)]
			if e.Ordinal != j+1 || e.Mapping.Cells["Mapping_ID"].Value != mapping[0] || e.Mapping.Cells["Ingredient_ID"].Value != ids[j] ||
				e.Mapping.Cells["Quantity"].Value != mapping[8] || e.Mapping.Cells["Unit"].Value != "g" ||
				e.IngredientMaster.Cells["Ingredient_ID"].Value != ids[j] || e.IngredientMaster.Cells["English_Name"].Value != master[ids[j]][1] {
				t.Fatal("ingredient mapping/master tuple changed", id, j)
			}
			assertEvidence(e.Mapping)
			assertEvidence(e.IngredientMaster)
		}
		if hasHold(row.Holds, "whole_soy_reformulation_missing") != slices.Contains(ids, "ING0075") ||
			hasHold(row.DataGaps, "soy_form_ambiguous") != slices.Contains(ids, "ING0074") {
			t.Fatal("ingredient identities do not match holds/gaps", id)
		}
		if slices.Contains(ids, "ING0075") {
			gotTofu++
			if slices.Contains(strings.Split(original[27], ";"), "Milk") {
				gotDairy[id] = true
			}
		}
		if slices.Contains(ids, "ING0074") {
			gotMixed++
			if !slices.Contains(ids, "ING0075") && len(row.Holds) != 0 {
				t.Fatal("mixed soy became a blanket hold", id)
			}
		}
		delete(want, id)
	}
	if len(want) != 0 || tofu == 0 || mixed == 0 || len(wantDairy) == 0 || gotTofu != tofu || gotMixed != mixed || !reflect.DeepEqual(gotDairy, wantDairy) ||
		evidence.InScopeRecipes != sourceIn || evidence.OutOfScopeRecipes != sourceOut || evidence.RecipeRows != sourceIn+sourceOut {
		t.Fatal("provider coverage does not match source identities", evidence)
	}
	if !gotDairy["MG-R-00904"] {
		t.Fatal("original row 908 tofu/ghee control missing")
	}
	first, _ := json.Marshal(struct {
		Rows   []TriageRecipe
		Source ProviderSourceEvidence
	}{rows, evidence})
	for repeat := 0; repeat < 2; repeat++ {
		dir := t.TempDir()
		for _, spec := range specs {
			data, err := os.ReadFile(filepath.Join("../../data/provider", spec.Workbook))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, spec.Workbook), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		copied, source, err := providerTriage(context.Background(), dir)
		second, _ := json.Marshal(struct {
			Rows   []TriageRecipe
			Source ProviderSourceEvidence
		}{copied, source})
		if err != nil || !bytes.Equal(first, second) || bytes.Contains(second, []byte(dir)) {
			t.Fatal("provider output depends on filesystem location", err)
		}
	}
	t.Logf("PORTABLE_PROVIDER_TUPLES_OK observed=%d tofu=%d mixed=%d dairy_controls=%d scope=%d excluded=%d", len(rows), gotTofu, gotMixed, len(gotDairy), sourceIn, sourceOut)
}

func TestProviderInputRejections(t *testing.T) {
	spec := providerWorkbookSpecs()[0]
	data, err := os.ReadFile(filepath.Join("../../data/provider", spec.Workbook))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseProviderWorkbook(context.Background(), data, spec); err != nil {
		t.Fatal("positive source rejected", err)
	}
	for _, tc := range []struct {
		name   string
		change func([]byte, *providerWorkbookSpec) []byte
		want   string
	}{
		{"wrong_hash", func(b []byte, _ *providerWorkbookSpec) []byte { b[len(b)-1] ^= 1; return b }, "SHA-256"},
		{"empty", func(_ []byte, _ *providerWorkbookSpec) []byte { return nil }, "size"},
		{"oversized", func(_ []byte, _ *providerWorkbookSpec) []byte { return make([]byte, maxProviderWorkbookBytes+1) }, "size"},
		{"malformed_workbook", func(_ []byte, s *providerWorkbookSpec) []byte {
			b := []byte("not a workbook")
			s.SHA256 = hash(b)
			return b
		}, "open provider workbook"},
		{"missing_sheet", func(b []byte, s *providerWorkbookSpec) []byte { s.Sheet = "missing"; return b }, "provider sheet"},
		{"wrong_header", func(b []byte, s *providerWorkbookSpec) []byte {
			book, err := excelize.OpenReader(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			defer book.Close()
			if err := book.SetCellValue(s.Sheet, "A4", "Wrong_ID"); err != nil {
				t.Fatal(err)
			}
			buf, err := book.WriteToBuffer()
			if err != nil {
				t.Fatal(err)
			}
			s.SHA256 = hash(buf.Bytes())
			return buf.Bytes()
		}, "header row"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixtureSpec := spec
			fixture := tc.change(append([]byte(nil), data...), &fixtureSpec)
			if _, err := parseProviderWorkbook(context.Background(), fixture, fixtureSpec); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal("invalid provider input not rejected at expected boundary", err)
			}
		})
	}
	dir := t.TempDir()
	if _, _, err := providerTriage(context.Background(), dir); err == nil {
		t.Fatal("missing provider files accepted")
	}
	if err := os.Mkdir(filepath.Join(dir, spec.Workbook), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := readProviderWorkbook(context.Background(), dir, spec); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatal("nonregular provider input accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := providerTriage(ctx, "../../data/provider"); !errors.Is(err, context.Canceled) {
		t.Fatal("provider cancellation ignored", err)
	}
	t.Log("PORTABLE_PROVIDER_INPUT_REJECTIONS_OK")
}

func TestProviderJoinRejections(t *testing.T) {
	sheets := providerTestSheets(t)
	if _, _, err := joinProviderTriage(context.Background(), sheets[0], sheets[1], sheets[2]); err != nil {
		t.Fatal("positive join rejected", err)
	}
	for _, tc := range []struct {
		name  string
		sheet int
		field string
		value string
	}{
		{"unknown_recipe", 1, "Recipe_ID", "MG-R-99999"},
		{"ordinal_mismatch", 1, "Sequence_No", "2"},
		{"unknown_ingredient", 1, "Ingredient_ID", "ING9999"},
		{"wrong_quantity", 1, "Quantity", "999"},
		{"wrong_unit", 1, "Unit", "mg"},
		{"mapping_scope", 1, "Region_Culture", "Bangladesh"},
		{"mapping_name", 1, "Ingredient_Name_EN", "different source name"},
		{"blank_recipe_id", 0, "Recipe_ID", ""},
		{"master_id_missing", 2, "Ingredient_ID", "ING9999"},
		{"duplicate_recipe_id", 0, "Recipe_ID", "MG-R-00002"},
		{"invalid_arrays", 0, "Ingredient_IDs", "ING0019"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copySheets := append([]providerSheet(nil), sheets...)
			s := &copySheets[tc.sheet]
			s.rows = append([]providerRow(nil), s.rows...)
			row := &s.rows[0]
			row.values = append([]string(nil), row.values...)
			row.fields = make(map[string]string)
			for j, name := range s.headers {
				if name == tc.field {
					row.values[j] = tc.value
				}
				row.fields[name] = row.values[j]
			}
			if _, _, err := joinProviderTriage(context.Background(), copySheets[0], copySheets[1], copySheets[2]); err == nil {
				t.Fatal("invalid source join accepted")
			}
		})
	}
	missing := sheets[1]
	missing.rows = missing.rows[1:]
	if _, _, err := joinProviderTriage(context.Background(), sheets[0], missing, sheets[2]); err == nil || !strings.Contains(err.Error(), "missing mapping ordinal") {
		t.Fatal("missing mapping accepted", err)
	}
	duplicate := sheets[1]
	duplicate.rows = append(append([]providerRow(nil), duplicate.rows...), duplicate.rows[0])
	if _, _, err := joinProviderTriage(context.Background(), sheets[0], duplicate, sheets[2]); err == nil {
		t.Fatal("duplicate occurrence accepted")
	}
	t.Log("PORTABLE_PROVIDER_JOIN_REJECTIONS_OK")
}

func TestTriageCancellationAndMissingInput(t *testing.T) {
	if _, err := Triage(context.Background(), nil, ""); err == nil {
		t.Fatal("missing intake accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Triage(ctx, triageDelivery(t), ""); !errors.Is(err, context.Canceled) {
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
