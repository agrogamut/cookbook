"use client";

import { useEffect, useState } from "react";
import {
  GenerateInput, getRegions, getCuisines, getAllergens, getEnums,
  getSpecialCareConditions,
} from "@/lib/api";
import type {
  Region, Cuisine, Allergen, ReferenceEnums, SpecialCareCondition,
} from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";

/** maxPhotoBytes mirrors the server's own cap in internal/book/photo.go.
 *
 *  Checked here as well so an operator learns their photograph is too large before a 16 MB
 *  upload, not after. The server still enforces it: this is a courtesy, not the control. */
const maxPhotoBytes = 8 * 1024 * 1024;
const photoTypes = ["image/png", "image/jpeg", "image/webp"];

type GrowthRow = {
  measured_on: string;
  weight_kg: string;
  height_cm: string;
  head_circumference_cm: string;
  weight_for_age_z: string;
  interpretation: string;
  measured_by: string;
};

const emptyGrowth: GrowthRow = {
  measured_on: "", weight_kg: "", height_cm: "", head_circumference_cm: "",
  weight_for_age_z: "", interpretation: "", measured_by: "",
};

/** num turns a form field into a number the API will accept, or undefined.
 *
 *  undefined, never 0. A blank measurement is one that was not taken, and the book prints a
 *  writing line for it; sending 0 would print a measured value of nothing. */
function num(v: string): number | undefined {
  const t = v.trim();
  if (t === "") return undefined;
  const n = Number(t);
  return Number.isFinite(n) ? n : undefined;
}

