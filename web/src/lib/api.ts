import type {
  ChildProfile, EngineResult, RecipeDetail, Ingredient, NutritionDiscrepancy,
  Gap, ImportRun, Region, Cuisine, NutritionTarget, Book1Block,
  Allergen, ClinicalMarker, ReferenceEnums, StoredProfile, EngineInputResult, SpecialCareCondition,
} from "./types";

/** Resolve the API base URL, tolerating an address given without a scheme.
 *
 *  Render's Blueprint supplies one service's address to another as a bare host
 *  ("madamgy-api.onrender.com"), because a service can be reached over more than one scheme.
 *  fetch() reads a scheme-less string as a *relative path*, so the call would quietly go to
 *  the console's own origin and 404 -- a failure that looks like a missing endpoint rather
 *  than a misconfigured URL. Resolving it here keeps the blueprint self-wiring instead of
 *  needing the full URL pasted in by hand, and re-pasted after any rename.
 *
 *  A local address keeps http, because a dev machine has no certificate. */
export function resolveBaseUrl(raw: string): string {
  const trimmed = raw.trim().replace(/\/+$/, "");
  if (/^https?:\/\//i.test(trimmed)) return trimmed;
  if (/^(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$/i.test(trimmed)) return `http://${trimmed}`;
  return `https://${trimmed}`;
}

const BASE_URL = resolveBaseUrl(process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080");

export class ApiError extends Error {
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

export function getProfile(childID: string): Promise<StoredProfile> {
  return request<StoredProfile>(`/api/profiles/${encodeURIComponent(childID)}`);
}

export function putProfile(childID: string, body: StoredProfile): Promise<StoredProfile> {
  return request<StoredProfile>(`/api/profiles/${encodeURIComponent(childID)}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

/** Returns the engine query derived from a stored profile, plus the stored facts the
 *  conversion dropped. Prefer this over getProfile when populating the search form: the
 *  form's fields are engine-query fields, and the dropped list is what stops the form
 *  looking like the whole profile. */
export function getProfileEngineInput(childID: string): Promise<EngineInputResult> {
  return request<EngineInputResult>(
    `/api/profiles/${encodeURIComponent(childID)}/engine-input`);
}

export function getSpecialCareConditions(): Promise<SpecialCareCondition[]> {
  return request<SpecialCareCondition[]>("/api/reference/special-care-conditions");
}

/**
 * A book preview, plus what the book does not contain. `omissions` comes from the
 * X-Book-Omissions header rather than the body, because the body is the document itself.
 * An empty array means the API reported no omissions -- never that none were checked.
 */
export interface BookPreview {
  html: string;
  omissions: string[];
}

/** Thrown when the engine stops generation for a clinical reason. Distinct from ApiError
 *  because an operator must never read a clinical stop as a fault. */
export class BookBlockedError extends Error {
  constructor(message: string, public reviewer?: string) {
    super(message);
    this.name = "BookBlockedError";
  }
}

/** Thrown when the print pipeline has no browser. An operational fault, not a clinical one.
 *  Retrying changes nothing until a browser is installed. */
export class RendererUnavailableError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RendererUnavailableError";
  }
}

/** Thrown when a browser was present and the print itself failed. Kept separate from
 *  RendererUnavailableError because this one may well be about the document: telling an
 *  operator nothing about the child changed would have them retry a data problem forever. */
export class PrintFailedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "PrintFailedError";
  }
}

async function bookError(res: Response): Promise<Error> {
  const body = await res.json().catch(() => ({ error: res.statusText }));
  if (res.status === 409) return new BookBlockedError(body.error ?? res.statusText, body.reviewer);
  if (res.status === 503) return new RendererUnavailableError(body.error ?? res.statusText);
  if (res.status === 500 && typeof body.error === "string" && body.error.startsWith("pdf render failed")) {
    return new PrintFailedError(body.error);
  }
  return new ApiError(res.status, body.error ?? res.statusText);
}

function omissionsOf(res: Response): string[] {
  const header = res.headers.get("X-Book-Omissions");
  if (!header) return [];
  return header.split(";").map((s) => s.trim()).filter(Boolean);
}

export async function getBookPreview(childID: string, book: "book1" | "book2"): Promise<BookPreview> {
  const res = await fetch(`${BASE_URL}/api/books/${encodeURIComponent(childID)}/${book}/preview`);
  if (!res.ok) throw await bookError(res);
  return { html: await res.text(), omissions: omissionsOf(res) };
}

export async function getBookPdf(childID: string, book: "book1" | "book2"): Promise<Blob> {
  const res = await fetch(`${BASE_URL}/api/books/${encodeURIComponent(childID)}/${book}.pdf`);
  if (!res.ok) throw await bookError(res);
  return res.blob();
}

/** One generation run: both of a child's books, from one profile read at one instant.
 *
 *  Omissions arrive split three ways. A profile omission is a fact about the child and holds
 *  for both books; a book omission is that book's own corpus coverage. Merging them would
 *  leave an operator unable to tell which of the two they are looking at. */
export interface BookSet {
  childID: string;
  asOf: string;
  book1Html: string;
  book2Html: string;
  profileOmissions: string[];
  book1Omissions: string[];
  book2Omissions: string[];
}

export async function getBookSet(childID: string): Promise<BookSet> {
  const res = await fetch(`${BASE_URL}/api/books/${encodeURIComponent(childID)}/preview`);
  if (!res.ok) throw await bookError(res);
  const body = await res.json();
  return {
    childID: body.child_id,
    asOf: body.as_of,
    book1Html: body.book1_html,
    book2Html: body.book2_html,
    // Defaulted rather than assumed: the API sends [] for each, and a client that renders a
    // count must not crash a whole preview over a missing list.
    profileOmissions: body.profile_omissions ?? [],
    book1Omissions: body.book1_omissions ?? [],
    book2Omissions: body.book2_omissions ?? [],
  };
}

/** Both books as printed PDFs, in one zip. Two books, two files -- not one merged PDF, which
 *  would renumber Book 2's pages behind Book 1's. */
export async function getBookSetZip(childID: string): Promise<Blob> {
  const res = await fetch(`${BASE_URL}/api/books/${encodeURIComponent(childID)}/books.zip`);
  if (!res.ok) throw await bookError(res);
  return res.blob();
}
