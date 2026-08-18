# Orphans and Special Care Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make three already-built features reachable, then import the provider's
special-care condition master and turn its six STOP-REVIEW conditions into a real engine
stop gate.

**Architecture:** Four of the seven tasks are wiring: code that exists, is tested, and has
no caller. The remaining three add the fourteenth provider workbook as nine new tables,
then reuse the engine's existing clinical escalation path so a special-care diagnosis
blocks generation instead of ranking a result list. Nothing here marks provider data
approved, and no special-care recipe candidate is ever served.

**Tech Stack:** Go 1.25 (chi, pgx/v5, golang-migrate), Postgres 16, Next.js App Router +
React + Tailwind v4 + shadcn/ui, vitest + testing-library.

**Spec:** There is no formal design doc for this work. The binding authorities are
`docs/next-steps.md` (sequence and rationale) and the provider workbook
`data/provider/MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx`
(its `README & Engine Logic`, `Condition Stop Gates` and `Output Rule Matrix` sheets).
Where this plan and `docs/next-steps.md` disagree, the workbook wins, because it is
provider-authored and the doc is this project's own argument.

## Global Constraints

Copied verbatim from `CLAUDE.md`. Every task's requirements implicitly include these.

- **Never invent data.** "Every value that reaches a user must trace to a verified source."
  A verified source is the provider's workbooks, a named external dataset with row id and
  URL, or a documented computation over those, labelled `derived`.
- **When data is missing, the correct output is an explicit gap** - `null`, "not
  available", a disabled filter option, or a shorter result list. Never a plausible-looking
  substitute.
- **Steps 1 (age) and 2 (allergy/safety) stay hard filters and must never be relaxed.**
  There is no operator override and no "show excluded anyway" toggle.
- **Never mark anything approved locally.** The provider's `Review_Status` and
  `Data_Quality` values are preserved verbatim and surfaced, never overridden.
- **`ingredient_master` is never modified.** Corrections live in views.
- Never mention claude, anthropic, or ai anywhere: not in chat, code, comments, commit
  messages, PR titles/descriptions, issue text, file headers, or READMEs. No co-author
  trailers referencing any ai tool.
- No emojis, anywhere.
- The migration DDL is the single source of truth for column names and types. The importer
  matches snake_cased worksheet headers against `information_schema`; a header with no
  column, or a `NOT NULL` column with no header, is an error, never a silent drop.
- Errors wrapped with `fmt.Errorf("...: %w", err)` at each boundary. Sentinel errors for
  conditions callers branch on.
- Table-driven tests, package-local (`foo_test.go`).
- Verify before calling backend work done: `go build ./...`, `go vet ./...`, and
  `TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...`.

## Measured starting state

Every number below was queried live on 18 August 2026 against a database rebuilt from
empty. Re-measure rather than trusting these indefinitely.

| Fact | Value |
|---|---|
| Latest migration | `0014_child_profile` |
| `gap_register` | 20 rows, 7 blocker |
| Provider workbooks in `data/provider/` | 14 (the special-care master is the newest) |
| Tables bound by `internal/importer/spec.go` | 30 |
| API routes | 14, none of them profile CRUD |
| `internal/profile` importers outside its own package | 0 |
| Occurrences of `suspected_allergens` in `web/src/` | 0 |
| Frontend test files | 1 (`provenance-chip.test.tsx`, 3 tests, all passing) |
| `test` script in `web/package.json` | absent |

## Architecture decisions made for this plan

Recorded here rather than asked, per `CLAUDE.md`'s standing instruction.

**1. The special-care workbook is imported and made a stop gate. Its recipe candidates are
never served.** The workbook's own `Coverage Dashboard` reads
`Clinical production approval | 0 | NOT YET`, and all 108 rows of `Special Recipe
Candidates` carry `Status = CANDIDATE-REVIEW`. Zero of their 102 distinct names exist in
`recipe_master` - they are archetypes to be mapped later, not recipes. So they are stored
and queryable, and no engine path selects from them. Storing them is honest; serving them
would be inventing a clinical recommendation.

**2. Blocking is safe to add without provider sign-off; serving is not.** Task 6 makes the
engine refuse to produce a ranked list for a child with a special-care condition, naming
the reviewer the provider's own sheet requires. That direction is strictly conservative -
a block never puts an unsafe recipe in front of an operator. GAP-019's current text says
the failure plainly: "A child with one of them is currently scored like any other child."
Stopping is better than scoring, and it needs no clearance.

**3. The stop gate reuses the existing escalation path rather than adding a parallel one.**
`internal/engine/clinical.go` already blocks and returns `Blocked` + `BlockReason` for
provider specialist-tier rules. A second, differently-shaped blocking mechanism would be
two things to keep correct. One code path, one shape of result.

**4. Only `Condition Stop Gates` drives behaviour. The other eight sheets are reference.**
`Output Rule Matrix` rules OR-002 through OR-014 depend on inputs this project does not
collect (feeding route, IDDSI level, fluid orders, post-op status). Implementing them
against absent inputs would mean inventing the inputs. They are imported so an operator can
read them, and only OR-001 - the condition-detected stop - has an engine implementation.

**5. Profile endpoints, and one console control that consumes them.** An endpoint with no
caller is the same orphan one level up. Task 4 gives the console a way to load a stored
profile into the form, which closes the loop without building a profile editor.

---

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `internal/db/migrations/0015_special_care.up.sql` | Nine special-care tables, FKs, and the gap-register rows the import makes measurable |
| `internal/db/migrations/0015_special_care.down.sql` | Drop them in reverse FK order |
| `internal/db/special_care_test.go` | Row counts, FK integrity, and the pin that candidates are never approved |
| `internal/engine/special_care.go` | The step-3 stop gate reading `special_care_condition_gate` |
| `internal/api/handlers/profiles.go` | Profile save, load, and derived engine-input endpoints |
| `internal/api/handlers/profiles_test.go` | Round-trip through HTTP, and the dropped-facts contract |
| `web/src/components/suspected-allergen-fieldset.tsx` | The suspected-allergen picker, separate from the declared one so the two can never be confused in markup |
| `web/src/components/suspected-allergen-fieldset.test.tsx` | Pins that suspected and confirmed render as visibly different states |

**Modified:**

| File | Change |
|---|---|
| `web/package.json` | Add the `test` script |
| `web/src/components/profile-form.tsx` | Suspected-allergen state, the new fieldset, profile loading |
| `web/src/lib/api.ts` | `getProfile`, `putProfile`, `getProfileEngineInput` |
| `web/src/lib/types.ts` | `StoredProfile`, `EngineInputResult` |
| `internal/api/router.go` | Three profile routes |
| `internal/importer/spec.go` | Nine special-care sheet bindings |
| `internal/importer/gaps.go` | Re-measures for the special-care gaps |
| `internal/engine/pipeline.go` | Call the stop gate inside step 3 |
| `CLAUDE.md`, `README.md`, `docs/next-steps.md` | Record what now exists |

---

## Task 1: Frontend test script

The harness works - vitest, testing-library, `vitest.config.ts` and `vitest.setup.ts` are
all present and three tests pass. There is no way to run it from `npm`, so it is invisible
and no verification loop mentions it. This task is first because Tasks 2 and 4 write
frontend tests and need a command to run them.

**Files:**
- Modify: `web/package.json` (the `scripts` block, currently lines 5-10)
- Modify: `CLAUDE.md` (the "Verify before calling backend work done" section)

**Interfaces:**
- Consumes: nothing
- Produces: `npm test` (single run, non-watch) and `npm run test:watch`, both runnable from
  `web/`. Later tasks invoke `npm test`.

- [ ] **Step 1: Confirm the harness runs before changing anything**

```bash
cd web && npx --no-install vitest run
```

Expected: `Test Files 1 passed (1)`, `Tests 3 passed (3)`. If this fails, stop and report -
the rest of this task assumes a working harness.

- [ ] **Step 2: Add the scripts**

In `web/package.json`, replace the `scripts` block with:

```json
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "eslint",
    "test": "vitest run",
    "test:watch": "vitest"
  },
```

`vitest run` rather than bare `vitest`: a bare invocation enters watch mode and never
exits, which hangs any script or agent that runs `npm test` expecting it to terminate.

- [ ] **Step 3: Run it through npm**

```bash
cd web && npm test
```

Expected: PASS, 1 file, 3 tests, and the process exits.

- [ ] **Step 4: Add the frontend to the documented verification loop**

In `CLAUDE.md`, find the fenced block under "### Verify before calling backend work done"
which currently reads:

```
go build ./...
go vet ./...
scripts/dev_db.fish up
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
```

Replace that block with:

```
go build ./...
go vet ./...
scripts/dev_db.fish up
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
cd web && npm test        # vitest, jsdom; needs no database and no API server
```

Then, directly beneath the existing paragraph that begins "The integrity suite needs a real
database", add this paragraph:

```
The frontend suite is separate and needs neither a database nor a running API: it renders
components against jsdom. It is small on purpose - it pins the rendering decisions that
carry meaning, such as a confidence never rounding up to 100%, and does not attempt to
cover every component.
```

- [ ] **Step 5: Verify the docs claim is true**

```bash
cd web && npm test
```

Expected: PASS with no `DATABASE_URL` or `TEST_DATABASE_URL` set, and no server on :8080.
This is the claim the paragraph makes; check it rather than assuming it.

- [ ] **Step 6: Commit**

```bash
git add web/package.json CLAUDE.md
git commit -m "Make the frontend test suite runnable and document it

vitest, testing-library, the config and the setup file were all present and
three tests passed, but package.json had no test script, so nothing ran the
suite and no verification loop mentioned it.

test is 'vitest run' rather than bare 'vitest': the bare form enters watch
mode and never exits, which hangs any caller expecting a single run."
```

---

## Task 2: Suspected allergens in the console

`models.ChildProfile.SuspectedAllergens` is accepted by the API, and
`applySuspectedAllergenRank` in `internal/engine/rank.go` implements it as step 2's ranking
half - it demotes by 0.15 and excludes nothing, because AS-002 marks a suspected allergy
`hard_block = N`. The console never sends the field, so the whole feature is unreachable
from the UI.

