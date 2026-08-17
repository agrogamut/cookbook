import { listIngredients } from "@/lib/api";
import { ProvenanceChip } from "@/components/provenance-chip";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

export default async function IngredientsPage() {
  const ingredients = await listIngredients(200);

  return (
    <div>
      <h1 className="mb-4 font-mono text-lg">Ingredients ({ingredients.length})</h1>
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
    </div>
  );
}
