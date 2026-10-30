package casesource

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const FinalArchiveSHA256 = "4efae52ed55f1b2fa6bf91df5ef00ac0c7801680c54f75d093366665d7689ef3"

//go:embed final_source.json
var finalContractBytes []byte

type IngredientReference struct {
	SourceID   string            `json:"source_id"`
	SourceName string            `json:"source_name"`
	File       string            `json:"reference_file"`
	SHA256     string            `json:"reference_sha256"`
	Sheet      string            `json:"reference_sheet"`
	Row        int               `json:"reference_row"`
	Fields     map[string]string `json:"reference_fields"`
}

var finalContract = func() struct {
	Version     string                         `json:"version"`
	Archive     string                         `json:"archive_sha256"`
	Rules       map[string]string              `json:"rules"`
	Ingredients map[string]IngredientReference `json:"ingredient_references"`
	Amendments  map[string][]TextAmendment     `json:"presentation_amendments"`
} {
	var c struct {
		Version     string                         `json:"version"`
		Archive     string                         `json:"archive_sha256"`
		Rules       map[string]string              `json:"rules"`
		Ingredients map[string]IngredientReference `json:"ingredient_references"`
		Amendments  map[string][]TextAmendment     `json:"presentation_amendments"`
	}
	if err := json.Unmarshal(finalContractBytes, &c); err != nil || c.Archive != FinalArchiveSHA256 {
		panic("invalid final source contract")
	}
	return c
}()

func AcceptedArchive(sha string) bool { return sha == finalContract.Archive }

type AllergenEvidence struct {
	Code      string               `json:"code"`
	Ordinal   int                  `json:"ordinal,omitempty"`
	Reference *IngredientReference `json:"reference,omitempty"`
	Basis     string               `json:"basis"`
}

type PolicyHold struct {
	Code     string `json:"code"`
	Field    string `json:"field"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence"`
}

type TextAmendment struct {
	Field          string `json:"field"`
	Language       string `json:"language"`
	Workbook       string `json:"workbook"`
	MemberSHA256   string `json:"member_sha256"`
	Sheet          string `json:"sheet"`
	Cell           string `json:"cell"`
	RawSHA256      string `json:"raw_sha256"`
	DisplaySHA256  string `json:"display_sha256"`
	ByteOffset     int    `json:"byte_offset"`
	Before         string `json:"before"`
	After          string `json:"after"`
	NounSourceCell string `json:"noun_source_cell"`
	NounSHA256     string `json:"noun_sha256"`
	Basis          string `json:"basis"`
}

func (r Recipe) PresentationText(field, language, raw string) string {
	if !AcceptedArchive(r.Citation.ArchiveSHA256) || r.CandidateID != CandidateID(r.Citation.ArchiveSHA256, r.Citation.RecipeID) || finalContract.Rules[r.Citation.RecipeID] != RuleFingerprint(r.Fields) {
		return raw
	}
	for _, a := range finalContract.Amendments[r.Citation.RecipeID] {
		if a.Field != field || a.Language != language || a.Workbook != r.Citation.Workbook || a.MemberSHA256 != r.Citation.MemberSHA256 || a.RawSHA256 != hash([]byte(raw)) {
			continue
		}
		end := a.ByteOffset + len(a.Before)
		if a.ByteOffset < 0 || end > len(raw) || raw[a.ByteOffset:end] != a.Before {
			return raw
		}
		text := raw[:a.ByteOffset] + a.After + raw[end:]
		if hash([]byte(text)) != a.DisplaySHA256 {
			return raw
		}
		return text
	}
	return raw
}

type Policy struct {
	Version          string             `json:"version"`
	ContractSHA256   string             `json:"contract_sha256"`
	RuleSHA256       string             `json:"rule_sha256"`
	AllergenCodes    []string           `json:"allergen_codes"`
	AllergenEvidence []AllergenEvidence `json:"allergen_evidence"`
	Holds            []PolicyHold       `json:"holds"`
	DataGaps         []PolicyHold       `json:"data_gaps"`
	TextAmendments   []TextAmendment    `json:"presentation_amendments,omitempty"`
}

