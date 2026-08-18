import { getRegions, getCuisines, getNutritionTargets, getBook1Blocks } from "@/lib/api";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ProvenanceChip } from "@/components/provenance-chip";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";

export default async function ReferencePage() {
  const [regions, cuisines, targets, book1Blocks] = await Promise.all([
    getRegions(), getCuisines(), getNutritionTargets(), getBook1Blocks(),
  ]);

  return (
    <Tabs defaultValue="regions">
      <TabsList>
        <TabsTrigger value="regions">Regions</TabsTrigger>
        <TabsTrigger value="cuisines">Cuisines</TabsTrigger>
        <TabsTrigger value="targets">Nutrition targets</TabsTrigger>
        <TabsTrigger value="book1">Book 1 blocks</TabsTrigger>
      </TabsList>

      <TabsContent value="regions">
        {regions.length === 0 ? (
          <Alert>
            <AlertTitle>No regions loaded</AlertTitle>
            <AlertDescription>
              The database has no rows in region_focus. Run `go run ./cmd/import` against
              a configured DATABASE_URL, then reload.
            </AlertDescription>
          </Alert>
        ) : (
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
        )}
      </TabsContent>

      <TabsContent value="cuisines">
        {cuisines.length === 0 ? (
          <Alert>
            <AlertTitle>No cuisines loaded</AlertTitle>
            <AlertDescription>
              The database has no rows with a nonzero recipe count in culture_region_map.
              Run `go run ./cmd/import` against a configured DATABASE_URL, then reload.
            </AlertDescription>
          </Alert>
        ) : (
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
        )}
      </TabsContent>

      <TabsContent value="targets">
        {targets.length === 0 ? (
          <Alert>
            <AlertTitle>No nutrition targets loaded</AlertTitle>
            <AlertDescription>
              The database has no rows in nutrition_target. Run `go run ./cmd/import`
              against a configured DATABASE_URL, then reload.
            </AlertDescription>
          </Alert>
        ) : (
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
        )}
      </TabsContent>

      <TabsContent value="book1">
        <p className="mb-2 text-xs text-muted-foreground">
          The 32 Book 1 content blocks in render order (book_order, not block id). Blocks
          marked <span className="font-mono">draft: no</span> are closed to generated text
          by the provider and may only ever carry provider-authored content.
        </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="text-right">#</TableHead>
              <TableHead>Block</TableHead>
              <TableHead>Section</TableHead>
              <TableHead>Fires when</TableHead>
              <TableHead>Needs</TableHead>
              <TableHead>Draft</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {book1Blocks.map((b) => (
              <TableRow key={b.block_id}>
                <TableCell className="text-right font-mono text-xs">{b.book_order}</TableCell>
                <TableCell className="font-mono text-xs">{b.block_id}</TableCell>
                <TableCell className="text-xs">
                  {b.section}
                  {b.subsection && <span className="text-muted-foreground"> / {b.subsection}</span>}
                </TableCell>
                <TableCell className="text-xs">{b.trigger_or_eligibility ?? "not available"}</TableCell>
                <TableCell className="max-w-xs truncate text-xs text-muted-foreground"
                           title={b.personalization_inputs ?? undefined}>
                  {b.personalization_inputs ?? "not available"}
                </TableCell>
                <TableCell>
                  <Badge variant="outline"
                         className={b.ai_can_draft === "N" ? "border-destructive text-destructive" : ""}>
                    {b.ai_can_draft === "N" ? "no" : "yes"}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TabsContent>
    </Tabs>
  );
}
