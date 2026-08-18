import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

/**
 * Renders beside every result set when a declared allergen screened nothing.
 *
 * This is not a nicety. Four allergen groups (Tree nuts, Crustacean/Mollusc, Mustard,
 * Sulphites) have no tag anywhere in the corpus, so declaring one is accepted and removes
 * zero recipes. A results page that looks identical to a screened one implies a protection
 * that does not exist, which is the failure mode CLAUDE.md's hard rule exists to prevent.
 * See gap GAP-017.
 */
export function UnscreenedAllergenAlert({ groups }: { groups: string[] }) {
  if (groups.length === 0) return null;
  return (
    <Alert variant="destructive" className="mb-4">
      <AlertTitle className="font-mono text-xs uppercase">
        Not screened - no corpus coverage
      </AlertTitle>
      <AlertDescription className="space-y-1 text-sm">
        <p>
          <span className="font-mono">{groups.join(", ")}</span>{" "}
          {groups.length === 1 ? "has" : "have"} no matching tag on any recipe or
          ingredient. {groups.length === 1 ? "It" : "They"} excluded zero recipes because
          nothing carries the tag, not because the filter passed.
        </p>
        <p className="text-xs">
          Every recipe below is unscreened for{" "}
          {groups.length === 1 ? "this allergen" : "these allergens"}. Check ingredients
          directly before serving.
        </p>
      </AlertDescription>
    </Alert>
  );
}
