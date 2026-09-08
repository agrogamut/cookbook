import type { ClinicalMarker, ClinicalMarkerValue } from "@/lib/types";

// A clinical flag must never be sendable as a value that cannot fire the rule it is named
// after. markerControl is a whitelist that mirrors the engine's own triggerFires switch
// (internal/engine/clinical.go) rather than inverting it: only the two operators that switch
// actually implements a live case for produce a control here. Everything else -- contains,
// less_than, incompatible_with, and any operator the provider invents later -- renders
// inert, because nothing on this console could make it fire.
//
// Within a supported operator, only VALUES the engine's own query loads are offered. That is
// now nearly all of them: clinicalFilter loads every domain but Age/Feeding and Data Quality.
//
// trigger_operator is singular and pure per marker (asserted by
// TestReferenceClinicalMarkersCoversEveryTriggerField), so this is a function of the
// marker as a whole, not a per-value branch -- except that the value LIST offered is
// filtered to loadable entries.
//
// Shared by ProfileForm (the engine console's search) and ChildInputForm (the /books
// generate action): both send a clinical flag the same way, and a control that could fire
// a rule from one screen but not the other would be a silent trap.
export type MarkerControl =
  | { kind: "toggle"; value: ClinicalMarkerValue; mixedCount: number }
  | { kind: "select"; values: ClinicalMarkerValue[]; mixedCount: number }
  | { kind: "inert"; note: string };

export function markerControl(m: ClinicalMarker): MarkerControl {
  const op = m.trigger_operator;
  if (op !== "equals" && op !== "in_list") {
    const note = op === "contains"
      ? "matches a substring rather than an exact value, so this console has no control " +
        "shape for it. A confirmed allergen belongs in Declared allergens above, not here."
      : `trigger_operator "${op}" has no case in the engine's own switch -- nothing here could fire it, so no control is offered.`;
    return { kind: "inert", note };
  }
  const offerable = m.values.filter((v) => v.loadable);
  const mixedCount = m.values.length - offerable.length;
  if (offerable.length === 0) {
    return {
      kind: "inert",
      note: "the engine loads no rule for this marker -- it sits in Age/Feeding, which " +
        "recipe_master's own age bounds already enforce, or in Data Quality, which " +
        "describes the dataset rather than the child.",
    };
  }
  if (offerable.length === 1) {
    return { kind: "toggle", value: offerable[0], mixedCount };
  }
  return { kind: "select", values: offerable, mixedCount };
}
