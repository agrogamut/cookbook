"use client";

import Link from "next/link";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { ProvenanceChip } from "@/components/provenance-chip";
import type { RankedRecipe } from "@/lib/types";

export function ResultsTable({ recipes }: { recipes: RankedRecipe[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Recipe</TableHead>
          <TableHead>Region</TableHead>
          <TableHead>Meal</TableHead>
          <TableHead>Clinical tag</TableHead>
          <TableHead className="text-right">Nutrition</TableHead>
          <TableHead className="text-right">Ranked</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {recipes.map((r) => (
          <TableRow key={r.recipe_id}>
            <TableCell>
              <Link href={`/recipe/${r.recipe_id}`} className="font-mono text-xs underline underline-offset-2">
                {r.recipe_id}
              </Link>
              <div className="text-sm">{r.recipe_name}</div>
            </TableCell>
            <TableCell className="text-xs">{r.region_culture}</TableCell>
            <TableCell className="text-xs">{r.meal_type}</TableCell>
            <TableCell className="text-xs">{r.clinical_tag}</TableCell>
            <TableCell className="text-right">
              <div className="flex items-center justify-end gap-2">
                <span className="font-mono text-xs">{r.nutrition_score.toFixed(3)}</span>
                <ProvenanceChip
                  source="derived"
                  explanation={`Scored axes: ${r.scored_axes}. sum(weight * normalised axis) / sum(weight), normalised within the ${r.age_group} age band.`}
                />
              </div>
            </TableCell>
            <TableCell className="text-right">
              <div className="flex items-center justify-end gap-2">
                <span className="font-mono text-xs">{r.ranked_score.toFixed(3)}</span>
                <ProvenanceChip
                  source="derived"
                  explanation='Nutrition score, then adjusted by whichever of the step 7 (region), 9 (availability), 10 (budget) and 12 (near-duplicate) rankers applied -- each adjustment is a small nudge (roughly +/-0.02 to +/-0.05), never large enough to override the nutrition ranking. See "Why this result" for exactly which steps ran and what each did to this recipe.'
                />
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
