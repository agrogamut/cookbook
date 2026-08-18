import { Badge } from "@/components/ui/badge";
import type { Allergen } from "@/lib/types";

interface SuspectedAllergenFieldsetProps {
  options: Allergen[];
  selected: string[];
  declared: string[];
  onToggle: (group: string) => void;
}

/**
 * Step 2's ranking half, as an operator control.
 *
 * A declared allergen is a hard filter and removes recipes. A suspected one only ranks
 * them down, by 0.15, and removes nothing -- AS-002 marks a suspected allergy
 * hard_block = N. Rendering the two the same way would let an operator read a demotion as
 * a protection, so this is a separate fieldset with its own wording rather than a second
 * mode of the declared picker.
 *
 * A group already declared confirmed is shown disabled rather than hidden: hiding it would
 * make the operator wonder whether the suspicion registered, and the honest answer is that
 * the confirmed filter already covers it.
 */
export function SuspectedAllergenFieldset({
  options, selected, declared, onToggle,
}: SuspectedAllergenFieldsetProps) {
  const anyDeclared = declared.some((d) => options.some((o) => o.allergen_group === d));

  return (
    <fieldset className="space-y-1">
      <legend className="text-xs uppercase text-muted-foreground">
        Suspected allergens
      </legend>
      <div className="flex flex-wrap gap-1">
        {options.map((a) => {
          const isDeclared = declared.includes(a.allergen_group);
          const on = selected.includes(a.allergen_group);
          return (
            <button
              key={a.allergen_group}
              type="button"
              disabled={isDeclared}
              onClick={() => { if (!isDeclared) onToggle(a.allergen_group); }}
              title={isDeclared
                ? "Already declared as a confirmed allergen; the hard filter covers it"
                : a.note}
              aria-pressed={on}
              className="focus-visible:ring-ring rounded focus-visible:outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Badge
                variant={on ? "secondary" : "outline"}
                className={a.screens ? "" : "border-dashed opacity-70"}
              >
                {a.allergen_group}
                {!a.screens && " - not screened"}
              </Badge>
            </button>
          );
        })}
      </div>
      <p className="text-xs text-muted-foreground">
        Suspected allergens are ranked down and never excluded - the provider&apos;s AS-002
        marks a suspected allergy <span className="font-mono">hard_block = N</span>. For a
        confirmed allergy use Declared allergens above, which is a hard filter.
      </p>
      {anyDeclared && (
        <p className="text-xs text-muted-foreground">
          Greyed groups are already declared confirmed above.
        </p>
      )}
    </fieldset>
  );
}
