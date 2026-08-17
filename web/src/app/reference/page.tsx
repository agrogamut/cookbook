import { getRegions, getCuisines, getNutritionTargets } from "@/lib/api";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ProvenanceChip } from "@/components/provenance-chip";

export default async function ReferencePage() {
  const [regions, cuisines, targets] = await Promise.all([
    getRegions(), getCuisines(), getNutritionTargets(),
  ]);

  return (
    <Tabs defaultValue="regions">
      <TabsList>
        <TabsTrigger value="regions">Regions</TabsTrigger>
        <TabsTrigger value="cuisines">Cuisines</TabsTrigger>
        <TabsTrigger value="targets">Nutrition targets</TabsTrigger>
      </TabsList>

      <TabsContent value="regions">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Region</TableHead><TableHead>Country</TableHead>
              <TableHead className="text-right">Tier</TableHead>
              <TableHead className="text-right">rank_weight</TableHead>
              <TableHead>Rationale</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {regions.map((r) => (
              <TableRow key={r.region_culture}>
                <TableCell>{r.region_culture}</TableCell>
                <TableCell>{r.country}</TableCell>
                <TableCell className="text-right font-mono text-xs">{r.focus_tier}</TableCell>
                <TableCell className="text-right font-mono text-xs">
                  <div className="flex items-center justify-end gap-2">
                    <span>{r.rank_weight.toFixed(2)}</span>
                    <ProvenanceChip
                      source="derived"
                      explanation="Derived: tier 1 = 1.00, tier 2 = 0.90 (flat 0.10 per tier). Reorders results within a candidate pool the nutrition rubric already ranked; never overrides it."
                    />
                  </div>
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">{r.rationale}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TabsContent>

      <TabsContent value="cuisines">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Cuisine cluster</TableHead><TableHead>Region</TableHead>
              <TableHead className="text-right">Recipes</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cuisines.map((c) => (
              <TableRow key={c.culture_code}>
                <TableCell>{c.cuisine_cluster}</TableCell>
                <TableCell className="text-xs">{c.region_culture}</TableCell>
                <TableCell className="text-right font-mono text-xs">{c.recipe_count}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TabsContent>

      <TabsContent value="targets">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Code</TableHead><TableHead>Name</TableHead>
              <TableHead className="text-right">E</TableHead><TableHead className="text-right">P</TableHead>
              <TableHead className="text-right">Fe</TableHead><TableHead className="text-right">Ca</TableHead>
              <TableHead className="text-right">FV</TableHead><TableHead className="text-right">Div</TableHead>
              <TableHead className="text-right">Cost</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {targets.map((t) => (
              <TableRow key={t.target_code}>
                <TableCell className="font-mono text-xs">{t.target_code}</TableCell>
                <TableCell className="text-xs">{t.target_name}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_energy}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_protein}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_iron}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_calcium}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_fruitveg}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_diversity}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_cost}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TabsContent>
    </Tabs>
  );
}