The reason it must render as visibly distinct from a declared allergen: a confirmed
allergen is a hard filter that removes recipes and can never be relaxed; a suspected one
only reorders. An operator who confuses the two either believes a child is protected when
they are not, or eliminates food unnecessarily - and unnecessary elimination is itself a
recognised cause of faltering growth.

**Files:**
- Create: `web/src/components/suspected-allergen-fieldset.tsx`
- Create: `web/src/components/suspected-allergen-fieldset.test.tsx`
- Modify: `web/src/components/profile-form.tsx` (state near line 108, submit payload near
  line 176, markup after the "Declared allergens" fieldset which ends near line 256)

**Interfaces:**
- Consumes: `npm test` from Task 1. `Allergen` from `web/src/lib/types.ts`, which carries
  `allergen_group: string`, `corpus_tag: string | null`, `screens: boolean`, `note: string`.
- Produces: `SuspectedAllergenFieldset`, a component taking
  `{ options: Allergen[]; selected: string[]; declared: string[]; onToggle: (group: string) => void }`.
  `ProfileForm` sends `suspected_allergens?: string[]` in its `onSubmit` payload.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/suspected-allergen-fieldset.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SuspectedAllergenFieldset } from "./suspected-allergen-fieldset";
import type { Allergen } from "@/lib/types";

const options: Allergen[] = [
  { allergen_group: "Peanut", corpus_tag: "Peanut", screens: true, note: "" },
  { allergen_group: "Milk", corpus_tag: "Milk", screens: true, note: "" },
  { allergen_group: "Tree nuts", corpus_tag: null, screens: false, note: "no corpus tag" },
];

