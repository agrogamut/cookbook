package importer

// TableSpec binds one worksheet to one Postgres table.
//
// Column names are not listed here on purpose. The importer reads the target table's
// columns from information_schema and matches them against the snake_cased worksheet
// headers, so the migration DDL stays the single source of truth for what a column is
// called and what type it holds. A header with no matching column, or a NOT NULL column
// with no matching header, is reported rather than silently dropped.
type TableSpec struct {
	Table      string   // Postgres table
	File       string   // workbook filename, relative to the xlsx directory
	Sheet      string   // worksheet name
	HeaderRow  int      // 1-indexed; row 3 or row 4, never guessed
	PrimaryKey []string // used for upsert and for the delete-not-present sweep
	FirstCol   string   // expected first header, verified so a re-layout fails loudly

	// ScopeColumn names a column holding a region. When set, rows whose region is not
	// listed in region_focus are skipped, which is how the project scope is enforced.
	// Widening or narrowing the scope is an edit to that seed table plus a re-import;
	// no workbook is ever modified and no row is ever rewritten.
	ScopeColumn string
}

// Specs are listed parent-first. The importer upserts in this order and deletes in
// reverse, so foreign keys hold at every point without needing CASCADE.
//
// Sheets deliberately not imported are recorded in gap_register rather than omitted
// silently: the Review/Version Control workbook (an empty scaffold), the Page Registry
// and Book 1/2 content blocks (PDF assembly, not consumed by the web engine).
var Specs = []TableSpec{
	{
		Table: "ingredient_source_register", File: "MadamGY_Ingredient_Super_Master_Complete_V1_Cost_Filled.xlsx",
		Sheet: "Source_Register", HeaderRow: 3,
		PrimaryKey: []string{"source_id"}, FirstCol: "source_id",
	},
	{
		Table: "ingredient_master", File: "MadamGY_Ingredient_Super_Master_Complete_V1_Cost_Filled.xlsx",
		Sheet: "Ingredient_Master", HeaderRow: 4,
		PrimaryKey: []string{"ingredient_id"}, FirstCol: "ingredient_id",
	},
	{
		Table: "recipe_master", File: "MadamGY_Recipe_Master_1000_Detailed_V2.xlsx",
		Sheet: "Recipe_Master", HeaderRow: 4,
		PrimaryKey: []string{"recipe_id"}, FirstCol: "recipe_id",
		ScopeColumn: "region_culture",
	},
	{
		Table: "recipe_ingredient_mapping", File: "MadamGY_Recipe_Ingredient_Mapping_Master_V1.xlsx",
		Sheet: "Recipe-Ingredient Mapping", HeaderRow: 4,
		PrimaryKey: []string{"mapping_id"}, FirstCol: "mapping_id",
		ScopeColumn: "region_culture",
	},
	{
		Table: "culture_location_master", File: "MadamGY_Culture_Location_Master_V1.xlsx",
		Sheet: "Culture-Location Master", HeaderRow: 4,
		PrimaryKey: []string{"culture_code"}, FirstCol: "culture_code",
		// Culture rows key on culture_code, not on a region string, so scope is applied
		// by culture_region_map membership rather than by a column value.
	},
	{
		Table: "age_feeding_stage_master", File: "MadamGY_Age_Feeding_Stage_Master_V1.xlsx",
		Sheet: "Age-Feeding Stage Master", HeaderRow: 4,
		PrimaryKey: []string{"stage_code"}, FirstCol: "stage_code",
	},
	{
		Table: "nutrition_target_master", File: "MadamGY_Nutrition_Target_Master_V1.xlsx",
		Sheet: "Nutrition Target Master", HeaderRow: 4,
		PrimaryKey: []string{"target_code"}, FirstCol: "target_code",
	},
	{
		Table: "nt_growth_trigger_reference", File: "MadamGY_Nutrition_Target_Master_V1.xlsx",
		Sheet: "Growth Trigger Reference", HeaderRow: 3,
		PrimaryKey: []string{"rule_code"}, FirstCol: "rule_code",
	},
	{
		Table: "nt_engine_priority_logic", File: "MadamGY_Nutrition_Target_Master_V1.xlsx",
		Sheet: "Engine Priority Logic", HeaderRow: 3,
		PrimaryKey: []string{"priority_order"}, FirstCol: "priority_order",
	},
	{
		Table: "clinical_rule_master", File: "MadamGY_Clinical_Rule_Master_V1.xlsx",
		Sheet: "Clinical Rule Master", HeaderRow: 4,
		PrimaryKey: []string{"rule_id"}, FirstCol: "rule_id",
	},
	{
		Table: "clinical_red_flag_escalation", File: "MadamGY_Clinical_Rule_Master_V1.xlsx",
		Sheet: "Red Flag Escalation", HeaderRow: 3,
		PrimaryKey: []string{"escalation_code"}, FirstCol: "escalation_code",
	},
	{
		Table: "clinical_rule_priority_logic", File: "MadamGY_Clinical_Rule_Master_V1.xlsx",
		Sheet: "Rule Priority Logic", HeaderRow: 3,
		PrimaryKey: []string{"priority_order"}, FirstCol: "priority_order",
	},
	{
		Table: "allergy_safety_master", File: "MadamGY_Allergy_Safety_Master_V1.xlsx",
		Sheet: "Allergy Safety Master", HeaderRow: 4,
		PrimaryKey: []string{"safety_id"}, FirstCol: "safety_id",
	},
	{
		Table: "allergen_mapping", File: "MadamGY_Allergy_Safety_Master_V1.xlsx",
		Sheet: "Allergen Mapping", HeaderRow: 3,
		PrimaryKey: []string{"allergen_id"}, FirstCol: "allergen_id",
	},
	{
		Table: "choking_texture_safety", File: "MadamGY_Allergy_Safety_Master_V1.xlsx",
		Sheet: "Choking Texture Safety", HeaderRow: 3,
		PrimaryKey: []string{"hazard_id"}, FirstCol: "hazard_id",
	},
	{
		Table: "food_safety_sop", File: "MadamGY_Allergy_Safety_Master_V1.xlsx",
		Sheet: "Food Safety SOP", HeaderRow: 3,
		PrimaryKey: []string{"sop_id"}, FirstCol: "sop_id",
	},
	{
		Table: "safety_engine_workflow", File: "MadamGY_Allergy_Safety_Master_V1.xlsx",
		Sheet: "Safety Engine Workflow", HeaderRow: 3,
		PrimaryKey: []string{"step"}, FirstCol: "step",
	},
	{
		Table: "evidence_reference_master", File: "MadamGY_Evidence_Reference_Master_V1.xlsx",
		Sheet: "Evidence Reference Master", HeaderRow: 4,
		PrimaryKey: []string{"evidence_id"}, FirstCol: "evidence_id",
	},
	{
		Table: "rule_evidence_mapping", File: "MadamGY_Evidence_Reference_Master_V1.xlsx",
		Sheet: "Rule Evidence Mapping", HeaderRow: 3,
		PrimaryKey: []string{"mapping_id"}, FirstCol: "mapping_id",
	},
	{
		Table: "recipe_selection_logic", File: "MadamGY_Book2_Content_Master_V1.xlsx",
		Sheet: "Recipe Selection Logic", HeaderRow: 4,
		PrimaryKey: []string{"priority"}, FirstCol: "priority",
	},
	{
		Table: "meal_category_target", File: "MadamGY_Book2_Content_Master_V1.xlsx",
		Sheet: "Meal Category Targets", HeaderRow: 4,
		PrimaryKey: []string{"meal_category_id"}, FirstCol: "meal_category_id",
	},
}
