import "server-only";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { cache } from "react";
import { ApiError, resolveBaseUrl } from "./api";
import type {
  Ingredient,
  NutritionDiscrepancy,
  Gap,
  ImportRun,
  Region,
  Cuisine,
  NutritionTarget,
  Book1Block,
  RecipeDetail,
} from "./types";
import type { StaffAccount, Registration } from "./portal-types";

export { ApiError } from "./api";

async function serverRequest<T>(path: string): Promise<T> {
  const session = (await cookies()).get("madamgy_session");
  if (!session) redirect("/login");
  const base = resolveBaseUrl(
    process.env.API_PROXY_URL ??
      process.env.NEXT_PUBLIC_API_URL ??
      "http://127.0.0.1:8080",
  );
  const response = await fetch(`${base}${path}`, {
    headers: { Cookie: `${session.name}=${session.value}` },
    cache: "no-store",
  });
  if (response.status === 401) redirect("/login");
  if (response.status === 403) redirect("/doctor");
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new ApiError(
      response.status,
      body.error ?? "Could not load this page.",
    );
  }
  return response.json() as Promise<T>;
}
export const requireStaff = cache(() =>
  serverRequest<StaffAccount>("/api/auth/me"),
);
export async function requireAdmin() {
  const actor = await requireStaff();
  if (actor.role !== "admin") redirect("/doctor");
  return actor;
}
export const listIngredients = (limit = 100) =>
  serverRequest<Ingredient[]>(`/api/ingredients?limit=${limit}`);
export const getNutritionAudit = () =>
  serverRequest<NutritionDiscrepancy[]>("/api/audit/nutrition");
export const getGaps = () => serverRequest<Gap[]>("/api/gaps");
export const getRuns = () => serverRequest<ImportRun[]>("/api/runs");
export const getRegions = () =>
  serverRequest<Region[]>("/api/reference/regions");
export const getCuisines = () =>
  serverRequest<Cuisine[]>("/api/reference/cuisines");
export const getNutritionTargets = () =>
  serverRequest<NutritionTarget[]>("/api/reference/nutrition-targets");
export const getBook1Blocks = () =>
  serverRequest<Book1Block[]>("/api/reference/book1-blocks");
export const getRecipe = (id: string) =>
  serverRequest<RecipeDetail>(`/api/recipes/${encodeURIComponent(id)}`);
export const getRegistration = (id: string) =>
  serverRequest<Registration>(`/api/registrations/${encodeURIComponent(id)}`);