describe("SuspectedAllergenFieldset", () => {
  it("says it ranks down rather than excludes", () => {
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={[]} onToggle={() => {}} />);
    // The whole safety point: an operator must not read this as protection.
    expect(screen.getByText(/ranked down.*never excluded|never excluded/i)).toBeInTheDocument();
  });

  it("offers a group that is already declared, but marks it superseded", () => {
    // Declaring Peanut confirmed and also suspecting it is not a contradiction the UI
    // should hide -- the confirmed hard filter already removed those recipes, so the
    // suspicion changes nothing and the row must say so rather than looking active.
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={["Peanut"]} onToggle={() => {}} />);
    expect(screen.getByRole("button", { name: /Peanut/ })).toBeDisabled();
    expect(screen.getByText(/already declared/i)).toBeInTheDocument();
  });

  it("marks a group with no corpus tag as demoting nothing", () => {
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={[]} onToggle={() => {}} />);
    expect(screen.getByText(/Tree nuts - not screened/)).toBeInTheDocument();
  });

  it("toggles a selectable group", async () => {
    const onToggle = vi.fn();
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={[]} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole("button", { name: /Milk/ }));
    expect(onToggle).toHaveBeenCalledWith("Milk");
  });

  it("does not toggle a group already declared confirmed", async () => {
    const onToggle = vi.fn();
    render(<SuspectedAllergenFieldset options={options} selected={[]} declared={["Peanut"]} onToggle={onToggle} />);
    await userEvent.click(screen.getByRole("button", { name: /Peanut/ }));
    expect(onToggle).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run it to make sure it fails**

```bash
cd web && npm test
```

Expected: FAIL with `Failed to resolve import "./suspected-allergen-fieldset"`.

- [ ] **Step 3: Check whether userEvent is installed**

```bash
cd web && node -e "require.resolve('@testing-library/user-event'); console.log('present')"
```

If it prints `present`, continue to Step 4. If it throws, install it first:

```bash
cd web && npm install --save-dev @testing-library/user-event
```

- [ ] **Step 4: Write the component**

Create `web/src/components/suspected-allergen-fieldset.tsx`:

```tsx
import { Badge } from "@/components/ui/badge";
import type { Allergen } from "@/lib/types";

interface SuspectedAllergenFieldsetProps {
  options: Allergen[];
  selected: string[];
  declared: string[];
  onToggle: (group: string) => void;
}

/**
 * Step 2's ranking half, as an operator control.
 *
 * A declared allergen is a hard filter and removes recipes. A suspected one only ranks
 * them down, by 0.15, and removes nothing -- AS-002 marks a suspected allergy
 * hard_block = N. Rendering the two the same way would let an operator read a demotion as
 * a protection, so this is a separate fieldset with its own wording rather than a second
 * mode of the declared picker.
 *
 * A group already declared confirmed is shown disabled rather than hidden: hiding it would
 * make the operator wonder whether the suspicion registered, and the honest answer is that
 * the confirmed filter already covers it.
 */
export function SuspectedAllergenFieldset({
  options, selected, declared, onToggle,
}: SuspectedAllergenFieldsetProps) {
  return (
    <fieldset className="space-y-1">
      <legend className="text-xs uppercase text-muted-foreground">
        Suspected allergens
      </legend>
      <div className="flex flex-wrap gap-1">
        {options.map((a) => {
          const isDeclared = declared.includes(a.allergen_group);
          const on = selected.includes(a.allergen_group);
          return (
            <button
              key={a.allergen_group}
              type="button"
              disabled={isDeclared}
              onClick={() => { if (!isDeclared) onToggle(a.allergen_group); }}
              title={isDeclared
                ? "Already declared as a confirmed allergen; the hard filter covers it"
                : a.note}
              aria-pressed={on}
              className="focus-visible:ring-ring rounded focus-visible:outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Badge
                variant={on ? "secondary" : "outline"}
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
        Suspected allergens are ranked down and never excluded - the provider&apos;s AS-002
        marks a suspected allergy <span className="font-mono">hard_block = N</span>. For a
        confirmed allergy use Declared allergens above, which is a hard filter.
      </p>
      {declared.some((d) => options.some((o) => o.allergen_group === d)) && (
        <p className="text-xs text-muted-foreground">
          Greyed groups are already declared confirmed above.
        </p>
      )}
    </fieldset>
  );
}
```

- [ ] **Step 5: Run the test to verify it passes**

```bash
cd web && npm test
```

Expected: PASS, 2 files, 8 tests.

- [ ] **Step 6: Wire it into the form**

Three edits to `web/src/components/profile-form.tsx`.

First, the import - add beside the other component imports at the top of the file:

```tsx
import { SuspectedAllergenFieldset } from "./suspected-allergen-fieldset";
```

Second, state and its toggle. After the line `const [allergens, setAllergens] = useState<string[]>([]);`
add:

```tsx
  const [suspectedAllergens, setSuspectedAllergens] = useState<string[]>([]);
```

and beside the existing `toggleAllergen` function add:

```tsx
  function toggleSuspectedAllergen(group: string) {
    setSuspectedAllergens((prev) =>
      prev.includes(group) ? prev.filter((g) => g !== group) : [...prev, group]);
  }
```

Third, the payload. In `handleSubmit`, directly after the `allergens:` line, add:

```tsx
      suspected_allergens: suspectedAllergens.length ? suspectedAllergens : undefined,
```

- [ ] **Step 7: Render the fieldset**

In `web/src/components/profile-form.tsx`, immediately after the closing `</fieldset>` of
the "Declared allergens" block (the one whose trailing paragraph reads "Dashed groups have
no tag anywhere in the corpus"), insert:

```tsx
      <SuspectedAllergenFieldset
        options={allergenOptions}
        selected={suspectedAllergens}
        declared={allergens}
        onToggle={toggleSuspectedAllergen}
      />
```

- [ ] **Step 8: Confirm the field reaches the engine**

Start the API and the console, then run a search with a suspected allergen selected and
open the "Why this result" panel.

```bash
DATABASE_URL=(scripts/dev_db.fish url) go run ./cmd/server &
cd web && npm run dev
```

Expected in the panel: a second step-2 row of kind `ranker` whose note reads
`N of M candidates carry a suspected allergen tag and were ranked down by 0.15; none were
excluded, because AS-002 marks a suspected allergy hard_block = N`.

Check the port is actually free first, and check it by request rather than by process list:

```bash
curl -sf -o /dev/null -m 2 http://localhost:8080/api/gaps && echo OCCUPIED || echo free
```

A `go run` server appears in the process table as `/tmp/go-build*/exe/server`, so
`pkill -f cmd/server` does not stop it and a stale server will answer your requests with
pre-change output.

- [ ] **Step 9: Typecheck and build**

```bash
cd web && npx --no-install tsc --noEmit && npm test
```

Expected: both clean.

- [ ] **Step 10: Commit**

```bash
git add web/src/components/suspected-allergen-fieldset.tsx \
        web/src/components/suspected-allergen-fieldset.test.tsx \
        web/src/components/profile-form.tsx
git commit -m "Send suspected allergens from the console

The engine implemented step 2's ranking half and the API accepted the field,
but the form never sent it, so the feature was unreachable from the UI.

Separate fieldset rather than a mode of the declared picker. A declared
allergen is a hard filter that removes recipes; a suspected one ranks them
down by 0.15 and removes nothing, because AS-002 marks a suspected allergy
hard_block = N. An operator who reads the second as the first believes a
child is protected when they are not.

A group already declared confirmed renders disabled rather than hidden, so
it is clear the confirmed filter already covers it."
```

---

## Task 3: Profile endpoints

`internal/profile` has `Save`, `Load` and `ToChildProfile`, is tested, and no package
outside itself imports it. The `child_profile` tables from migration `0014` are storage
nothing writes to.

`ToChildProfile` returns a second value that makes this worth exposing: a `[]string` naming
every stored fact that did not reach the engine query and why. That is the honest-gap rule
as an API response, and it is the reason the third endpoint exists.

**Files:**
- Create: `internal/api/handlers/profiles.go`
- Create: `internal/api/handlers/profiles_test.go`
- Modify: `internal/api/router.go` (route list, currently lines 33-46)

**Interfaces:**
- Consumes: `profile.Save(ctx, pool, profile.Stored) error`,
  `profile.Load(ctx, pool, childID string) (profile.Stored, error)`,
  `profile.Stored.ToChildProfile(asOf time.Time) (models.ChildProfile, []string, error)`,
  `profile.ErrNotFound`, `profile.ErrInvalidProfile`.
- Produces: three routes -
  `PUT /api/profiles/{childID}`, `GET /api/profiles/{childID}`,
  `GET /api/profiles/{childID}/engine-input`. The third returns
  `{"profile": ChildProfile, "dropped": string[], "as_of": "YYYY-MM-DD"}`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/handlers/profiles_test.go`:

```go
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// putProfileBody is the minimum a profile needs: an id and a date of birth. Everything
// else is optional, because a consultation does not always capture everything and a
// half-filled profile is more useful than a rejected one.
const putProfileBody = `{
  "child_id": "TEST-CHILD-001",
  "case_id": "TEST-CASE-001",
  "date_of_birth": "2023-08-18",
  "diet_type": "Vegetarian",
  "region_culture": "West Bengal / East India",
  "created_by": "integration-test",
  "allergens": [{"group": "Peanut", "status": "confirmed", "source": "clinician_documented", "entered_by": "integration-test"}]
}`

func profileRouter(t *testing.T) (*chi.Mux, *Handlers) {
	t.Helper()
	h := New(testPool(t))
	r := chi.NewRouter()
	r.Put("/api/profiles/{childID}", h.PutProfile)
	r.Get("/api/profiles/{childID}", h.GetProfile)
	r.Get("/api/profiles/{childID}/engine-input", h.GetProfileEngineInput)
	return r, h
}

func TestProfileRoundTripsThroughHTTP(t *testing.T) {
	r, h := profileRouter(t)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			`DELETE FROM child_profile WHERE child_id = 'TEST-CHILD-001'`)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/profiles/TEST-CHILD-001",
		bytes.NewBufferString(putProfileBody))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/profiles/TEST-CHILD-001", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		ChildID     string `json:"child_id"`
		DateOfBirth string `json:"date_of_birth"`
		DietType    string `json:"diet_type"`
		Allergens   []struct {
			Group  string `json:"group"`
			Status string `json:"status"`
		} `json:"allergens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ChildID != "TEST-CHILD-001" {
		t.Fatalf("child_id round-trip: got %q", got.ChildID)
	}
	if got.DateOfBirth != "2023-08-18" {
		t.Fatalf("date_of_birth must round-trip as a plain date, got %q", got.DateOfBirth)
	}
	if len(got.Allergens) != 1 || got.Allergens[0].Group != "Peanut" {
		t.Fatalf("child allergens did not round-trip: %+v", got.Allergens)
	}
}

func TestGetProfileReturns404ForUnknownChild(t *testing.T) {
	r, _ := profileRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/profiles/NO-SUCH-CHILD", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown child, got %d", rec.Code)
	}
}

func TestEngineInputReportsDroppedFacts(t *testing.T) {
	r, h := profileRouter(t)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			`DELETE FROM child_profile WHERE child_id = 'TEST-CHILD-001'`)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/profiles/TEST-CHILD-001",
		bytes.NewBufferString(putProfileBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET",
		"/api/profiles/TEST-CHILD-001/engine-input", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("engine-input: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Profile struct {
			AgeMonths int      `json:"age_months"`
			Allergens []string `json:"allergens"`
		} `json:"profile"`
		Dropped []string `json:"dropped"`
		AsOf    string   `json:"as_of"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Born 2023-08-18, so at any point after August 2026 the child is at least 36 months.
	if got.Profile.AgeMonths < 36 {
		t.Fatalf("age must be derived from date_of_birth, got %d months", got.Profile.AgeMonths)
	}
	if len(got.Profile.Allergens) != 1 || got.Profile.Allergens[0] != "Peanut" {
		t.Fatalf("a confirmed allergen must reach the engine query: %+v", got.Profile.Allergens)
	}
	if got.AsOf == "" {
		t.Fatal("as_of must be reported: age is derived from it, so a response without it is not reproducible")
	}
	// dropped may legitimately be empty for this fixture. The contract is that the key is
	// always present and never null, so a caller can render it without a nil check.
	if got.Dropped == nil {
		t.Fatal("dropped must serialise as [] rather than null")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/api/handlers/ -run Profile -v
```

Expected: FAIL to build, `h.PutProfile undefined`.

- [ ] **Step 3: Write the handlers**

Create `internal/api/handlers/profiles.go`:

```go
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/profile"
)

// dateLayout is the wire format for every date this resource carries. Dates, not
// timestamps: a date of birth has no time of day, and RFC3339 would invite a timezone
// into a value that has none. profile.ageMonths already normalises to UTC for exactly
// this reason.
const dateLayout = "2006-01-02"

type profileAllergenDTO struct {
	Group          string `json:"group"`
	Status         string `json:"status"`
	Severity       string `json:"severity,omitempty"`
	Source         string `json:"source,omitempty"`
	LastReactionOn string `json:"last_reaction_on,omitempty"`
	EnteredBy      string `json:"entered_by,omitempty"`
}

type profileDTO struct {
	ChildID              string               `json:"child_id"`
	CaseID               string               `json:"case_id,omitempty"`
	DisplayName          string               `json:"display_name,omitempty"`
	DateOfBirth          string               `json:"date_of_birth"`
	Sex                  string               `json:"sex,omitempty"`
	LanguageID           string               `json:"language_id,omitempty"`
	RegionCulture        string               `json:"region_culture,omitempty"`
	CuisineCode          string               `json:"cuisine_code,omitempty"`
	DietType             string               `json:"diet_type,omitempty"`
	Vegan                bool                 `json:"vegan,omitempty"`
	ReligiousRestriction string               `json:"religious_restriction,omitempty"`
	BudgetBand           string               `json:"budget_band,omitempty"`
	MaxPrepTimeMin       int                  `json:"max_prep_time_min,omitempty"`
	MaxCookTimeMin       int                  `json:"max_cook_time_min,omitempty"`
	CreatedBy            string               `json:"created_by,omitempty"`
	Allergens            []profileAllergenDTO `json:"allergens"`
}

func toDTO(s profile.Stored) profileDTO {
	d := profileDTO{
		ChildID: s.ChildID, CaseID: s.CaseID, DisplayName: s.DisplayName,
		DateOfBirth: s.DateOfBirth.UTC().Format(dateLayout),
		Sex:         s.Sex, LanguageID: s.LanguageID, RegionCulture: s.RegionCulture,
		CuisineCode: s.CuisineCode, DietType: s.DietType, Vegan: s.Vegan,
		ReligiousRestriction: s.ReligiousRestriction, BudgetBand: s.BudgetBand,
		MaxPrepTimeMin: s.MaxPrepTimeMin, MaxCookTimeMin: s.MaxCookTimeMin,
		CreatedBy: s.CreatedBy,
		// Never nil: a nil slice marshals to null, and a client rendering a list should
		// not need a null check to show "no allergens declared".
		Allergens: []profileAllergenDTO{},
	}
	for _, a := range s.Allergens {
		dto := profileAllergenDTO{
			Group: a.Group, Status: a.Status, Severity: a.Severity,
			Source: a.Source, EnteredBy: a.EnteredBy,
		}
		if a.LastReactionOn != nil {
			dto.LastReactionOn = a.LastReactionOn.UTC().Format(dateLayout)
		}
		d.Allergens = append(d.Allergens, dto)
	}
	return d
}

func fromDTO(d profileDTO) (profile.Stored, error) {
	dob, err := time.Parse(dateLayout, d.DateOfBirth)
	if err != nil {
		return profile.Stored{}, err
	}
	s := profile.Stored{
		ChildID: d.ChildID, CaseID: d.CaseID, DisplayName: d.DisplayName,
		DateOfBirth: dob, Sex: d.Sex, LanguageID: d.LanguageID,
		RegionCulture: d.RegionCulture, CuisineCode: d.CuisineCode,
		DietType: d.DietType, Vegan: d.Vegan,
		ReligiousRestriction: d.ReligiousRestriction, BudgetBand: d.BudgetBand,
		MaxPrepTimeMin: d.MaxPrepTimeMin, MaxCookTimeMin: d.MaxCookTimeMin,
		CreatedBy: d.CreatedBy,
	}
	for _, a := range d.Allergens {
		da := profile.DeclaredAllergen{
			Group: a.Group, Status: a.Status, Severity: a.Severity,
			Source: a.Source, EnteredBy: a.EnteredBy,
		}
		if a.LastReactionOn != "" {
			t, err := time.Parse(dateLayout, a.LastReactionOn)
			if err != nil {
				return profile.Stored{}, err
			}
			da.LastReactionOn = &t
		}
		s.Allergens = append(s.Allergens, da)
	}
	return s, nil
}

// PutProfile creates or replaces one child's stored profile. profile.Save is an upsert,
// so PUT rather than POST: the same body sent twice leaves the same state.
func (h *Handlers) PutProfile(w http.ResponseWriter, r *http.Request) {
	childID := chi.URLParam(r, "childID")
	var d profileDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "malformed profile body: "+err.Error())
		return
	}
	// The path is authoritative. A body naming a different child would otherwise write to
	// one id while the caller believes it wrote to another.
	if d.ChildID != "" && d.ChildID != childID {
		writeError(w, http.StatusBadRequest,
			"child_id in the body does not match the path")
		return
	}
	d.ChildID = childID

	s, err := fromDTO(d)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid profile field: "+err.Error())
		return
	}
	if err := profile.Save(r.Context(), h.pool, s); err != nil {
		writeError(w, http.StatusInternalServerError, "profile save failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDTO(s))
}

// GetProfile returns the stored profile verbatim, without deriving anything from it.
func (h *Handlers) GetProfile(w http.ResponseWriter, r *http.Request) {
	s, err := profile.Load(r.Context(), h.pool, chi.URLParam(r, "childID"))
	if errors.Is(err, profile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no profile for that child id")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile load failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDTO(s))
}

// GetProfileEngineInput returns what the engine would actually receive for this child
// today, alongside every stored fact that did not survive the conversion.
//
// The dropped list is the point of the endpoint. A stored profile holds more than the
// engine query can express -- growth trends, allergen severity, expired acute conditions --
// and silently discarding those would make the console show a profile that is richer than
// the search it produced. Naming what was dropped is the honest-gap rule applied to a
// conversion rather than to missing data.
func (h *Handlers) GetProfileEngineInput(w http.ResponseWriter, r *http.Request) {
	s, err := profile.Load(r.Context(), h.pool, chi.URLParam(r, "childID"))
	if errors.Is(err, profile.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no profile for that child id")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile load failed: "+err.Error())
		return
	}

	asOf := time.Now().UTC()
	cp, dropped, err := s.ToChildProfile(asOf)
	if errors.Is(err, profile.ErrInvalidProfile) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "engine input derivation failed: "+err.Error())
		return
	}
	if dropped == nil {
		dropped = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile": cp,
		"dropped": dropped,
		// Age is derived from this date, so the same profile read next month yields a
		// different query. Returning it makes the response reproducible.
		"as_of": asOf.Format(dateLayout),
	})
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/api/handlers/ -run Profile -v
```

Expected: PASS, three tests.

- [ ] **Step 5: Register the routes**

In `internal/api/router.go`, after the line
`r.Get("/api/reference/book1-blocks", h.ReferenceBook1Blocks)`, add:

```go
	r.Put("/api/profiles/{childID}", h.PutProfile)
	r.Get("/api/profiles/{childID}", h.GetProfile)
	r.Get("/api/profiles/{childID}/engine-input", h.GetProfileEngineInput)
