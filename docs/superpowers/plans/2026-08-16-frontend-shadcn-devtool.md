# Frontend: Next.js + shadcn Internal Devtool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the seven-screen Next.js frontend in `web/`, wired to the Go API from
`docs/superpowers/plans/2026-08-16-backend-engine-api.md`, following CLAUDE.md's "Frontend -
internal devtool, not a consumer app" section exactly: dense tables, provenance on every
derived value, keyboard-first navigation, no decorative anything.

**Architecture:** This plan depends on the backend plan being implemented first (or in
parallel against a running `go run ./cmd/server` — never against mock data; see "No
client-side invention" below). One design decision resolves the tension between the
`frontend-design` skill's push for a distinctive visual identity and this project's explicit
brief: the identity here is not a color story, it's an **information density and provenance
story**. The signature element is the **provenance chip** — one component that renders a
derived value's source, confidence and verification state inline, used on every score,
percentage and corrected nutrition figure in the app. That is the one place this plan spends
its "aesthetic risk" (per the design skill's "spend your boldness in one place"); everything
else is deliberately quiet: zinc, dark-default, Geist Sans for prose, Geist Mono carrying the
identity for every ID, score and quantity.

**Tech Stack:** Next.js (App Router) + React + TypeScript + Tailwind v4 + shadcn/ui
(`new-york` style, Radix base). Deployed target is Vercel per the project's stack defaults,
not set up in this plan (no deployment task here — local dev only).

**Spec:** `/home/ghoul/graveyard/recipie/CLAUDE.md`, section "Frontend - internal devtool, not
a consumer app" (rules 1-8, the 7-route screen table, and "The 'why this result' panel"); and
`docs/superpowers/plans/2026-08-16-backend-engine-api.md` for the exact API contract (request/
response JSON shapes) this plan's TypeScript types must mirror field-for-field.

## Global Constraints

- No decorative anything: no gradients, no illustrations, no marketing copy, no animated
  transitions beyond what shadcn ships.
- Density first: `Table` over `Card`, compact row height, show 30 rows not 6.
- Provenance is a column, never a footnote. A bare derived number with no source/confidence
  next to it is a bug in this codebase.
- Keyboard first: `Command` palette on cmd+K, arrow keys move table selection, every dialog
  closes on Escape.
- Monospace (Geist Mono) for IDs, scores, percentages, nutrient values. Proportional text
  (Geist Sans) for prose only.
- Dark and light both work; respect `prefers-color-scheme`, never force one.
- **No client-side invention.** The frontend never computes a nutrition figure, fills a blank
  with a default, or rounds a gap away. It renders what the API returns, including `null`,
  which renders as "not available" text, never as `0` or an empty cell that reads as zero.
- Steps 1 (age) and 2 (allergy) are hard safety filters with no operator override anywhere in
  this codebase — no "show excluded anyway" toggle, ever, on any screen.
- All API calls funneled through one `src/lib/api.ts` client — no scattered `fetch` calls in
  page components.

---

### Task 1: Scaffold Next.js + shadcn, fix the Geist font gotcha

**Files:**
- Create: `web/` (entire Next.js project via CLI)
- Modify: `web/src/app/globals.css`
- Modify: `web/src/app/layout.tsx`

**Interfaces:**
- Produces: a running `pnpm dev` dev server at `localhost:3000` with shadcn's `new-york`
  style installed and `components.json` configured with `baseColor: "zinc"`.

- [ ] **Step 1: Scaffold the project**

```bash
cd /home/ghoul/graveyard/recipie
npx shadcn@latest init --template next -d
```

This creates `web/` with Next.js App Router, Tailwind v4, and a base `components.json`. If
the CLI asks where to put the project, target `web/`.

- [ ] **Step 2: Fix the known Geist font break**

`shadcn init` rewrites `globals.css` with a circular `--font-sans: var(--font-sans)`, which
Tailwind v4's `@theme inline` cannot resolve (it resolves at parse time, not runtime). Open
`web/src/app/globals.css` and replace the font declarations inside `@theme inline` with
literal names:

```css
@theme inline {
  --font-sans: "Geist", "Geist Fallback", ui-sans-serif, system-ui, sans-serif;
  --font-mono: "Geist Mono", "Geist Mono Fallback", ui-monospace, monospace;
  /* ...leave every other @theme inline line as the CLI generated it... */
}
```

- [ ] **Step 3: Move font variables to `<html>`**

Open `web/src/app/layout.tsx`. Move the `geistSans.variable`/`geistMono.variable` classNames
from `<body>` to `<html>`:

```tsx
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";

const geistSans = Geist({ variable: "--font-geist-sans", subsets: ["latin"] });
const geistMono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin"] });

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning className={`${geistSans.variable} ${geistMono.variable}`}>
      <body className="antialiased">{children}</body>
    </html>
  );
}
```

(`suppressHydrationWarning` is required once Task 2 adds `next-themes`, which mutates the
`<html>` class before hydration — add it now so Task 2 doesn't have to touch this file again.)

- [ ] **Step 4: Verify the dev server runs with the font fix applied**

```bash
cd web && pnpm dev
```

Open `localhost:3000` in a browser (or via the Chrome tools if available) and confirm the
default Next.js page renders in Geist Sans, not a fallback serif — a broken font fix is
visually obvious (system serif instead of Geist's geometric sans).

- [ ] **Step 5: Commit**

```bash
cd /home/ghoul/graveyard/recipie
git add web
git commit -m "Scaffold Next.js and shadcn with the Geist font fix applied"
```

---

### Task 2: Design tokens — dark-default zinc palette, provenance colors, density scale

**Files:**
- Modify: `web/src/app/globals.css`
- Modify: `web/src/app/layout.tsx`
- Create: `web/src/components/theme-provider.tsx`

**Interfaces:**
- Produces: `--color-verified`, `--color-unverified` CSS tokens (see rationale below); a
  `ThemeProvider` wrapping the app, defaulting to dark, togglable to light.

**Design rationale (per the `frontend-design` skill's "brainstorm, then critique" process,
condensed since the visual direction is already pinned by `CLAUDE.md` rather than open):**

The live schema only supports a genuine **two-state** provenance signal on the value that
matters most (ingredient nutrition): `value_source = 'ifct'` (139 ingredients, IFCT 2017-
backed, `verified = true`) or `value_source = 'provider'` (267 ingredients, the workbook's own
group-level placeholder, `verified = false`). A three-state "verified / unverified /
placeholder" taxonomy — floated during design brainstorming — does not match this data and
would be an invented distinction the schema doesn't draw. The chip therefore encodes exactly
two axes that really are independent in the data: **value provenance** (`verified: boolean`,
green vs. amber) and, separately, **provider review status** (`Draft` / `Needs Validation`
free text, rendered as a neutral outline badge, never colored to imply pass/fail — the
provider's own review process hasn't run yet, so implying a verdict here would be dishonest).

```css
/* web/src/app/globals.css -- add alongside the shadcn-generated tokens */
@theme inline {
  /* ...existing shadcn tokens (background, foreground, card, border, ring, etc)... */

  /* Provenance signal. Reused verbatim from the palette already shown for this project's
     shadcn skill guidance (status-done / priority-high), not a new invented pair -- picked
     because green-for-IFCT-backed and amber-for-provider-placeholder is exactly the
     "verified vs not" read an operator needs at a glance. */
  --color-verified: oklch(0.723 0.219 149.579);   /* IFCT-backed, value_source = 'ifct' */
  --color-unverified: oklch(0.705 0.213 47.604);  /* provider placeholder, value_source = 'provider' */
}
```

- [ ] **Step 1: Add the two provenance tokens to `globals.css`**

Insert the block above into the existing `@theme inline` rule shadcn generated in Task 1,
directly after the last shadcn-generated `--color-*` line. Do not touch or reorder the
existing tokens.

- [ ] **Step 2: Install `next-themes` and add `ThemeProvider`**

```bash
cd web && pnpm add next-themes
```

```tsx
// web/src/components/theme-provider.tsx
"use client";

import { ThemeProvider as NextThemesProvider } from "next-themes";
import type { ComponentProps } from "react";

export function ThemeProvider({ children, ...props }: ComponentProps<typeof NextThemesProvider>) {
  return (
    <NextThemesProvider attribute="class" defaultTheme="dark" enableSystem {...props}>
      {children}
    </NextThemesProvider>
  );
}
```

- [ ] **Step 3: Wrap the app in `layout.tsx`**

```tsx
// web/src/app/layout.tsx -- add inside <body>, around {children}
import { ThemeProvider } from "@/components/theme-provider";

// ...
<body className="antialiased">
  <ThemeProvider>{children}</ThemeProvider>
</body>
```

- [ ] **Step 4: Verify light and dark both render correctly**

```bash
pnpm dev
```

In the browser, toggle the OS theme (or use dev tools' rendering emulation) and confirm the
page background and text swap between the shadcn dark and light token sets without any
hardcoded color surviving the switch (grep the diff for stray hex/rgb values outside
`globals.css` — there should be none).

- [ ] **Step 5: Commit**

```bash
git add web/src/app/globals.css web/src/app/layout.tsx web/src/components/theme-provider.tsx
git commit -m "Add dark-default theme provider and the two-state provenance tokens"
```

---

### Task 3: API client and TypeScript types mirroring the Go models

**Files:**
- Create: `web/src/lib/api.ts`
- Create: `web/src/lib/types.ts`
- Create: `web/.env.local.example`

**Interfaces:**
- Produces: every type in `types.ts` mirrors a Go struct from `internal/models` or a handler
  response shape 1:1 (same field names, snake_case preserved since the Go JSON tags are
  snake_case and this file must not silently rename anything). `api.ts` exports one async
  function per endpoint; every page component in later tasks imports from here, never calls
  `fetch` directly.

- [ ] **Step 1: Write `web/src/lib/types.ts`**

```ts
// Mirrors internal/models/profile.go and engine.go field-for-field. Keep in sync by hand --
// there is no shared codegen between Go and TypeScript in this project, so a field added to
// ChildProfile or EngineResult on the Go side must be added here in the same change.

export interface ChildProfile {
  age_months: number;
  diet_type?: "Vegetarian" | "Non-vegetarian" | "Eggetarian";
  vegan?: boolean;
  allergens?: string[];
  clinical_flags?: Record<string, string>;
  clinical_marker?: string;
  region_culture?: string;
  cuisine_code?: string;
  meal_type?: string;
  budget_band?: "Low" | "Moderate" | "Premium";
  max_prep_time_min?: number;
  max_cook_time_min?: number;
  limit?: number;
}

export interface StepResult {
  step: number;
  name: string;
  kind: "hard_filter" | "ranker" | "target" | "escalation";
  candidates_in: number;
  candidates_out: number;
  note?: string;
  excluded?: ExclusionReason[];
}

export interface ExclusionReason {
  recipe_id: string;
  recipe_name: string;
  reason: string;
}

export interface RankedRecipe {
  recipe_id: string;
  recipe_name: string;
  region_culture: string;
  meal_type: string;
  clinical_tag: string;
  age_group: string;
  nutrition_score: number;
  ranked_score: number;
  scored_axes: string;
  value_kind: "derived";
}

export interface EngineResult {
  recipes: RankedRecipe[];
  steps: StepResult[];
  active_target: string;
  target_reason: string;
  blocked: boolean;
  block_reason?: string;
}

export interface RecipeMethodCard {
  recipe_id: string;
  recipe_name: string;
  region_culture: string;
  provider_method: string;
  provider_review_status: string;
  suggested_method_external: string | null;
  suggested_method_source: string | null;
  suggested_method_url: string | null;
  suggested_method_confidence: number | null;
  suggested_method_region_match: string | null;
  suggestion_disclosure: string;
}

export interface RecipeNutritionRecomputed {
  energy_kcal: number;
  protein_g: number;
  iron_mg: number;
  calcium_mg: number;
  ingredient_coverage: number;
  fully_verified: boolean;
  provider_energy_kcal: number;
  provider_protein_g: number;
  provider_iron_mg: number;
  provider_calcium_mg: number;
  energy_pct_diff: number | null;
  iron_pct_diff: number | null;
  value_kind: "derived";
  formula: string;
}

export interface RecipeDetail {
  method: RecipeMethodCard;
  nutrition: RecipeNutritionRecomputed;
}

// Every field here is optional-nullable except ingredient_id/english_name/food_group/
// value_source/verified -- mirrors ingredient_nutrition_corrected column-for-column
// (internal/api/handlers/ingredients.go).
export interface Ingredient {
  ingredient_id: string;
  english_name: string;
  bengali_name: string | null;
  food_group: string;
  ifct_food_code: string | null;
  ifct_food_name: string | null;
  ifct_match_exactness: string | null;
  ifct_resolved_by: string | null;
  value_source: "ifct" | "provider";
  verified: boolean;
  energy_kcal_100g: number;
  protein_g_100g: number;
  iron_mg_100g: number;
  calcium_mg_100g: number;
  provider_energy_kcal_100g: number;
  provider_protein_g_100g: number;
  provider_iron_mg_100g: number;
  provider_calcium_mg_100g: number;
  provider_review_status: string;
  provider_data_quality: string;
}

export interface NutritionDiscrepancy {
  ingredient_id: string;
  english_name: string;
  matched_ifct_food: string | null;
  used_in_recipes: number;
  provider_energy: number | null;
  external_energy: number | null;
  energy_pct_diff: number | null;
  provider_protein: number | null;
  external_protein: number | null;
  protein_pct_diff: number | null;
  provider_iron: number | null;
  external_iron: number | null;
  iron_pct_diff: number | null;
  provider_calcium: number | null;
  external_calcium: number | null;
  calcium_pct_diff: number | null;
}

export type GapSeverity = "blocker" | "major" | "minor" | "parked";

export interface Gap {
  gap_id: string;
  severity: GapSeverity;
  area: string;
  source_table: string | null;
  source_column: string | null;
  description: string;
  affected_rows: number | null;
  measured_by: "seed" | "importer";
  ui_behaviour: string;
  resolution_path: string;
  measured_at: string | null;
}

export interface ImportTableStat {
  table_name: string;
  rows_read: number;
  rows_written: number;
  rows_skipped: number;
  content_hash: string;
}

export interface ImportRun {
  run_id: number;
  started_at: string;
  finished_at: string | null;
  source_dir: string;
  ok: boolean;
  tables: ImportTableStat[];
}

export interface Region {
  region_culture: string;
  country: string;
  focus_tier: number;
  rank_weight: number;
  enrichment_scope: boolean;
  rationale: string;
}

export interface Cuisine {
  culture_code: string;
  cuisine_cluster: string;
  country: string;
  state_province: string | null;
  region_culture: string;
  focus_tier: number;
  rank_weight: number;
  recipe_count: number;
}

export interface NutritionTarget {
  target_code: string;
  target_name: string;
  target_category: string | null;
  age_from_months: number;
  age_to_months: number;
  trigger_input: string | null;
  trigger_logic: string | null;
  recipe_score_energy: number;
  recipe_score_protein: number;
  recipe_score_iron: number;
  recipe_score_calcium: number;
  recipe_score_fruitveg: number;
  recipe_score_diversity: number;
  recipe_score_cost: number;
  hard_exclusions: string | null;
  soft_penalties: string | null;
}
```

- [ ] **Step 2: Write `web/src/lib/api.ts`**

```ts
import type {
  ChildProfile, EngineResult, RecipeDetail, Ingredient, NutritionDiscrepancy,
  Gap, ImportRun, Region, Cuisine, NutritionTarget,
} from "./types";

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, body.error ?? res.statusText);
  }
  return res.json() as Promise<T>;
}

export function search(profile: ChildProfile): Promise<EngineResult> {
  return request<EngineResult>("/api/search", { method: "POST", body: JSON.stringify(profile) });
}

export function getRecipe(recipeId: string): Promise<RecipeDetail> {
  return request<RecipeDetail>(`/api/recipes/${encodeURIComponent(recipeId)}`);
}

export function listIngredients(limit = 100): Promise<Ingredient[]> {
  return request<Ingredient[]>(`/api/ingredients?limit=${limit}`);
}

export function getNutritionAudit(): Promise<NutritionDiscrepancy[]> {
  return request<NutritionDiscrepancy[]>("/api/audit/nutrition");
}

export function getGaps(): Promise<Gap[]> {
  return request<Gap[]>("/api/gaps");
}

export function getRuns(): Promise<ImportRun[]> {
  return request<ImportRun[]>("/api/runs");
}

export function getRegions(): Promise<Region[]> {
  return request<Region[]>("/api/reference/regions");
}

export function getCuisines(): Promise<Cuisine[]> {
  return request<Cuisine[]>("/api/reference/cuisines");
}

export function getNutritionTargets(): Promise<NutritionTarget[]> {
  return request<NutritionTarget[]>("/api/reference/nutrition-targets");
}

export { ApiError };
```

- [ ] **Step 3: Write `web/.env.local.example`**

```
NEXT_PUBLIC_API_URL=http://localhost:8080
```

- [ ] **Step 4: Verify it compiles**

```bash
cd web && pnpm exec tsc --noEmit
```

Expected: no type errors (this file has no runtime to test yet — Task 6 exercises it against
a live server).

- [ ] **Step 5: Commit**

```bash
cd /home/ghoul/graveyard/recipie
git add web/src/lib web/.env.local.example
git commit -m "Add typed API client mirroring the Go models"
```

---

### Task 4: Provenance chip — the signature component

**Files:**
- Create: `web/src/components/provenance-chip.tsx`
- Test: `web/src/components/provenance-chip.test.tsx`

**Interfaces:**
- Consumes: nothing external.
- Produces: `<ProvenanceChip>` — imported by every screen from Task 6 onward that renders a
  derived or source-attributed value.

- [ ] **Step 1: Add shadcn primitives this component wraps**

```bash
cd web && npx shadcn@latest add badge tooltip hover-card
```

- [ ] **Step 2: Write `web/src/components/provenance-chip.tsx`**

```tsx
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
          {confidence !== undefined && ` · ${(confidence * 100).toFixed(0)}%`}
        </Badge>
      </HoverCardTrigger>
      <HoverCardContent className="w-80 font-mono text-xs">
        {explanation}
      </HoverCardContent>
    </HoverCard>
  );
}
```

- [ ] **Step 3: Write the test**

```bash
cd web && pnpm add -D @testing-library/react @testing-library/jest-dom vitest jsdom
```

```tsx
// web/src/components/provenance-chip.test.tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ProvenanceChip } from "./provenance-chip";

describe("ProvenanceChip", () => {
  it("renders the source and confidence", () => {
    render(<ProvenanceChip source="ifct" confidence={0.92} explanation="IFCT 2017 exact match" />);
    expect(screen.getByText(/ifct/i)).toBeInTheDocument();
    expect(screen.getByText(/92%/)).toBeInTheDocument();
  });

  it("renders without a confidence badge when none is given", () => {
    render(<ProvenanceChip source="provider" explanation="Provider group-level placeholder" />);
    expect(screen.getByText(/provider/i)).toBeInTheDocument();
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });
});
```

Add a minimal `web/vitest.config.ts` if the scaffold didn't create one:

```ts
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: { environment: "jsdom", globals: true },
});
```

- [ ] **Step 4: Run the test**

```bash
cd web && pnpm add -D @vitejs/plugin-react && pnpm exec vitest run
```

Expected: PASS, both cases.

- [ ] **Step 5: Commit**

```bash
cd /home/ghoul/graveyard/recipie
git add web/src/components/provenance-chip.tsx web/src/components/provenance-chip.test.tsx web/vitest.config.ts web/package.json web/pnpm-lock.yaml
git commit -m "Add the provenance chip signature component"
```

---

### Task 5: App shell — sidebar nav, command palette, providers

**Files:**
- Create: `web/src/components/app-sidebar.tsx`
- Create: `web/src/components/command-palette.tsx`
- Modify: `web/src/app/layout.tsx`

**Interfaces:**
- Produces: every route from Task 6 onward renders inside this shell. `Cmd+K` opens the
  command palette from anywhere in the app.

- [ ] **Step 1: Add shadcn primitives**

```bash
cd web && npx shadcn@latest add sidebar command dialog tooltip separator
```

- [ ] **Step 2: Write `web/src/components/app-sidebar.tsx`**

```tsx
"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Sidebar, SidebarContent, SidebarGroup, SidebarGroupContent, SidebarGroupLabel,
  SidebarMenu, SidebarMenuButton, SidebarMenuItem,
} from "@/components/ui/sidebar";

// The 7 routes from CLAUDE.md's "Screens" table, in the order an operator actually
// works through them: search first, then per-recipe detail, then the audit/reference
// screens a specific query rarely needs but a reviewer does.
const routes = [
  { href: "/", label: "Engine console" },
  { href: "/ingredients", label: "Ingredients" },
  { href: "/audit/nutrition", label: "Nutrition audit" },
  { href: "/audit/gaps", label: "Gap register" },
  { href: "/runs", label: "Import runs" },
  { href: "/reference", label: "Reference" },
];

export function AppSidebar() {
  const pathname = usePathname();
  return (
    <Sidebar collapsible="icon">
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel className="font-mono text-xs">MadamGY</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {routes.map((r) => (
                <SidebarMenuItem key={r.href}>
                  <SidebarMenuButton asChild isActive={pathname === r.href}>
                    <Link href={r.href}>{r.label}</Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}
```

(`/recipe/[id]` is deliberately absent from the sidebar — it's only reached by clicking a
result row, per CLAUDE.md's route table; it is not a top-level destination.)

- [ ] **Step 3: Write `web/src/components/command-palette.tsx`**

```tsx
"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList,
} from "@/components/ui/command";

const destinations = [
  { label: "Engine console", href: "/" },
  { label: "Ingredients", href: "/ingredients" },
  { label: "Nutrition audit", href: "/audit/nutrition" },
  { label: "Gap register", href: "/audit/gaps" },
  { label: "Import runs", href: "/runs" },
  { label: "Reference", href: "/reference" },
];

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const router = useRouter();

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((v) => !v);
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, []);

  return (
    <CommandDialog open={open} onOpenChange={setOpen}>
      <CommandInput placeholder="Jump to a screen, or paste a recipe / ingredient ID..." />
      <CommandList>
        <CommandEmpty>No match.</CommandEmpty>
        <CommandGroup heading="Screens">
          {destinations.map((d) => (
            <CommandItem key={d.href} onSelect={() => { setOpen(false); router.push(d.href); }}>
              {d.label}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
```

(ID lookup — jumping straight to `/recipe/MG-R-00042` from the palette — is deferred to
Task 6, once the recipe detail route exists to jump to; this task only wires the keyboard
shortcut and screen navigation.)

- [ ] **Step 4: Wire the shell into `layout.tsx`**

```tsx
// web/src/app/layout.tsx
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AppSidebar } from "@/components/app-sidebar";
import { CommandPalette } from "@/components/command-palette";

// ...inside <ThemeProvider>...
<ThemeProvider>
  <TooltipProvider>
    <SidebarProvider>
      <AppSidebar />
      <main className="flex-1 overflow-x-auto p-6">{children}</main>
      <CommandPalette />
    </SidebarProvider>
  </TooltipProvider>
</ThemeProvider>
```

- [ ] **Step 5: Verify in the browser**

```bash
pnpm dev
```

Confirm the sidebar renders with all 6 top-level links, and `Cmd+K` (or `Ctrl+K`) opens the
command palette and navigates on selection.

- [ ] **Step 6: Commit**

```bash
cd /home/ghoul/graveyard/recipie
git add web/src/components web/src/app/layout.tsx
git commit -m "Add app shell: sidebar navigation and command palette"
```

---

### Task 6: `/` engine console — profile form and ranked results table

**Files:**
- Create: `web/src/app/page.tsx`
- Create: `web/src/components/profile-form.tsx`
- Create: `web/src/components/results-table.tsx`
- Create: `web/src/components/why-this-result-sheet.tsx`

**Interfaces:**
- Consumes: `search()` from `api.ts`, `ChildProfile`/`EngineResult` types, `<ProvenanceChip>`.

- [ ] **Step 1: Add shadcn primitives**

```bash
cd web && npx shadcn@latest add form select slider table sheet skeleton alert button input
```

- [ ] **Step 2: Write `web/src/components/profile-form.tsx`**

```tsx
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
```

- [ ] **Step 3: Write `web/src/components/why-this-result-sheet.tsx`**

```tsx
import {
  Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger,
} from "@/components/ui/sheet";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { EngineResult } from "@/lib/types";

/**
 * CLAUDE.md: "The single most valuable screen in an internal tool." Shows every one of
 * the engine's steps, how many candidates each removed, and why -- this is the answer
 * to "where did recipe X go" without anyone needing a SQL client.
 */
export function WhyThisResultSheet({ result }: { result: EngineResult }) {
  return (
    <Sheet>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm">Why this result</Button>
      </SheetTrigger>
      <SheetContent side="right" className="w-full sm:max-w-xl overflow-y-auto">
        <SheetHeader>
          <SheetTitle className="font-mono">
            Target: {result.active_target}
          </SheetTitle>
          <p className="text-xs text-muted-foreground">{result.target_reason}</p>
        </SheetHeader>
        <div className="space-y-3 p-4">
          {result.blocked && (
            <div className="rounded border border-destructive p-3 text-sm text-destructive">
              Blocked: {result.block_reason}
            </div>
          )}
          {result.steps.map((s) => (
            <div key={s.step} className="border-b pb-2 font-mono text-xs">
              <div className="flex items-center justify-between">
                <span className="font-semibold">{s.step}. {s.name}</span>
                <Badge variant="outline">{s.kind}</Badge>
              </div>
              <div className="text-muted-foreground">
                {s.candidates_in >= 0 ? s.candidates_in : "?"} in -&gt; {s.candidates_out} out
              </div>
              {s.note && <div className="mt-1 text-muted-foreground">{s.note}</div>}
            </div>
          ))}
        </div>
      </SheetContent>
    </Sheet>
  );
}
```

- [ ] **Step 4: Write `web/src/components/results-table.tsx`**

```tsx
"use client";

import Link from "next/link";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import { ProvenanceChip } from "@/components/provenance-chip";
import type { RankedRecipe } from "@/lib/types";

export function ResultsTable({ recipes }: { recipes: RankedRecipe[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Recipe</TableHead>
          <TableHead>Region</TableHead>
          <TableHead>Meal</TableHead>
          <TableHead>Clinical tag</TableHead>
          <TableHead className="text-right">Score</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {recipes.map((r) => (
          <TableRow key={r.recipe_id}>
            <TableCell>
              <Link href={`/recipe/${r.recipe_id}`} className="font-mono text-xs underline underline-offset-2">
                {r.recipe_id}
              </Link>
              <div className="text-sm">{r.recipe_name}</div>
            </TableCell>
            <TableCell className="text-xs">{r.region_culture}</TableCell>
            <TableCell className="text-xs">{r.meal_type}</TableCell>
            <TableCell className="text-xs">{r.clinical_tag}</TableCell>
            <TableCell className="text-right">
              <div className="flex items-center justify-end gap-2">
                <span className="font-mono text-xs">{r.ranked_score.toFixed(3)}</span>
                <ProvenanceChip
                  source="derived"
                  explanation={`Scored axes: ${r.scored_axes}. sum(weight * normalised axis) / sum(weight), normalised within the ${r.age_group} age band.`}
                />
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
```

- [ ] **Step 5: Write `web/src/app/page.tsx`**

```tsx
"use client";

import { useState } from "react";
import { search, ApiError } from "@/lib/api";
import type { ChildProfile, EngineResult } from "@/lib/types";
import { ProfileForm } from "@/components/profile-form";
import { ResultsTable } from "@/components/results-table";
import { WhyThisResultSheet } from "@/components/why-this-result-sheet";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";

export default function EngineConsolePage() {
  const [result, setResult] = useState<EngineResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSearch(profile: ChildProfile) {
    setLoading(true);
    setError(null);
    try {
      setResult(await search(profile));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "The search failed. Check that the API server is running.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-[320px_1fr]">
      <aside>
        <ProfileForm onSubmit={handleSearch} loading={loading} />
      </aside>

      <section>
        {error && (
          <Alert variant="destructive" className="mb-4">
            <AlertTitle>Search failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {loading && (
          <div className="space-y-2">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        )}

        {!loading && result?.blocked && (
          <Alert variant="destructive">
            <AlertTitle>Automated recipe generation blocked</AlertTitle>
            <AlertDescription>{result.block_reason}</AlertDescription>
          </Alert>
        )}

        {!loading && result && !result.blocked && result.recipes.length === 0 && (
          <Alert>
            <AlertTitle>No recipes matched</AlertTitle>
            <AlertDescription>
              Every step is recorded in "Why this result" -- open it to see which step
              emptied the pool.
            </AlertDescription>
          </Alert>
        )}

        {!loading && result && !result.blocked && result.recipes.length > 0 && (
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="font-mono text-xs text-muted-foreground">
                {result.recipes.length} recipes, target {result.active_target}
              </span>
              <WhyThisResultSheet result={result} />
            </div>
            <ResultsTable recipes={result.recipes} />
          </div>
        )}

        {!loading && !result && !error && (
          <p className="text-sm text-muted-foreground">
            Enter a child profile and search. Age is the only required field.
          </p>
        )}
      </section>
    </div>
  );
}
```

- [ ] **Step 6: Manual browser verification (golden path + edge cases)**

Requires `go run ./cmd/server` running against the imported dev database (backend plan Task
12). With `pnpm dev` also running:

1. Submit age-only (24 months) — confirm a non-empty table renders and "Why this result"
   lists all 13 steps.
2. Add allergen "Peanut" — confirm the result count drops and no result row's recipe, when
   opened via `getRecipe`, contains peanut in its ingredients (spot-check one).
3. Set a clinical flag that maps to an escalation-only domain is not reachable from this
   form yet (clinical_flags isn't wired to the UI in this task — confirm the blocked-state
   `Alert` renders correctly by testing it directly against the API with `curl` posting
   `{"age_months":36,"clinical_flags":{"CKD":"Yes"}}` and confirming the UI would render
   `result.blocked === true` correctly if that response reached it. Wiring a clinical-flags
   input to the form is out of scope for this task — flag it as a follow-up, don't silently
   drop it).
4. Submit with no age — confirm the Search button stays disabled rather than sending an
   invalid request.

- [ ] **Step 7: Commit**

```bash
git add web/src/app/page.tsx web/src/components/profile-form.tsx web/src/components/results-table.tsx web/src/components/why-this-result-sheet.tsx
git commit -m "Build the engine console: profile form, ranked results, why-this-result panel"
```

---

### Task 7: `/recipe/[id]` — method and nutrition detail

**Files:**
- Create: `web/src/app/recipe/[id]/page.tsx`

**Interfaces:**
- Consumes: `getRecipe()`, `RecipeDetail` type.

- [ ] **Step 1: Add shadcn primitives**

```bash
cd web && npx shadcn@latest add tabs separator
```

- [ ] **Step 2: Write `web/src/app/recipe/[id]/page.tsx`**

```tsx
import { notFound } from "next/navigation";
import { getRecipe, ApiError } from "@/lib/api";
import { ProvenanceChip } from "@/components/provenance-chip";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

export default async function RecipeDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  let detail;
  try {
    detail = await getRecipe(id);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }

  const { method, nutrition } = detail;

  return (
    <div className="max-w-3xl space-y-6">
      <div>
        <h1 className="font-mono text-lg">{method.recipe_id}</h1>
        <p className="text-sm text-muted-foreground">{method.recipe_name} &middot; {method.region_culture}</p>
        <Badge variant="outline" className="mt-1">{method.provider_review_status}</Badge>
      </div>

      <Separator />

      <Tabs defaultValue="provider">
        <TabsList>
          <TabsTrigger value="provider">Provider method</TabsTrigger>
          <TabsTrigger value="suggested" disabled={!method.suggested_method_external}>
            Suggested method {method.suggested_method_external ? "" : "(none)"}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="provider" className="whitespace-pre-wrap text-sm">
          {method.provider_method}
        </TabsContent>
        <TabsContent value="suggested" className="space-y-2 text-sm">
          {method.suggested_method_external && (
            <>
              <ProvenanceChip
                source={method.suggested_method_source ?? "external"}
                confidence={method.suggested_method_confidence ?? undefined}
                explanation={method.suggestion_disclosure}
              />
              <p className="whitespace-pre-wrap">{method.suggested_method_external}</p>
              {method.suggested_method_url && (
                <a href={method.suggested_method_url} target="_blank" rel="noreferrer" className="text-xs underline">
                  Source
                </a>
              )}
            </>
          )}
        </TabsContent>
      </Tabs>

      <Separator />

      <section>
        <h2 className="mb-2 font-mono text-sm">Nutrition, provider vs. recomputed</h2>
        <ProvenanceChip
          source={nutrition.fully_verified ? "ifct" : "provider"}
          confidence={nutrition.ingredient_coverage}
          explanation={`${nutrition.formula}. ${(nutrition.ingredient_coverage * 100).toFixed(0)}% of recipe mass is IFCT-backed.`}
        />
        <table className="mt-2 w-full text-right font-mono text-xs">
          <thead>
            <tr className="text-left text-muted-foreground">
              <th className="text-left">Field</th>
              <th>Provider</th>
              <th>Recomputed</th>
              <th>Diff</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td className="text-left">Energy (kcal)</td>
              <td>{nutrition.provider_energy_kcal}</td>
              <td>{nutrition.energy_kcal}</td>
              <td>{nutrition.energy_pct_diff !== null ? `${nutrition.energy_pct_diff}%` : "not available"}</td>
            </tr>
            <tr>
              <td className="text-left">Iron (mg)</td>
              <td>{nutrition.provider_iron_mg}</td>
              <td>{nutrition.iron_mg}</td>
              <td>{nutrition.iron_pct_diff !== null ? `${nutrition.iron_pct_diff}%` : "not available"}</td>
            </tr>
            <tr>
              <td className="text-left">Protein (g)</td>
              <td>{nutrition.provider_protein_g}</td>
              <td>{nutrition.protein_g}</td>
              <td>not available</td>
            </tr>
            <tr>
              <td className="text-left">Calcium (mg)</td>
              <td>{nutrition.provider_calcium_mg}</td>
              <td>{nutrition.calcium_mg}</td>
              <td>not available</td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
  );
}
```

(`not available` is used verbatim for the two diffs the API doesn't compute — protein and
calcium pct_diff aren't columns on `recipe_nutrition_recomputed` per the backend plan's
Task 8; showing a dash or `0` there would misrepresent an uncomputed value as a computed
zero, which the "No client-side invention" rule forbids.)

- [ ] **Step 3: Verify in the browser**

With both servers running, click a result row from `/` and confirm the detail page loads,
the provider/suggested tabs switch correctly, and a recipe with no `recipe_method_external`
row shows a disabled "Suggested method (none)" tab rather than an empty one.

- [ ] **Step 4: Commit**

```bash
git add web/src/app/recipe
git commit -m "Build the recipe detail screen"
```

---

### Task 8: `/ingredients` — provider vs. IFCT-corrected table

**Files:**
- Create: `web/src/app/ingredients/page.tsx`

**Interfaces:**
- Consumes: `listIngredients()`, `Ingredient` type.

- [ ] **Step 1: Write `web/src/app/ingredients/page.tsx`**

```tsx
import { listIngredients } from "@/lib/api";
import { ProvenanceChip } from "@/components/provenance-chip";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";

export default async function IngredientsPage() {
  const ingredients = await listIngredients(200);

  return (
    <div>
      <h1 className="mb-4 font-mono text-lg">Ingredients ({ingredients.length})</h1>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>Name</TableHead>
            <TableHead>Food group</TableHead>
            <TableHead className="text-right">Energy (kcal/100g)</TableHead>
            <TableHead className="text-right">Iron (mg/100g)</TableHead>
            <TableHead>Source</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {ingredients.map((i) => (
            <TableRow key={i.ingredient_id}>
              <TableCell className="font-mono text-xs">{i.ingredient_id}</TableCell>
              <TableCell>{i.english_name}</TableCell>
              <TableCell className="text-xs">{i.food_group}</TableCell>
              <TableCell className="text-right font-mono text-xs">
                {i.energy_kcal_100g}
                {!i.verified && (
                  <span className="ml-1 text-muted-foreground">(provider: {i.provider_energy_kcal_100g})</span>
                )}
              </TableCell>
              <TableCell className="text-right font-mono text-xs">
                {i.iron_mg_100g}
                {!i.verified && (
                  <span className="ml-1 text-muted-foreground">(provider: {i.provider_iron_mg_100g})</span>
                )}
              </TableCell>
              <TableCell>
                <ProvenanceChip
                  source={i.value_source}
                  explanation={
                    i.verified
                      ? `IFCT 2017 match: ${i.ifct_food_name} (${i.ifct_match_exactness}, resolved by ${i.ifct_resolved_by}).`
                      : `No IFCT counterpart identified. Provider value stands, review status: ${i.provider_review_status}, data quality: ${i.provider_data_quality}.`
                  }
                />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
```

- [ ] **Step 2: Verify in the browser**

Confirm the table shows 200 rows, and every unverified row displays its provider value
inline next to the corrected value rather than hiding it — an operator must be able to see
both numbers without a second click.

- [ ] **Step 3: Commit**

```bash
git add web/src/app/ingredients
git commit -m "Build the ingredients screen with provider/IFCT values side by side"
```

---

### Task 9: `/audit/nutrition` and `/audit/gaps`

**Files:**
- Create: `web/src/app/audit/nutrition/page.tsx`
- Create: `web/src/app/audit/gaps/page.tsx`

**Interfaces:**
- Consumes: `getNutritionAudit()`, `getGaps()`, `NutritionDiscrepancy`/`Gap` types.

- [ ] **Step 1: Write `web/src/app/audit/nutrition/page.tsx`**

```tsx
import { getNutritionAudit } from "@/lib/api";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export default async function NutritionAuditPage() {
  const rows = await getNutritionAudit();

  return (
    <div>
      <h1 className="mb-1 font-mono text-lg">Nutrition discrepancy report</h1>
      <p className="mb-4 text-xs text-muted-foreground">
        Confirmed exact-name matches where the provider disagrees with IFCT 2017 by more
        than 20%. This is the list to hand the provider -- nothing here is corrected
        locally.
      </p>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Ingredient</TableHead>
            <TableHead>Matched IFCT food</TableHead>
            <TableHead className="text-right">Used in</TableHead>
            <TableHead className="text-right">Provider kcal</TableHead>
            <TableHead className="text-right">IFCT kcal</TableHead>
            <TableHead className="text-right">Diff</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((r) => (
            <TableRow key={r.ingredient_id}>
              <TableCell>{r.english_name}</TableCell>
              <TableCell className="text-xs">{r.matched_ifct_food ?? "not available"}</TableCell>
              <TableCell className="text-right font-mono text-xs">{r.used_in_recipes}</TableCell>
              <TableCell className="text-right font-mono text-xs">{r.provider_energy ?? "not available"}</TableCell>
              <TableCell className="text-right font-mono text-xs">{r.external_energy ?? "not available"}</TableCell>
              <TableCell className="text-right font-mono text-xs font-semibold">
                {r.energy_pct_diff !== null ? `${r.energy_pct_diff > 0 ? "+" : ""}${r.energy_pct_diff}%` : "not available"}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
```

(CSV export, listed under "Primary shadcn parts" for this route in CLAUDE.md, is a
follow-up: it needs no new data, just a client-side "Export" button serializing the
already-fetched `rows` — deliberately deferred so this task stays focused on rendering the
report correctly first.)

- [ ] **Step 2: Write `web/src/app/audit/gaps/page.tsx`**

```bash
cd web && npx shadcn@latest add accordion
```

```tsx
import { getGaps } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import type { GapSeverity } from "@/lib/types";

const severityOrder: GapSeverity[] = ["blocker", "major", "minor", "parked"];

const severityColor: Record<GapSeverity, string> = {
  blocker: "border-destructive text-destructive",
  major: "border-[var(--color-unverified)] text-[var(--color-unverified)]",
  minor: "border-muted-foreground text-muted-foreground",
  parked: "border-muted-foreground text-muted-foreground opacity-60",
};

export default async function GapsPage() {
  const gaps = await getGaps();

  return (
    <div>
      <h1 className="mb-4 font-mono text-lg">Gap register ({gaps.length})</h1>
      {severityOrder.map((severity) => {
        const inSeverity = gaps.filter((g) => g.severity === severity);
        if (inSeverity.length === 0) return null;
        return (
          <div key={severity} className="mb-6">
            <h2 className="mb-2 font-mono text-xs uppercase text-muted-foreground">{severity} ({inSeverity.length})</h2>
            <Accordion type="multiple">
              {inSeverity.map((g) => (
                <AccordionItem key={g.gap_id} value={g.gap_id}>
                  <AccordionTrigger className="font-mono text-sm">
                    <span className="flex items-center gap-2">
                      <Badge variant="outline" className={severityColor[g.severity]}>{g.gap_id}</Badge>
                      {g.area}
                      {g.affected_rows !== null && (
                        <span className="text-xs text-muted-foreground">({g.affected_rows} rows)</span>
                      )}
                    </span>
                  </AccordionTrigger>
                  <AccordionContent className="space-y-2 text-sm">
                    <p>{g.description}</p>
                    <p className="text-xs text-muted-foreground">UI behaviour: {g.ui_behaviour}</p>
                    <p className="text-xs text-muted-foreground">Resolution: {g.resolution_path}</p>
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          </div>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 3: Verify in the browser**

Confirm both screens load, the gap register groups by severity with blockers first, and
every accordion item expands to show its `ui_behaviour` and `resolution_path` text verbatim
from the API (not paraphrased client-side).

- [ ] **Step 4: Commit**

```bash
git add web/src/app/audit
git commit -m "Build the nutrition audit and gap register screens"
```

---

### Task 10: `/runs` and `/reference`

**Files:**
- Create: `web/src/app/runs/page.tsx`
- Create: `web/src/app/reference/page.tsx`

**Interfaces:**
- Consumes: `getRuns()`, `getRegions()`, `getCuisines()`, `getNutritionTargets()`.

- [ ] **Step 1: Write `web/src/app/runs/page.tsx`**

```bash
cd web && npx shadcn@latest add collapsible
```

```tsx
import { getRuns } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";

export default async function RunsPage() {
  const runs = await getRuns();

  return (
    <div>
      <h1 className="mb-4 font-mono text-lg">Import runs</h1>
      <div className="space-y-4">
        {runs.map((run) => (
          <Collapsible key={run.run_id} className="rounded border p-3">
            <CollapsibleTrigger className="flex w-full items-center justify-between font-mono text-sm">
              <span>
                Run {run.run_id} &middot; {run.started_at}
                {run.ok ? (
                  <Badge variant="outline" className="ml-2 border-[var(--color-verified)] text-[var(--color-verified)]">ok</Badge>
                ) : (
                  <Badge variant="outline" className="ml-2 border-destructive text-destructive">failed</Badge>
                )}
              </span>
              <span className="text-xs text-muted-foreground">{run.tables.length} tables</span>
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-2 space-y-1 font-mono text-xs">
              {run.tables.map((t) => (
                <div key={t.table_name} className="flex justify-between border-t pt-1">
                  <span>{t.table_name}</span>
                  <span className="text-muted-foreground">
                    read {t.rows_read} / written {t.rows_written} / skipped {t.rows_skipped}
                  </span>
                  <span className="truncate text-muted-foreground" title={t.content_hash}>
                    {t.content_hash.slice(0, 12)}
                  </span>
                </div>
              ))}
            </CollapsibleContent>
          </Collapsible>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Write `web/src/app/reference/page.tsx`**

```tsx
import { getRegions, getCuisines, getNutritionTargets } from "@/lib/api";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export default async function ReferencePage() {
  const [regions, cuisines, targets] = await Promise.all([
    getRegions(), getCuisines(), getNutritionTargets(),
  ]);

  return (
    <Tabs defaultValue="regions">
      <TabsList>
        <TabsTrigger value="regions">Regions</TabsTrigger>
        <TabsTrigger value="cuisines">Cuisines</TabsTrigger>
        <TabsTrigger value="targets">Nutrition targets</TabsTrigger>
      </TabsList>

      <TabsContent value="regions">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Region</TableHead><TableHead>Country</TableHead>
              <TableHead className="text-right">Tier</TableHead>
              <TableHead className="text-right">rank_weight</TableHead>
              <TableHead>Rationale</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {regions.map((r) => (
              <TableRow key={r.region_culture}>
                <TableCell>{r.region_culture}</TableCell>
                <TableCell>{r.country}</TableCell>
                <TableCell className="text-right font-mono text-xs">{r.focus_tier}</TableCell>
                <TableCell className="text-right font-mono text-xs">{r.rank_weight.toFixed(2)}</TableCell>
                <TableCell className="text-xs text-muted-foreground">{r.rationale}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TabsContent>

      <TabsContent value="cuisines">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Cuisine cluster</TableHead><TableHead>Region</TableHead>
              <TableHead className="text-right">Recipes</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cuisines.map((c) => (
              <TableRow key={c.culture_code}>
                <TableCell>{c.cuisine_cluster}</TableCell>
                <TableCell className="text-xs">{c.region_culture}</TableCell>
                <TableCell className="text-right font-mono text-xs">{c.recipe_count}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TabsContent>

      <TabsContent value="targets">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Code</TableHead><TableHead>Name</TableHead>
              <TableHead className="text-right">E</TableHead><TableHead className="text-right">P</TableHead>
              <TableHead className="text-right">Fe</TableHead><TableHead className="text-right">Ca</TableHead>
              <TableHead className="text-right">FV</TableHead><TableHead className="text-right">Div</TableHead>
              <TableHead className="text-right">Cost</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {targets.map((t) => (
              <TableRow key={t.target_code}>
                <TableCell className="font-mono text-xs">{t.target_code}</TableCell>
                <TableCell className="text-xs">{t.target_name}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_energy}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_protein}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_iron}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_calcium}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_fruitveg}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_diversity}</TableCell>
                <TableCell className="text-right font-mono text-xs">{t.recipe_score_cost}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TabsContent>
    </Tabs>
  );
}
```

- [ ] **Step 3: Verify in the browser**

Confirm all three tabs render, the cuisines tab shows only cuisines with `recipe_count > 0`
(already guaranteed server-side by `cuisine_option`, but visually confirm no zero appears),
and the nutrition targets tab shows all 13 rows NT00-NT12.

- [ ] **Step 4: Commit**

```bash
git add web/src/app/runs web/src/app/reference
git commit -m "Build the import runs and reference screens"
```

---

### Task 11: Empty, loading and error states across every screen

**Files:**
- Modify: `web/src/app/ingredients/page.tsx`
- Modify: `web/src/app/audit/nutrition/page.tsx`
- Modify: `web/src/app/audit/gaps/page.tsx`
- Modify: `web/src/app/runs/page.tsx`
- Modify: `web/src/app/reference/page.tsx`
- Create: `web/src/app/error.tsx`
- Create: `web/src/app/ingredients/loading.tsx` (and one `loading.tsx` per server-fetched route)

**Interfaces:**
- Consumes: existing pages from Tasks 8-10; adds Next.js's `loading.tsx`/`error.tsx`
  file-based conventions per route segment.

- [ ] **Step 1: Write `web/src/app/error.tsx`**

```tsx
"use client";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

export default function GlobalError({ error, reset }: { error: Error; reset: () => void }) {
  return (
    <Alert variant="destructive" className="m-6">
      <AlertTitle>Something failed</AlertTitle>
      <AlertDescription className="space-y-2">
        <p className="font-mono text-xs">{error.message}</p>
        <p>
          If this is a connection error, confirm the API server is running
          (`go run ./cmd/server`) and `NEXT_PUBLIC_API_URL` in `.env.local` points at it.
        </p>
        <Button size="sm" onClick={reset}>Retry</Button>
      </AlertDescription>
    </Alert>
  );
}
```

- [ ] **Step 2: Add a `loading.tsx` for each server-fetched route**

```tsx
// web/src/app/ingredients/loading.tsx (identical pattern for audit/nutrition, audit/gaps,
// runs, reference -- one file per directory, each importing Skeleton)
import { Skeleton } from "@/components/ui/skeleton";

export default function Loading() {
  return (
    <div className="space-y-2">
      {Array.from({ length: 10 }).map((_, i) => <Skeleton key={i} className="h-8 w-full" />)}
    </div>
  );
}
```

Duplicate this file (same content) at:
`web/src/app/audit/nutrition/loading.tsx`, `web/src/app/audit/gaps/loading.tsx`,
`web/src/app/runs/loading.tsx`, `web/src/app/reference/loading.tsx`.

- [ ] **Step 3: Add empty states to each table page**

For each page from Tasks 8-10, wrap the table render in a length check. Example for
`web/src/app/ingredients/page.tsx`:

```tsx
{ingredients.length === 0 ? (
  <Alert>
    <AlertTitle>No ingredients loaded</AlertTitle>
    <AlertDescription>
      The database has no rows in ingredient_nutrition_corrected. Run `go run ./cmd/import`
      against a configured DATABASE_URL, then reload.
    </AlertDescription>
  </Alert>
) : (
  <Table>{/* ...existing table... */}</Table>
)}
```

Apply the same pattern (empty-state `Alert` naming which command fixes it) to
`audit/nutrition`, `audit/gaps`, `runs`, and `reference`'s three tabs.

- [ ] **Step 4: Verify by stopping the API server and reloading each page**

With `pnpm dev` running and the Go server stopped, load each of the 6 routes and confirm
`error.tsx` renders the connection-error message rather than a blank page or an unhandled
Next.js error overlay.

- [ ] **Step 5: Commit**

```bash
git add web/src/app
git commit -m "Add loading, error, and empty states to every data screen"
```

---

### Task 12: Final verification pass

**Files:** none (verification only).

- [ ] **Step 1: Full stack smoke test**

```bash
# terminal 1
cd /home/ghoul/graveyard/recipie
scripts/dev_db.fish up
set -x DATABASE_URL (scripts/dev_db.fish url)
go run ./cmd/import && go run ./cmd/enrich
go run ./cmd/server

# terminal 2
cd /home/ghoul/graveyard/recipie/web
cp .env.local.example .env.local
pnpm dev
```

- [ ] **Step 2: Click through the golden path**

1. `/` — search age 8, diet Vegetarian, allergen Milk, marker iron_deficiency. Confirm
   results, open "Why this result", confirm 13 steps listed with real in/out counts.
2. Click a result row -> `/recipe/[id]` — confirm provider method renders; if the recipe
   has a suggested method (166 of 940 do), confirm the tab is enabled and shows a
   `ProvenanceChip` with a real confidence percentage.
3. `/ingredients` — confirm ~139 rows show a green `ifct` chip and ~267 show an amber
   `provider` chip with the provider figure visible inline.
4. `/audit/nutrition` — confirm the brinjal-vs-egg-adjacent finding (or whatever the top
   row is post-correction) appears with a real `+`/`-` percentage.
5. `/audit/gaps` — confirm 20 gaps (12 seeded, 4 from cmd/enrich, 4 from migration 0012), grouped by severity, blockers first.
6. `/runs` — confirm at least one run with per-table content hashes.
7. `/reference` — confirm all 3 tabs populate, cuisines tab has zero rows with
   `recipe_count = 0`.
8. Toggle dark/light — confirm every screen re-themes correctly, no hardcoded colors.
9. `Cmd+K` from every screen — confirm the palette opens and navigates.

- [ ] **Step 3: Report deferred items back to the user**

This plan deliberately deferred two things flagged inline rather than silently dropped:
- CSV export on `/audit/nutrition` (Task 9).
- Wiring `clinical_flags` into the profile form so an operator can trigger the step-3
  escalation path from the UI, not just via `curl` (Task 6, Step 6).

Both are small, scoped follow-ups once this plan lands — call them out to the user rather
than starting them unprompted.
