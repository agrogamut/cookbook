import { getNutritionAudit } from "@/lib/api";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ProvenanceChip } from "@/components/provenance-chip";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { PageHeader } from "@/components/page-header";

export default async function NutritionAuditPage() {
  const rows = await getNutritionAudit();

  return (
    <div>
      <PageHeader
        title="Nutrition discrepancy report"
        description="Confirmed exact-name matches where the provider disagrees with IFCT 2017 by more than 20%. The list to hand the provider -- nothing here is corrected locally."
        meta={`${rows.length} rows`}
      />
      {rows.length === 0 ? (
        <Alert>
          <AlertTitle>No discrepancies loaded</AlertTitle>
          <AlertDescription>
            The database has no rows in nutrition_discrepancy_report. Run `go run
            ./cmd/enrich` against a configured DATABASE_URL, then reload.
          </AlertDescription>
        </Alert>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Ingredient</TableHead>
              <TableHead>Matched IFCT food</TableHead>
              <TableHead className="text-right">Used in</TableHead>
              <TableHead className="text-right">Provider kcal</TableHead>
              <TableHead className="text-right">IFCT kcal</TableHead>
              <TableHead className="text-right">Diff</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.ingredient_id}>
                <TableCell>{r.english_name}</TableCell>
                <TableCell className="text-xs">{r.matched_ifct_food ?? "not available"}</TableCell>
                <TableCell className="text-right font-mono text-xs">{r.used_in_recipes}</TableCell>
                <TableCell className="text-right font-mono text-xs">{r.provider_energy ?? "not available"}</TableCell>
                <TableCell className="text-right font-mono text-xs">{r.external_energy ?? "not available"}</TableCell>
                <TableCell className="text-right font-mono text-xs font-semibold">
                  <div className="flex items-center justify-end gap-2">
                    <span>
                      {r.energy_pct_diff !== null ? `${r.energy_pct_diff > 0 ? "+" : ""}${r.energy_pct_diff}%` : "not available"}
                    </span>
                    <ProvenanceChip
                      source="derived"
                      explanation="Confirmed exact-name matches where the provider disagrees with IFCT 2017 by more than 20%."
                    />
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