```

- [ ] **Step 6: Verify the whole backend**

```bash
go build ./... && go vet ./... && TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
```

Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers/profiles.go internal/api/handlers/profiles_test.go internal/api/router.go
git commit -m "Serve the stored child profile over HTTP

internal/profile had Save, Load and ToChildProfile, was tested, and no
package outside itself imported it. The child_profile tables from migration
0014 were storage nothing wrote to.

Three routes. PUT rather than POST because profile.Save is an upsert, so the
same body twice leaves the same state. The engine-input route is the reason
this is worth exposing: ToChildProfile returns every stored fact that did not
reach the engine query, and returning that list keeps a rich stored profile
from silently producing a thinner search than the operator expects.

Dates on the wire, not timestamps: a date of birth has no time of day, and
RFC3339 would invite a timezone into a value that has none."
```

---

## Task 4: Load a stored profile into the console

Task 3's endpoints have no caller, which is the same orphan one level up. This closes the
loop with one control: enter a child id, fetch the profile, populate the form.

**Files:**
- Modify: `web/src/lib/types.ts` (add the two types)
- Modify: `web/src/lib/api.ts` (add the three functions)
- Modify: `web/src/components/profile-form.tsx` (the load control and its handler)

**Interfaces:**
- Consumes: `PUT/GET /api/profiles/{childID}` and `GET /api/profiles/{childID}/engine-input`
  from Task 3; `suspectedAllergens` state from Task 2.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Add the types**

In `web/src/lib/types.ts`, append:

```ts
export interface StoredProfileAllergen {
  group: string;
  status: string;
  severity?: string;
  source?: string;
  last_reaction_on?: string;
  entered_by?: string;
}

export interface StoredProfile {
  child_id: string;
  case_id?: string;
  display_name?: string;
  date_of_birth: string;
  sex?: string;
  language_id?: string;
  region_culture?: string;
  cuisine_code?: string;
  diet_type?: string;
  vegan?: boolean;
  religious_restriction?: string;
  budget_band?: string;
  max_prep_time_min?: number;
  max_cook_time_min?: number;
  created_by?: string;
  allergens: StoredProfileAllergen[];
}

/**
 * What the engine would receive for this child today, plus every stored fact that did not
 * survive the conversion. `dropped` is always an array, never null.
 */
export interface EngineInputResult {
  profile: ChildProfile;
  dropped: string[];
  as_of: string;
}
```

- [ ] **Step 2: Add the client functions**

In `web/src/lib/api.ts`, add `StoredProfile` and `EngineInputResult` to the type import
list at the top, then append:

```ts
export function getProfile(childID: string): Promise<StoredProfile> {
  return request<StoredProfile>(`/api/profiles/${encodeURIComponent(childID)}`);
}

export function putProfile(childID: string, body: StoredProfile): Promise<StoredProfile> {
  return request<StoredProfile>(`/api/profiles/${encodeURIComponent(childID)}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export function getProfileEngineInput(childID: string): Promise<EngineInputResult> {
  return request<EngineInputResult>(
    `/api/profiles/${encodeURIComponent(childID)}/engine-input`);
}
```

- [ ] **Step 3: Add the load control to the form**

In `web/src/components/profile-form.tsx`, add to the imports:

```tsx
import { getProfileEngineInput } from "@/lib/api";
```

(`getProfileEngineInput` rather than `getProfile`: the endpoint that already applies the
conversion is the one whose answer the form should show, and it carries the dropped list.)

Add state beside the other `useState` calls:

```tsx
  const [loadChildID, setLoadChildID] = useState("");
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadDropped, setLoadDropped] = useState<string[]>([]);
```

Add the handler beside the other handlers:

```tsx
  // Populates the form from a stored profile. Only fields the engine query actually
  // carries are set -- everything else the profile holds is reported in loadDropped
  // rather than silently ignored, so the operator can see that the stored record is
  // richer than the search it produced.
  async function handleLoadProfile() {
    if (!loadChildID.trim()) return;
    setLoadError(null);
    try {
      const res = await getProfileEngineInput(loadChildID.trim());
      const p = res.profile;
      setAgeMonths(p.age_months);
      setDietType(p.diet_type ?? "");
      setVegan(Boolean(p.vegan));
      setAllergens(p.allergens ?? []);
      setSuspectedAllergens(p.suspected_allergens ?? []);
      setClinicalFlags(p.clinical_flags ?? {});
      setRegionCulture(p.region_culture ?? "");
      setCuisineCode(p.cuisine_code ?? "");
      setBudgetBand(p.budget_band ?? "");
      setMaxPrep(p.max_prep_time_min ? String(p.max_prep_time_min) : "");
      setMaxCook(p.max_cook_time_min ? String(p.max_cook_time_min) : "");
      setLoadDropped(res.dropped);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
      setLoadDropped([]);
    }
  }
```

- [ ] **Step 4: Render the control**

Insert at the top of the `<form>` body, before the age field:

```tsx
      <div className="space-y-1 border-b pb-3">
        <label htmlFor="load-child" className={label}>Load stored profile</label>
        <div className="flex gap-1">
          <input
            id="load-child" type="text" placeholder="child id"
            value={loadChildID}
            onChange={(e) => setLoadChildID(e.target.value)}
            className="border-input bg-background w-full rounded border px-2 py-1 font-mono text-xs"
          />
          <button
            type="button" onClick={handleLoadProfile}
            className="border-input rounded border px-2 py-1 text-xs"
          >
            Load
          </button>
        </div>
        {loadError && <p className="text-xs text-destructive">{loadError}</p>}
        {loadDropped.length > 0 && (
          <div className="text-xs text-muted-foreground">
            <p>Stored facts that do not reach the engine query:</p>
            <ul className="list-disc pl-4">
              {loadDropped.map((d) => <li key={d}>{d}</li>)}
            </ul>
          </div>
        )}
      </div>
```

- [ ] **Step 5: Typecheck, test and build**

```bash
cd web && npx --no-install tsc --noEmit && npm test && npm run build
```

Expected: all clean. `npm run build` prerenders `/reference`, so the API must be running -
check the port by request first, as in Task 2 Step 8.

- [ ] **Step 6: Verify against a real profile**

With the API running, create a profile and load it:

```bash
curl -sf -X PUT http://localhost:8080/api/profiles/DEMO-001 \
  -H 'Content-Type: application/json' \
  -d '{"date_of_birth":"2023-02-01","diet_type":"Vegetarian","region_culture":"West Bengal / East India","created_by":"manual-check","allergens":[{"group":"Peanut","status":"confirmed"}]}'
```

Then type `DEMO-001` into the console's load field and press Load. Expected: age populates
from the date of birth, diet and region populate, Peanut appears under Declared allergens.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/types.ts web/src/lib/api.ts web/src/components/profile-form.tsx
git commit -m "Load a stored profile into the console form

The profile endpoints had no caller, which is the same orphan one level up.

The form reads the engine-input endpoint rather than the raw profile: that
route already applies the stored-to-query conversion, and it returns the
facts the conversion dropped. Those render under the control, so an operator
can see that the stored record holds more than the search does instead of
assuming the form is the whole profile."
```

---

## Task 5: Import the special-care condition master

The fourteenth provider workbook, delivered 18 August. Nine data sheets, all with headers
on row 4, verified by reading the file. `README & Engine Logic` is prose and is not
imported, like the other README sheets.

FK integrity was checked before writing this task and is clean: every
`Special Recipe Candidates.Condition_ID` and `.Food_Type_ID` resolves, every
`Feeding Style Protocol.Condition_ID` resolves, all 108 candidate ids are unique, and all
30 parameter ids are unique.

**Files:**
- Create: `internal/db/migrations/0015_special_care.up.sql`
- Create: `internal/db/migrations/0015_special_care.down.sql`
- Create: `internal/db/special_care_test.go`
- Modify: `internal/importer/spec.go` (append nine specs to `Specs`)

**Interfaces:**
- Consumes: `importer.TableSpec{Table, File, Sheet, HeaderRow, PrimaryKey, FirstCol}`.
- Produces: tables `special_care_condition_gate`, `special_care_parameter`,
  `special_care_feeding_style`, `special_care_food_type`, `special_care_recipe_candidate`,
  `special_care_output_rule`, `special_care_evidence_source`, `special_care_qa_check`,
  `special_care_coverage_metric`. Task 6 reads `special_care_condition_gate`.

- [ ] **Step 1: Write the migration**

Create `internal/db/migrations/0015_special_care.up.sql`:

