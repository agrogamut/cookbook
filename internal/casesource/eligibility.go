package casesource

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

//go:embed eligibility_fingerprints.json
var eligibilityFingerprints []byte

var knownRules = func() map[string]string {
	var rules map[string]string
	if err := json.Unmarshal(eligibilityFingerprints, &rules); err != nil {
		panic(err)
	}
	return rules
}()

type Input struct {
	AgeMonths        int
	Diet             string
	Vegan            bool
	AllergenCodes    []string
	UnmappedClinical []string
}

type Gap struct {
	Code     string   `json:"code"`
	Scope    string   `json:"scope"`
	Field    string   `json:"field"`
	Reason   string   `json:"reason"`
	Evidence string   `json:"evidence"`
	Source   Citation `json:"source"`
}

func RuleFingerprint(fields map[string]string) string {
	keys := []string{"Ingredient_IDs", "Ingredient_Names", "Ingredient_Quantities_g", "Preparation_Method_FULL", "Clinical_Tag", "Safety_Rule", "Book1_Use", "Book2_Use", "AI_Adaptation_Rule"}
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, fields[key])
	}
	b, _ := json.Marshal(values)
	return hash(b)
}

type prerequisite struct{ field, reason string }

var casePrerequisites = map[int]prerequisite{
	14: {"Safety_Rule", "Fluid tolerance, alertness and safe swallowing are not recorded."},
	17: {"Safety_Rule", "Verified gluten-free ingredient labels and cross-contact controls are not recorded."},
	18: {"Safety_Rule", "The individualized carbohydrate distribution and diabetes plan are not recorded."},
	19: {"Safety_Rule", "Renal-team review, current laboratory results and prescribed nutrient/fluid limits are not recorded."},
	20: {"AI_Adaptation_Rule", "The diagnosis-specific hepatology nutrition plan is not recorded."},
	21: {"Book1_Use", "Exact metabolic diagnosis, current sick-day status, prescribed allowance and specialist approval are not recorded."},
	22: {"Book1_Use", "The diagnosis, symptom phase and prescribed fibre/fat/lactose plan are not recorded."},
	23: {"Book1_Use", "The treating team's supervised meal-support plan is not recorded."},
	24: {"Book1_Use", "Swallowing assessment, prescribed IDDSI food/liquid levels and positioning/pacing/supervision plan are not recorded."},
	25: {"Book1_Use", "Corrected age, developmental readiness, medical stability, feeding route, swallowing/fluid/growth and fortification instructions are not recorded."},
	26: {"Book1_Use", "Previously tolerated ingredient history and the child's normal feeding plan are not recorded."},
	28: {"Book1_Use", "The whole-day dietary pattern and growth-trend assessment are not recorded."},
	29: {"Safety_Rule", "Developmental ability for the specified texture and finger-food size is not recorded."},
	34: {"Safety_Rule", "Current tolerance of oral food and fluids is not recorded."},
	35: {"Safety_Rule", "Current oral-feeding safety is not recorded."},
	40: {"Safety_Rule", "Packaged-ingredient gluten verification is not recorded."},
	41: {"Safety_Rule", "Current ingredient-label and kitchen cross-contact checks are not recorded."},
	42: {"Safety_Rule", "The individualized carbohydrate distribution, hypoglycaemia and sick-day plans are not recorded."},
	43: {"Safety_Rule", "The individualized diabetes, hypoglycaemia and sick-day plans are not recorded."},
	44: {"Safety_Rule", "The prescribed daily protein target and allowed renal protein sources are not recorded; the source requires selection to stop."},
	45: {"Safety_Rule", "Current laboratory results and prescribed fluid allowance are not recorded; the source requires selection to stop."},
	46: {"Safety_Rule", "The diagnosis-specific hepatology fat and sodium plan is not recorded."},
	47: {"Safety_Rule", "The hepatology energy/protein prescription for the exact diagnosis and growth trajectory is not recorded."},
	48: {"Book1_Use", "Source BLOCKED / NOT SELECTABLE: exact metabolic diagnosis, current specialist plan and emergency instructions are not recorded."},
	49: {"Book1_Use", "Source BLOCKED / NOT SELECTABLE: written protein/amino-acid prescription and remaining allowance are not recorded."},
	50: {"Safety_Rule", "The diagnosis- and symptom-phase GI plan matching this exact portion is not recorded."},
	51: {"Safety_Rule", "The diagnosis-specific GI plan for the current dated phase is not recorded."},
	52: {"Safety_Rule", "The prescribed meal-support and supervision plan is not recorded."},
	53: {"Safety_Rule", "The treating team's supervision, pacing, completion and contingency instructions are not recorded."},
	54: {"Book1_Use", "Prescribed IDDSI food/drink levels, texture test results and swallowing plan are not recorded."},
	55: {"Book1_Use", "Source STOP: swallowing assessment, oral/NPO status, feeding route and current clinician plan are not recorded."},
	56: {"Safety_Rule", "Corrected age, developmental readiness, prescribed route/texture, nutrition target and monitoring plan are not recorded."},
	57: {"Book1_Use", "Specialist approval, total fluid allowance, recipe fluid allocation and exact fortification instructions are not recorded."},
	58: {"Book1_Use", "Vaccination context, known tolerated ingredients and current intake/hydration status are not recorded."},
	59: {"Book1_Use", "Longitudinal growth, individualized energy/protein goal and dietary variety assessment are not recorded."},
	60: {"Book1_Use", "The feeding assessment, documented self-feeding skills and supervision/routine plan are not recorded."},
}

