package casesource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const TriageVersion = "source-triage-v2"

const MaxCorrectionBytes = 4 << 20

var nutrientUnits = map[string]string{
	"Energy_kcal": "kcal", "Protein_g": "g", "Carb_g": "g", "Fat_g": "g",
	"Fibre_g": "g", "Iron_mg": "mg", "Calcium_mg": "mg",
}

type SourceRequirement struct {
	Field string `json:"field"`
	Unit  string `json:"unit,omitempty"`
	Basis string `json:"basis"`
}

type TriageRecipe struct {
	SourceKind       string                  `json:"source_kind"`
	Source           Citation                `json:"source"`
	RevisionID       string                  `json:"revision_id,omitempty"`
	RawRowSHA256     *string                 `json:"raw_row_sha256"`
	ProviderEvidence *ProviderRecipeEvidence `json:"provider_evidence,omitempty"`
	RuleSHA256       string                  `json:"rule_sha256,omitempty"`
	AllergenCodes    []string                `json:"derived_allergen_codes,omitempty"`
	Current          map[string]*string      `json:"current"`
	Ingredients      []Occurrence            `json:"ingredients"`
	Findings         []Finding               `json:"findings"`
	Holds            []PolicyHold            `json:"holds"`
	DataGaps         []PolicyHold            `json:"data_gaps"`
	Required         []SourceRequirement     `json:"required_source_fields"`
}

type TriageReport struct {
	Version                  string                  `json:"version"`
	ArchiveSHA256            string                  `json:"archive_sha256"`
	ParserVersion            string                  `json:"parser_version"`
	AcceptedFinalSource      bool                    `json:"accepted_final_source"`
	ActivationPermitted      bool                    `json:"activation_permitted"`
	SupplierApprovalVerified bool                    `json:"supplier_approval_verified"`
	CorrectionInput          string                  `json:"correction_input"`
	ProviderCoverage         string                  `json:"provider_coverage"`
	ProviderSource           *ProviderSourceEvidence `json:"provider_source,omitempty"`
	CasePending              int                     `json:"case_pending_reformulations"`
	ProviderPending          *int                    `json:"provider_reported_pending_reformulations"`
	CombinedPending          *int                    `json:"combined_reported_pending_reformulations"`
	Recipes                  []TriageRecipe          `json:"recipes"`
}

func rawRowHash(r Row) string {
	b, _ := json.Marshal(r.Cells)
	return hash(b)
}

