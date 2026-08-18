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

  function toggleFlag(field: string) {
    setClinicalFlags((prev) => {
      const next = { ...prev };
      if (field in next) delete next[field];
      else next[field] = "Yes";
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

      <fieldset className="space-y-1">
        <legend className={label}>Clinical flags</legend>
        <div className="flex flex-wrap gap-1">
          {markerOptions.map((m) => {
            const on = m.trigger_field in clinicalFlags;
            return (
              <button
                key={m.trigger_field}
                type="button"
                onClick={() => toggleFlag(m.trigger_field)}
                title={`${m.rule_ids} - ${m.engine_actions}`}
                className="focus-visible:ring-ring rounded focus-visible:outline-none focus-visible:ring-2"
              >
                <Badge variant={on ? "default" : "outline"} className={m.escalates ? "border-destructive" : ""}>
                  {m.trigger_field}
                  {m.escalates && " - holds"}
                </Badge>
              </button>
            );
          })}
        </div>
        <p className="text-xs text-muted-foreground">
          Flags outlined in red hold generation for specialist review rather than filtering
          it. No recipe list is returned for those.
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
