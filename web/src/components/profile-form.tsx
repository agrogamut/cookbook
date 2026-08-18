"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { SuspectedAllergenFieldset } from "./suspected-allergen-fieldset";
import { getAllergens, getClinicalMarkers, getEnums, getRegions, getCuisines } from "@/lib/api";
import type {
  Allergen, ChildProfile, ClinicalMarker, ClinicalMarkerValue, Cuisine, Region, ReferenceEnums,
} from "@/lib/types";

interface ProfileFormProps {
  onSubmit: (profile: ChildProfile) => void;
  loading: boolean;
}

// The operator-facing marker keys the engine's selectTarget understands. These are engine
// concepts (which nutrition target to activate), not clinical_rule_master trigger fields,
// which is why they are a separate control from clinical flags below.
const CLINICAL_MARKERS = [
  { value: "growth_faltering", label: "Growth faltering" },
  { value: "thinness", label: "Thinness (BMI-for-age)" },
  { value: "overweight_under5", label: "Overweight risk under 5" },
  { value: "overweight_5to19", label: "Overweight / obesity 5-19" },
  { value: "iron_deficiency", label: "Iron-deficiency risk" },
  { value: "calcium_bone", label: "Calcium / bone health" },
  { value: "high_protein", label: "High-protein emphasis" },
  { value: "vegetarian", label: "Vegetarian adequacy" },
  { value: "vegan", label: "Vegan adequacy" },
  { value: "picky_eating", label: "Picky eating / low variety" },
  { value: "illness_recovery", label: "Illness / recovery" },
];

const NONE = "__none__"; // Radix Select forbids an empty-string item value

// A clinical flag must never be sendable as a value that cannot fire the rule it is named
// after -- a badge reading "holds" next to a control that cannot actually trigger the hold
// is a false assurance. markerControl is a whitelist that mirrors the engine's own
// triggerFires switch (internal/engine/clinical.go) rather than inverting it: only the two
// operators that switch actually implements a live case for produce a control here.
// Everything else -- contains, less_than, incompatible_with, and any operator the provider
// invents later -- renders inert, because nothing on this console could make it fire.
//
// Within a supported operator, only VALUES the engine's own query would load are ever
// offered. A value with loadable=false (e.g. Coeliac_Status's Suspected_Not_Confirmed) is
// real data the provider recorded, but clinicalFilter's WHERE clause never reads the rule
// behind it, so offering it as a live control would promise a hold that cannot happen.
//
// trigger_operator is singular and pure per marker (asserted by
// TestReferenceClinicalMarkersCoversEveryTriggerField), so this is a function of the
// marker as a whole, not a per-value branch -- except that the value LIST offered is
// filtered to loadable entries.
type MarkerControl =
  | { kind: "toggle"; value: ClinicalMarkerValue; mixedCount: number }
  | { kind: "select"; values: ClinicalMarkerValue[]; mixedCount: number }
  | { kind: "inert"; note: string };

function markerControl(m: ClinicalMarker): MarkerControl {
  const op = m.trigger_operator;
  if (op !== "equals" && op !== "in_list") {
    const note = op === "contains"
      ? "matches a substring, not an exact value -- firing this rule trips the engine's " +
        "unclassified-rule error by design. A confirmed allergen belongs in Declared " +
        "allergens above, not here."
      : `trigger_operator "${op}" has no case in the engine's own switch -- nothing here could fire it, so no control is offered.`;
    return { kind: "inert", note };
  }
  // Only escalating values are offerable, and `loadable` alone is not enough to qualify.
  // For a rule the engine loads and that fires there are exactly two outcomes, never three:
  // it escalates (blocked = true, with the provider's specialist named), or the engine
  // refuses the whole profile with an unclassified-rule error. There is no "filters
  // something" outcome, because no recipe-side column expresses these conditions. So a value
  // with loadable = true and escalates = false always errors, and offering it as a live
  // control would promise a screen that cannot happen -- the same false affordance this
  // control has already been fixed for twice.
  //
  // Today exactly one such rule exists (CR-ALL-001) and it is unreachable only because its
  // operator is `contains`, caught by the whitelist above. That is a coincidence of one
  // column value, not a guarantee: an `equals` rule at 'Clinical approval' with
  // hard_exclude_yn = 'Y' outside the ten escalation domains would reinstate the bug.
  // TestUnclassifiedMarkerValuesArePinned pins the known set so a new one breaks the build.
  const offerable = m.values.filter((v) => v.loadable && v.escalates);
  const refusing = m.values.filter((v) => v.loadable && !v.escalates);
  const mixedCount = m.values.length - offerable.length;
  if (offerable.length === 0) {
    return {
      kind: "inert",
      note: refusing.length > 0
        ? "the engine loads a rule for this marker but cannot classify it, so setting it " +
          "would make generation refuse rather than filter. Nothing is offered here."
        : "the engine has no rule it can act on for this marker -- every value the provider " +
          "recorded here sits below the tier clinicalFilter loads.",
    };
  }
  if (offerable.length === 1) {
    return { kind: "toggle", value: offerable[0], mixedCount };
  }
  return { kind: "select", values: offerable, mixedCount };
}

