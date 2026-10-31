package casesource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const TriageVersion = "source-triage-v1"

// This supplement is an existing intake observation, not a replacement recipe source.
const ProviderTriageSHA256 = "7f7690d207692f5adfefe32656e2fab2ca9f4a609991e508ad1f4b94c110cfeb"

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
	SourceKind           string              `json:"source_kind"`
	Source               Citation            `json:"source"`
	RevisionID           string              `json:"revision_id,omitempty"`
	RawRowSHA256         *string             `json:"raw_row_sha256"`
	EvidenceRecordSHA256 string              `json:"evidence_record_sha256,omitempty"`
	RuleSHA256           string              `json:"rule_sha256,omitempty"`
	AllergenCodes        []string            `json:"derived_allergen_codes,omitempty"`
	Current              map[string]*string  `json:"current"`
	Ingredients          []Occurrence        `json:"ingredients"`
	Findings             []Finding           `json:"findings"`
	Holds                []PolicyHold        `json:"holds"`
	DataGaps             []PolicyHold        `json:"data_gaps"`
	Required             []SourceRequirement `json:"required_source_fields"`
}

type TriageReport struct {
	Version                  string         `json:"version"`
	ArchiveSHA256            string         `json:"archive_sha256"`
	ParserVersion            string         `json:"parser_version"`
	AcceptedFinalSource      bool           `json:"accepted_final_source"`
	ActivationPermitted      bool           `json:"activation_permitted"`
	SupplierApprovalVerified bool           `json:"supplier_approval_verified"`
	CorrectionInput          string         `json:"correction_input"`
	ProviderCoverage         string         `json:"provider_coverage"`
	ProviderReportSHA256     string         `json:"provider_report_sha256,omitempty"`
	CasePending              int            `json:"case_pending_reformulations"`
	ProviderPending          *int           `json:"provider_reported_pending_reformulations"`
	CombinedPending          *int           `json:"combined_reported_pending_reformulations"`
	Recipes                  []TriageRecipe `json:"recipes"`
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
func Triage(ctx context.Context, i *Intake, providerReport []byte) (TriageReport, error) {
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
	if len(providerReport) != 0 {
		rows, err := providerTriage(providerReport)
		if err != nil {
			return TriageReport{}, err
		}
		n, total := len(rows), len(rows)+out.CasePending
		out.ProviderCoverage, out.ProviderReportSHA256 = "hash_bound_intake_observation", ProviderTriageSHA256
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

func providerTriage(data []byte) ([]TriageRecipe, error) {
	if hash(data) != ProviderTriageSHA256 {
		return nil, fmt.Errorf("provider supplement is not the hash-bound intake observation")
	}
	var report struct {
		ProviderInputs map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"provider_inputs"`
		Tofu struct {
			Records []json.RawMessage `json:"provider_records"`
		} `json:"tofu"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("read provider supplement: %w", err)
	}
	var out []TriageRecipe
	seen := map[string]bool{}
	for _, raw := range report.Tofu.Records {
		var v struct {
			RecipeID  string `json:"recipe_id"`
			Workbook  string `json:"workbook"`
			Sheet     string `json:"sheet"`
			Row       int    `json:"row"`
			IDs       string `json:"ingredient_ids"`
			Names     string `json:"ingredient_names"`
			Grams     string `json:"raw_grams"`
			Allergens string `json:"allergen_tags"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		values := make([]string, 14)
		values[11], values[12], values[13] = v.IDs, v.Names, v.Grams
		items, err := ingredients(values)
		if err != nil || v.RecipeID == "" || seen[v.RecipeID] || v.Sheet != "Recipe_Master" || v.Row < 1 || !validDigest(report.ProviderInputs[v.Workbook].SHA256) {
			return nil, fmt.Errorf("invalid provider observation %q", v.RecipeID)
		}
		seen[v.RecipeID] = true
		r := TriageRecipe{SourceKind: "provider_observation", Source: Citation{MemberSHA256: report.ProviderInputs[v.Workbook].SHA256,
			Workbook: v.Workbook, Sheet: v.Sheet, Row: v.Row, RecipeID: v.RecipeID}, EvidenceRecordSHA256: hash(raw),
			Current: map[string]*string{"Allergen_Tags": sourceValue(v.Allergens)}, Ingredients: []Occurrence{},
			Findings: []Finding{{Code: "provider_snapshot_only", RecipeID: v.RecipeID, Evidence: map[string]string{
				"reason": "Exact observed workbook and row; full raw-row hash and runtime revision are unavailable in this supplement. Not a current database query."}}},
			Holds: []PolicyHold{}, DataGaps: []PolicyHold{}, Required: reformulationRequirements()}
		for _, item := range items {
			r.Ingredients = append(r.Ingredients, Occurrence{Ordinal: item.Ordinal, RawID: item.RawID, RawName: item.RawName, RawGrams: item.RawGrams, Grams: item.Grams})
			if strings.TrimSpace(item.RawID) == "ING0075" && strings.TrimSpace(item.RawName) == "Tofu" {
				r.Holds = append(r.Holds, PolicyHold{"whole_soy_reformulation_missing", "Ingredient_Names", "Observed tofu formulation has no supplied whole-bean replacement.", fmt.Sprintf("ordinal %d: %s (%s g)", item.Ordinal, item.RawName, item.Grams)})
			}
		}
		if len(r.Holds) == 0 {
			return nil, fmt.Errorf("provider observation %s has no exact tofu occurrence", v.RecipeID)
		}
		out = append(out, r)
	}
	return out, nil
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
