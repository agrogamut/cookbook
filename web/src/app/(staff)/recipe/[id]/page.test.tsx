import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, getRecipe } from "@/lib/server-api";
import RecipeDetailPage from "./page";

vi.mock("@/lib/server-api", async () => ({
  ApiError: (await import("@/lib/api")).ApiError,
  getRecipe: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  notFound: () => { throw new Error("NEXT_NOT_FOUND"); },
  redirect: () => { throw new Error("UNEXPECTED_REDIRECT"); },
}));

const archive = "3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2";
const sourceID = `case:${archive}:MG-WB-C26-001`;
const getRecipeMock = vi.mocked(getRecipe);
const detail = {
  method: { recipe_id: sourceID, recipe_name: "Source recipe", provider_method: "Original method", provider_review_status: "Original status" },
  nutrition: null,
} as Awaited<ReturnType<typeof getRecipe>>;

describe("recipe page parameter boundary", () => {
  beforeEach(() => {
    getRecipeMock.mockReset().mockResolvedValue(detail);
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it.each([
    ["source literal", sourceID, sourceID],
    ["source canonical", encodeURIComponent(sourceID), sourceID],
    ["source lowercase escapes", encodeURIComponent(sourceID).replaceAll("%3A", "%3a"), sourceID],
    ["source escaped letters", encodeURIComponent(sourceID).replace("case", "%63ase").replace("MG-", "%4dG%2d"), sourceID],
    ["legacy literal", "MG-R-00001", "MG-R-00001"],
    ["legacy escaped separators", "MG%2dR%2D00001", "MG-R-00001"],
    ["legacy escaped letters", "%4dG-R-00001", "MG-R-00001"],
    ["literal plus", "recipe+plus", "recipe+plus"],
    ["encoded plus", "recipe%2Bplus", "recipe+plus"],
    ["literal percent identity", "recipe%25value", "recipe%value"],
    ["percent escape identity", "recipe%253Avalue", "recipe%3Avalue"],
  ])("passes the exact decoded identity to the API for %s", async (_, parameter, id) => {
    await RecipeDetailPage({ params: Promise.resolve({ id: parameter }) });
    expect(getRecipeMock).toHaveBeenCalledExactlyOnceWith(id);
  });

  it.each([sourceID, "MG-R-00001"])("does not alias a double-encoded parameter for %s", async (id) => {
    const encoded = id.startsWith("case:") ? encodeURIComponent(id) : id.replaceAll("-", "%2D");
    getRecipeMock.mockRejectedValue(new ApiError(404, "Recipe not found"));
    await expect(RecipeDetailPage({ params: Promise.resolve({ id: encodeURIComponent(encoded) }) })).rejects.toThrow("NEXT_NOT_FOUND");
    expect(getRecipeMock).toHaveBeenCalledExactlyOnceWith(encoded);
    expect(getRecipeMock.mock.calls[0][0]).not.toBe(id);
  });

  it.each(["%", "%2", "%GG", "%FF"])("rejects malformed parameter %s before fetching", async (id) => {
    await expect(RecipeDetailPage({ params: Promise.resolve({ id }) })).rejects.toThrow("NEXT_NOT_FOUND");
    expect(getRecipeMock).not.toHaveBeenCalled();
  });
});
