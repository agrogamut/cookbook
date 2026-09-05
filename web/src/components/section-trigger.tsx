import { AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";

/** An accordion heading that says what its section holds while the section is shut.
 *
 *  Collapsing is only safe if closed does not read as empty. Both console forms run to
 *  dozens of controls, so most sections are collapsed most of the time; the summary is what
 *  keeps a filled section legible without opening it -- "Allergens, 2 declared" rather than a
 *  heading that looks identical whether the operator entered five allergies or none.
 *
 *  An absent summary is deliberate rather than a zero. A section holding nothing shows no
 *  badge at all, because "0 declared" reads as a recorded value and a blank field is not one.
 *
 *  Shared by the engine console's profile form and the book generator's child form so a
 *  section header means the same thing on both screens. */
export function SectionTrigger({ label, summary }: { label: string; summary?: string }) {
  return (
    <AccordionTrigger className="py-2 hover:no-underline">
      <span className="flex flex-wrap items-baseline gap-2">
        <span className="font-mono text-xs uppercase tracking-wide text-muted-foreground">
          {label}
        </span>
        {summary && (
          <Badge variant="secondary" className="font-mono text-[10px] font-normal">
            {summary}
          </Badge>
        )}
      </span>
    </AccordionTrigger>
  );
}

/** Joins the parts of a section summary, dropping every empty one. Returns undefined rather
 *  than an empty string so a section with nothing in it renders no badge. */
export function summarise(...parts: (string | false | null | undefined)[]): string | undefined {
  const kept = parts.filter((p): p is string => typeof p === "string" && p !== "");
  return kept.length > 0 ? kept.join(" · ") : undefined;
}