```sql
-- The provider's Special-Care Condition Feeding & Recipe Engine Master V1, delivered
-- 18 August 2026. Nine data sheets from
-- data/provider/MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx.
--
-- Its governing rule, verbatim from the README & Engine Logic sheet:
--   "Condition is a STOP GATE, not a simple recipe filter."
-- A positive diagnosis pauses generation and routes to a clinician. That is what
-- internal/engine/special_care.go implements, and it is the only sheet here with an
-- engine implementation.
--
-- Nothing in this workbook is approved. Its own Coverage Dashboard reads
-- "Clinical production approval | 0 | NOT YET", and all 108 recipe candidates carry
-- Status = CANDIDATE-REVIEW. Zero of their names exist in recipe_master: they are
-- archetypes to be mapped onto validated recipes later, which is why there is no foreign
-- key from special_care_recipe_candidate to recipe_master and no engine path selects from
-- it. Storing them is honest; serving them would be inventing a clinical recommendation.
--
-- Columns are the workbook's headers snake_cased, per the importer's contract. Every free
-- text column stays text: these are clinical instructions for a human reviewer, and
-- parsing them into flags would be this project deciding clinical scope.

CREATE TABLE special_care_condition_gate (
    condition_id             text PRIMARY KEY,
    condition                text NOT NULL,
    gate_level               text,
    automatic_action         text,
    mandatory_minimum_inputs text,
    stop_if                  text,
    proceed_if               text,
    feeding_route_logic      text,
    texture_logic            text,
    energy_volume_logic      text,
    mandatory_reviewer       text,
    book1_output             text,
    book2_output             text,
    source_ids               text
);

CREATE TABLE special_care_parameter (
    parameter_id           text PRIMARY KEY,
    parameter              text NOT NULL,
    data_type              text,
    allowed_values_or_unit text,
    required_when          text,
    hard_stop_if_missing   text,
    source                 text,
    reviewer               text,
    engine_use             text,
    book1_use              text,
    book2_use              text,
    notes                  text
);

CREATE TABLE special_care_food_type (
    food_type_id        text PRIMARY KEY,
    food_type           text NOT NULL,
    suitable_when       text,
    use_caution_when    text,
    hard_exclude_when   text,
    typical_conditions  text,
    texture_or_effort_tag text,
    nutrition_tag       text,
    volume_tag          text,
    sensory_tag         text,
    examples            text
);

-- No surrogate key in the sheet: a condition has several phenotype rows. The provider
-- ships 13 rows over 6 conditions and the pair is unique across them.
CREATE TABLE special_care_feeding_style (
    condition_id               text NOT NULL REFERENCES special_care_condition_gate(condition_id),
    phenotype_or_trigger       text NOT NULL,
    recommended_feeding_style  text,
    food_texture_principle     text,
    meal_pattern_principle     text,
    caregiver_method           text,
    avoid_or_stop              text,
    escalation                 text,
    engine_output_tag          text,
    source_ids                 text,
    clinical_note              text,
    PRIMARY KEY (condition_id, phenotype_or_trigger)
);

CREATE TABLE special_care_recipe_candidate (
    candidate_id          text PRIMARY KEY,
    condition_id          text NOT NULL REFERENCES special_care_condition_gate(condition_id),
    recipe_name           text NOT NULL,
    meal_category         text,
    food_type_id          text REFERENCES special_care_food_type(food_type_id),
    age_band              text,
    default_texture_tag   text,
    sensory_profile       text,
    energy_density        text,
    protein_density       text,
    fluid_load            text,
    chewing_effort        text,
    culture_region        text,
    suitable_when         text,
    do_not_use_when       text,
    required_input_params text,
    reviewer_gate         text,
    status                text NOT NULL
);

CREATE TABLE special_care_output_rule (
    rule_id      text PRIMARY KEY,
    if_condition text NOT NULL,
    then_action  text NOT NULL,
    hard_or_soft text,
    book1_output text,
    book2_output text,
    reviewer_gate text,
    error_code   text,
    notes        text
);

CREATE TABLE special_care_evidence_source (
    source_id         text PRIMARY KEY,
    organization      text,
    topic             text,
    key_engine_use    text,
    current_reference text,
    url               text,
    accessed          text,
    notes             text
);

CREATE TABLE special_care_qa_check (
    qa_id          text PRIMARY KEY,
    domain         text,
    check_         text,
    mandatory      text,
    reviewer       text,
    failure_action text,
    status         text
);

CREATE TABLE special_care_coverage_metric (
    metric text PRIMARY KEY,
    value  text,
    status text,
    notes  text
);

-- GAP-021 and GAP-022 record what importing this workbook makes measurable. GAP-019 is
-- rewritten rather than deleted: the gap it named is real and is now sourced, not closed.
UPDATE gap_register SET
    description = 'Down syndrome, cerebral palsy, congenital heart disease, cleft lip and '
        || 'palate, autism and intellectual disability have no row in clinical_rule_master. '
        || 'The provider''s Special-Care master (delivered 18 August 2026) now defines all '
        || 'six as STOP-REVIEW gates, so the engine stops for them instead of ranking them '
        || 'like any other child. What is still missing is their representation in the '
        || 'clinical rule master itself, and any recipe validated for them.',
    ui_behaviour = 'The engine blocks and names the reviewer the provider''s sheet requires. '
        || 'No ranked list is produced for a child with one of these conditions.',
    resolution_path = 'Provider extends clinical_rule_master, and maps the 108 special-care '
        || 'recipe candidates onto validated Recipe Master rows. Outstanding question 10.'
WHERE gap_id = 'GAP-019';

INSERT INTO gap_register
    (gap_id, severity, area, source_table, source_column, description, affected_rows,
     measured_by, ui_behaviour, resolution_path)
VALUES
    ('GAP-021', 'blocker', 'Special care',
     'special_care_recipe_candidate', 'status',
     'All special-care recipe candidates carry Status = CANDIDATE-REVIEW and the '
     || 'workbook''s own Coverage Dashboard records clinical production approval as NOT '
     || 'YET. None of their recipe names exist in recipe_master, so they are archetypes '
     || 'rather than recipes: there is nothing to serve even if approval existed.',
     0, 'importer',
     'Candidates are never returned by any engine path. They are readable as reference '
     || 'only, with their CANDIDATE-REVIEW status shown.',
     'Provider maps each candidate onto a validated Recipe Master row and completes the '
     || 'multidisciplinary review its Review & QA sheet requires.'),
    ('GAP-022', 'major', 'Special care',
     'special_care_output_rule', 'if_condition',
     'Only OR-001, the condition-detected stop, has an engine implementation. OR-002 '
     || 'through OR-014 depend on inputs this project does not collect: feeding route, '
     || 'prescribed IDDSI level, cardiology fluid orders, post-operative status, pica and '
     || 'sensory profile. Implementing them against absent inputs would mean inventing '
     || 'the inputs.',
     0, 'importer',
     'The rules are readable in full. The engine acts on OR-001 only, and the stop it '
     || 'produces is what prevents the unimplemented rules from mattering: no ranked list '
     || 'is generated for a special-care child in the first place.',
     'Collect the special-care parameters in the intake form (30 are specified in '
     || 'special_care_parameter), then implement the rules whose inputs exist.');
```

Create `internal/db/migrations/0015_special_care.down.sql`:

```sql
DELETE FROM gap_register WHERE gap_id IN ('GAP-021', 'GAP-022');

-- GAP-019's pre-0015 text, restored so a down migration leaves the register as it found it.
UPDATE gap_register SET
    description = 'Down syndrome, cerebral palsy, congenital heart disease, cleft lip and '
        || 'palate, autism and intellectual disability have no rule row. Each changes '
        || 'feeding through texture, energy density, oral-motor ability, mealtime '
        || 'behaviour and sometimes fluid restriction. A child with one of them is '
        || 'currently scored like any other child.',
    ui_behaviour = 'No behaviour today: the engine cannot know about a condition with no '
        || 'trigger_field. It holds only for conditions the masters name.',
    resolution_path = 'Provider extends clinical_rule_master. The list cannot be written '
        || 'on this side without inventing clinical scope. Outstanding question 10.'
WHERE gap_id = 'GAP-019';

DROP TABLE IF EXISTS special_care_coverage_metric;
DROP TABLE IF EXISTS special_care_qa_check;
DROP TABLE IF EXISTS special_care_evidence_source;
DROP TABLE IF EXISTS special_care_output_rule;
DROP TABLE IF EXISTS special_care_recipe_candidate;
DROP TABLE IF EXISTS special_care_feeding_style;
DROP TABLE IF EXISTS special_care_food_type;
DROP TABLE IF EXISTS special_care_parameter;
DROP TABLE IF EXISTS special_care_condition_gate;
```

- [ ] **Step 2: Bind the sheets**

In `internal/importer/spec.go`, append these nine entries to the end of the `Specs` slice.
Parent-first ordering matters: the importer upserts in this order and deletes in reverse,
so the two referenced tables come before the tables referencing them.

```go
	// The provider's Special-Care Condition Feeding & Recipe Engine Master, delivered
	// 18 August 2026. Every sheet's headers are on row 4, verified by reading the file.
	// "README & Engine Logic" is prose and is not imported, like the other README sheets.
	{
		Table: "special_care_condition_gate", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Condition Stop Gates", HeaderRow: 4,
		PrimaryKey: []string{"condition_id"}, FirstCol: "condition_id",
	},
	{
		Table: "special_care_parameter", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Parameter Input Master", HeaderRow: 4,
		PrimaryKey: []string{"parameter_id"}, FirstCol: "parameter_id",
	},
	{
		Table: "special_care_food_type", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Food Type Indications", HeaderRow: 4,
		PrimaryKey: []string{"food_type_id"}, FirstCol: "food_type_id",
	},
	{
		Table: "special_care_feeding_style", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Feeding Style Protocol", HeaderRow: 4,
		PrimaryKey: []string{"condition_id", "phenotype_or_trigger"}, FirstCol: "condition_id",
	},
	{
		Table: "special_care_recipe_candidate", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Special Recipe Candidates", HeaderRow: 4,
		PrimaryKey: []string{"candidate_id"}, FirstCol: "candidate_id",
	},
	{
		Table: "special_care_output_rule", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Output Rule Matrix", HeaderRow: 4,
		PrimaryKey: []string{"rule_id"}, FirstCol: "rule_id",
	},
	{
		Table: "special_care_evidence_source", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Evidence Protocol Master", HeaderRow: 4,
		PrimaryKey: []string{"source_id"}, FirstCol: "source_id",
	},
	{
		Table: "special_care_qa_check", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Review & QA", HeaderRow: 4,
		PrimaryKey: []string{"qa_id"}, FirstCol: "qa_id",
	},
	{
		Table: "special_care_coverage_metric", File: "MadamGY_Special_Care_Condition_Feeding_Recipe_Engine_Master_V1.xlsx",
		Sheet: "Coverage Dashboard", HeaderRow: 4,
		PrimaryKey: []string{"metric"}, FirstCol: "metric",
	},
```

