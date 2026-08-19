import { getGaps } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { PageHeader } from "@/components/page-header";
import type { GapSeverity } from "@/lib/types";

// Rendered per request, never prerendered at build.
//
// These pages read the live database through the API. Statically generated, they would bake
// whatever the API returned at build time into HTML and serve it as current -- an ingredient
// value, a gap count or an audit finding frozen at a build nobody can see the date of. That
// is the same failure the provenance columns exist to prevent, reached from the other side:
// the number would carry its source and still be stale.
//
// It also means the build does not need a reachable API, which prerendering did.
export const dynamic = "force-dynamic";


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
      <PageHeader
        title="Gap register"
        description="Every missing or provisional field, counted. Nothing here is silently absent."
        meta={`${gaps.length} entries`}
      />
      {gaps.length === 0 ? (
        <Alert>
          <AlertTitle>No gaps loaded</AlertTitle>
          <AlertDescription>
            The database has no rows in gap_register. Run `go run ./cmd/import` against a
            configured DATABASE_URL, then reload.
          </AlertDescription>
        </Alert>
      ) : (
        severityOrder.map((severity) => {
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
        })
      )}
    </div>
  );
}
