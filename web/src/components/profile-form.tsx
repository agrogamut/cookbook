"use client";

import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { getAllergens, getClinicalMarkers, getEnums, getRegions, getCuisines } from "@/lib/api";
import type {
  Allergen, ChildProfile, ClinicalMarker, Cuisine, Region, ReferenceEnums,
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
// is a false assurance. clinical_rule_master.trigger_operator determines what a correct
// control looks like; every trigger_field carries exactly one operator (asserted by
// TestReferenceClinicalMarkersCoversEveryTriggerField), so this is a pure function of the
// marker, not a per-row branch.
type MarkerControl =
  | { kind: "toggle"; value: string }
  | { kind: "select"; values: string[] }
  | { kind: "text"; matches: string }
  | { kind: "unsupported" };

function markerControl(m: ClinicalMarker): MarkerControl {
  const op = m.trigger_operators;
  // Age_Months (less_than) and Texture_Skill (incompatible_with) are enforced
  // structurally by step 1 and excluded from clinicalFilter's own query by domain -- a
  // control for either would be inert, so neither is offered as a working control.
  if (op === "less_than" || op === "incompatible_with") return { kind: "unsupported" };
  if (op === "contains") return { kind: "text", matches: m.trigger_values };
  const values = op === "in_list"
    ? Array.from(new Set(
        m.trigger_values.split("|").flatMap((v) => v.split(";").map((s) => s.trim()).filter(Boolean)),
      ))
    : m.trigger_values.split("|").filter(Boolean);
  if (values.length <= 1) return { kind: "toggle", value: values[0] ?? "Yes" };
  return { kind: "select", values };
}

export function ProfileForm({ onSubmit, loading }: ProfileFormProps) {
  const [ageMonths, setAgeMonths] = useState<number | "">("");
  const [dietType, setDietType] = useState("");
  const [vegan, setVegan] = useState(false);
  const [allergens, setAllergens] = useState<string[]>([]);
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

  // clinical_flags is Record<field, value> -- one value per field, matching what the
  // engine's triggerFires actually compares. Every setter below sources its value from
  // the marker's own trigger_values, never a literal, so a set field is guaranteed to be
  // a value the rule can match.
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
            const escalateClass = m.escalates ? "border-destructive" : "";
            const title = `${m.rule_ids} - ${m.engine_actions}`;

            if (control.kind === "unsupported") {
              return (
                <div key={m.trigger_field} className="flex items-center gap-2 opacity-50" title={title}>
                  <Badge variant="outline" className="border-dashed">{m.trigger_field}</Badge>
                  <span className="text-xs text-muted-foreground">
                    enforced structurally at step 1 -- no control here
                  </span>
                </div>
              );
            }

            if (control.kind === "toggle") {
              const on = clinicalFlags[m.trigger_field] === control.value;
              return (
                <button
                  key={m.trigger_field}
                  type="button"
                  onClick={() => toggleFlag(m.trigger_field, control.value)}
                  title={title}
                  className="focus-visible:ring-ring w-fit rounded focus-visible:outline-none focus-visible:ring-2"
                >
                  <Badge variant={on ? "default" : "outline"} className={escalateClass}>
                    {m.trigger_field}
                    {m.escalates && " - holds"}
                  </Badge>
                </button>
              );
            }

            if (control.kind === "select") {
              const current = clinicalFlags[m.trigger_field] ?? NONE;
              return (
                <div key={m.trigger_field} className="flex items-center gap-2" title={title}>
                  <Badge variant={current !== NONE ? "default" : "outline"} className={escalateClass}>
                    {m.trigger_field}
                    {m.escalates && " - holds"}
                  </Badge>
                  <Select
                    value={current}
                    onValueChange={(v) => (v === NONE ? clearFlag(m.trigger_field) : setFlag(m.trigger_field, v))}
                  >
                    <SelectTrigger className="h-7 w-44 text-xs"><SelectValue placeholder="not set" /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value={NONE}>not set</SelectItem>
                      {control.values.map((v) => (
                        <SelectItem key={v} value={v}>{v}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              );
            }

            // text: the one case where free entry is correct, since the rule matches a
            // substring of whatever is typed rather than an exact vocabulary value.
            const current = clinicalFlags[m.trigger_field] ?? "";
            return (
              <div key={m.trigger_field} className="flex items-center gap-2" title={title}>
                <Badge variant={current ? "default" : "outline"} className={escalateClass}>
                  {m.trigger_field}
                  {m.escalates && " - holds"}
                </Badge>
                <Input
                  className="h-7 w-52 text-xs"
                  placeholder={`matches text containing "${control.matches}"`}
                  value={current}
                  onChange={(e) => {
                    const v = e.target.value;
                    if (v.trim() === "") clearFlag(m.trigger_field);
                    else setFlag(m.trigger_field, v);
                  }}
                />
              </div>
            );
          })}
        </div>
        <p className="text-xs text-muted-foreground">
          Flags outlined in red hold generation for specialist review rather than filtering
          it; no recipe list is returned for those. Most flags are a toggle badge because
          the rule fires on the literal value "Yes". A few render as a dropdown instead --
          Diabetes_Type, Coeliac_Status, BMI_for_Age_Classification and others -- because
          their rule only fires on a specific value ("Type 1", "Confirmed",
          "Overweight" ...) that is never "Yes"; picking the wrong shape of control there
          would let an operator believe a hold is active when it is not. One field is free
          text because its rule matches a substring rather than an exact value. Two fields
          render disabled: step 1 already enforces Age_Months and Texture_Skill
          structurally, so a control for either here would have no effect.
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
