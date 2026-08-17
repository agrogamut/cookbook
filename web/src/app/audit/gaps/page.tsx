import { getGaps } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import type { GapSeverity } from "@/lib/types";

const severityOrder: GapSeverity[] = ["blocker", "major", "minor", "parked"];

const severityColor: Record<GapSeverity, string> = {
  blocker: "border-destructive text-destructive",
  major: "border-[var(--color-unverified)] text-[var(--color-unverified)]",
  minor: "border-muted-foreground text-muted-foreground",
  parked: "border-muted-foreground text-muted-foreground opacity-60",
};

export default async function GapsPage() {
  const gaps = await getGaps();

  return (
    <div>
      <h1 className="mb-4 font-mono text-lg">Gap register ({gaps.length})</h1>
      {severityOrder.map((severity) => {
        const inSeverity = gaps.filter((g) => g.severity === severity);
        if (inSeverity.length === 0) return null;
        return (
          <div key={severity} className="mb-6">
            <h2 className="mb-2 font-mono text-xs uppercase text-muted-foreground">{severity} ({inSeverity.length})</h2>
            <Accordion type="multiple">
              {inSeverity.map((g) => (
                <AccordionItem key={g.gap_id} value={g.gap_id}>
                  <AccordionTrigger className="font-mono text-sm">
                    <span className="flex items-center gap-2">
                      <Badge variant="outline" className={severityColor[g.severity]}>{g.gap_id}</Badge>
                      {g.area}
                      {g.affected_rows !== null && (
                        <span className="text-xs text-muted-foreground">({g.affected_rows} rows)</span>
                      )}
                    </span>
                  </AccordionTrigger>
                  <AccordionContent className="space-y-2 text-sm">
                    <p>{g.description}</p>
                    <p className="text-xs text-muted-foreground">UI behaviour: {g.ui_behaviour}</p>
                    <p className="text-xs text-muted-foreground">Resolution: {g.resolution_path}</p>
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          </div>
        );
      })}
    </div>
  );
}
