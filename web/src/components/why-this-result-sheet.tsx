import {
  Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger,
} from "@/components/ui/sheet";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { EngineResult } from "@/lib/types";

/**
 * CLAUDE.md: "The single most valuable screen in an internal tool." Shows every one of
 * the engine's steps, how many candidates each removed, and why -- this is the answer
 * to "where did recipe X go" without anyone needing a SQL client.
 */
export function WhyThisResultSheet({ result }: { result: EngineResult }) {
  return (
    <Sheet>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm">Why this result</Button>
      </SheetTrigger>
      <SheetContent side="right" className="w-full sm:max-w-xl overflow-y-auto">
        <SheetHeader>
          <SheetTitle className="font-mono">
            Target: {result.active_target}
          </SheetTitle>
          <p className="text-xs text-muted-foreground">{result.target_reason}</p>
        </SheetHeader>
        <div className="space-y-3 p-4">
          {result.blocked && (
            <div className="rounded border border-destructive p-3 text-sm text-destructive">
              Blocked: {result.block_reason}
            </div>
          )}
          {/* Keyed on step number AND kind, not the number alone: step 4 is recorded twice,
              once as its hard filter and once as the diet-preference ranker, so step numbers
              are no longer unique across the array. A bare key={s.step} lets React duplicate
              or omit one of the two, on the one screen whose job is to account for every
              step. */}
          {result.steps.map((s) => (
            <div key={`${s.step}-${s.kind}`} className="border-b pb-2 font-mono text-xs">
              <div className="flex items-center justify-between">
                <span className="font-semibold">{s.step}. {s.name}</span>
                <Badge variant="outline">{s.kind}</Badge>
              </div>
              <div className="text-muted-foreground">
                {s.candidates_in >= 0 ? s.candidates_in : "?"} in -&gt; {s.candidates_out} out
              </div>
              {s.note && <div className="mt-1 text-muted-foreground">{s.note}</div>}
              {s.excluded && s.excluded.length > 0 && (
                <ul className="mt-1 space-y-0.5">
                  {s.excluded.map((ex) => (
                    <li key={ex.recipe_id} className="text-muted-foreground">
                      {ex.recipe_id} {ex.recipe_name}: {ex.reason}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          ))}
        </div>
      </SheetContent>
    </Sheet>
  );
}
