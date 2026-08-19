import { listIngredients } from "@/lib/api";
import { ProvenanceChip } from "@/components/provenance-chip";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { PageHeader } from "@/components/page-header";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

export default async function IngredientsPage() {
  // 500 is the API's hard cap (internal/api/handlers/ingredients.go) and exceeds the full
  // 406-row ingredient_master, so this fetches everything -- the heading below can then
  // read as the true total rather than an arbitrary page size an operator would mistake
  // for the whole table.
  const ingredients = await listIngredients(500);

  return (
    <div>
      <PageHeader
        title="Ingredients"
        description="Provider values verbatim, IFCT-corrected where an alias resolves. Unverified rows show the provider figure alongside."
        meta={`${ingredients.length} rows`}
      />
      {ingredients.length === 0 ? (
        <Alert>
          <AlertTitle>No ingredients loaded</AlertTitle>
          <AlertDescription>
            The database has no rows in ingredient_nutrition_corrected. Run `go run
            ./cmd/import` against a configured DATABASE_URL, then reload.
          </AlertDescription>
        </Alert>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Food group</TableHead>
              <TableHead className="text-right">Energy (kcal/100g)</TableHead>
              <TableHead className="text-right">Iron (mg/100g)</TableHead>
              <TableHead>Source</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {ingredients.map((i) => (
              <TableRow key={i.ingredient_id}>
                <TableCell className="font-mono text-xs">{i.ingredient_id}</TableCell>
                <TableCell>{i.english_name}</TableCell>
                <TableCell className="text-xs">{i.food_group}</TableCell>
                <TableCell className="text-right font-mono text-xs">
                  {i.energy_kcal_100g}
                  {!i.verified && (
                    <span className="ml-1 text-muted-foreground">(provider: {i.provider_energy_kcal_100g})</span>
                  )}
                </TableCell>
                <TableCell className="text-right font-mono text-xs">
                  {i.iron_mg_100g}
                  {!i.verified && (
                    <span className="ml-1 text-muted-foreground">(provider: {i.provider_iron_mg_100g})</span>
                  )}
                </TableCell>
                <TableCell>
                  <ProvenanceChip
                    source={i.value_source}
                    explanation={
                      i.verified
                        ? `IFCT 2017 match: ${i.ifct_food_name} (${i.ifct_match_exactness}, resolved by ${i.ifct_resolved_by}).`
                        : `No IFCT counterpart identified. Provider value stands, review status: ${i.provider_review_status}, data quality: ${i.provider_data_quality}.`
                    }
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
