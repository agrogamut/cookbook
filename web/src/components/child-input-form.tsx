"use client";

import { useEffect, useRef, useState } from "react";
import {
  GenerateInput, getRegions, getCuisines, getAllergens, getEnums,
  getSpecialCareConditions, getClinicalMarkers, matchProfiles, getProfile,
} from "@/lib/api";
import type {
  Region, Cuisine, Allergen, ReferenceEnums, SpecialCareCondition, ClinicalMarker,
  MatchCandidate,
} from "@/lib/types";
import { ClinicalFlagsFieldset } from "@/components/clinical-flags-fieldset";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Accordion, AccordionContent, AccordionItem,
} from "@/components/ui/accordion";
import { SectionTrigger, summarise } from "@/components/section-trigger";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";

/** maxPhotoBytes mirrors the server's own cap in internal/book/photo.go.
 *
 *  Checked here as well so an operator learns their photograph is too large before a 16 MB
 *  upload, not after. The server still enforces it: this is a courtesy, not the control. */
const maxPhotoBytes = 8 * 1024 * 1024;
const photoTypes = ["image/png", "image/jpeg", "image/webp"];

const field = "space-y-1";

type Photo = { dataUri: string; name: string } | null;

type GrowthRow = {
  measured_on: string;
  weight_kg: string;
  height_cm: string;
  head_circumference_cm: string;
  // The three z-scores book1.go's growth trend table reads by name ("weight-for-age",
  // "height-for-age", "BMI-for-age" -- see growthZScores in book1.go). Only weight_for_age_z
  // used to be collected here; the other two were accepted by GenerateInput.growth and
  // stored/printed by profile.GrowthMeasurement, but this form had nowhere to enter them.
  weight_for_age_z: string;
  height_for_age_z: string;
  bmi_for_age_z: string;
  interpretation: string;
  measured_by: string;
};

