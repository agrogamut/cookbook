import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { markerControl } from "@/lib/clinical-marker-control";
import type { ClinicalMarker } from "@/lib/types";

const NONE = "__none__"; // Radix Select forbids an empty-string item value

interface ClinicalFlagsFieldsetProps {
  markers: ClinicalMarker[];
  flags: Record<string, string>;
  onSet: (field: string, value: string) => void;
  onClear: (field: string) => void;
}

/**
 * The clinical_rule_master picker, as a form control. Shared by ProfileForm (the engine
 * console's search) and ChildInputForm (the /books generate action) rather than duplicated,
 * so a rule offerable from one screen is offerable from both -- see markerControl's own
 * doc comment for why the two must never drift apart.
 *
 * Two states appear here. A live control (toggle badge or dropdown) sends a value the
 * engine's rule query actually loads: the rule is recorded in the step list with the
 * provider's own reason and specialist text, and it feeds nutrition-target selection and
 * the drafted per-recipe modification notes. A dashed, unclickable marker has nothing this
 * console can usefully send -- either its operator has no case in the engine's switch, or
 * its rules sit in a domain clinicalFilter excludes.
 */
export function ClinicalFlagsFieldset({
  markers, flags, onSet, onClear,
}: ClinicalFlagsFieldsetProps) {
  function toggle(field: string, value: string) {
    if (flags[field] === value) onClear(field);
    else onSet(field, value);
  }

  return (
    <fieldset className="space-y-1.5">
      <legend className="text-xs uppercase text-muted-foreground">Clinical flags</legend>
      <div className="flex flex-col gap-1.5">
        {markers.map((m) => {
          const control = markerControl(m);
          const title = `${m.rule_ids} - ${m.engine_actions}`;

          if (control.kind === "inert") {
            return (
              <div key={m.trigger_field} className="flex items-center gap-2 opacity-50" title={title}>
                <Badge variant="outline" className="border-dashed">{m.trigger_field}</Badge>
                <span className="text-xs text-muted-foreground">{control.note}</span>
              </div>
            );
          }

          if (control.kind === "toggle") {
            const on = flags[m.trigger_field] === control.value.value;
            return (
              <div key={m.trigger_field} className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => toggle(m.trigger_field, control.value.value)}
                  title={title}
                  aria-pressed={on}
                  aria-label={m.trigger_field}
                  className="focus-visible:ring-ring w-fit rounded focus-visible:outline-none focus-visible:ring-2"
                >
                  <Badge variant={on ? "default" : "outline"}>{m.trigger_field}</Badge>
                </button>
                {control.mixedCount > 0 && (
                  <span className="text-xs text-muted-foreground">
                    +{control.mixedCount} recorded value(s) not offered: the engine loads no rule for them
                  </span>
                )}
              </div>
            );
          }

          const current = flags[m.trigger_field] ?? NONE;
          return (
            <div key={m.trigger_field} className="flex items-center gap-2" title={title}>
              <Badge variant={current !== NONE ? "default" : "outline"}>
                {m.trigger_field}
              </Badge>
              <Select
                value={current}
                onValueChange={(v) => (v === NONE ? onClear(m.trigger_field) : onSet(m.trigger_field, v))}
              >
                <SelectTrigger className="h-7 w-44 text-xs" aria-label={m.trigger_field}>
                  <SelectValue placeholder="not set" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>not set</SelectItem>
                  {control.values.map((v) => (
                    <SelectItem key={v.value} value={v.value}>{v.value}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {control.mixedCount > 0 && (
                <span className="text-xs text-muted-foreground">
                  +{control.mixedCount} recorded value(s) not offered: the engine loads no rule for them
                </span>
              )}
            </div>
          );
        })}
      </div>
    </fieldset>
  );
}
