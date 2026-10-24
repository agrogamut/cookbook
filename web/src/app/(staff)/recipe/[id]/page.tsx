import { notFound } from "next/navigation";
import { getRecipe, ApiError } from "@/lib/server-api";
import { ProvenanceChip } from "@/components/provenance-chip";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/page-header";
import { safeHref } from "@/lib/utils";

export default async function RecipeDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id: parameter } = await params;
  let id: string;
  try {
    // The pinned Next runtime supplies URI-encoded dynamic parameters.
    id = decodeURIComponent(parameter);
  } catch {
    notFound();
  }

  let detail;
  try {
    detail = await getRecipe(id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  const { method, nutrition } = detail;
  const sourceHref = method.suggested_method_url ? safeHref(method.suggested_method_url) : null;

  return (
    <div className="max-w-3xl space-y-6">
      <PageHeader
        title={method.recipe_id}
        description={`${method.recipe_name} · ${method.region_culture}`}
        meta={<Badge variant="outline">{method.provider_review_status}</Badge>}
      />

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
              {sourceHref && (
                <a href={sourceHref} target="_blank" rel="noreferrer" className="text-xs underline">
                  Source
                </a>
              )}
            </>
          )}
        </TabsContent>
      </Tabs>

      <Separator />

      {nutrition ? <section>
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
      </section> : <p className="text-sm">{detail.nutrition_gap ?? "Verified nutrition is unavailable."}</p>}
      {detail.case_source && (
        <section className="space-y-3 text-xs">
          <h2 className="font-mono text-sm">Source revision</h2>
          <p>{detail.case_source.source.workbook}, {detail.case_source.source.sheet}, row {detail.case_source.source.row}</p>
          <p className="break-all font-mono">Archive: {detail.case_source.source.archive_sha256}</p>
          <p className="break-all font-mono">Member: {detail.case_source.source.member_sha256}</p>
          <p>Collection membership does not establish eligibility for a child. Original source estimates and review text are retained below.</p>
          <table className="w-full text-left">
            <thead><tr><th>Ordinal</th><th>Raw ID</th><th>Source name</th><th>Grams</th></tr></thead>
            <tbody>{detail.case_source.ingredients.map((ingredient) => (
              <tr key={ingredient.ordinal}><td>{ingredient.ordinal}</td><td className="font-mono">{ingredient.raw_id}</td><td>{ingredient.raw_name}</td><td className="font-mono">{ingredient.grams}</td></tr>
            ))}</tbody>
          </table>
          <table className="w-full text-left">
            <thead><tr><th>Source field</th><th>Original value</th></tr></thead>
            <tbody>{Object.entries(detail.case_source.fields).map(([field, value]) => (
              <tr key={field}><td className="align-top font-mono">{field}</td><td className="whitespace-pre-wrap">{value}</td></tr>
            ))}</tbody>
          </table>
        </section>
      )}
    </div>
  );
}