export function ProfileForm({ onSubmit, loading }: ProfileFormProps) {
  const [ageMonths, setAgeMonths] = useState<number | "">("");
  const [dietType, setDietType] = useState("");
  const [vegan, setVegan] = useState(false);
  const [allergens, setAllergens] = useState<string[]>([]);
  const [suspectedAllergens, setSuspectedAllergens] = useState<string[]>([]);
  const [clinicalMarker, setClinicalMarker] = useState("");
  const [clinicalFlags, setClinicalFlags] = useState<Record<string, string>>({});
  const [mealType, setMealType] = useState("");
  const [budgetBand, setBudgetBand] = useState("");
  const [regionCulture, setRegionCulture] = useState("");
  const [cuisineCode, setCuisineCode] = useState("");
  const [maxPrep, setMaxPrep] = useState("");
  const [maxCook, setMaxCook] = useState("");
  const [limit, setLimit] = useState("");

  const [allergenOptions, setAllergenOptions] = useState<Allergen[]>([]);
  const [markerOptions, setMarkerOptions] = useState<ClinicalMarker[]>([]);
  const [enums, setEnums] = useState<ReferenceEnums>({});
  const [regions, setRegions] = useState<Region[]>([]);
  const [cuisines, setCuisines] = useState<Cuisine[]>([]);
  const [refError, setRefError] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([getAllergens(), getClinicalMarkers(), getEnums(), getRegions(), getCuisines()])
      .then(([a, m, e, r, c]) => {
        setAllergenOptions(a);
        setMarkerOptions(m);
        setEnums(e);
        setRegions(r);
        setCuisines(c);
      })
      .catch((err) => setRefError(err instanceof Error ? err.message : String(err)));
  }, []);

  function toggleAllergen(group: string) {
    setAllergens((prev) =>
      prev.includes(group) ? prev.filter((g) => g !== group) : [...prev, group]);
  }

  function toggleSuspectedAllergen(group: string) {
    setSuspectedAllergens((prev) =>
      prev.includes(group) ? prev.filter((g) => g !== group) : [...prev, group]);
  }

  // Declaring a group confirmed supersedes any suspicion of it. The fieldset disables a
  // group that is already declared, but an operator can suspect a group first and confirm
  // it afterwards, which leaves both set. Sending both would ask the engine to demote
  // recipes its hard filter has already removed -- harmless, but it makes the step note
  // claim work that did not happen. Resolved at submit rather than in the toggle, so
  // unticking the confirmed allergen restores the suspicion instead of silently losing it.
  const effectiveSuspected = suspectedAllergens.filter((g) => !allergens.includes(g));

  // clinical_flags is Record<field, value> -- one value per field, matching what the
  // engine's triggerFires actually compares. Every setter below sources its value from a
  // loadable ClinicalMarkerValue, never a literal, so a set field is guaranteed to be a
  // value the rule can actually match.
  function setFlag(field: string, value: string) {
    setClinicalFlags((prev) => ({ ...prev, [field]: value }));
  }

  function clearFlag(field: string) {
    setClinicalFlags((prev) => {
      if (!(field in prev)) return prev;
      const next = { ...prev };
      delete next[field];
      return next;
    });
  }

  function toggleFlag(field: string, value: string) {
    setClinicalFlags((prev) => {
      const next = { ...prev };
      if (next[field] === value) delete next[field];
      else next[field] = value;
      return next;
    });
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (ageMonths === "") return;
    onSubmit({
      age_months: Number(ageMonths),
      diet_type: dietType ? (dietType as ChildProfile["diet_type"]) : undefined,
      vegan: vegan || undefined,
      allergens: allergens.length ? allergens : undefined,
      suspected_allergens: effectiveSuspected.length ? effectiveSuspected : undefined,
      clinical_marker: clinicalMarker || undefined,
      clinical_flags: Object.keys(clinicalFlags).length ? clinicalFlags : undefined,
      meal_type: mealType || undefined,
      budget_band: budgetBand ? (budgetBand as ChildProfile["budget_band"]) : undefined,
      region_culture: regionCulture || undefined,
      cuisine_code: cuisineCode || undefined,
      max_prep_time_min: maxPrep ? Number(maxPrep) : undefined,
      max_cook_time_min: maxCook ? Number(maxCook) : undefined,
      limit: limit ? Number(limit) : undefined,
    });
  }

  const enumValues = (key: string) => enums[key] ?? [];
  const label = "text-xs uppercase text-muted-foreground";

  return (
    <form onSubmit={handleSubmit} className="space-y-4 font-mono text-sm">
      {refError && (
        <p className="text-xs text-destructive">
          Reference vocabularies failed to load ({refError}). Controls below are empty
          rather than guessed; confirm the API server is running.
        </p>
      )}

      <div className="space-y-1">
        <label htmlFor="age" className={label}>Age (months) *</label>
        <Input
          id="age" type="number" min={0} max={216} required
          value={ageMonths}
          onChange={(e) => setAgeMonths(e.target.value === "" ? "" : Number(e.target.value))}
        />
      </div>

      <div className="space-y-1">
        <span className={label}>Diet type</span>
        <Select value={dietType || NONE} onValueChange={(v) => setDietType(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference</SelectItem>
            {enumValues("diet_type").map((v) => (
              <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <label className="flex items-start gap-2 text-xs">
        <input type="checkbox" checked={vegan} onChange={(e) => setVegan(e.target.checked)} className="mt-0.5" />
        <span>Vegan - additional to diet type; excludes dairy, fish and animal-protein food groups</span>
      </label>

      <fieldset className="space-y-1">
        <legend className={label}>Declared allergens</legend>
        <div className="flex flex-wrap gap-1">
          {allergenOptions.map((a) => {
            const on = allergens.includes(a.allergen_group);
            return (
              <button
                key={a.allergen_group}
                type="button"
                onClick={() => toggleAllergen(a.allergen_group)}
                title={a.note}
                aria-pressed={on}
                className="focus-visible:ring-ring rounded focus-visible:outline-none focus-visible:ring-2"
              >
                <Badge
                  variant={on ? "default" : "outline"}
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
          Dashed groups have no tag anywhere in the corpus. They stay selectable so the
          allergy can be recorded, and every result is labelled unscreened for them.
        </p>
      </fieldset>

      <SuspectedAllergenFieldset
        options={allergenOptions}
        selected={suspectedAllergens}
        declared={allergens}
        onToggle={toggleSuspectedAllergen}
      />

      <div className="space-y-1">
        <span className={label}>Nutrition target marker</span>
        <Select value={clinicalMarker || NONE} onValueChange={(v) => setClinicalMarker(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="None - age-default ranking" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>None - age-default ranking</SelectItem>
            {CLINICAL_MARKERS.map((m) => (
              <SelectItem key={m.value} value={m.value}>{m.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <fieldset className="space-y-1.5">
        <legend className={label}>Clinical flags</legend>
        <div className="flex flex-col gap-1.5">
          {markerOptions.map((m) => {
            const control = markerControl(m);
            // "holds" is a fact about a VALUE, not the field (finding 1). When something
            // is selected, show it only if that selected value escalates. When nothing is
            // selected, show it if any of the marker's values escalate -- a true statement
            // about the marker that primes the operator before they pick anything.
            const anyEscalates = m.values.some((v) => v.escalates);
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
              const on = clinicalFlags[m.trigger_field] === control.value.value;
              const holds = on ? control.value.escalates : anyEscalates;
              return (
                <div key={m.trigger_field} className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => toggleFlag(m.trigger_field, control.value.value)}
                    title={title}
                    aria-pressed={on}
                    aria-label={m.trigger_field}
                    className="focus-visible:ring-ring w-fit rounded focus-visible:outline-none focus-visible:ring-2"
                  >
                    <Badge variant={on ? "default" : "outline"} className={holds ? "border-destructive" : ""}>
                      {m.trigger_field}
                      {holds && " - holds"}
                    </Badge>
                  </button>
                  {control.mixedCount > 0 && (
                    <span className="text-xs text-muted-foreground">
                      +{control.mixedCount} recorded value(s) not offered: no loadable rule, or a loaded rule the engine cannot classify
                    </span>
                  )}
                </div>
              );
            }

            // select: several offerable values. Only values that are both loadable AND
            // escalating are listed. Two kinds of value are left off: one the engine's query
            // never loads, and one it loads but cannot classify -- the latter would make
            // generation refuse rather than filter, so offering it would promise a screen
            // that cannot happen.
            const current = clinicalFlags[m.trigger_field] ?? NONE;
            const selected = control.values.find((v) => v.value === current);
            const holds = selected ? selected.escalates : anyEscalates;
            return (
              <div key={m.trigger_field} className="flex items-center gap-2" title={title}>
                <Badge variant={current !== NONE ? "default" : "outline"} className={holds ? "border-destructive" : ""}>
                  {m.trigger_field}
                  {holds && " - holds"}
                </Badge>
                <Select
                  value={current}
                  onValueChange={(v) => (v === NONE ? clearFlag(m.trigger_field) : setFlag(m.trigger_field, v))}
                >
                  <SelectTrigger className="h-7 w-44 text-xs" aria-label={m.trigger_field}>
                    <SelectValue placeholder="not set" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NONE}>not set</SelectItem>
                    {control.values.map((v) => (
                      <SelectItem key={v.value} value={v.value}>
                        {v.value}
                        {v.escalates && " (holds)"}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {control.mixedCount > 0 && (
                  <span className="text-xs text-muted-foreground">
                    +{control.mixedCount} recorded value(s) not offered: no loadable rule, or a loaded rule the engine cannot classify
                  </span>
                )}
              </div>
            );
          })}
        </div>
        <p className="text-xs text-muted-foreground">
          Three states appear here. A live control (toggle badge or dropdown) sends a value
          the engine's own rule query actually loads; outlined in red when the selected (or,
          if nothing is selected, any offerable) value holds generation for specialist
          review rather than filtering it -- no recipe list is returned for those. A dashed,
          unclickable marker has nothing this console can usefully send: either its operator
          has no case in the engine's switch (Age_Months, Texture_Skill, and the one
          substring-matching allergy flag, which belongs in Declared allergens instead), or
          every value the provider recorded for it sits below the tier clinicalFilter loads,
          or the engine loads a rule for it but cannot classify it, in which case setting it
          would make generation refuse rather than filter. A dropdown may still show a "+N
          recorded, not offered" note, which covers both of the last two cases: the value is
          left off the list rather than hidden entirely, because the provider did record it.
        </p>
      </fieldset>

      <div className="space-y-1">
        <span className={label}>Region</span>
        <Select value={regionCulture || NONE} onValueChange={(v) => setRegionCulture(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference - West Bengal first" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference - West Bengal first</SelectItem>
            {regions.map((r) => (
              <SelectItem key={r.region_culture} value={r.region_culture}>
                {r.region_culture} (tier {r.focus_tier})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1">
        <span className={label}>Cuisine cluster</span>
        <Select value={cuisineCode || NONE} onValueChange={(v) => setCuisineCode(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference</SelectItem>
            {cuisines.map((c) => (
              <SelectItem key={c.culture_code} value={c.culture_code}>
                {c.cuisine_cluster} - {c.region_culture} ({c.recipe_count})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1">
        <span className={label}>Meal type</span>
        <Select value={mealType || NONE} onValueChange={(v) => setMealType(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference</SelectItem>
            {enumValues("meal_type").map((v) => (
              <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1">
        <span className={label}>Budget band</span>
        <Select value={budgetBand || NONE} onValueChange={(v) => setBudgetBand(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>No preference</SelectItem>
            {enumValues("budget_band").map((v) => (
              <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <div className="space-y-1">
          <span className={label}>Max prep (min)</span>
          <Select value={maxPrep || NONE} onValueChange={(v) => setMaxPrep(v === NONE ? "" : v)}>
            <SelectTrigger><SelectValue placeholder="Any" /></SelectTrigger>
            <SelectContent>
              <SelectItem value={NONE}>Any</SelectItem>
              {enumValues("prep_time_min").map((v) => (
                <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1">
          <span className={label}>Max cook (min)</span>
          <Select value={maxCook || NONE} onValueChange={(v) => setMaxCook(v === NONE ? "" : v)}>
            <SelectTrigger><SelectValue placeholder="Any" /></SelectTrigger>
            <SelectContent>
              <SelectItem value={NONE}>Any</SelectItem>
              {enumValues("cook_time_min").map((v) => (
                <SelectItem key={v.value} value={v.value}>{v.value} ({v.count})</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="space-y-1">
        <label htmlFor="limit" className={label}>Result limit</label>
        <Input
          id="limit" type="number" min={1} max={200} placeholder="25 (meal_category_target)"
          value={limit} onChange={(e) => setLimit(e.target.value)}
        />
      </div>

      <Button type="submit" disabled={ageMonths === "" || loading} className="w-full">
        {loading ? "Running engine..." : "Search"}
      </Button>
    </form>
  );
}
