import type {
  ChildProfile, EngineResult, RecipeDetail, Ingredient, NutritionDiscrepancy,
  Gap, ImportRun, Region, Cuisine, NutritionTarget, Book1Block,
  Allergen, ClinicalMarker, ReferenceEnums,
} from "./types";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, body.error ?? res.statusText);
  }
  return res.json() as Promise<T>;
}

export function search(profile: ChildProfile): Promise<EngineResult> {
  return request<EngineResult>("/api/search", { method: "POST", body: JSON.stringify(profile) });
}

export function getRecipe(recipeId: string): Promise<RecipeDetail> {
  return request<RecipeDetail>(`/api/recipes/${encodeURIComponent(recipeId)}`);
}

export function listIngredients(limit = 100): Promise<Ingredient[]> {
  return request<Ingredient[]>(`/api/ingredients?limit=${limit}`);
}

export function getNutritionAudit(): Promise<NutritionDiscrepancy[]> {
  return request<NutritionDiscrepancy[]>("/api/audit/nutrition");
}

export function getGaps(): Promise<Gap[]> {
  return request<Gap[]>("/api/gaps");
}

export function getRuns(): Promise<ImportRun[]> {
  return request<ImportRun[]>("/api/runs");
}

export function getRegions(): Promise<Region[]> {
  return request<Region[]>("/api/reference/regions");
}

export function getCuisines(): Promise<Cuisine[]> {
  return request<Cuisine[]>("/api/reference/cuisines");
}

export function getNutritionTargets(): Promise<NutritionTarget[]> {
  return request<NutritionTarget[]>("/api/reference/nutrition-targets");
}

export function getAllergens(): Promise<Allergen[]> {
  return request<Allergen[]>("/api/reference/allergens");
}

export function getClinicalMarkers(): Promise<ClinicalMarker[]> {
  return request<ClinicalMarker[]>("/api/reference/clinical-markers");
}

export function getEnums(): Promise<ReferenceEnums> {
  return request<ReferenceEnums>("/api/reference/enums");
}

export function getBook1Blocks(): Promise<Book1Block[]> {
  return request<Book1Block[]>("/api/reference/book1-blocks");
}

export { ApiError };