- [ ] **Step 3: Rebuild and import**

```bash
scripts/dev_db.fish down && scripts/dev_db.fish up
DATABASE_URL=(scripts/dev_db.fish url) go run ./cmd/import
```

Expected in the per-table output: nine new rows with counts 6, 30, 10, 13, 108, 14, 14, 12,
9 and `skipped = 0` on each.

If the importer reports a header with no matching column, the DDL is wrong and the DDL is
what changes - never the workbook. The `check_` column is the one to watch: the sheet's
header is `Check`, which snake_cases to `check`, a reserved word. If the importer cannot
bind it, rename the column in the migration to exactly what the importer reports it is
looking for, and record the reason in a comment.

- [ ] **Step 4: Write the integrity test**

Create `internal/db/special_care_test.go`:

```go
package db

import (
	"context"
	"testing"
)

// TestSpecialCareRowCounts pins what the workbook ships. A count that moves means the
// provider reissued the file, which is a thing to notice rather than absorb silently.
func TestSpecialCareRowCounts(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, c := range []struct {
		table string
		want  int
	}{
		{"special_care_condition_gate", 6},
		{"special_care_parameter", 30},
		{"special_care_food_type", 10},
		{"special_care_feeding_style", 13},
		{"special_care_recipe_candidate", 108},
		{"special_care_output_rule", 14},
		{"special_care_evidence_source", 14},
		{"special_care_qa_check", 12},
		{"special_care_coverage_metric", 9},
	} {
		t.Run(c.table, func(t *testing.T) {
			var got int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+c.table).Scan(&got); err != nil {
				t.Fatalf("count %s: %v", c.table, err)
			}
			if got != c.want {
				t.Fatalf("%s: expected %d rows, got %d", c.table, c.want, got)
			}
		})
	}
}

// TestEverySpecialCareConditionIsAStopGate is the invariant the engine's stop gate depends
// on. The workbook's README states the rule as "Condition is a STOP GATE, not a simple
// recipe filter", and all six rows carry Gate_Level = STOP-REVIEW. If the provider ever
// ships a condition at a lower gate level, the engine must not keep blocking it silently
// as though the provider still required that -- so this fails and forces the decision.
func TestEverySpecialCareConditionIsAStopGate(t *testing.T) {
	pool := testPool(t)
	var notStop int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM special_care_condition_gate WHERE gate_level <> 'STOP-REVIEW'`).
		Scan(&notStop)
	if err != nil {
		t.Fatalf("gate level query: %v", err)
	}
	if notStop != 0 {
		t.Fatalf("%d special-care conditions are not STOP-REVIEW; internal/engine/special_care.go "+
			"blocks every one of them, so a non-stop condition needs an explicit decision "+
			"rather than an inherited block", notStop)
	}
}

// TestEverySpecialCareConditionNamesAReviewer pins the other half of the block: the engine
// quotes mandatory_reviewer into its block reason, and a block that cannot say who to
// escalate to is a dead end for the operator.
func TestEverySpecialCareConditionNamesAReviewer(t *testing.T) {
	pool := testPool(t)
	var missing int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM special_care_condition_gate
		 WHERE mandatory_reviewer IS NULL OR btrim(mandatory_reviewer) = ''`).Scan(&missing)
	if err != nil {
		t.Fatalf("reviewer query: %v", err)
	}
	if missing != 0 {
		t.Fatalf("%d special-care conditions name no mandatory reviewer", missing)
	}
}

// TestSpecialCareCandidatesAreNeverApproved is the safety pin. All 108 candidates ship as
// CANDIDATE-REVIEW and none of their names exist in recipe_master. If a future import
// carried an approved-looking status, an engine path might reasonably start serving them,
// and this test is what stops that happening without a deliberate decision.
func TestSpecialCareCandidatesAreNeverApproved(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var notCandidate int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM special_care_recipe_candidate WHERE status <> 'CANDIDATE-REVIEW'`).
		Scan(&notCandidate); err != nil {
		t.Fatalf("status query: %v", err)
	}
	if notCandidate != 0 {
		t.Fatalf("%d special-care candidates are no longer CANDIDATE-REVIEW; nothing may be "+
			"served from this table without a recorded clinical decision", notCandidate)
	}

	var mapped int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM special_care_recipe_candidate c
		 JOIN recipe_master r ON r.recipe_name = c.recipe_name`).Scan(&mapped); err != nil {
		t.Fatalf("mapping query: %v", err)
	}
	if mapped != 0 {
		t.Fatalf("%d candidate names now match a Recipe Master row. That mapping is the "+
			"provider's job and changes what the engine could serve, so it needs a "+
			"decision rather than an inherited assumption", mapped)
	}
}
```

- [ ] **Step 5: Run the test**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/db/ -run SpecialCare -v
```

Expected: PASS, four tests, the first with nine subtests.

- [ ] **Step 6: Verify idempotency**

```bash
DATABASE_URL=(scripts/dev_db.fish url) go run ./cmd/import
```

Then check no table's content changed between the two runs:

```bash
docker exec recipie-pg psql -U recipie -d recipie -c "
SELECT a.table_name FROM import_table_stat a
JOIN import_table_stat b USING (table_name)
WHERE a.run_id = (SELECT min(run_id) FROM import_run)
  AND b.run_id = (SELECT max(run_id) FROM import_run)
  AND a.content_hash <> b.content_hash"
```

Expected: `(0 rows)`.

- [ ] **Step 7: Full verification**

```bash
go build ./... && go vet ./... && TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
```

Expected: all green, including the pre-existing suites.

- [ ] **Step 8: Commit**

```bash
git add internal/db/migrations/0015_special_care.up.sql \
        internal/db/migrations/0015_special_care.down.sql \
        internal/db/special_care_test.go internal/importer/spec.go
git commit -m "Import the provider's special-care condition master

Nine sheets from the fourteenth provider workbook, delivered 18 August:
six STOP-REVIEW conditions, 30 input parameters, 13 feeding-style protocols,
10 food-type archetypes, 108 recipe candidates, 14 output rules, 14 evidence
sources, 12 QA checks and the coverage dashboard. Headers on row 4 on every
sheet, verified by reading the file rather than assumed.

The 108 recipe candidates get no foreign key to recipe_master and no engine
path selects from them. Zero of their 102 distinct names exist there: they
are archetypes to be mapped onto validated recipes, and the workbook's own
dashboard records clinical production approval as NOT YET. Pinned by
TestSpecialCareCandidatesAreNeverApproved, which fails if a future import
makes them look servable.

GAP-019 is rewritten rather than closed: the conditions now have a source,
and still have no row in clinical_rule_master and no validated recipe.
GAP-021 and GAP-022 record what the import makes measurable."
```

---

## Task 6: The special-care stop gate

The payoff. GAP-019's own words for the current behaviour: "A child with one of them is
currently scored like any other child." The workbook's answer: "Condition is a STOP GATE,
not a simple recipe filter."

This reuses the block that `internal/engine/clinical.go` already produces for provider
specialist-tier rules, rather than adding a second blocking mechanism.

**Files:**
- Create: `internal/engine/special_care.go`
- Create: `internal/engine/special_care_test.go`
- Modify: `internal/engine/pipeline.go` (step 3, where `clinicalFilter` is called)
- Modify: `internal/models/profile.go` (add the input field)

**Interfaces:**
- Consumes: `special_care_condition_gate` from Task 5.
  `models.EngineResult` already carries `Blocked bool` and `BlockReason string`.
- Produces: `models.ChildProfile.SpecialCareCondition string` (a
  `special_care_condition_gate.condition_id`, e.g. `SC-CP`), and
  `specialCareGate(ctx, pool, p) (models.StepResult, bool, string, error)` returning the
  step record, whether it blocked, and the block reason.

- [ ] **Step 1: Add the input field**

In `internal/models/profile.go`, add to `ChildProfile` after the `ClinicalFlags` field:

```go
	// SpecialCareCondition is a special_care_condition_gate.condition_id (SC-DS, SC-CP,
	// SC-CHD, SC-CLP, SC-ASD, SC-ID). All six are STOP-REVIEW in the provider's master:
	// the engine stops and names the required reviewer rather than ranking recipes.
	//
	// Blocking needs no clinical sign-off because it is the conservative direction -- a
	// block never puts an unsafe recipe in front of an operator. Serving a special-care
	// recipe would need sign-off, and nothing here does that.
	SpecialCareCondition string `json:"special_care_condition,omitempty"`
```

- [ ] **Step 2: Write the failing test**

Create `internal/engine/special_care_test.go`:

```go
package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/models"
)

