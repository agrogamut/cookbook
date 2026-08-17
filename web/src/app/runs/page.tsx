import { getRuns } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";

export default async function RunsPage() {
  const runs = await getRuns();

  return (
    <div>
      <h1 className="mb-4 font-mono text-lg">Import runs</h1>
      <div className="space-y-4">
        {runs.map((run) => (
          <Collapsible key={run.run_id} className="rounded border p-3">
            <CollapsibleTrigger className="flex w-full items-center justify-between font-mono text-sm">
              <span>
                Run {run.run_id} &middot; {run.started_at}
                {run.ok ? (
                  <Badge variant="outline" className="ml-2 border-[var(--color-verified)] text-[var(--color-verified)]">ok</Badge>
                ) : (
                  <Badge variant="outline" className="ml-2 border-destructive text-destructive">failed</Badge>
                )}
              </span>
              <span className="text-xs text-muted-foreground">{run.tables.length} tables</span>
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-2 space-y-1 font-mono text-xs">
              {run.tables.map((t) => (
                <div key={t.table_name} className="flex justify-between border-t pt-1">
                  <span>{t.table_name}</span>
                  <span className="text-muted-foreground">
                    read {t.rows_read} / written {t.rows_written} / skipped {t.rows_skipped}
                  </span>
                  <span className="truncate text-muted-foreground" title={t.content_hash}>
                    {t.content_hash.slice(0, 12)}
                  </span>
                </div>
              ))}
            </CollapsibleContent>
          </Collapsible>
        ))}
      </div>
    </div>
  );
}
