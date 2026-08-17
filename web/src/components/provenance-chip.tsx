import { Badge } from "@/components/ui/badge";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { cn } from "@/lib/utils";

interface ProvenanceChipProps {
  /** "ifct" | "provider" for ingredient nutrition; "derived" for a computed score;
   *  an external source_key (e.g. "anupam007-indian-recipes") for a suggested method. */
  source: string;
  /** 0-1. Omit for values with no match confidence (e.g. a straight provider column). */
  confidence?: number;
  /** Free text explaining exactly what this value is and how it was produced -- this is
   *  the disclosure text CLAUDE.md requires next to every derived value, never a footnote. */
  explanation: string;
}

/**
 * The one component this app is built around: every derived, corrected or externally
 * sourced value on screen renders one of these beside it. A bare number with no
 * ProvenanceChip next to it is a bug -- see CLAUDE.md, "Provenance is a column, never a
 * footnote."
 */
export function ProvenanceChip({ source, confidence, explanation }: ProvenanceChipProps) {
  const variant = source === "ifct" ? "verified" : source === "provider" ? "unverified" : "derived";

  return (
    <HoverCard openDelay={150}>
      <HoverCardTrigger asChild>
        <Badge
          variant="outline"
          className={cn(
            "font-mono text-[10px] uppercase tracking-wide cursor-help",
            variant === "verified" && "border-[var(--color-verified)] text-[var(--color-verified)]",
            variant === "unverified" && "border-[var(--color-unverified)] text-[var(--color-unverified)]",
          )}
        >
          {source}
          {confidence !== undefined && ` · ${(confidence * 100).toFixed(1)}%`}
        </Badge>
      </HoverCardTrigger>
      <HoverCardContent className="w-80 font-mono text-xs">
        {explanation}
      </HoverCardContent>
    </HoverCard>
  );
}