func TestSpecialCareConditionBlocksAndNamesTheReviewer(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, c := range []struct {
		conditionID string
		name        string
	}{
		{"SC-DS", "Down syndrome"},
		{"SC-CP", "cerebral palsy"},
		{"SC-CHD", "congenital heart disease"},
		{"SC-CLP", "cleft lip/palate"},
		{"SC-ASD", "autism"},
		{"SC-ID", "intellectual disability"},
	} {
		t.Run(c.conditionID, func(t *testing.T) {
			step, blocked, reason, err := specialCareGate(ctx, pool,
				models.ChildProfile{AgeMonths: 36, SpecialCareCondition: c.conditionID})
			if err != nil {
				t.Fatalf("specialCareGate: %v", err)
			}
			if !blocked {
				t.Fatalf("%s is STOP-REVIEW in the provider's master and must block", c.conditionID)
			}
			if reason == "" {
				t.Fatal("a block with no reason leaves the operator no next step")
			}
			// The reviewer is the operator's actual next action, so it has to be in the text.
			if !strings.Contains(strings.ToLower(reason), "clinic") &&
				!strings.Contains(strings.ToLower(reason), "team") {
				t.Fatalf("block reason must name the required reviewer, got %q", reason)
			}
			if step.Kind != "hard_filter" {
				t.Fatalf("the stop gate is a hard filter, got kind %q", step.Kind)
			}
		})
	}
}

func TestSpecialCareGateIsANoOpWhenNoConditionGiven(t *testing.T) {
	pool := testPool(t)
	step, blocked, reason, err := specialCareGate(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36})
	if err != nil {
		t.Fatalf("specialCareGate: %v", err)
	}
	if blocked || reason != "" {
		t.Fatalf("no condition declared must not block: blocked=%v reason=%q", blocked, reason)
	}
	if step.Note == "" {
		t.Fatal("a step that did nothing must say so rather than looking like it ran")
	}
}

// An unknown condition id is an error, not a silent pass. Accepting it would mean the
// operator believes they recorded a condition the engine never saw.
func TestSpecialCareGateRejectsAnUnknownCondition(t *testing.T) {
	pool := testPool(t)
	_, _, _, err := specialCareGate(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, SpecialCareCondition: "SC-NOPE"})
	if err == nil {
		t.Fatal("an unrecognised special-care condition id must error, not pass silently")
	}
}