export function ChildInputForm({
  busy, onGenerate,
}: {
  busy: boolean;
  onGenerate: (input: GenerateInput) => void;
}) {
  const [name, setName] = useState("");
  const [dob, setDob] = useState("");
  const [sex, setSex] = useState("");
  const [language, setLanguage] = useState("");
  const [region, setRegion] = useState("");
  const [cuisine, setCuisine] = useState("");
  const [diet, setDiet] = useState("");
  const [budget, setBudget] = useState("");
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const [suspected, setSuspected] = useState<string[]>([]);
  const [specialCare, setSpecialCare] = useState("");
  const [growth, setGrowth] = useState<GrowthRow[]>([]);
  const [photo, setPhoto] = useState<{ dataUri: string; name: string } | null>(null);
  const [caption, setCaption] = useState("");
  const [photoError, setPhotoError] = useState("");

  const [regions, setRegions] = useState<Region[]>([]);
  const [cuisines, setCuisines] = useState<Cuisine[]>([]);
  const [allergens, setAllergens] = useState<Allergen[]>([]);
  const [enums, setEnums] = useState<ReferenceEnums>({});
  const [conditions, setConditions] = useState<SpecialCareCondition[]>([]);

  // Every option list comes from the database rather than being written here, so a re-import
  // that changes the corpus changes the form with it. A hardcoded list would keep offering a
  // region the corpus no longer carries, and the write path would reject it.
  useEffect(() => {
    getRegions().then(setRegions).catch(() => {});
    getCuisines().then(setCuisines).catch(() => {});
    getAllergens().then(setAllergens).catch(() => {});
    getEnums().then(setEnums).catch(() => {});
    getSpecialCareConditions().then(setConditions).catch(() => {});
  }, []);

  function toggle(list: string[], set: (v: string[]) => void, value: string) {
    set(list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);
  }

  function readPhoto(file: File) {
    setPhotoError("");
    if (!photoTypes.includes(file.type)) {
      setPhotoError(`${file.type || "that file"} cannot be printed. Use PNG, JPEG or WebP.`);
      return;
    }
    if (file.size > maxPhotoBytes) {
      setPhotoError(`${(file.size / 1048576).toFixed(1)} MB is over the 8 MB limit.`);
      return;
    }
    const reader = new FileReader();
    reader.onload = () => setPhoto({ dataUri: String(reader.result), name: file.name });
    reader.onerror = () => setPhotoError("Could not read that file.");
    reader.readAsDataURL(file);
  }

  function submit() {
    const input: GenerateInput = {
      date_of_birth: dob,
      display_name: name || undefined,
      sex: sex || undefined,
      language_id: language || undefined,
      region_culture: region || undefined,
      cuisine_code: cuisine || undefined,
      diet_type: diet || undefined,
      budget_band: budget || undefined,
      allergens: [
        // source is parent_reported because that is what a consultation form is. It is one of
        // exactly two values the database accepts; the other, clinician_documented, is a
        // stronger claim than this screen can make on a clinician's behalf.
        ...confirmed.map((g) => ({ group: g, status: "confirmed", source: "parent_reported" })),
        ...suspected.map((g) => ({ group: g, status: "suspected", source: "parent_reported" })),
      ],
      photo_data_uri: photo?.dataUri,
      photo_caption: caption || undefined,
    };
    if (specialCare) {
      input.conditions = [{
        trigger_field: "Special_Care_Condition", flag_value: specialCare, class: "chronic",
      }];
    }
    const rows = growth
      .filter((g) => g.measured_on.trim() !== "")
      .map((g) => ({
        measured_on: g.measured_on,
        weight_kg: num(g.weight_kg),
        height_cm: num(g.height_cm),
        head_circumference_cm: num(g.head_circumference_cm),
        weight_for_age_z: num(g.weight_for_age_z),
        interpretation: g.interpretation || undefined,
        measured_by: g.measured_by || undefined,
      }));
    if (rows.length > 0) input.growth = rows;
    onGenerate(input);
  }

  const field = "space-y-1";
  const legend = "text-xs font-semibold uppercase tracking-wide text-muted-foreground";

  return (
    <div className="space-y-5">
      <section className="space-y-2">
        <p className={legend}>Child</p>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div className={field}>
            <Label htmlFor="g-name">Name</Label>
            <Input id="g-name" value={name} onChange={(e) => setName(e.target.value)}
                   placeholder="as it should print" />
          </div>
          <div className={field}>
            {/* The one required field: every book states the child's age, and an age with no
                birth date behind it would be a number with no source. */}
            <Label htmlFor="g-dob">Date of birth <span className="text-destructive">*</span></Label>
            <Input id="g-dob" type="date" className="font-mono" value={dob}
                   onChange={(e) => setDob(e.target.value)} />
          </div>
          <div className={field}>
            <Label htmlFor="g-sex">Sex</Label>
            <Select value={sex} onValueChange={setSex}>
              <SelectTrigger id="g-sex"><SelectValue placeholder="not recorded" /></SelectTrigger>
              <SelectContent>
                {["male", "female", "other"].map((v) => (
                  <SelectItem key={v} value={v}>{v}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className={field}>
            <Label htmlFor="g-lang">Language</Label>
            <Input id="g-lang" value={language} onChange={(e) => setLanguage(e.target.value)}
                   placeholder="e.g. bn" className="font-mono" />
          </div>
        </div>
      </section>

      <section className="space-y-2">
        <p className={legend}>Food practice and place</p>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div className={field}>
            <Label htmlFor="g-region">Region</Label>
            <Select value={region} onValueChange={setRegion}>
              <SelectTrigger id="g-region"><SelectValue placeholder="West Bengal first" /></SelectTrigger>
              <SelectContent>
                {regions.map((r) => (
                  <SelectItem key={r.region_culture} value={r.region_culture}>{r.region_culture}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className={field}>
            <Label htmlFor="g-cuisine">Cuisine</Label>
            <Select value={cuisine} onValueChange={setCuisine}>
              <SelectTrigger id="g-cuisine"><SelectValue placeholder="No preference" /></SelectTrigger>
              <SelectContent>
                {cuisines.map((c) => (
                  <SelectItem key={c.culture_code} value={c.culture_code}>{c.cuisine_cluster}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className={field}>
            <Label htmlFor="g-diet">Diet</Label>
            <Select value={diet} onValueChange={setDiet}>
              <SelectTrigger id="g-diet"><SelectValue placeholder="Any" /></SelectTrigger>
              <SelectContent>
                {(enums.diet_type ?? []).map((v) => (
                  <SelectItem key={v.value} value={v.value}>{v.value}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className={field}>
            <Label htmlFor="g-budget">Budget band</Label>
            <Select value={budget} onValueChange={setBudget}>
              <SelectTrigger id="g-budget"><SelectValue placeholder="Any" /></SelectTrigger>
              <SelectContent>
                {(enums.budget_band ?? []).map((v) => (
                  <SelectItem key={v.value} value={v.value}>{v.value}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      </section>

      <section className="space-y-2">
        <p className={legend}>Allergies</p>
        {/* Two lists, not one with a toggle, because the two do different things and an
            operator has to see which they picked. Confirmed removes recipes outright;
            suspected ranks them down and prints on the child's profile page (AS-002). */}
        <div className="grid gap-3 lg:grid-cols-2">
          <div className="space-y-1">
            <Label className="text-xs">Confirmed &mdash; excludes recipes</Label>
            <div className="flex flex-wrap gap-1">
              {allergens.map((a) => (
                <button key={a.allergen_group} type="button"
                        onClick={() => toggle(confirmed, setConfirmed, a.allergen_group)}
                        className="cursor-pointer">
                  <Badge variant={confirmed.includes(a.allergen_group) ? "destructive" : "outline"}>
                    {a.allergen_group}
                  </Badge>
                </button>
              ))}
            </div>
          </div>
          <div className="space-y-1">
            <Label className="text-xs">Suspected &mdash; ranks down, never excludes</Label>
            <div className="flex flex-wrap gap-1">
              {allergens.map((a) => (
                <button key={a.allergen_group} type="button"
                        onClick={() => toggle(suspected, setSuspected, a.allergen_group)}
                        className="cursor-pointer">
                  <Badge variant={suspected.includes(a.allergen_group) ? "secondary" : "outline"}>
                    {a.allergen_group}
                  </Badge>
                </button>
              ))}
            </div>
          </div>
        </div>
      </section>

      <section className="space-y-2">
        <p className={legend}>Clinical</p>
        <div className="grid gap-3 sm:grid-cols-2">
          <div className={field}>
            <Label htmlFor="g-sc">Special-care condition</Label>
            <Select value={specialCare} onValueChange={setSpecialCare}>
              <SelectTrigger id="g-sc"><SelectValue placeholder="none declared" /></SelectTrigger>
              <SelectContent>
                {conditions.map((c) => (
                  <SelectItem key={c.condition_id} value={c.condition_id}>
                    {c.condition}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {specialCare && (
              <p className="text-xs text-muted-foreground">
                A declared special-care condition is the provider&apos;s stop gate. Generation
                will halt and no book is produced, for either book.
              </p>
            )}
          </div>
        </div>
      </section>

      <section className="space-y-2">
        <div className="flex items-center gap-3">
          <p className={legend}>Growth measurements</p>
          <Button type="button" variant="outline" size="sm"
                  onClick={() => setGrowth([...growth, { ...emptyGrowth }])}>
            Add a visit
          </Button>
        </div>
        {growth.length === 0 && (
          <p className="text-xs text-muted-foreground">
            None recorded. The growth page prints only if there is at least one measurement.
          </p>
        )}
        {growth.map((g, i) => (
          <div key={i} className="grid gap-2 rounded border p-2 sm:grid-cols-3 lg:grid-cols-7">
            <Input type="date" className="font-mono" value={g.measured_on} placeholder="date"
                   onChange={(e) => setGrowth(growth.map((r, j) => j === i ? { ...r, measured_on: e.target.value } : r))} />
            <Input className="font-mono" value={g.weight_kg} placeholder="weight kg"
                   onChange={(e) => setGrowth(growth.map((r, j) => j === i ? { ...r, weight_kg: e.target.value } : r))} />
            <Input className="font-mono" value={g.height_cm} placeholder="height cm"
                   onChange={(e) => setGrowth(growth.map((r, j) => j === i ? { ...r, height_cm: e.target.value } : r))} />
            <Input className="font-mono" value={g.head_circumference_cm} placeholder="head cm"
                   onChange={(e) => setGrowth(growth.map((r, j) => j === i ? { ...r, head_circumference_cm: e.target.value } : r))} />
            <Input className="font-mono" value={g.weight_for_age_z} placeholder="wt-for-age z"
                   onChange={(e) => setGrowth(growth.map((r, j) => j === i ? { ...r, weight_for_age_z: e.target.value } : r))} />
            <Input value={g.interpretation} placeholder="clinician note"
                   onChange={(e) => setGrowth(growth.map((r, j) => j === i ? { ...r, interpretation: e.target.value } : r))} />
            <div className="flex gap-1">
              <Input value={g.measured_by} placeholder="measured by"
                     onChange={(e) => setGrowth(growth.map((r, j) => j === i ? { ...r, measured_by: e.target.value } : r))} />
              <Button type="button" variant="ghost" size="sm"
                      onClick={() => setGrowth(growth.filter((_, j) => j !== i))}>&times;</Button>
            </div>
          </div>
        ))}
      </section>

      <section className="space-y-2">
        <p className={legend}>Cover photograph</p>
        <div className="grid gap-3 sm:grid-cols-3">
          <div className={field}>
            <Label htmlFor="g-photo">Image</Label>
            <Input id="g-photo" type="file" accept="image/png,image/jpeg,image/webp"
                   onChange={(e) => { const f = e.target.files?.[0]; if (f) readPhoto(f); }} />
          </div>
          <div className={field}>
            <Label htmlFor="g-caption">Caption</Label>
            <Input id="g-caption" value={caption} onChange={(e) => setCaption(e.target.value)}
                   placeholder="e.g. Aarav with his mother, July 2026" />
          </div>
          {photo && (
            <div className="space-y-1">
              <Label className="text-xs">Preview</Label>
              <div className="flex items-center gap-2">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src={photo.dataUri} alt="Cover preview"
                     className="h-16 w-16 rounded border object-cover" />
                <Button type="button" variant="ghost" size="sm"
                        onClick={() => { setPhoto(null); setPhotoError(""); }}>Remove</Button>
              </div>
            </div>
          )}
        </div>
        {photoError && <p className="text-xs text-destructive">{photoError}</p>}
      </section>

      <div className="flex items-center gap-3 border-t pt-4">
        <Button onClick={submit} disabled={!dob || busy} size="lg">
          {busy ? "Generating…" : "Generate both books"}
        </Button>
        {!dob && (
          <span className="text-xs text-muted-foreground">
            A date of birth is needed: every book states the child&apos;s age.
          </span>
        )}
      </div>
    </div>
  );
}