func Assess(r Recipe, input Input) []Gap {
	var gaps []Gap
	add := func(code, scope, field, reason string) {
		gaps = append(gaps, Gap{Code: code, Scope: scope, Field: field, Reason: reason, Evidence: r.Fields[field], Source: r.Citation})
	}
	min, minErr := strconv.Atoi(r.Fields["Min_Age_Months"])
	max, maxErr := strconv.Atoi(r.Fields["Max_Age_Months"])
	if minErr != nil || maxErr != nil || min > max {
		add("age_unknown", "recipe", "Min_Age_Months", "Source age bounds are unavailable or invalid.")
	} else if input.AgeMonths < min || input.AgeMonths > max {
		add("age_excluded", "recipe", "Min_Age_Months", fmt.Sprintf("Source age range is %d-%d months; recorded age is %d months.", min, max, input.AgeMonths))
	}
	tags := map[string]bool{}
	knownAllergens := map[string]bool{"ALG-MILK": true, "ALG-EGG": true, "ALG-PEANUT": true, "ALG-TREENUT": true, "ALG-WHEAT": true, "ALG-SOY": true, "ALG-SESAME": true, "ALG-FISH": true, "ALG-SHELLFISH": true, "ALG-MUSTARD": true, "ALG-SULPHITE": true}
	for _, tag := range strings.Split(r.Fields["Allergen_Tags"], ";") {
		tag = strings.TrimSpace(tag)
		tags[tag] = true
		if !knownAllergens[tag] && tag != "None of the 11 coded allergens in ingredient list" {
			add("allergen_unknown", "recipe", "Allergen_Tags", "Source allergen classification is missing or unsupported: "+tag+".")
		}
	}
	for _, code := range input.AllergenCodes {
		if tags[code] {
			add("allergen_excluded", "recipe", "Allergen_Tags", "Source contains confirmed allergen "+code+".")
		}
	}
	diet := strings.ToLower(r.Fields["Diet_Type"])
	if diet != "vegetarian" && diet != "eggetarian" && diet != "non-vegetarian" {
		add("diet_unknown", "recipe", "Diet_Type", "Source diet classification is unsupported.")
	} else if (input.Diet == "Vegetarian" && diet != "vegetarian") || (input.Diet == "Eggetarian" && diet == "non-vegetarian") {
		add("diet_excluded", "recipe", "Diet_Type", "Source diet does not fit the recorded dietary practice.")
	}
	if input.Vegan {
		add("vegan_unmapped", "recipe", "Ingredient_Names", "Source ingredients have no verified vegan classification; vegetarian and absent coded allergens alone do not establish vegan suitability.")
	}
	if len(input.UnmappedClinical) > 0 {
		add("clinical_unmapped", "recipe", "Clinical_Tag", "No source-backed clinical crosswalk for recorded conditions: "+strings.Join(input.UnmappedClinical, ", ")+".")
	}
	if knownRules[r.Citation.RecipeID] != RuleFingerprint(r.Fields) {
		add("rule_unmapped", "recipe", "Safety_Rule", "Source safety/selection content differs from the mapped source version; eligibility requires a matching rule mapping.")
		return gaps
	}
	if r.Citation.Case >= 1 && r.Citation.Case <= 11 {
		add("source_prerequisite", "case", "AI_Adaptation_Rule", "Required ingredient-label, hidden-allergen and cross-contact checks before selection are not recorded.")
	}
	if rule, ok := casePrerequisites[r.Citation.Case]; ok {
		add("source_prerequisite", "case", rule.field, rule.reason)
	}
	return gaps
}
