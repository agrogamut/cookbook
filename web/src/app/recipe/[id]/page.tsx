import { notFound } from "next/navigation";
import { getRecipe, ApiError } from "@/lib/api";
import { ProvenanceChip } from "@/components/provenance-chip";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

export default async function RecipeDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  let detail;
  try {
    detail = await getRecipe(id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  const { method, nutrition } = detail;

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="font-mono text-lg">{method.recipe_id}</h1>
        <p className="text-sm text-muted-foreground">{method.recipe_name} &middot; {method.region_culture}</p>
        <Badge variant="outline" className="mt-1">{method.provider_review_status}</Badge>
      </div>

      <Separator />

      <Tabs defaultValue="provider">
        <TabsList>
          <TabsTrigger value="provider">Provider method</TabsTrigger>
          <TabsTrigger value="suggested" disabled={!method.suggested_method_external}>
            Suggested method {method.suggested_method_external ? "" : "(none)"}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="provider" className="whitespace-pre-wrap text-sm">
          {method.provider_method}
        </TabsContent>
        <TabsContent value="suggested" className="space-y-2 text-sm">
          {method.suggested_method_external && (
            <>
              <ProvenanceChip
                source={method.suggested_method_source ?? "external"}
                confidence={method.suggested_method_confidence ?? undefined}
                explanation={method.suggestion_disclosure}
              />
              <p className="whitespace-pre-wrap">{method.suggested_method_external}</p>
              {method.suggested_method_url && (
                <a href={method.suggested_method_url} target="_blank" rel="noreferrer" className="text-xs underline">
                  Source
                </a>
              )}
            </>
          )}
        </TabsContent>
      </Tabs>

      <Separator />

      <section>
        <h2 className="mb-2 font-mono text-sm">Nutrition, provider vs. recomputed</h2>
        <ProvenanceChip
          source={nutrition.fully_verified ? "ifct" : "provider"}
          confidence={nutrition.ingredient_coverage}
          explanation={`${nutrition.formula}. ${(nutrition.ingredient_coverage * 100).toFixed(0)}% of recipe mass is IFCT-backed.`}
        />
        <table className="mt-2 w-full text-right font-mono text-xs">
          <thead>
            <tr className="text-left text-muted-foreground">
              <th className="text-left">Field</th>
              <th>Provider</th>
              <th>Recomputed</th>
              <th>Diff</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="text-left">Energy (kcal)</td>
              <td>{nutrition.provider_energy_kcal}</td>
              <td>{nutrition.energy_kcal}</td>
              <td>{nutrition.energy_pct_diff !== null ? `${nutrition.energy_pct_diff}%` : "not available"}</td>
            </tr>
            <tr>
              <td className="text-left">Iron (mg)</td>
              <td>{nutrition.provider_iron_mg}</td>
              <td>{nutrition.iron_mg}</td>
              <td>{nutrition.iron_pct_diff !== null ? `${nutrition.iron_pct_diff}%` : "not available"}</td>
            </tr>
            <tr>
              <td className="text-left">Protein (g)</td>
              <td>{nutrition.provider_protein_g}</td>
              <td>{nutrition.protein_g}</td>
              <td>not available</td>
            </tr>
            <tr>
              <td className="text-left">Calcium (mg)</td>
              <td>{nutrition.provider_calcium_mg}</td>
              <td>{nutrition.calcium_mg}</td>
              <td>not available</td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
  );
}