func sourceValue(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func reformulationRequirements() []SourceRequirement {
	return []SourceRequirement{
		{"ingredient_identity_and_form", "", "Supplier formulation identifying whole bean and its dry, soaked or cooked state; no tofu equivalence."},
		{"ingredient_quantities", "g", "Exact per-occurrence quantities, including added water."},
		{"preparation_and_texture", "", "Supplied method, processing times, age/stage texture and safety instructions."},
		{"cooked_yield_and_serving", "g", "Measured yield and serving basis; no inferred yield or equal-mass substitution."},
		{"nutrition_and_cost_basis", "", "Ingredient source rows, calculation or measurement reference, units and INR cost basis."},
		{"source_review_evidence", "", "Identifiable supplier document/version and review reference; a typed name is not verified approval."},
	}
}

func hasHold(holds []PolicyHold, code string) bool {
	for _, h := range holds {
		if h.Code == code {
			return true
		}
	}
	return false
}

// Triage reports supplied facts. It does not evaluate a child's clinical eligibility.
func Triage(ctx context.Context, i *Intake, providerDir string) (TriageReport, error) {
	if i == nil || i.Archive == nil || i.Archive.SHA256 == "" {
		return TriageReport{}, fmt.Errorf("triage requires parsed source intake")
	}
	out := TriageReport{Version: TriageVersion, ArchiveSHA256: i.Archive.SHA256,
		ParserVersion: i.Report.ParserVersion, AcceptedFinalSource: AcceptedArchive(i.Archive.SHA256),
		CorrectionInput: "not_supplied", ProviderCoverage: "not_supplied", Recipes: []TriageRecipe{}}
	for _, w := range i.Archive.Workbooks {
		s := sheetByName(w, "Recipe_Master")
		for _, row := range s.Rows {
			if err := ctx.Err(); err != nil {
				return TriageReport{}, err
			}
			if row.RecipeID == "" {
				continue
			}
			f := Fields(s, row)
			rowHash := rawRowHash(row)
			r := TriageRecipe{SourceKind: "case", Source: Citation{ArchiveSHA256: i.Archive.SHA256,
				MemberSHA256: w.SHA256, Workbook: w.Name, Case: w.Case, Sheet: s.Name, Row: row.Number, RecipeID: row.RecipeID},
				RevisionID: CandidateID(i.Archive.SHA256, row.RecipeID), RawRowSHA256: &rowHash,
				RuleSHA256: RuleFingerprint(f), Current: map[string]*string{}, Ingredients: []Occurrence{},
				Findings: []Finding{}, Holds: []PolicyHold{}, DataGaps: []PolicyHold{}, Required: []SourceRequirement{}}
			for _, name := range []string{"Clinical_Tag", "Review_Status", "Nutrient_Gap", "Serving_Size_g", "Cost_per_Serving_INR", "Allergen_Tags"} {
				r.Current[name] = sourceValue(f[name])
			}
			keys := make([]string, 0, len(nutrientUnits))
			for name := range nutrientUnits {
				keys = append(keys, name)
			}
			sort.Strings(keys)
			for _, name := range keys {
				r.Current[name] = sourceValue(f[name])
				if f[name] == "" {
					r.Findings = append(r.Findings, Finding{Code: "nutrition_missing", Case: w.Case, RecipeID: row.RecipeID,
						Sheet: s.Name, Row: row.Number, Evidence: map[string]string{"field": name, "unit": nutrientUnits[name], "state": "null"}})
					r.Required = append(r.Required, SourceRequirement{name, nutrientUnits[name], "Explicit recipe/serving/100g basis and traceable source inputs; never restore superseded values."})
				}
			}
			if f["Clinical_Tag"] == "General" {
				r.Findings = append(r.Findings, Finding{Code: "clinical_general_source_value", Case: w.Case, RecipeID: row.RecipeID,
					Sheet: s.Name, Row: row.Number, Evidence: map[string]string{"value": "General", "meaning": "Literal source applicability, not a code defect or condition-specific clearance."}})
				r.Required = append(r.Required, SourceRequirement{"condition_specific_applicability", "", "Only if a clinical correction is supplied: source-backed indication, exclusions, prerequisites and review reference."})
			}
			for _, ing := range row.Ingredients {
				r.Ingredients = append(r.Ingredients, Occurrence{Ordinal: ing.Ordinal, MeaningID: MeaningID(ing.RawID, ing.RawName),
					RawID: ing.RawID, RawName: ing.RawName, RawGrams: ing.RawGrams, Grams: ing.Grams})
			}
			if out.AcceptedFinalSource {
				policy, err := finalPolicy(f, row.Ingredients)
				if err != nil {
					return TriageReport{}, err
				}
				r.Holds = append(r.Holds, policy.Holds...)
				r.DataGaps = append(r.DataGaps, policy.DataGaps...)
				r.AllergenCodes = append([]string(nil), policy.AllergenCodes...)
			} else {
				r.Findings = append(r.Findings, Finding{Code: "source_not_final_contract", RecipeID: row.RecipeID,
					Evidence: map[string]string{"reason": "No final-source policy or activation acceptance inferred."}})
			}
			if hasHold(r.Holds, "whole_soy_reformulation_missing") {
				out.CasePending++
				r.Required = append(r.Required, reformulationRequirements()...)
			}
			out.Recipes = append(out.Recipes, r)
		}
	}
	if providerDir != "" {
		rows, evidence, err := providerTriage(ctx, providerDir)
		if err != nil {
			return TriageReport{}, err
		}
		n := 0
		for _, row := range rows {
			if hasHold(row.Holds, "whole_soy_reformulation_missing") {
				n++
			}
		}
		total := n + out.CasePending
		out.ProviderCoverage, out.ProviderSource = "version_bound_workbook_observation", &evidence
		out.ProviderPending = &n
		if out.AcceptedFinalSource {
			out.CombinedPending = &total
		}
		out.Recipes = append(out.Recipes, rows...)
	}
	sort.Slice(out.Recipes, func(a, b int) bool {
		x, y := out.Recipes[a], out.Recipes[b]
		if x.SourceKind != y.SourceKind {
			return x.SourceKind < y.SourceKind
		}
		return x.Source.RecipeID < y.Source.RecipeID
	})
	return out, nil
}

const maxProviderWorkbookBytes = 2 << 20

type ProviderWorkbookEvidence struct {
	Workbook     string `json:"workbook"`
	SHA256       string `json:"sha256"`
	Sheet        string `json:"sheet"`
	HeaderRow    int    `json:"header_row"`
	HeaderSHA256 string `json:"header_sha256"`
}

type ProviderSourceEvidence struct {
	Version           string                     `json:"version"`
	Workbooks         []ProviderWorkbookEvidence `json:"workbooks"`
	ScopeReference    string                     `json:"scope_reference"`
	Regions           []string                   `json:"regions"`
	RecipeRows        int                        `json:"recipe_rows"`
	InScopeRecipes    int                        `json:"in_scope_recipes"`
	OutOfScopeRecipes int                        `json:"out_of_scope_recipes"`
	MappingRows       int                        `json:"mapping_rows"`
	IngredientRows    int                        `json:"ingredient_rows"`
}

type ProviderCell struct {
	Ref   string `json:"ref"`
	Value string `json:"value"`
}

type ProviderRowEvidence struct {
	Source       Citation                `json:"source"`
	RawRowSHA256 string                  `json:"raw_row_sha256"`
	Cells        map[string]ProviderCell `json:"cells"`
}

type ProviderIngredientEvidence struct {
	Ordinal          int                 `json:"ordinal"`
	Mapping          ProviderRowEvidence `json:"mapping"`
	IngredientMaster ProviderRowEvidence `json:"ingredient_master"`
}

type ProviderRecipeEvidence struct {
	Recipe      ProviderRowEvidence          `json:"recipe"`
	Ingredients []ProviderIngredientEvidence `json:"ingredients"`
}

type providerWorkbookSpec struct {
	ProviderWorkbookEvidence
	width int
}

func providerWorkbookSpecs() []providerWorkbookSpec {
	return []providerWorkbookSpec{
		{ProviderWorkbookEvidence{"MadamGY_Recipe_Master_1000_Detailed_V2.xlsx", "7501efae36375161612218887f1db34e9c6205bab3173f88e021a3674be7ed87", "Recipe_Master", 4, "58399625495468201d71d722bfdda1a5b35e9d05d80672240a7eeb729fbc63f9"}, 37},
		{ProviderWorkbookEvidence{"MadamGY_Recipe_Ingredient_Mapping_Master_V1.xlsx", "1887cf4fcb68fd7feb117cbf2c48b9cd8f8e0ad872cd3e2b45f30b8fdb6a2d88", "Recipe-Ingredient Mapping", 4, "82386f7d47bb9ff5185522164623c0e35eeb90f6aff9b13429c9452faf1e6062"}, 40},
		{ProviderWorkbookEvidence{"MadamGY_Ingredient_Super_Master_Complete_V1_Cost_Filled.xlsx", "8e09de5d28c69f6f2e90cc4aa5be2543d0285eac06580271c6bdf5abd47ce613", "Ingredient_Master", 4, "27a50740b93c0615f8a32d2605f3c792567eeb106ff26297950b7c6c064f9c94"}, 29},
	}
}

// This is the committed source scope, not a query of mutable region_focus state.
func providerRegions() []string {
	return []string{"Bangladesh", "Central / Tribal India", "Himalayan India", "North India", "Northeast India", "South India", "West Bengal / East India", "West India"}
}

type providerRow struct {
	number int
	values []string
	fields map[string]string
}

type providerSheet struct {
	spec    providerWorkbookSpec
	headers []string
	rows    []providerRow
}

func readProviderWorkbook(ctx context.Context, dir string, spec providerWorkbookSpec) (providerSheet, error) {
	if err := ctx.Err(); err != nil {
		return providerSheet{}, err
	}
	path := filepath.Join(dir, spec.Workbook)
	info, err := os.Lstat(path)
	if err != nil {
		return providerSheet{}, fmt.Errorf("provider %s unavailable: %w", spec.Workbook, err)
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxProviderWorkbookBytes {
		return providerSheet{}, fmt.Errorf("provider %s must be a regular file of 1..%d bytes", spec.Workbook, maxProviderWorkbookBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return providerSheet{}, fmt.Errorf("open provider %s: %w", spec.Workbook, err)
	}
	b, readErr := io.ReadAll(io.LimitReader(f, maxProviderWorkbookBytes+1))
	closeErr := f.Close()
	if readErr != nil {
		return providerSheet{}, fmt.Errorf("read provider %s: %w", spec.Workbook, readErr)
	}
	if closeErr != nil {
		return providerSheet{}, fmt.Errorf("close provider %s: %w", spec.Workbook, closeErr)
	}
	return parseProviderWorkbook(ctx, b, spec)
}

func parseProviderWorkbook(ctx context.Context, b []byte, spec providerWorkbookSpec) (providerSheet, error) {
	if len(b) == 0 || len(b) > maxProviderWorkbookBytes || hash(b) != spec.SHA256 {
		return providerSheet{}, fmt.Errorf("provider %s size or SHA-256 differs from source contract", spec.Workbook)
	}
	book, err := excelize.OpenReader(bytes.NewReader(b), excelize.Options{UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 32 << 20})
	if err != nil {
		return providerSheet{}, fmt.Errorf("open provider workbook %s: %w", spec.Workbook, err)
	}
	defer book.Close()
	rows, err := book.Rows(spec.Sheet)
	if err != nil {
		return providerSheet{}, fmt.Errorf("provider sheet %s: %w", spec.Sheet, err)
	}
	defer rows.Close()
	out := providerSheet{spec: spec}
	for n := 1; rows.Next(); n++ {
		if err := ctx.Err(); err != nil {
			return providerSheet{}, err
		}
		if n > 5000 {
			return providerSheet{}, fmt.Errorf("provider %s exceeds 5000 physical rows", spec.Workbook)
		}
		values, err := rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return providerSheet{}, fmt.Errorf("provider %s row %d: %w", spec.Workbook, n, err)
		}
		if n < spec.HeaderRow {
			continue
		}
		if n == spec.HeaderRow {
			header, err := json.Marshal(values)
			if err != nil || len(values) != spec.width || hash(header) != spec.HeaderSHA256 {
				return providerSheet{}, fmt.Errorf("provider %s header row %d differs from source contract", spec.Workbook, n)
			}
			out.headers = append([]string(nil), values...)
			continue
		}
		if len(values) > spec.width {
			return providerSheet{}, fmt.Errorf("provider %s row %d exceeds header width", spec.Workbook, n)
		}
		if strings.TrimSpace(strings.Join(values, "")) == "" {
			continue
		}
		padded := make([]string, spec.width)
		copy(padded, values)
		r := providerRow{number: n, values: padded, fields: map[string]string{}}
		for j, field := range out.headers {
			r.fields[field] = padded[j]
		}
		out.rows = append(out.rows, r)
	}
	if err := rows.Error(); err != nil {
		return providerSheet{}, fmt.Errorf("read provider %s: %w", spec.Workbook, err)
	}
	if len(out.headers) != spec.width || len(out.rows) == 0 {
		return providerSheet{}, fmt.Errorf("provider %s lacks header or data rows", spec.Workbook)
	}
	return out, nil
}

func providerEvidence(sheet providerSheet, row providerRow, recipeID string, fields ...string) ProviderRowEvidence {
	b, _ := json.Marshal(row.values)
	out := ProviderRowEvidence{Source: Citation{MemberSHA256: sheet.spec.SHA256, Workbook: sheet.spec.Workbook,
		Sheet: sheet.spec.Sheet, Row: row.number, RecipeID: recipeID}, RawRowSHA256: hash(b), Cells: map[string]ProviderCell{}}
	for j, name := range sheet.headers {
		for _, field := range fields {
			if name == field {
				ref, _ := excelize.CoordinatesToCellName(j+1, row.number)
				out.Cells[field] = ProviderCell{ref, row.values[j]}
			}
		}
	}
	return out
}

func providerTriage(ctx context.Context, dir string) ([]TriageRecipe, ProviderSourceEvidence, error) {
	sheets := make([]providerSheet, 0, 3)
	for _, spec := range providerWorkbookSpecs() {
		s, err := readProviderWorkbook(ctx, dir, spec)
		if err != nil {
			return nil, ProviderSourceEvidence{}, err
		}
		sheets = append(sheets, s)
	}
	return joinProviderTriage(ctx, sheets[0], sheets[1], sheets[2])
}

func joinProviderTriage(ctx context.Context, recipes, mappings, ingredientsSheet providerSheet) ([]TriageRecipe, ProviderSourceEvidence, error) {
	evidence := ProviderSourceEvidence{Version: "provider-workbook-triage-v1",
		ScopeReference: "internal/db/migrations/0002_annotations_and_seeds.up.sql:region_focus + 0004_scope_india_bangladesh.up.sql",
		Regions:        providerRegions(), RecipeRows: len(recipes.rows), MappingRows: len(mappings.rows), IngredientRows: len(ingredientsSheet.rows)}
	for _, sheet := range []providerSheet{recipes, mappings, ingredientsSheet} {
		evidence.Workbooks = append(evidence.Workbooks, sheet.spec.ProviderWorkbookEvidence)
	}
	ingredientByID, recipeByID := map[string]providerRow{}, map[string]providerRow{}
	itemsByRecipe := map[string][]Ingredient{}
	for _, row := range ingredientsSheet.rows {
		id := row.fields["Ingredient_ID"]
		if id == "" || ingredientByID[id].number != 0 || row.fields["English_Name"] == "" {
			return nil, evidence, fmt.Errorf("provider ingredient identity missing or duplicated at row %d", row.number)
		}
		ingredientByID[id] = row
	}
	if ingredientByID["ING0075"].fields["English_Name"] != "Tofu" || ingredientByID["ING0074"].fields["English_Name"] != "Soybean/soy chunks" {
		return nil, evidence, fmt.Errorf("provider soy identities differ from source contract")
	}
	for _, row := range recipes.rows {
		id := row.fields["Recipe_ID"]
		if id == "" || recipeByID[id].number != 0 || row.fields["Recipe_Name"] == "" {
			return nil, evidence, fmt.Errorf("provider recipe identity missing or duplicated at row %d", row.number)
		}
		items, err := ingredients(row.values)
		if err != nil {
			return nil, evidence, fmt.Errorf("provider recipe %s: %w", id, err)
		}
		recipeByID[id], itemsByRecipe[id] = row, items
	}
	type key struct {
		id      string
		ordinal int
	}
	mappingByKey := map[key]providerRow{}
	mappingIDs := map[string]bool{}
	for _, row := range mappings.rows {
		if err := ctx.Err(); err != nil {
			return nil, evidence, err
		}
		f := row.fields
		id := f["Recipe_ID"]
		seq, err := strconv.Atoi(f["Sequence_No"])
		k := key{id, seq}
		items := itemsByRecipe[id]
		if err != nil || seq < 1 || seq > len(items) || mappingByKey[k].number != 0 ||
			f["Mapping_ID"] != fmt.Sprintf("MAP-%s-%02d", id, seq) || mappingIDs[f["Mapping_ID"]] {
			return nil, evidence, fmt.Errorf("provider mapping row %d has unknown recipe, invalid identity or duplicate ordinal", row.number)
		}
		item, recipe := items[seq-1], recipeByID[id]
		master, exists := ingredientByID[strings.TrimSpace(item.RawID)]
		mappingQuantity, ok := new(big.Rat).SetString(f["Quantity"])
		recipeQuantity, _ := new(big.Rat).SetString(item.Grams)
		if !exists || f["Ingredient_ID"] != strings.TrimSpace(item.RawID) || f["Ingredient_Name_EN"] != strings.TrimSpace(item.RawName) ||
			f["Unit"] != "g" || !decimal.MatchString(f["Quantity"]) || !ok || mappingQuantity.Cmp(recipeQuantity) != 0 ||
			f["Recipe_Name"] != recipe.fields["Recipe_Name"] || f["Country"] != recipe.fields["Country"] ||
			f["Region_Culture"] != recipe.fields["Region_Culture"] || f["Ingredient_Allergen_Tag"] != master.fields["Allergen_Tags"] {
			return nil, evidence, fmt.Errorf("provider mapping row %d ingredient/quantity/scope join differs for %s ordinal %d", row.number, id, seq)
		}
		mappingByKey[k], mappingIDs[f["Mapping_ID"]] = row, true
	}
	var out []TriageRecipe
	for _, row := range recipes.rows {
		if err := ctx.Err(); err != nil {
			return nil, evidence, err
		}
		f, items := row.fields, itemsByRecipe[row.fields["Recipe_ID"]]
		id, region := f["Recipe_ID"], f["Region_Culture"]
		inScope := false
		for _, allowed := range evidence.Regions {
			inScope = inScope || region == allowed
		}
		country := "India"
		if region == "Bangladesh" || region == "Nepal" {
			country = region
		}
		if (!inScope && region != "Nepal") || f["Country"] != country {
			return nil, evidence, fmt.Errorf("provider recipe %s has unknown or inconsistent source scope", id)
		}
		hasTofu, hasMixed := false, false
		for _, item := range items {
			if mappingByKey[key{id, item.Ordinal}].number == 0 {
				return nil, evidence, fmt.Errorf("provider recipe %s is missing mapping ordinal %d", id, item.Ordinal)
			}
			hasTofu = hasTofu || strings.TrimSpace(item.RawID) == "ING0075"
			hasMixed = hasMixed || strings.TrimSpace(item.RawID) == "ING0074"
		}
		if !inScope {
			evidence.OutOfScopeRecipes++
			continue
		}
		evidence.InScopeRecipes++
		if !hasTofu && !hasMixed {
			continue
		}
		source := providerEvidence(recipes, row, id, "Recipe_ID", "Recipe_Name", "Country", "Region_Culture", "Ingredient_IDs", "Ingredient_Names", "Ingredient_Quantities_g", "Allergen_Tags", "Review_Status")
		r := TriageRecipe{SourceKind: "provider_observation", Source: source.Source, RawRowSHA256: &source.RawRowSHA256,
			Current: map[string]*string{}, Ingredients: []Occurrence{}, Holds: []PolicyHold{}, DataGaps: []PolicyHold{},
			Findings: []Finding{{Code: "provider_snapshot_only", RecipeID: id, Sheet: recipes.spec.Sheet, Row: row.number,
				Evidence: map[string]string{"reason": "Version-bound workbook rows under committed source scope; not current database state, a runtime revision or clinical approval."}}},
			Required: []SourceRequirement{}, ProviderEvidence: &ProviderRecipeEvidence{Recipe: source, Ingredients: []ProviderIngredientEvidence{}}}
		for _, field := range []string{"Recipe_Name", "Country", "Region_Culture", "Clinical_Tag", "Review_Status", "Serving_Size_g", "Cost_per_Serving_INR", "Allergen_Tags", "Energy_kcal", "Protein_g", "Carb_g", "Fat_g", "Fibre_g", "Iron_mg", "Calcium_mg"} {
			r.Current[field] = sourceValue(f[field])
		}
		for _, item := range items {
			mapping := mappingByKey[key{id, item.Ordinal}]
			master := ingredientByID[strings.TrimSpace(item.RawID)]
			r.Ingredients = append(r.Ingredients, Occurrence{Ordinal: item.Ordinal, RawID: item.RawID, RawName: item.RawName, RawGrams: item.RawGrams, Grams: item.Grams})
			r.ProviderEvidence.Ingredients = append(r.ProviderEvidence.Ingredients, ProviderIngredientEvidence{item.Ordinal,
				providerEvidence(mappings, mapping, id, "Mapping_ID", "Recipe_ID", "Sequence_No", "Ingredient_ID", "Ingredient_Name_EN", "Quantity", "Unit", "Region_Culture", "Country", "Ingredient_Allergen_Tag", "Ingredient_Review_Status", "Recipe_Review_Status", "Mapping_Status", "Notes"),
				providerEvidence(ingredientsSheet, master, "", "Ingredient_ID", "English_Name", "Allergen_Tags", "Source_ID", "Review_Status", "Data_Quality", "Validation_Required", "Preparation_Safety_Note")})
			if item.RawName != master.fields["English_Name"] {
				r.Findings = append(r.Findings, Finding{Code: "provider_ingredient_name_differs", RecipeID: id, Sheet: recipes.spec.Sheet, Row: row.number,
					Evidence: map[string]string{"ordinal": strconv.Itoa(item.Ordinal), "ingredient_id": item.RawID, "recipe_name": item.RawName, "master_name": master.fields["English_Name"], "meaning": "Explicit ID join; both source names retained without inferred name equivalence."}})
			}
		}
		if hasTofu {
			r.Holds = append(r.Holds, PolicyHold{"whole_soy_reformulation_missing", "Ingredient_IDs", "Source tofu formulation has no supplied whole-bean replacement.", "ING0075 joined by Recipe_ID and Sequence_No to ingredient master Tofu."})
			r.Required = append(r.Required, reformulationRequirements()...)
		}
		if hasMixed {
			r.DataGaps = append(r.DataGaps, PolicyHold{"soy_form_ambiguous", "Ingredient_IDs", "Ingredient master combines whole soybean and soy chunks; source form needs clarification.", "ING0074: Soybean/soy chunks. No blanket exclusion or inferred whole-bean formulation."})
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source.RecipeID < out[j].Source.RecipeID })
	return out, evidence, nil
}

type CorrectionManifest struct {
	Version                string               `json:"version"`
	BaseArchiveSHA256      string               `json:"base_archive_sha256"`
	CandidateArchiveSHA256 string               `json:"candidate_archive_sha256"`
	Corrections            []DeclaredCorrection `json:"corrections"`
}

type DeclaredCorrection struct {
	RecipeID              string                  `json:"recipe_id"`
	BaseMemberSHA256      string                  `json:"base_member_sha256"`
	BaseRowSHA256         string                  `json:"base_row_sha256"`
	CandidateMemberSHA256 string                  `json:"candidate_member_sha256"`
	CandidateRowSHA256    string                  `json:"candidate_row_sha256"`
	Fields                map[string]string       `json:"fields"`
	Evidence              CorrectionEvidence      `json:"evidence"`
	Translations          []CorrectionTranslation `json:"translations"`
}

type CorrectionEvidence struct {
	DocumentSHA256    string `json:"document_sha256"`
	DocumentReference string `json:"document_reference"`
	ReviewReference   string `json:"review_reference"`
	NutritionBasis    string `json:"nutrition_basis"`
	NutritionSource   string `json:"nutrition_source"`
	CookedYieldGrams  string `json:"cooked_yield_g"`
	CostSource        string `json:"cost_source"`
	ClinicalSource    string `json:"clinical_source"`
}

type CorrectionTranslation struct {
	Field         string `json:"field"`
	Ordinal       int    `json:"ordinal"`
	EnglishSHA256 string `json:"english_sha256"`
	BengaliSHA256 string `json:"bengali_sha256"`
}

type CorrectionValidation struct {
	Version                  string `json:"version"`
	BaseArchiveSHA256        string `json:"base_archive_sha256"`
	CandidateArchiveSHA256   string `json:"candidate_archive_sha256"`
	ManifestSHA256           string `json:"manifest_sha256"`
	Recipes                  int    `json:"recipes"`
	StructurallyValid        bool   `json:"structurally_valid"`
	ActivationPermitted      bool   `json:"activation_permitted"`
	SupplierApprovalVerified bool   `json:"supplier_approval_verified"`
	ClinicalApprovalVerified bool   `json:"clinical_approval_verified"`
	ReviewStatus             string `json:"review_status"`
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func strictJSON(data []byte, dst any) error {
	if len(data) == 0 || len(data) > MaxCorrectionBytes {
		return fmt.Errorf("correction manifest must contain 1..%d bytes", MaxCorrectionBytes)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return fmt.Errorf("correction manifest nesting exceeds limit")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate or invalid manifest key %v", key)
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = d.Token()
		case json.Delim('['):
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = d.Token()
		}
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("correction manifest has trailing data")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

type boundRow struct {
	member string
	row    Row
	fields map[string]string
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func intakeRows(i *Intake) (map[string]boundRow, error) {
	if i == nil || i.Archive == nil {
		return nil, fmt.Errorf("parsed intake is required")
	}
	rows := map[string]boundRow{}
	for _, w := range i.Archive.Workbooks {
		s := sheetByName(w, "Recipe_Master")
		for _, r := range s.Rows {
			if r.RecipeID == "" {
				continue
			}
			if _, exists := rows[r.RecipeID]; exists {
				return nil, fmt.Errorf("duplicate recipe %s", r.RecipeID)
			}
			rows[r.RecipeID] = boundRow{w.SHA256, r, Fields(s, r)}
		}
	}
	return rows, nil
}

// ValidateCorrections binds declarations to parsed bytes, never to a clinical approval.
// Formulations are deliberately unsupported until a supplier formulation contract exists.
func ValidateCorrections(ctx context.Context, base, candidate *Intake, data []byte) (CorrectionValidation, error) {
	var manifest CorrectionManifest
	if err := strictJSON(data, &manifest); err != nil {
		return CorrectionValidation{}, fmt.Errorf("correction manifest: %w", err)
	}
	before, err := intakeRows(base)
	if err != nil {
		return CorrectionValidation{}, err
	}
	after, err := intakeRows(candidate)
	if err != nil {
		return CorrectionValidation{}, err
	}
	if manifest.Version != "source-correction-v1" || !AcceptedArchive(base.Archive.SHA256) ||
		manifest.BaseArchiveSHA256 != base.Archive.SHA256 || manifest.CandidateArchiveSHA256 != candidate.Archive.SHA256 ||
		base.Archive.SHA256 == candidate.Archive.SHA256 || candidate.Report.ParserVersion != LocaleParserVersion ||
		len(before) != len(after) || len(manifest.Corrections) == 0 {
		return CorrectionValidation{}, fmt.Errorf("correction version, source binding or recipe set differs")
	}
	if !sameGlossaryContent(base.Glossary, candidate.Glossary) {
		return CorrectionValidation{}, fmt.Errorf("unsupported glossary change")
	}
	declared := map[string]DeclaredCorrection{}
	for _, c := range manifest.Corrections {
		b, bok := before[c.RecipeID]
		a, aok := after[c.RecipeID]
		if _, duplicate := declared[c.RecipeID]; duplicate || !bok || !aok || len(c.Fields) == 0 ||
			c.BaseMemberSHA256 != b.member || c.CandidateMemberSHA256 != a.member ||
			c.BaseRowSHA256 != rawRowHash(b.row) || c.CandidateRowSHA256 != rawRowHash(a.row) {
			return CorrectionValidation{}, fmt.Errorf("unknown, duplicate or stale correction %q", c.RecipeID)
		}
		declared[c.RecipeID] = c
	}
	baseLocales, candidateLocales := map[string][]LocaleRow{}, map[string][]LocaleRow{}
	for _, l := range base.Locales {
		baseLocales[l.RecipeID] = append(baseLocales[l.RecipeID], l)
	}
	for _, l := range candidate.Locales {
		if _, ok := after[l.RecipeID]; !ok {
			return CorrectionValidation{}, fmt.Errorf("translation references unknown recipe %s", l.RecipeID)
		}
		candidateLocales[l.RecipeID] = append(candidateLocales[l.RecipeID], l)
	}
	for _, id := range sortedKeys(before) {
		if err := ctx.Err(); err != nil {
			return CorrectionValidation{}, err
		}
		b := before[id]
		a, ok := after[id]
		if !ok {
			return CorrectionValidation{}, fmt.Errorf("candidate omits recipe %s", id)
		}
		c, declaredRow := declared[id]
		changed := map[string]string{}
		for _, field := range sortedKeys(a.fields) {
			value := a.fields[field]
			if b.fields[field] != value {
				unit, supported := correctionUnit(field)
				if !supported {
					return CorrectionValidation{}, fmt.Errorf("unsupported correction %s %s; ingredient/method reformulations require a separate source contract", id, field)
				}
				changed[field] = unit
			}
		}
		if len(changed) == 0 && !declaredRow {
			if err := validateCorrectionTranslations(baseLocales[id], candidateLocales[id], DeclaredCorrection{RecipeID: id}); err != nil {
				return CorrectionValidation{}, fmt.Errorf("%s: %w", id, err)
			}
			continue
		}
		if !declaredRow || len(changed) == 0 || !reflect.DeepEqual(changed, c.Fields) {
			return CorrectionValidation{}, fmt.Errorf("declared fields or units do not match all changed source fields for %s", id)
		}
		if err := validateCorrectionEvidence(c, a.fields); err != nil {
			return CorrectionValidation{}, fmt.Errorf("%s: %w", id, err)
		}
		if err := validateCorrectionTranslations(baseLocales[id], candidateLocales[id], c); err != nil {
			return CorrectionValidation{}, fmt.Errorf("%s: %w", id, err)
		}
	}
	return CorrectionValidation{Version: manifest.Version, BaseArchiveSHA256: manifest.BaseArchiveSHA256,
		CandidateArchiveSHA256: manifest.CandidateArchiveSHA256, ManifestSHA256: hash(data), Recipes: len(declared),
		StructurallyValid: true, ReviewStatus: "external_source_and_clinical_review_not_verified"}, nil
}

func sameGlossaryContent(before, after []GlossaryRow) bool {
	counts := map[GlossaryRow]int{}
	for _, row := range before {
		row.Row = 0
		counts[row]++
	}
	for _, row := range after {
		row.Row = 0
		counts[row]--
		if counts[row] == 0 {
			delete(counts, row)
		}
	}
	return len(counts) == 0
}

func correctionUnit(field string) (string, bool) {
	if unit, ok := nutrientUnits[field]; ok {
		return unit, true
	}
	switch field {
	case "Cost_per_Serving_INR":
		return "INR", true
	case "Clinical_Tag", "Safety_Rule", "Nutrient_Gap":
		return "text", true
	default:
		return "", false
	}
}

func validateCorrectionEvidence(c DeclaredCorrection, fields map[string]string) error {
	e := c.Evidence
	if !validDigest(e.DocumentSHA256) || strings.TrimSpace(e.DocumentReference) == "" || strings.TrimSpace(e.ReviewReference) == "" {
		return fmt.Errorf("source document hash/reference and review reference are required; approval remains unverified")
	}
	nutrition := false
	for _, field := range sortedKeys(c.Fields) {
		if _, ok := nutrientUnits[field]; ok || field == "Nutrient_Gap" {
			nutrition = true
		}
		if field == "Clinical_Tag" || field == "Safety_Rule" {
			if strings.TrimSpace(fields[field]) == "" || strings.TrimSpace(e.ClinicalSource) == "" {
				return fmt.Errorf("clinical correction needs nonempty source text and indication/exclusion/prerequisite source reference")
			}
		}
		if field == "Cost_per_Serving_INR" && (!validSourceNumber(fields[field], false) || strings.TrimSpace(e.CostSource) == "") {
			return fmt.Errorf("cost correction requires nonnegative INR and a cost source")
		}
	}
	if nutrition {
		if e.NutritionBasis != "per_recipe" && e.NutritionBasis != "per_serving" && e.NutritionBasis != "per_100g" {
			return fmt.Errorf("nutrition basis must be per_recipe, per_serving or per_100g")
		}
		if strings.TrimSpace(e.NutritionSource) == "" || !validSourceNumber(e.CookedYieldGrams, true) || !validSourceNumber(fields["Serving_Size_g"], true) {
			return fmt.Errorf("nutrition correction requires source inputs, positive cooked yield g and serving g")
		}
		for _, field := range sortedKeys(nutrientUnits) {
			if !validSourceNumber(fields[field], false) {
				return fmt.Errorf("nutrition correction is incomplete or invalid: %s", field)
			}
		}
	}
	return nil
}

func validSourceNumber(s string, positive bool) bool {
	return len(s) <= 100 && decimal.MatchString(s) && (!positive || strings.Trim(s, "0.") != "")
}

func validateCorrectionTranslations(before, after []LocaleRow, c DeclaredCorrection) error {
	key := func(field string, ordinal int) string { return field + ":" + strconv.Itoa(ordinal) }
	old := map[string]LocaleRow{}
	for _, l := range before {
		if l.RecipeID == c.RecipeID {
			old[key(l.Field, l.Ordinal)] = l
		}
	}
	declared := map[string]CorrectionTranslation{}
	for _, l := range c.Translations {
		k := key(l.Field, l.Ordinal)
		if _, ok := declared[k]; ok {
			return fmt.Errorf("duplicate translation declaration")
		}
		declared[k] = l
	}
	seen := map[string]bool{}
	for _, l := range after {
		if l.RecipeID != c.RecipeID {
			continue
		}
		k := key(l.Field, l.Ordinal)
		previous, ok := old[k]
		if !ok || seen[k] || l.EnglishSHA256 != hash([]byte(l.English)) {
			return fmt.Errorf("translation ordinal or English hash mismatch")
		}
		if l.Note != previous.Note {
			return fmt.Errorf("unsupported translation note change")
		}
		seen[k] = true
		if l.English == previous.English {
			if l.Bengali != previous.Bengali || l.RawQuantity != previous.RawQuantity {
				return fmt.Errorf("unsupported translation-only or quantity change")
			}
			continue
		}
		d, ok := declared[k]
		if !ok || d.EnglishSHA256 != l.EnglishSHA256 || d.BengaliSHA256 != hash([]byte(l.Bengali)) ||
			strings.TrimSpace(l.Bengali) == "" || l.Bengali == previous.Bengali {
			return fmt.Errorf("changed English requires a newly bound supplied Bengali translation")
		}
		delete(declared, k)
	}
	if len(seen) != len(old) {
		return fmt.Errorf("candidate has missing translations")
	}
	if len(declared) != 0 {
		return fmt.Errorf("translation declaration has no changed source occurrence")
	}
	return nil
}