// The whole pipeline must stop, not merely record a step. A ranked list alongside a block
// is exactly the false assurance the gate exists to prevent.
func TestRunReturnsNoRecipesForASpecialCareChild(t *testing.T) {
	pool := testPool(t)
	res, err := Run(context.Background(), pool,
		models.ChildProfile{AgeMonths: 36, SpecialCareCondition: "SC-CP"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Blocked {
		t.Fatal("a special-care condition must block the pipeline")
	}
	if len(res.Recipes) != 0 {
		t.Fatalf("a blocked result must carry no recipes, got %d", len(res.Recipes))
	}
	if res.BlockReason == "" {
		t.Fatal("a blocked result must carry a reason")
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/ -run SpecialCare -v
```

Expected: FAIL to build, `undefined: specialCareGate`.

- [ ] **Step 4: Write the gate**

Create `internal/engine/special_care.go`:

```go
package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
)

// specialCareGate implements OR-001 from the provider's Special-Care Output Rule Matrix:
// "special_care_condition != null ... -> STOP_SPECIAL_CARE_GENERATION", error code
// SC-HOLD-001, classified HARD.
//
// The workbook's README states the principle it enforces: "Condition is a STOP GATE, not a
// simple recipe filter." A positive diagnosis pauses generation, because the feeding
// decision for these children turns on swallowing assessment, feeding route, prescribed
// texture and sometimes a fluid order -- none of which this project collects, and none of
// which may be guessed.
//
// This blocks rather than ranks on purpose. Blocking is the conservative direction and
// needs no clinical sign-off: a block never puts an unsafe recipe in front of an operator.
// Before this gate existed the engine scored a child with cerebral palsy exactly like any
// other child, which GAP-019 named as its failure.
//
// Only OR-001 is implemented. OR-002 through OR-014 depend on inputs this project does not
// collect, and the stop is what makes their absence safe: no ranked list is produced for
// these children at all. GAP-022 records that.
func specialCareGate(ctx context.Context, pool *pgxpool.Pool, p models.ChildProfile) (models.StepResult, bool, string, error) {
	if p.SpecialCareCondition == "" {
		return models.StepResult{
			Step: 3, Name: "Special-care condition stop gate", Kind: "hard_filter",
			CandidatesIn: -1, CandidatesOut: -1,
			Note: "no special-care condition declared, gate is a no-op",
		}, false, "", nil
	}

	var condition, gateLevel, reviewer, automaticAction, stopIf string
	err := pool.QueryRow(ctx, `
		SELECT condition, coalesce(gate_level, ''), coalesce(mandatory_reviewer, ''),
		       coalesce(automatic_action, ''), coalesce(stop_if, '')
		FROM special_care_condition_gate
		WHERE condition_id = $1`, p.SpecialCareCondition).
		Scan(&condition, &gateLevel, &reviewer, &automaticAction, &stopIf)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.StepResult{}, false, "", fmt.Errorf(
			"engine: unknown special-care condition %q: %w", p.SpecialCareCondition, ErrInvalidProfile)
	}
	if err != nil {
		return models.StepResult{}, false, "", fmt.Errorf("engine: special-care gate lookup: %w", err)
	}

	// Every row the provider ships is STOP-REVIEW, pinned by
	// TestEverySpecialCareConditionIsAStopGate. If a reissued workbook ever carries a
	// lower gate level, refuse rather than inheriting a block the provider no longer
	// asks for -- and refuse rather than silently proceeding, because either choice is a
	// clinical decision this code cannot make.
	if gateLevel != "STOP-REVIEW" {
		return models.StepResult{}, false, "", fmt.Errorf(
			"engine: special-care condition %s carries gate_level %q rather than STOP-REVIEW; "+
				"classify it explicitly before the engine runs against it",
			p.SpecialCareCondition, gateLevel)
	}

	// The reason quotes the provider's own text rather than paraphrasing it. Paraphrasing
	// clinical instruction is how a summary becomes advice.
	reason := fmt.Sprintf(
		"%s (%s) is a STOP-REVIEW condition in the provider's Special-Care master. "+
			"Required action: %s Required reviewer: %s. "+
			"The provider's stop condition reads: %s",
		condition, p.SpecialCareCondition, automaticAction, reviewer, stopIf)

	return models.StepResult{
		Step: 3, Name: "Special-care condition stop gate", Kind: "hard_filter",
		CandidatesIn: -1, CandidatesOut: 0,
		Note: fmt.Sprintf("blocked by %s (OR-001, error code SC-HOLD-001)", p.SpecialCareCondition),
	}, true, reason, nil
}
```

- [ ] **Step 5: Wire it into the pipeline**

In `internal/engine/pipeline.go`, find where step 3 runs. It looks like this:

```go
	ids, step3, blocked, blockReason, err := clinicalFilter(ctx, pool, p, ids)
```

Immediately **before** that call, insert the gate so a special-care condition stops the
pipeline before any other clinical rule is consulted:

```go
	// The special-care stop gate runs ahead of the clinical rule filter. Both produce a
	// block, and this one is the broader statement: a STOP-REVIEW diagnosis pauses
	// generation regardless of what any individual rule says about the child's other
	// flags.
	scStep, scBlocked, scReason, err := specialCareGate(ctx, pool, p)
	if err != nil {
		return models.EngineResult{}, err
	}
	steps = append(steps, scStep)
	if scBlocked {
		return models.EngineResult{
			Steps:       steps,
			Blocked:     true,
			BlockReason: scReason,
		}, nil
	}
```

Match the surrounding early-return's field list exactly - read the existing blocked return
in `clinicalFilter`'s caller and copy its shape, including `UnscreenedAllergens` if that
return carries it.

- [ ] **Step 6: Fix the step-count assertion**

The pipeline now records one more step. In `internal/engine/pipeline_test.go`, the persona
assertion currently expects 15. Update the number and the explanation:

```go
			if len(result.Steps) != 16 {
				t.Fatalf("persona %q: expected 16 recorded steps (1-13, with steps 2 and 4 each "+
					"recorded twice -- a hard filter plus a ranker half -- and step 3 recorded "+
					"twice as the special-care stop gate plus the clinical rule filter; step 8 "+
					"has no data source and step 14 is a human release gate, neither runs in "+
					"the engine), got %d", c.name, len(result.Steps))
			}
```

- [ ] **Step 7: Run the engine tests**

```bash
TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./internal/engine/ -v 2>&1 | tail -40
```

Expected: PASS, including the four new special-care tests and the five personas at 16 steps.

- [ ] **Step 8: Fix the why-panel key comment**

`web/src/components/why-this-result-sheet.tsx` keys rows on `${s.step}-${s.kind}`. Step 3
now appears twice with the same kind (`hard_filter`), so the composite key is no longer
unique and React will warn.

Change the key to include the step name, which does distinguish them:

```tsx
          {result.steps.map((s) => (
            <div key={`${s.step}-${s.kind}-${s.name}`} className="border-b pb-2 font-mono text-xs">
```

and replace the comment above it with:

```tsx
          {/* Keyed on step number, kind AND name: steps 2 and 4 are each recorded twice as
              a hard filter plus a ranker half, and step 3 is recorded twice as two hard
              filters -- the special-care stop gate and the clinical rule filter -- so
              neither the number nor the number-and-kind pair is unique. Name is what
              separates the two step-3 rows, on the one screen whose job is to account for
              every step. */}
```

- [ ] **Step 9: Verify end to end**

```bash
go build ./... && go vet ./... && TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
cd web && npx --no-install tsc --noEmit && npm test
```

Then, with the API running, confirm the block over HTTP:

```bash
curl -s -X POST http://localhost:8080/api/search -H 'Content-Type: application/json' \
  -d '{"age_months":36,"special_care_condition":"SC-CP"}'
```

Expected: `"blocked": true`, an empty or absent `recipes`, and a `block_reason` naming
cerebral palsy and its required reviewer.

- [ ] **Step 10: Commit**

```bash
git add internal/engine/special_care.go internal/engine/special_care_test.go \
        internal/engine/pipeline.go internal/engine/pipeline_test.go \
        internal/models/profile.go web/src/components/why-this-result-sheet.tsx
git commit -m "Stop generation for a special-care condition

GAP-019 described the failure exactly: a child with cerebral palsy was
scored like any other child. The provider's Special-Care master answers it
in one line -- 'Condition is a STOP GATE, not a simple recipe filter' -- and
marks all six conditions STOP-REVIEW.

This implements OR-001 (STOP_SPECIAL_CARE_GENERATION, error code
SC-HOLD-001) and nothing else from the Output Rule Matrix. OR-002 through
OR-014 need feeding route, prescribed IDDSI level, fluid orders and post-op
status, none of which this project collects; the stop is what makes their
absence safe, because no ranked list is produced for these children at all.

Blocking needs no clinical sign-off: it is the conservative direction, and a
block never puts an unsafe recipe in front of an operator. The reason quotes
the provider's own automatic_action, mandatory_reviewer and stop_if verbatim
rather than paraphrasing them, because paraphrasing clinical instruction is
how a summary turns into advice.

A gate_level other than STOP-REVIEW errors rather than defaulting either
way: inheriting a block the provider no longer asks for, and proceeding
without one, are both clinical decisions this code cannot make.

The engine now records 16 steps; step 3 appears twice, and the why-panel key
takes the step name to keep the two rows distinct."
```

---

## Task 7: Surface the gate in the console, and update the docs

The gate is unreachable from the UI without a control, which would leave it the same kind
of orphan Tasks 2 and 4 just closed.

**Files:**
- Modify: `web/src/lib/types.ts` (`ChildProfile.special_care_condition`, `SpecialCareCondition`)
- Modify: `web/src/lib/api.ts` (`getSpecialCareConditions`)
- Modify: `internal/api/handlers/reference.go` (the conditions endpoint)
- Modify: `internal/api/router.go` (one route)
- Modify: `web/src/components/profile-form.tsx` (the select)
- Modify: `CLAUDE.md`, `README.md`, `docs/next-steps.md`

**Interfaces:**
- Consumes: `special_care_condition_gate` from Task 5,
  `models.ChildProfile.SpecialCareCondition` from Task 6.
- Produces: `GET /api/reference/special-care-conditions`.

- [ ] **Step 1: Add the reference endpoint**

Append to `internal/api/handlers/reference.go`:

```go
// ReferenceSpecialCareConditions returns the six STOP-REVIEW conditions, so the console's
// picker is built from the provider's master rather than a hardcoded list that could drift
// from what the engine actually blocks on.
//
// mandatory_reviewer is included because it is the operator's next action once the engine
// stops, and a picker that hides it makes the block look like a dead end.
func (h *Handlers) ReferenceSpecialCareConditions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT condition_id, condition, coalesce(gate_level, ''),
		       coalesce(mandatory_reviewer, ''), coalesce(automatic_action, '')
		FROM special_care_condition_gate ORDER BY condition_id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "special-care condition list failed: "+err.Error())
		return
	}
	defer rows.Close()

	type condition struct {
		ConditionID       string `json:"condition_id"`
		Condition         string `json:"condition"`
		GateLevel         string `json:"gate_level"`
		MandatoryReviewer string `json:"mandatory_reviewer"`
		AutomaticAction   string `json:"automatic_action"`
	}
	out := []condition{}
	for rows.Next() {
		var c condition
		if err := rows.Scan(&c.ConditionID, &c.Condition, &c.GateLevel,
			&c.MandatoryReviewer, &c.AutomaticAction); err != nil {
			writeError(w, http.StatusInternalServerError, "special-care condition scan failed: "+err.Error())
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "special-care condition rows failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
```

In `internal/api/router.go`, after the `book1-blocks` route:

```go
	r.Get("/api/reference/special-care-conditions", h.ReferenceSpecialCareConditions)
```

- [ ] **Step 2: Add the client type and function**

In `web/src/lib/types.ts`, add `special_care_condition?: string;` to the `ChildProfile`
interface, and append:

```ts
export interface SpecialCareCondition {
  condition_id: string;
  condition: string;
  gate_level: string;
  mandatory_reviewer: string;
  automatic_action: string;
}
```

In `web/src/lib/api.ts`, add `SpecialCareCondition` to the type import list and append:

```ts
export function getSpecialCareConditions(): Promise<SpecialCareCondition[]> {
  return request<SpecialCareCondition[]>("/api/reference/special-care-conditions");
}
```

- [ ] **Step 3: Add the control**

In `web/src/components/profile-form.tsx`, add to the imports, state, and the reference
fetch. State:

```tsx
  const [specialCare, setSpecialCare] = useState("");
  const [specialCareOptions, setSpecialCareOptions] = useState<SpecialCareCondition[]>([]);
```

Extend the existing `Promise.all` in the `useEffect` to include
`getSpecialCareConditions()` and `setSpecialCareOptions` its result.

Add to the `handleSubmit` payload:

```tsx
      special_care_condition: specialCare || undefined,
```

Render it after the clinical marker select:

```tsx
      <div className="space-y-1">
        <span className={label}>Special-care condition</span>
        <Select value={specialCare || NONE} onValueChange={(v) => setSpecialCare(v === NONE ? "" : v)}>
          <SelectTrigger><SelectValue placeholder="None" /></SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>None</SelectItem>
            {specialCareOptions.map((c) => (
              <SelectItem key={c.condition_id} value={c.condition_id}>
                {c.condition} ({c.condition_id})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          All six are STOP-REVIEW in the provider&apos;s master. Selecting one stops
          generation and names the required reviewer - it does not filter or rank.
        </p>
      </div>
```

- [ ] **Step 4: Verify the block renders**

```bash
cd web && npx --no-install tsc --noEmit && npm test && npm run build
```

Then select a special-care condition in the console and search. Expected: no results table,
and the block reason displayed naming the condition and its required reviewer.

- [ ] **Step 5: Update the three documents**

In `README.md`, add to the endpoint table:

```
| `/api/profiles/{childID}` | PUT | Create or replace one child's stored profile |
| `/api/profiles/{childID}` | GET | The stored profile verbatim |
| `/api/profiles/{childID}/engine-input` | GET | What the engine receives today, plus every stored fact the conversion dropped |
| `/api/reference/special-care-conditions` | GET | The 6 STOP-REVIEW conditions and their required reviewers |
```

In `CLAUDE.md`, add to the migration list:

```
  0015_special_care             9 special-care tables, 6 STOP-REVIEW condition gates
```

and update the engine step description to say the pipeline records 16 steps, with step 3
recorded twice - the special-care stop gate and the clinical rule filter.

In `docs/next-steps.md`, mark steps 1 through 6 done, and replace the "Step 7 onward"
preamble's first line with a note that the special-care master is imported and its OR-001
stop is implemented, while OR-002 through OR-014 remain blocked on intake fields that do
not exist.

- [ ] **Step 6: Final full verification**

```bash
scripts/dev_db.fish down && scripts/dev_db.fish up
DATABASE_URL=(scripts/dev_db.fish url) go run ./cmd/import
DATABASE_URL=(scripts/dev_db.fish url) go run ./cmd/enrich
go build ./... && go vet ./... && TEST_DATABASE_URL=(scripts/dev_db.fish url) go test ./...
cd web && npm test && npx --no-install tsc --noEmit
```

Expected: all green. `gap_register` should now hold 22 rows:

```bash
docker exec recipie-pg psql -U recipie -d recipie -c "SELECT count(*) FROM gap_register"
```

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/types.ts web/src/lib/api.ts web/src/components/profile-form.tsx \
        internal/api/handlers/reference.go internal/api/router.go \
        CLAUDE.md README.md docs/next-steps.md
git commit -m "Offer the special-care conditions in the console

The stop gate had no control, which would have left it the same kind of
orphan this branch just closed twice.

The picker is built from special_care_condition_gate rather than a hardcoded
list, so it cannot offer a condition the engine does not block on. Each
option carries the provider's required reviewer, because that is the
operator's next action once the engine stops and a picker that hides it makes
the block look like a dead end."
```

---

## Not in this plan, and why

- **Serving special-care recipe candidates.** 108 rows at `CANDIDATE-REVIEW`, zero mapped
  to `recipe_master`, and the workbook's own dashboard records clinical approval as NOT
  YET. Recorded as `GAP-021`.
- **OR-002 through OR-014.** They need feeding route, prescribed IDDSI level, cardiology
  fluid orders, post-operative status, pica and sensory profile. None are collected, and
  inventing them is what the hard rule forbids. Recorded as `GAP-022`.
- **The 221-field intake form.** `MadamGY_Complete_Pediatric_Google_Form_Blueprint_V1.docx`
  specifies an intake far wider than the engine's 13 inputs. Which subset to collect is a
  product decision, not an engineering one, and building 221 fields against an engine that
  reads 13 would produce a form that mostly discards its own input.
- **Filling the 6-11 month iron gap, writing preparation text, or marking anything
  approved.** Unchanged from every previous plan.

## Self-review

**Spec coverage.** The two authorities are `docs/next-steps.md` and the workbook.
next-steps steps 1-6 were already built before this plan; its remaining engineering item
was the Book Engine, which is out of scope here and stated as such. The workbook's
`Condition Stop Gates` sheet drives Task 6; the other eight sheets are imported by Task 5;
`Output Rule Matrix` OR-001 is implemented and OR-002..OR-014 are recorded as `GAP-022`
rather than silently skipped.

**Placeholder scan.** No TBD, no "add error handling", no "similar to Task N". Every code
step carries the actual code. Task 5 Step 3 names a specific failure mode (the `check`
reserved word) with a specific response rather than a vague "fix any issues".

**Type consistency.** `specialCareGate` returns
`(models.StepResult, bool, string, error)` in Task 6's interface block, its test, and its
implementation. `SpecialCareCondition` is the Go field in Task 6 and the TS interface name
in Task 7; the JSON key is `special_care_condition` in both. `profileDTO`/`toDTO`/`fromDTO`
are used consistently within Task 3. `StoredProfile` and `EngineInputResult` are defined in
Task 4 Step 1 and used in Steps 2-3.

**Known ordering hazard.** Task 6 changes the persona step count from 15 to 16 and Task 7
documents it. If Tasks 6 and 7 are split across sessions, the doc and the test disagree in
between. Task 6 Step 6 updates the test, so the build stays green either way.
