# Engine console reorientation - design

## Problem

`ProfileForm` (`web/src/components/profile-form.tsx`) renders roughly fifteen
field blocks - load-stored-profile, age, diet type, vegan, declared
allergens, suspected allergens, nutrition target marker, special-care
condition, clinical flags, region, cuisine cluster, meal type, budget band,
prep/cook time, result limit - as one unbroken vertical stack inside a fixed
300px `aside`. On the primary screen of the tool, an operator scrolls past
the whole list before reaching Search, and the page reads as mostly empty
scroll rather than a working console. CLAUDE.md's own screen table names
`Resizable` as a primary component for this route; it is not implemented,
so the split between form and results is a fixed ratio the operator cannot
adjust.

## Fix

Two changes, both presentational, no API or engine change:

1. **Resizable split.** Wrap the existing `aside` / `section` pair in
   shadcn's `ResizablePanelGroup` (`react-resizable-panels`), matching the
   component CLAUDE.md already names for this screen. Default proportions
   match the current 300px-ish split; the operator can drag it.

2. **Grouped, collapsible form.** Replace the flat stack with `Accordion`
   sections. Age (the one required field, and a hard filter) and Allergens
   (the other hard filter) stay open by default - nothing safety-relevant
   is ever hidden behind a click. Everything else - clinical flags, culture,
   logistics - is a ranker or a stop-gate an operator opens when the case
   calls for it, and starts collapsed:

   | Section | Default | Contents |
   |---|---|---|
   | Load stored profile | collapsed | child-id loader (unchanged) |
   | Basics | **open** | age *, diet type, vegan |
   | Allergens | **open** | declared allergens, suspected allergens |
   | Clinical | collapsed | nutrition target marker, special-care condition, clinical flags |
   | Culture & region | collapsed | region, cuisine cluster |
   | Logistics | collapsed | meal type, budget band, max prep/cook, result limit |

   The Search button moves outside the accordion and becomes a sticky
   footer (`sticky bottom-0`, background + top border) inside the form
   panel, so it never requires scrolling past a collapsed section to reach.

3. **Empty-state copy.** The results pane's placeholder ("No query run
   yet") gains one real sentence telling the operator what's required -
   not decoration, just the one fact the dashed box currently omits.

## Non-goals

- No change to `internal/api`, `internal/engine`, or any request/response
  shape. `ChildProfile` construction in `handleSubmit` is untouched.
- No change to which fields exist or what they send - purely how they are
  grouped and revealed.
- Other dense list/table pages (ingredients, audits, runs, gaps) keep their
  current single-column table layout - that density is the point of a
  devtool per CLAUDE.md's Frontend section, and is not the "very vertical"
  complaint this fixes.

## Files touched

- `web/src/components/ui/resizable.tsx` - new, via `npx shadcn add resizable` (pulls in `react-resizable-panels`).
- `web/src/app/page.tsx` - swap the plain two-column grid for `ResizablePanelGroup` / `ResizablePanel` / `ResizableHandle`; reword the empty state.
- `web/src/components/profile-form.tsx` - wrap field groups in `Accordion` sections; move Search to a sticky footer.
- `web/src/components/profile-form.test.tsx` - new, covers the collapse/reveal behavior and that the submit button stays reachable.

## Testing

`ProfileForm` fetches reference data on mount via six functions in
`@/lib/api`. The new test mocks that module (`vi.mock("@/lib/api")`,
each function resolving to `[]`) so the component renders without a
network call, matching the existing pattern in
`suspected-allergen-fieldset.test.tsx` (render + `fireEvent`, no
`user-event` dependency).

`npm test` and `npm run build` must both stay green; no Go-side test is
affected since nothing under `internal/` changes.