const emptyGrowth: GrowthRow = {
  measured_on: "", weight_kg: "", height_cm: "", head_circumference_cm: "",
  weight_for_age_z: "", height_for_age_z: "", bmi_for_age_z: "",
  interpretation: "", measured_by: "",
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

function countFilled(...values: string[]): number {
  return values.filter((v) => v.trim() !== "").length;
}

/** One upload slot. All three photographs take the same file, caption and preview, so they
 *  are one component rather than three near-identical blocks -- which is also what lets the
 *  three of them share a single section instead of a third of the form. */
function PhotoRow({
  id, label, hint, photo, caption, error, onFile, onCaption, onClear,
}: {
  id: string;
  label: string;
  hint: string;
  photo: Photo;
  caption: string;
  error: string;
  onFile: (f: File) => void;
  onCaption: (v: string) => void;
  onClear: () => void;
}) {
  return (
    <div className="space-y-1 rounded border p-2">
      <div className="flex items-baseline justify-between gap-2">
        <Label htmlFor={id} className="text-xs font-medium">{label}</Label>
        {photo && (
          <Button type="button" variant="ghost" size="sm" className="h-5 px-1 text-xs"
                  onClick={onClear}>
            Remove
          </Button>
        )}
      </div>
      <p className="text-[11px] leading-snug text-muted-foreground">{hint}</p>
      <div className="flex items-center gap-2">
        {photo && (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={photo.dataUri} alt={`${label} preview`}
               className="size-10 shrink-0 rounded border object-cover" />
        )}
        <div className="min-w-0 flex-1 space-y-1">
          <Input id={id} type="file" accept="image/png,image/jpeg,image/webp"
                 className="h-8 text-xs"
                 onChange={(e) => { const f = e.target.files?.[0]; if (f) onFile(f); }} />
          <Input value={caption} onChange={(e) => onCaption(e.target.value)}
                 className="h-8 text-xs" placeholder="Caption (optional)" />
        </div>
      </div>
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  );
}

export function ChildInputForm({
  busy, onGenerate, initialChild,
}: {
  busy: boolean;
  onGenerate: (input: GenerateInput) => void;
  initialChild?: { display_name: string; date_of_birth: string };
}) {
  const [name, setName] = useState(initialChild?.display_name ?? "");
  const [dob, setDob] = useState(initialChild?.date_of_birth ?? "");
  const [caseId, setCaseId] = useState("");
  const [motherName, setMotherName] = useState("");
  const [matches, setMatches] = useState<MatchCandidate[]>([]);
  const [matchedIdentity, setMatchedIdentity] = useState("");
  const [dismissedIdentity, setDismissedIdentity] = useState("");
  const identityKey = JSON.stringify([caseId, name, dob, motherName]);
  const [loadMatchError, setLoadMatchError] = useState("");
  // Set just before loadMatch rewrites caseId/name/dob/motherName -- the debounce effect's
  // own dependency array -- so that self-triggered effect run can be told apart from the
  // operator actually typing and skipped instead of re-querying and re-showing the banner
  // the operator just dismissed by loading it.
  const justLoadedRef = useRef(false);
  const [sex, setSex] = useState("");
  const [language, setLanguage] = useState("");
  const [region, setRegion] = useState("");
  const [cuisine, setCuisine] = useState("");
  const [diet, setDiet] = useState("");
  const [vegan, setVegan] = useState(false);
  const [religiousRestriction, setReligiousRestriction] = useState("");
  const [budget, setBudget] = useState("");
  const [maxPrep, setMaxPrep] = useState("");
  const [maxCook, setMaxCook] = useState("");
  const [confirmed, setConfirmed] = useState<string[]>([]);
  const [suspected, setSuspected] = useState<string[]>([]);
  const [specialCare, setSpecialCare] = useState("");
  // clinical_rule_master flags, the same picker and wire shape ProfileForm sends -- one
  // trigger_field per set value. A separate control from Special-care condition above: that
  // one is the six STOP-REVIEW archetypes, this is the other 27 clinical_rule_master domains
  // (anemia/iron risk, growth faltering, and so on) that feed nutrition-target selection and
  // the drafted per-recipe modification notes. Previously the only way to set one of these
  // through a generated book was to hand-build the request outside this form.
  const [clinicalFlags, setClinicalFlags] = useState<Record<string, string>>({});
  const [growth, setGrowth] = useState<GrowthRow[]>([]);
  const [photo, setPhoto] = useState<Photo>(null);
  const [caption, setCaption] = useState("");
  const [photoError, setPhotoError] = useState("");
  // Book 1's back cover -- a separate upload from the front cover's child photo above, of
  // whoever the family wants on the closing page. Optional, same as the front cover.
  const [parentsPhoto, setParentsPhoto] = useState<Photo>(null);
  const [parentsCaption, setParentsCaption] = useState("");
  const [parentsPhotoError, setParentsPhotoError] = useState("");
  // A photograph or scan of a prescription a doctor has already written and signed on paper.
  // Printed on B1-PRESCRIPTION-01 in place of that page's blank form -- see
  // internal/book/templates/book1/prescription.html. Not a diagnosis or dose entered here:
  // this attaches a real document someone already holds.
  const [prescriptionPhoto, setPrescriptionPhoto] = useState<Photo>(null);
  const [prescriptionCaption, setPrescriptionCaption] = useState("");
  const [prescriptionPhotoError, setPrescriptionPhotoError] = useState("");

  const [regions, setRegions] = useState<Region[]>([]);
  const [cuisines, setCuisines] = useState<Cuisine[]>([]);
  const [allergens, setAllergens] = useState<Allergen[]>([]);
  const [enums, setEnums] = useState<ReferenceEnums>({});
  const [conditions, setConditions] = useState<SpecialCareCondition[]>([]);
  const [markerOptions, setMarkerOptions] = useState<ClinicalMarker[]>([]);

  // Every option list comes from the database rather than being written here, so a re-import
  // that changes the corpus changes the form with it. A hardcoded list would keep offering a
  // region the corpus no longer carries, and the write path would reject it.
  useEffect(() => {
    getRegions().then(setRegions).catch(() => {});
    getCuisines().then(setCuisines).catch(() => {});
    getAllergens().then(setAllergens).catch(() => {});
    getEnums().then(setEnums).catch(() => {});
    getSpecialCareConditions().then(setConditions).catch(() => {});
    getClinicalMarkers().then(setMarkerOptions).catch(() => {});
  }, []);

  // Debounced: fires 500ms after the operator stops typing, so a check doesn't fire on
  // every keystroke. Fires on a case id alone, or once name+date-of-birth+mother's-name are
  // ALL filled in -- matching profile.FindMatches's own rule that name+dob alone is never
  // enough. Cleared and never fired again once the operator explicitly dismisses a
  // suggestion for the current inputs, so accepting "this is a new child" does not re-show
  // the same banner on every keystroke afterward.
  useEffect(() => {
    // loadMatch just rewrote these same four fields to load a chosen record -- that is a
    // self-triggered run of this effect, not the operator typing, so skip it entirely rather
    // than resetting dismissedMatch and re-querying to find (and re-show a banner for) the
    // very record the operator just loaded.
    if (justLoadedRef.current) {
      justLoadedRef.current = false;
      return;
    }
    if (!caseId && !(name && dob && motherName)) {
      return;
    }
    let active = true;
    const handle = setTimeout(() => {
      matchProfiles({
        case_id: caseId || undefined,
        display_name: name || undefined,
        mother_name: motherName || undefined,
        date_of_birth: dob || undefined,
      })
        .then((found) => {
          if (!active) return;
          setMatches(found);
          setMatchedIdentity(identityKey);
          setLoadMatchError("");
        })
        .catch(() => { if (active) { setMatches([]); setMatchedIdentity(identityKey); } }); // A failed check is not itself an error worth surfacing --
        // it only means the suggestion banner does not appear, and generation proceeds exactly
        // as it does when there genuinely is no match.
    }, 500);
    return () => { active = false; clearTimeout(handle); };
  }, [caseId, name, dob, motherName, identityKey]);

  async function loadMatch(childID: string) {
    let p;
    try {
      p = await getProfile(childID);
    } catch {
      setLoadMatchError("Could not load this record. Try again.");
      return;
    }
    justLoadedRef.current = true;
    setName(p.display_name ?? "");
    setDob(p.date_of_birth);
    setSex(p.sex ?? "");
    setLanguage(p.language_id ?? "");
    setRegion(p.region_culture ?? "");
    setCuisine(p.cuisine_code ?? "");
    setDiet(p.diet_type ?? "");
    setVegan(Boolean(p.vegan));
    setReligiousRestriction(p.religious_restriction ?? "");
    setBudget(p.budget_band ?? "");
    setMaxPrep(p.max_prep_time_min ? String(p.max_prep_time_min) : "");
    setMaxCook(p.max_cook_time_min ? String(p.max_cook_time_min) : "");
    setCaseId(p.case_id ?? "");
    setMotherName(p.mother_name ?? "");
    setConfirmed(p.allergens.filter((a) => a.status === "confirmed").map((a) => a.group));
    setSuspected(p.allergens.filter((a) => a.status === "suspected").map((a) => a.group));
    // A clinical condition or growth visit typed for what turns out to be a different,
    // already-existing child must not survive under the loaded child's identity -- the API
    // has no clinical/growth fields on a stored profile to correctly refill these from (see
    // profileDTO), so the honest fix is to clear them, not to guess.
    setSpecialCare("");
    setClinicalFlags({});
    setGrowth([]);
    setLoadMatchError("");
    setMatches([]);
    setDismissedIdentity(identityKey);
  }

  function toggle(list: string[], set: (v: string[]) => void, value: string) {
    set(list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);
  }

  // clinical_flags is Record<field, value> -- one value per field, matching what the engine's
  // triggerFires actually compares. Mirrors ProfileForm's own setFlag/clearFlag exactly, so
  // the same marker behaves identically from either screen.
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

  // Shared by all three photo fields (front cover, back cover, prescription): same allowlist,
  // same size cap, same failure modes, just a different pair of setters to land in.
  function readPhotoInto(
    file: File,
    setValue: (p: Photo) => void,
    setError: (message: string) => void,
  ) {
    setError("");
    if (!photoTypes.includes(file.type)) {
      setError(`${file.type || "that file"} cannot be printed. Use PNG, JPEG or WebP.`);
      return;
    }
    if (file.size > maxPhotoBytes) {
      setError(`${(file.size / 1048576).toFixed(1)} MB is over the 8 MB limit.`);
      return;
    }
    const reader = new FileReader();
    reader.onload = () => setValue({ dataUri: String(reader.result), name: file.name });
    reader.onerror = () => setError("Could not read that file.");
    reader.readAsDataURL(file);
  }

  function submit() {
    const input: GenerateInput = {
      date_of_birth: dob,
      display_name: name || undefined,
      case_id: caseId || undefined,
      mother_name: motherName || undefined,
      sex: sex || undefined,
      language_id: language || undefined,
      region_culture: region || undefined,
      cuisine_code: cuisine || undefined,
      diet_type: diet || undefined,
      vegan: vegan || undefined,
      religious_restriction: religiousRestriction || undefined,
      budget_band: budget || undefined,
      max_prep_time_min: num(maxPrep),
      max_cook_time_min: num(maxCook),
      allergens: [
        // source is parent_reported because that is what a consultation form is. It is one of
        // exactly two values the database accepts; the other, clinician_documented, is a
        // stronger claim than this screen can make on a clinician's behalf.
        ...confirmed.map((g) => ({ group: g, status: "confirmed", source: "parent_reported" })),
        ...suspected.map((g) => ({ group: g, status: "suspected", source: "parent_reported" })),
      ],
      photo_data_uri: photo?.dataUri,
      photo_caption: caption || undefined,
      parents_photo_data_uri: parentsPhoto?.dataUri,
      parents_photo_caption: parentsCaption || undefined,
      prescription_photo_data_uri: prescriptionPhoto?.dataUri,
      prescription_photo_caption: prescriptionCaption || undefined,
    };
    // Special-care condition and clinical flags are two different pickers over two different
    // provider tables (special_care_condition_gate's six STOP-REVIEW archetypes vs
    // clinical_rule_master's other 27 domains) but the same wire shape -- both land in
    // profile.Stored's one Conditions list, so a book generated from this form can now carry
    // both, or several clinical flags at once, rather than the single hardcoded slot this form
    // used to be limited to.
    const conditionRows = [
      ...(specialCare
        ? [{ trigger_field: "Special_Care_Condition", flag_value: specialCare, class: "chronic" }]
        : []),
      ...Object.entries(clinicalFlags).map(([trigger_field, flag_value]) => (
        { trigger_field, flag_value, class: "chronic" }
      )),
    ];
    if (conditionRows.length > 0) input.conditions = conditionRows;
    const rows = growth
      .filter((g) => g.measured_on.trim() !== "")
      .map((g) => ({
        measured_on: g.measured_on,
        weight_kg: num(g.weight_kg),
        height_cm: num(g.height_cm),
        head_circumference_cm: num(g.head_circumference_cm),
        weight_for_age_z: num(g.weight_for_age_z),
        height_for_age_z: num(g.height_for_age_z),
        bmi_for_age_z: num(g.bmi_for_age_z),
        interpretation: g.interpretation || undefined,
        measured_by: g.measured_by || undefined,
      }));
    if (rows.length > 0) input.growth = rows;
    onGenerate(input);
  }

  const childFilled = countFilled(caseId, name, dob, sex, language, motherName);
  const practiceFilled = countFilled(region, cuisine, diet, budget, religiousRestriction, maxPrep, maxCook)
    + (vegan ? 1 : 0);
  const allergySummary = summarise(
    confirmed.length > 0 && `${confirmed.length} confirmed`,
    suspected.length > 0 && `${suspected.length} suspected`,
  );
  const clinicalCount = (specialCare ? 1 : 0) + Object.keys(clinicalFlags).length;
  const growthVisits = growth.filter((g) => g.measured_on.trim() !== "").length;
  const photoCount = [photo, parentsPhoto, prescriptionPhoto].filter(Boolean).length;

  function growthCell(i: number, key: keyof GrowthRow) {
    return (v: string) => setGrowth(growth.map((r, j) => (j === i ? { ...r, [key]: v } : r)));
  }

  return (
    // The form owns its own scroll so the Generate action can sit outside it. An operator
    // running twenty consultations an hour should never have to scroll past three optional
    // uploads to reach the button that does the work.
    <div className="flex h-full flex-col">
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto pr-2">
        {matches.length > 0 && matchedIdentity === identityKey && dismissedIdentity !== identityKey && (
          <Alert>
            <AlertTitle>
              {matches.length === 1 ? "This may already be a saved child" : `${matches.length} possible matches found`}
            </AlertTitle>
            <AlertDescription className="space-y-2">
              {matches.map((m) => (
                <div key={m.child_id} className="flex flex-wrap items-center justify-between gap-2">
                  <span className="font-mono text-xs">
                    {m.child_id} {m.case_id && `· case ${m.case_id}`} · {m.display_name} · DOB {m.date_of_birth}
                  </span>
                  <Button type="button" size="sm" variant="outline" onClick={() => loadMatch(m.child_id)}>
                    Load this record
                  </Button>
                </div>
              ))}
              <Button type="button" size="sm" variant="ghost" onClick={() => setDismissedIdentity(identityKey)}>
                This is a different child
              </Button>
              {loadMatchError && <p className="text-xs text-destructive">{loadMatchError}</p>}
            </AlertDescription>
          </Alert>
        )}

        {/* Open by default: the three sections every consultation fills. The rest start shut
            with their counts showing, because most children have no special-care condition,
            no measurement taken today and no photograph to attach. */}
        <Accordion type="multiple" defaultValue={["child", "practice", "allergies"]}>
          <AccordionItem value="child">
            <SectionTrigger label="Child"
                            summary={childFilled > 0 ? `${childFilled} of 6` : undefined} />
            <AccordionContent className="grid gap-3 sm:grid-cols-2">
              <div className={field}>
                <Label htmlFor="g-case-id">Case ID</Label>
                <Input id="g-case-id" value={caseId} onChange={(e) => setCaseId(e.target.value)}
                       placeholder="clinic/case number, if any" className="font-mono" />
              </div>
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
                  <SelectTrigger id="g-sex" className="w-full"><SelectValue placeholder="not recorded" /></SelectTrigger>
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
              <div className={field}>
                <Label htmlFor="g-mother">Mother&apos;s name</Label>
                <Input id="g-mother" value={motherName} onChange={(e) => setMotherName(e.target.value)}
                       placeholder="for matching an existing record" />
              </div>
            </AccordionContent>
          </AccordionItem>

          <AccordionItem value="practice">
            <SectionTrigger label="Food practice and place"
                            summary={practiceFilled > 0 ? `${practiceFilled} of 7` : undefined} />
            <AccordionContent className="grid gap-3 sm:grid-cols-2">
              <div className={field}>
                <Label htmlFor="g-region">Region</Label>
                <Select value={region} onValueChange={setRegion}>
                  <SelectTrigger id="g-region" className="w-full"><SelectValue placeholder="West Bengal first" /></SelectTrigger>
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
                  <SelectTrigger id="g-cuisine" className="w-full"><SelectValue placeholder="No preference" /></SelectTrigger>
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
                  <SelectTrigger id="g-diet" className="w-full"><SelectValue placeholder="Any" /></SelectTrigger>
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
                  <SelectTrigger id="g-budget" className="w-full"><SelectValue placeholder="Any" /></SelectTrigger>
                  <SelectContent>
                    {(enums.budget_band ?? []).map((v) => (
                      <SelectItem key={v.value} value={v.value}>{v.value}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className={field}>
                <Label htmlFor="g-religious">Religious/cultural restriction</Label>
                <Input id="g-religious" value={religiousRestriction}
                       onChange={(e) => setReligiousRestriction(e.target.value)}
                       placeholder="e.g. Halal, Jain, no beef" />
              </div>
              <div className={field}>
                <Label htmlFor="g-prep">Max prep (min)</Label>
                <Select value={maxPrep} onValueChange={setMaxPrep}>
                  <SelectTrigger id="g-prep" className="w-full"><SelectValue placeholder="Any" /></SelectTrigger>
                  <SelectContent>
                    {(enums.prep_time_min ?? []).map((v) => (
                      <SelectItem key={v.value} value={v.value}>{v.value}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className={field}>
                <Label htmlFor="g-cook">Max cook (min)</Label>
                <Select value={maxCook} onValueChange={setMaxCook}>
                  <SelectTrigger id="g-cook" className="w-full"><SelectValue placeholder="Any" /></SelectTrigger>
                  <SelectContent>
                    {(enums.cook_time_min ?? []).map((v) => (
                      <SelectItem key={v.value} value={v.value}>{v.value}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <label className="flex items-start gap-2 text-xs sm:col-span-2">
                <input type="checkbox" checked={vegan} onChange={(e) => setVegan(e.target.checked)}
                       className="mt-0.5" />
                <span>Vegan &mdash; additional to diet type; excludes dairy, fish and animal-protein food groups</span>
              </label>
            </AccordionContent>
          </AccordionItem>

          <AccordionItem value="allergies">
            <SectionTrigger label="Allergies" summary={allergySummary} />
            {/* Two lists, not one with a toggle, because the two do different things and an
                operator has to see which they picked. Confirmed removes recipes outright;
                suspected ranks them down and prints on the child's profile page (AS-002). */}
            <AccordionContent className="space-y-3">
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
            </AccordionContent>
          </AccordionItem>

          <AccordionItem value="clinical">
            <SectionTrigger label="Clinical"
                            summary={clinicalCount > 0 ? `${clinicalCount} set` : undefined} />
            <AccordionContent className="space-y-3">
              <div className={field}>
                <Label htmlFor="g-sc">Special-care condition</Label>
                <Select value={specialCare} onValueChange={setSpecialCare}>
                  <SelectTrigger id="g-sc" className="w-full"><SelectValue placeholder="none declared" /></SelectTrigger>
                  <SelectContent>
                    {conditions.map((c) => (
                      <SelectItem key={c.condition_id} value={c.condition_id}>
                        {c.condition}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {/* The reviewer the provider names for this condition, printed verbatim.
                    Nothing here withholds a result waiting for that reviewer -- it records the
                    provider's own action, reviewer and stop text in the step list, and the
                    child is ranked like any other. */}
                {specialCare && (
                  <p className="text-xs text-muted-foreground">
                    Provider&apos;s named reviewer:{" "}
                    {conditions.find((c) => c.condition_id === specialCare)?.mandatory_reviewer}
                  </p>
                )}
              </div>
              {/* The other 27 clinical_rule_master domains (anemia/iron risk, growth
                  faltering, and so on) -- distinct from Special-care condition above, which is
                  only the six STOP-REVIEW archetypes. Both feed the same profile.Stored
                  Conditions list a generated book reads. */}
              <ClinicalFlagsFieldset
                markers={markerOptions}
                flags={clinicalFlags}
                onSet={setFlag}
                onClear={clearFlag}
              />
            </AccordionContent>
          </AccordionItem>

          <AccordionItem value="growth">
            <SectionTrigger
              label="Growth measurements"
              summary={growthVisits > 0 ? `${growthVisits} visit${growthVisits === 1 ? "" : "s"}` : undefined}
            />
            <AccordionContent className="space-y-2">
              {growth.length === 0 && (
                <p className="text-xs text-muted-foreground">
                  None recorded. The growth page prints only if there is at least one measurement.
                </p>
              )}
              {growth.map((g, i) => (
                <div key={i} className="grid gap-2 rounded border p-2 sm:grid-cols-2">
                  <Input type="date" className="font-mono" value={g.measured_on} placeholder="date"
                         onChange={(e) => growthCell(i, "measured_on")(e.target.value)} />
                  <Input className="font-mono" value={g.weight_kg} placeholder="weight kg"
                         onChange={(e) => growthCell(i, "weight_kg")(e.target.value)} />
                  <Input className="font-mono" value={g.height_cm} placeholder="height cm"
                         onChange={(e) => growthCell(i, "height_cm")(e.target.value)} />
                  <Input className="font-mono" value={g.head_circumference_cm} placeholder="head cm"
                         onChange={(e) => growthCell(i, "head_circumference_cm")(e.target.value)} />
                  <Input className="font-mono" value={g.weight_for_age_z} placeholder="wt-for-age z"
                         onChange={(e) => growthCell(i, "weight_for_age_z")(e.target.value)} />
                  <Input className="font-mono" value={g.height_for_age_z} placeholder="ht-for-age z"
                         onChange={(e) => growthCell(i, "height_for_age_z")(e.target.value)} />
                  <Input className="font-mono" value={g.bmi_for_age_z} placeholder="BMI-for-age z"
                         onChange={(e) => growthCell(i, "bmi_for_age_z")(e.target.value)} />
                  <Input value={g.measured_by} placeholder="measured by"
                         onChange={(e) => growthCell(i, "measured_by")(e.target.value)} />
                  <div className="flex gap-1 sm:col-span-2">
                    <Input value={g.interpretation} placeholder="clinician note"
                           onChange={(e) => growthCell(i, "interpretation")(e.target.value)} />
                    <Button type="button" variant="ghost" size="sm"
                            onClick={() => setGrowth(growth.filter((_, j) => j !== i))}>&times;</Button>
                  </div>
                </div>
              ))}
              <Button type="button" variant="outline" size="sm"
                      onClick={() => setGrowth([...growth, { ...emptyGrowth }])}>
                Add a visit
              </Button>
            </AccordionContent>
          </AccordionItem>

          {/* One section for all three uploads. They were three, each with its own heading,
              file input, caption and preview, and together they ran a third of the form's
              height for content that is optional on every book. */}
          <AccordionItem value="photographs">
            <SectionTrigger
              label="Photographs"
              summary={photoCount > 0 ? `${photoCount} of 3` : undefined}
            />
            <AccordionContent className="space-y-2">
              <PhotoRow
                id="g-photo" label="Front cover"
                hint="The child. Prints full-bleed on Book 1's cover."
                photo={photo} caption={caption} error={photoError}
                onFile={(f) => readPhotoInto(f, setPhoto, setPhotoError)}
                onCaption={setCaption}
                onClear={() => { setPhoto(null); setPhotoError(""); }}
              />
              <PhotoRow
                id="g-parents-photo" label="Back cover"
                hint="Typically the parents. Prints full-bleed on Book 1's closing page."
                photo={parentsPhoto} caption={parentsCaption} error={parentsPhotoError}
                onFile={(f) => readPhotoInto(f, setParentsPhoto, setParentsPhotoError)}
                onCaption={setParentsCaption}
                onClear={() => { setParentsPhoto(null); setParentsPhotoError(""); }}
              />
              <PhotoRow
                id="g-rx-photo" label="Prescription"
                hint="A prescription already written and signed on paper. Replaces the blank form on the Prescription page: this attaches a real document, it does not generate one."
                photo={prescriptionPhoto} caption={prescriptionCaption} error={prescriptionPhotoError}
                onFile={(f) => readPhotoInto(f, setPrescriptionPhoto, setPrescriptionPhotoError)}
                onCaption={setPrescriptionCaption}
                onClear={() => { setPrescriptionPhoto(null); setPrescriptionPhotoError(""); }}
              />
            </AccordionContent>
          </AccordionItem>
        </Accordion>
      </div>

      <div className="shrink-0 space-y-1 border-t pt-3">
        <Button onClick={submit} disabled={!dob || busy} className="w-full">
          {busy ? "Generating…" : "Generate both books"}
        </Button>
        {!dob && (
          <p className="text-xs text-muted-foreground">
            A date of birth is needed: every book states the child&apos;s age.
          </p>
        )}
      </div>
    </div>
  );
}