func finalPolicy(fields map[string]string, occurrences []Ingredient) (Policy, error) {
	p := Policy{Version: finalContract.Version, ContractSHA256: hash(finalContractBytes), RuleSHA256: RuleFingerprint(fields), AllergenCodes: []string{}, AllergenEvidence: []AllergenEvidence{}, Holds: []PolicyHold{}}
	if finalContract.Rules[fields["Recipe_ID"]] != p.RuleSHA256 {
		return p, fmt.Errorf("final rule mapping differs for %s", fields["Recipe_ID"])
	}
	p.TextAmendments = append([]TextAmendment(nil), finalContract.Amendments[fields["Recipe_ID"]]...)
	tags, unknown := comparisonAllergens(fields["Allergen_Tags"])
	for code := range tags {
		p.AllergenEvidence = append(p.AllergenEvidence, AllergenEvidence{Code: code, Basis: "Recipe_Master.Allergen_Tags literal vocabulary normalization"})
	}
	for _, token := range unknown {
		p.Holds = append(p.Holds, PolicyHold{"allergen_unknown", "Allergen_Tags", "Unmapped final source allergen label.", token})
	}
	for _, occurrence := range occurrences {
		id, name := strings.TrimSpace(occurrence.RawID), strings.TrimSpace(occurrence.RawName)
		if id == "ING0075" && name == "Tofu" {
			p.Holds = append(p.Holds, PolicyHold{"whole_soy_reformulation_missing", "Ingredient_Names", "Tofu is retired from selection. Whole-soyabean quantity, preparation, texture, yield and nutrient/cost basis for this recipe have not been supplied.", fmt.Sprintf("ordinal %d: %s %s (%s g)", occurrence.Ordinal, id, name, occurrence.Grams)})
		}
		if id == "ING0074" && name == "Soybean/soy chunks" {
			p.DataGaps = append(p.DataGaps, PolicyHold{"soy_form_ambiguous", "Ingredient_Names", "The final source combines whole soyabean and processed chunks. No form-specific nutrient, yield or cost equivalence is inferred.", fmt.Sprintf("ordinal %d: %s %s (%s g)", occurrence.Ordinal, id, name, occurrence.Grams)})
		}
		ref, ok := finalContract.Ingredients[id+"|"+name]
		if !ok {
			continue
		}
		mapped, unrecognized := comparisonAllergens(ref.Fields["Allergen_Tags"])
		if len(unrecognized) != 0 {
			return p, fmt.Errorf("unsupported reference allergen vocabulary for %s", id)
		}
		for code := range mapped {
			tags[code] = true
			copy := ref
			p.AllergenEvidence = append(p.AllergenEvidence, AllergenEvidence{Code: code, Ordinal: occurrence.Ordinal, Reference: &copy, Basis: "Exact present ingredient ID and English name match; reference Allergen_Tags"})
		}
		if id == "ING0062" && name == "Mustard oil" {
			tags["ALG-MUSTARD"] = true
			copy := ref
			p.AllergenEvidence = append(p.AllergenEvidence, AllergenEvidence{Code: "ALG-MUSTARD", Ordinal: occurrence.Ordinal, Reference: &copy, Basis: "allergen_mapping.ALG-MUSTARD common_derivatives_or_hidden_sources names Mustard oil; existing ingredient_allergen_override ING0062"})
		}
	}
	for code := range tags {
		p.AllergenCodes = append(p.AllergenCodes, code)
	}
	sort.Strings(p.AllergenCodes)
	sort.Slice(p.AllergenEvidence, func(i, j int) bool {
		a, b := p.AllergenEvidence[i], p.AllergenEvidence[j]
		if a.Ordinal != b.Ordinal {
			return a.Ordinal < b.Ordinal
		}
		return a.Code < b.Code
	})
	if strings.EqualFold(fields["Diet_Type"], "Vegetarian") && (tags["ALG-FISH"] || tags["ALG-SHELLFISH"] || tags["ALG-EGG"]) {
		p.Holds = append(p.Holds, PolicyHold{"diet_contradiction", "Diet_Type", "Final vegetarian declaration conflicts with a present ingredient or declared allergen.", fields["Diet_Type"] + ": " + strings.Join(p.AllergenCodes, ";")})
	}
	return p, nil
}
