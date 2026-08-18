// Mirrors internal/models/profile.go and engine.go field-for-field. Keep in sync by hand --
// there is no shared codegen between Go and TypeScript in this project, so a field added to
// ChildProfile or EngineResult on the Go side must be added here in the same change.

export interface ChildProfile {
  age_months: number;
  diet_type?: "Vegetarian" | "Non-vegetarian" | "Eggetarian";
  vegan?: boolean;
  allergens?: string[];
  clinical_flags?: Record<string, string>;
  clinical_marker?: string;
  region_culture?: string;
  cuisine_code?: string;
  meal_type?: string;
  budget_band?: "Low" | "Moderate" | "Premium";
  max_prep_time_min?: number;
  max_cook_time_min?: number;
  limit?: number;
}

export interface StepResult {
  step: number;
  name: string;
  kind: "hard_filter" | "ranker" | "target" | "escalation";
  candidates_in: number;
  candidates_out: number;
  note?: string;
  excluded?: ExclusionReason[];
}

export interface ExclusionReason {
  recipe_id: string;
  recipe_name: string;
  reason: string;
}

export interface RankedRecipe {
  recipe_id: string;
  recipe_name: string;
  region_culture: string;
  meal_type: string;
  clinical_tag: string;
  age_group: string;
  nutrition_score: number;
  ranked_score: number;
  scored_axes: string;
  value_kind: "derived";
}

export interface EngineResult {
  recipes: RankedRecipe[];
  steps: StepResult[];
  active_target: string;
  target_reason: string;
  blocked: boolean;
  block_reason?: string;
}

export interface RecipeMethodCard {
  recipe_id: string;
  recipe_name: string;
  region_culture: string;
  provider_method: string;
  provider_review_status: string;
  suggested_method_external: string | null;
  suggested_method_source: string | null;
  suggested_method_url: string | null;
  suggested_method_confidence: number | null;
  suggested_method_region_match: string | null;
  suggestion_disclosure: string;
}

export interface RecipeNutritionRecomputed {
  energy_kcal: number;
  protein_g: number;
  iron_mg: number;
  calcium_mg: number;
  ingredient_coverage: number;
  fully_verified: boolean;
  provider_energy_kcal: number;
  provider_protein_g: number;
  provider_iron_mg: number;
  provider_calcium_mg: number;
  energy_pct_diff: number | null;
  iron_pct_diff: number | null;
  value_kind: "derived";
  formula: string;
}

export interface RecipeDetail {
  method: RecipeMethodCard;
  nutrition: RecipeNutritionRecomputed;
}

// Every field here is optional-nullable except ingredient_id/english_name/food_group/
// value_source/verified -- mirrors ingredient_nutrition_corrected column-for-column
// (internal/api/handlers/ingredients.go).
export interface Ingredient {
  ingredient_id: string;
  english_name: string;
  bengali_name: string | null;
  food_group: string;
  ifct_food_code: string | null;
  ifct_food_name: string | null;
  ifct_match_exactness: string | null;
  ifct_resolved_by: string | null;
  value_source: "ifct" | "provider";
  verified: boolean;
  energy_kcal_100g: number;
  protein_g_100g: number;
  iron_mg_100g: number;
  calcium_mg_100g: number;
  provider_energy_kcal_100g: number;
  provider_protein_g_100g: number;
  provider_iron_mg_100g: number;
  provider_calcium_mg_100g: number;
  provider_review_status: string;
  provider_data_quality: string;
}

export interface NutritionDiscrepancy {
  ingredient_id: string;
  english_name: string;
  matched_ifct_food: string | null;
  used_in_recipes: number;
  provider_energy: number | null;
  external_energy: number | null;
  energy_pct_diff: number | null;
  provider_protein: number | null;
  external_protein: number | null;
  protein_pct_diff: number | null;
  provider_iron: number | null;
  external_iron: number | null;
  iron_pct_diff: number | null;
  provider_calcium: number | null;
  external_calcium: number | null;
  calcium_pct_diff: number | null;
}

export type GapSeverity = "blocker" | "major" | "minor" | "parked";

export interface Gap {
  gap_id: string;
  severity: GapSeverity;
  area: string;
  source_table: string | null;
  source_column: string | null;
  description: string;
  affected_rows: number | null;
  measured_by: "seed" | "importer";
  ui_behaviour: string;
  resolution_path: string;
  measured_at: string | null;
}

export interface ImportTableStat {
  table_name: string;
  rows_read: number;
  rows_written: number;
  rows_skipped: number;
  content_hash: string;
}

export interface ImportRun {
  run_id: number;
  started_at: string;
  finished_at: string | null;
  source_dir: string;
  ok: boolean;
  tables: ImportTableStat[];
}

export interface Region {
  region_culture: string;
  country: string;
  focus_tier: number;
  rank_weight: number;
  enrichment_scope: boolean;
  rationale: string;
}

export interface Cuisine {
  culture_code: string;
  cuisine_cluster: string;
  country: string;
  state_province: string | null;
  region_culture: string;
  focus_tier: number;
  rank_weight: number;
  recipe_count: number;
}

export interface NutritionTarget {
  target_code: string;
  target_name: string;
  target_category: string | null;
  age_from_months: number;
  age_to_months: number;
  trigger_input: string | null;
  trigger_logic: string | null;
  recipe_score_energy: number;
  recipe_score_protein: number;
  recipe_score_iron: number;
  recipe_score_calcium: number;
  recipe_score_fruitveg: number;
  recipe_score_diversity: number;
  recipe_score_cost: number;
  hard_exclusions: string | null;
  soft_penalties: string | null;
}

export interface Book1Block {
  block_id: string;
  book_order: number;
  part: string | null;
  section: string | null;
  subsection: string | null;
  age_from_mo: number | null;
  age_to_mo: number | null;
  trigger_or_eligibility: string | null;
  personalization_inputs: string | null;
  table_or_format: string | null;
  nutrition_target_link: string | null;
  clinical_rule_link: string | null;
  safety_link: string | null;
  ai_can_draft: "Y" | "N";
  human_approval: string | null;
  status: string | null;
}
