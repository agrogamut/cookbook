"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import type { ChildProfile } from "@/lib/types";

interface ProfileFormProps {
  onSubmit: (profile: ChildProfile) => void;
  loading: boolean;
}

const DIET_TYPES = ["Vegetarian", "Non-vegetarian", "Eggetarian"] as const;
const MEAL_TYPES = ["Breakfast", "Mid-morning", "Lunch", "Snack", "Dinner", "School Tiffin", "Recovery Meal"];
const BUDGET_BANDS = ["Low", "Moderate", "Premium"] as const;
const CLINICAL_MARKERS = [
  { value: "growth_faltering", label: "Growth faltering" },
  { value: "thinness", label: "Thinness (BMI-for-age)" },
  { value: "iron_deficiency", label: "Iron-deficiency risk" },
  { value: "calcium_bone", label: "Calcium / bone health" },
  { value: "high_protein", label: "High-protein emphasis" },
  { value: "picky_eating", label: "Picky eating / low variety" },
  { value: "illness_recovery", label: "Illness / recovery" },
];

/**
 * Age-appropriate and safety fields (age, allergens) are always visible and required
 * where the engine requires them. Every other field defaults to unset, matching
 * models.ChildProfile's zero value: an operator who submits with nothing but age gets
 * the routine NT00 ranking, not a form that silently narrowed the query.
 */
export function ProfileForm({ onSubmit, loading }: ProfileFormProps) {
  const [ageMonths, setAgeMonths] = useState<number | "">("");
  const [dietType, setDietType] = useState<string>("");
  const [vegan, setVegan] = useState(false);
  const [allergensRaw, setAllergensRaw] = useState("");
  const [clinicalMarker, setClinicalMarker] = useState<string>("");
  const [mealType, setMealType] = useState<string>("");
  const [budgetBand, setBudgetBand] = useState<string>("");

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (ageMonths === "") return;
    const allergens = allergensRaw.split(",").map((a) => a.trim()).filter(Boolean);
    onSubmit({
      age_months: Number(ageMonths),
      diet_type: dietType ? (dietType as ChildProfile["diet_type"]) : undefined,
      vegan: vegan || undefined,
      allergens: allergens.length ? allergens : undefined,
      clinical_marker: clinicalMarker || undefined,
      meal_type: mealType || undefined,
      budget_band: budgetBand ? (budgetBand as ChildProfile["budget_band"]) : undefined,
    });
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4 font-mono text-sm">
      <div className="space-y-1">
        <label htmlFor="age" className="text-xs uppercase text-muted-foreground">Age (months) *</label>
        <Input
          id="age" type="number" min={0} max={216} required
          value={ageMonths}
          onChange={(e) => setAgeMonths(e.target.value === "" ? "" : Number(e.target.value))}
        />
      </div>

      <div className="space-y-1">
        <label className="text-xs uppercase text-muted-foreground">Diet type</label>
        <Select value={dietType} onValueChange={setDietType}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            {DIET_TYPES.map((d) => <SelectItem key={d} value={d}>{d}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <label className="flex items-center gap-2 text-xs">
        <input type="checkbox" checked={vegan} onChange={(e) => setVegan(e.target.checked)} />
        Vegan (additional to Vegetarian: excludes dairy, fish, egg/animal-protein groups)
      </label>

      <div className="space-y-1">
        <label htmlFor="allergens" className="text-xs uppercase text-muted-foreground">
          Declared allergens (comma-separated, e.g. Peanut, Milk)
        </label>
        <Input id="allergens" value={allergensRaw} onChange={(e) => setAllergensRaw(e.target.value)} />
      </div>

      <div className="space-y-1">
        <label className="text-xs uppercase text-muted-foreground">Clinical marker</label>
        <Select value={clinicalMarker} onValueChange={setClinicalMarker}>
          <SelectTrigger><SelectValue placeholder="None -- age-default ranking" /></SelectTrigger>
          <SelectContent>
            {CLINICAL_MARKERS.map((m) => <SelectItem key={m.value} value={m.value}>{m.label}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1">
        <label className="text-xs uppercase text-muted-foreground">Meal type</label>
        <Select value={mealType} onValueChange={setMealType}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            {MEAL_TYPES.map((m) => <SelectItem key={m} value={m}>{m}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1">
        <label className="text-xs uppercase text-muted-foreground">Budget band</label>
        <Select value={budgetBand} onValueChange={setBudgetBand}>
          <SelectTrigger><SelectValue placeholder="No preference" /></SelectTrigger>
          <SelectContent>
            {BUDGET_BANDS.map((b) => <SelectItem key={b} value={b}>{b}</SelectItem>)}
          </SelectContent>
        </Select>
      </div>

      <Button type="submit" disabled={ageMonths === "" || loading} className="w-full">
        {loading ? "Running engine..." : "Search"}
      </Button>
    </form>
  );
}
