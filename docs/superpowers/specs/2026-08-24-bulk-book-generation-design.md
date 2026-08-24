# Bulk book generation from a Google Form export

## Context

CLAUDE.md's blocker #6: doctors enter children into a Google Form, the export is
uploaded once, and the system generates a book set per child. The blocker has always
been the mapping — Google Forms exports the question text as the header row, and that
text changes whenever the form is edited, so the mapping has to come from a real export
rather than a guessed field list.

A real export landed in this session: one live submission, plus a screenshot of the
form's special-care question showing it is a **fixed checkbox list**, not free text.
Both are used directly below rather than a constructed sample. The raw export (real
child PII) is not committed anywhere in this repo or its history — only the header
row and the mapping derived from it are checked in.

## Decisions

Three architectural choices, made with the user directly:

1. **Mapping mechanism: hand-written seed**, not a mapping UI. Like
   `culture_region_map` — a checked-in Go map from exact header text to a profile
   field. Breaks loudly (rejects the whole file) if the provider edits the form
   wording; fixed by editing one file and redeploying. No new screen, no saved mapping
   table.
2. **Persistence: stateless per row.** Mirrors `POST /api/books/generate` — a row
   becomes `profile.Stored` in memory, produces PDFs, and is never written to
   `child_profile`. No dedup/upsert logic, no history, no regenerate-later. If an
   operator wants a child to persist, they re-enter that child through the existing
   single-child console flow afterward.
3. **Job execution: in-process, same dyno.** A worker goroutine in `madamgy-api`
   drains a DB-backed job queue table, one child at a time — never parallel, to
   respect the Chromium print's memory ceiling on the free instance (already
   documented as tight in `render.yaml`). No new Render service. Job state lives in
   Postgres, not memory, so it survives a dyno restart mid-batch.

## Scope: which form columns map, and why the rest don't

The real export has roughly 140 columns: consultation logistics, consent checkboxes,
document-upload links, and clinical detail no provider table has a matching field for.
Mapping everything would mean either dropping data silently (the thing this project's
hard rule forbids) or inventing a place to put it (the same rule, the other direction).
The resolution: map only what has a clean, non-invented path into `child_profile` or
the engine's input, and **name every dropped column's reason** rather than omitting it
quietly.

### Mapped

| `child_profile` field | Form column(s) | Transform |
|---|---|---|
| `display_name` | Child Full Name | direct |
| `date_of_birth` | Date of Birth | parse `DD/MM/YYYY` |
| `sex` | Sex used for pediatric growth reference | Female/Male → `female`/`male` |
| `region_culture` | State / Union Territory, Country | new hand-written state→region seed (same shape as `culture_region_map`); `Country = Bangladesh` short-circuits straight to the Bangladesh region regardless of the state field |
| `diet_type` | Household food practice | free text → `{Vegetarian, Eggetarian, Non-vegetarian}` by keyword, matching `recipe_master.diet_type`'s exact vocabulary (`internal/engine/diet.go`) |
| `vegan` | same column | `"vegan"` keyword |
| `religious_restriction` | Religious/cultural food restrictions | direct |
| growth measurement (weight, height, head circumference, date) | Current weight (kg), Current standing height, Head circumference, Date of current measurement | direct; z-scores stay empty — the form has none, and none is ever computed (same rule as the console path) |
| allergens | Known food allergy/intolerance?, Known/suspected food allergens, History of anaphylaxis? | free text → the 11-value `allergen_group` vocabulary from `allergen_tag_vocabulary`; anaphylaxis → `severity = systemic`; default `status = 'confirmed'` — see "Allergy status default" below |
| special-care condition | Does the child have any special-care diagnosis..., If yes, which condition(s)? | fixed checkbox list → `special_care_condition_gate.condition_id` — see "Special-care mapping" below |

### Deliberately dropped, with the reason logged per row when relevant

- `budget_band` — the form asks an open-ended amount, and there is no provider-defined
  bucket threshold anywhere in the masters. Inventing a bucketing rule to fill this
  field is exactly the "plausible-looking number" the hard rule forbids. Left null;
  the budget ranker step simply doesn't boost for these children, same as any other
  child with no budget preference stated.
- `max_prep_time_min` / `max_cook_time_min` — no matching question on the form
  (`Meal duration` is how long the child takes to eat, not a cooking time budget).
- `cuisine_code` — ranker-only field, no clean source column.
- Every clinical condition outside the special-care six — `clinical_rule_master`'s
  `trigger_field` vocabulary (`Dysphagia_Suspected`, `Coeliac_Status`, etc.) is coded,
  and the form's free-text "Current / past medical conditions" doesn't map to those
  codes without guessing. Left unset; the clinical-rule ranker step runs with fewer
  active triggers for these children, which is the existing behavior for any child
  whose conditions the ranker doesn't recognize.
- Consultation logistics, consent checkboxes, upload-document links (Google Drive
  URLs) — no fetch of arbitrary remote files, consistent with the existing "no bulk
  cover photograph" decision in CLAUDE.md's Scope section. These columns are read and
  immediately discarded, never fetched, never stored.

### Allergy status default

The form doesn't distinguish "confirmed diagnosis" from "family suspects" in its
allergen free-text column. Two wrong defaults are possible: marking everything
`suspected` under-restricts (an actually-allergic child sees recipes containing the
allergen, ranked down but not filtered); marking everything `confirmed` over-restricts
(a merely-suspected allergen removes recipes a child could safely eat). Given the
project's own stated asymmetry — a block is always the safe direction, over-exclusion
costs a shorter list, under-exclusion costs a reaction — the default is
`status = 'confirmed'`, `source = 'parent_reported'`.

### Special-care mapping

The real form's second special-care question is a fixed checkbox list, confirmed from
a screenshot of the live form, not inferred from free text:

| Form checkbox | `condition_id` |
|---|---|
| Down Syndrome | `SC-DS` |
| Cerebral palsy | `SC-CP` |
| Congenital heart disease | `SC-CHD` |
| Cleft lip/palate | `SC-CLP` |
| ASD with feeding selectivity/sensory issue | `SC-ASD` |
| Intellectual disability/global developmental delay with feeding issue | `SC-ID` |
| Neuromuscular disorder | *(no gate row)* |
| Dysphagia/aspiration risk | *(no gate row)* |
| Tube/enteral feeding | *(no gate row)* |
| Other (free text) | *(no gate row)* |

Any row answering **Yes or Unsure** to the primary special-care question is a
candidate stop, regardless of which box is checked. The six mapped boxes route
through the existing `engine.SpecialCareBlock` / `specialCareGate` unchanged — same
function the console and the single-child generate path already call, so there is no
second implementation of the safety gate to drift from the first.

The three unmapped boxes and "Other" — and a Yes/Unsure answer with no checked box
that maps — are **rejected from generation**, reported by name as "special-care
condition declared, not covered by the engine's stop-gate table — manual clinical
review needed." The row never falls through to a normal book: the engine has zero
filter for these conditions, so generating one would silently ignore a declared
feeding-safety concern, which is the dangerous direction. Attempting to force an
unmapped value through `specialCareGate` would also just error (it validates
`condition_id` against the DB table), so rejection is both the safe and the correct
technical outcome.

This resolves against the one real row available: the two dedicated special-care
columns read **No** / blank, while the "Relevant family history" and behavioral-detail
columns elsewhere on the same row describe autism in free text. That mismatch is
itself a rejection reason — "special-care question answered No but other fields
describe a special-care condition — mismatch, needs manual review" — never silently
generated as a general-population book on the strength of the dedicated question
alone.

## Components

```
internal/bulkintake/                    new package
  headers.go        exact header-text -> field mapping table (the checked-in seed)
  region_seed.go     state/UT -> region_culture, hand-written, same shape as
                      culture_region_map
  mapping.go         MapRow(rawRow) -> (profile.Stored, *book.ChildPhoto, rejectReason)
  csv.go              ParseRows(io.Reader) -> []rawRow, header validation

internal/api/handlers/bulk.go            new handlers
  POST   /api/bulk/jobs                  multipart CSV upload, returns job_id, enqueues
  GET    /api/bulk/jobs/{id}             status + counts (queued/running/done/failed)
  GET    /api/bulk/jobs/{id}/archive.zip download once done

internal/api/handlers/book_set.go        light refactor
  extract the core of renderSetWithPhoto (AssembleSet + ErrBlocked handling) into a
  helper that doesn't require http.ResponseWriter, so the bulk worker and the live
  HTTP handler call the identical path -- one implementation of "how a profile becomes
  a Set," not two that can drift.

internal/db/migrations/00NN_bulk_job.up.sql   new table, see below -- number picked at
                                                implementation time from the latest
                                                migration on disk; other sessions in this
                                                tree are landing migrations concurrently
```

### `bulk_job` table

```sql
CREATE TABLE bulk_job (
    job_id          bigserial PRIMARY KEY,
    status          text NOT NULL CHECK (status IN ('queued','running','done','failed'))
                        DEFAULT 'queued',
    uploaded_csv    bytea NOT NULL,
    total_rows      integer,
    processed_rows  integer NOT NULL DEFAULT 0,
    archive_zip     bytea,
    report_csv      bytea,
    error           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz
);
```

No foreign key into any provider table — this is pure job bookkeeping, not part of
the imported dataset, and carries no relationship the integrity suite needs to assert.
`uploaded_csv` and both output blobs live as `bytea` on the same row: no object
storage, consistent with the in-process/same-Postgres decisions above. A job is
naturally ephemeral (an operator downloads the archive once); retention/cleanup is
out of scope for v1.

## Data flow

```
operator uploads CSV
  -> POST /api/bulk/jobs stores the file in bulk_job (status=queued), returns job_id
  -> worker goroutine claims it: UPDATE ... SET status='running' WHERE job_id=$1 AND
     status='queued'  (atomic claim -- safe even across a dyno restart mid-poll)
  -> ParseRows validates the header row against the seed; a form-wording drift fails
     the whole job loudly (status=failed, error names the missing/unexpected column)
     rather than silently misaligning columns
  -> for each row, sequentially:
       MapRow -> profile.Stored, *book.ChildPhoto, rejectReason
       reject?              -> report row: rejected, reason
       special-care stop?   -> report row: special_care_stop, condition, reviewer
       else                 -> renderSetWithPhoto core -> two PDFs -> add to archive
                                report row: generated
       processed_rows++
  -> job done: archive_zip and report_csv written, status=done, finished_at set
operator polls GET /api/bulk/jobs/{id}, downloads archive.zip once done
```

Photographs: the export's upload-link columns (child photo, parent photo,
prescription, reports) are read from the CSV and discarded. No fetch of the Drive
URLs they contain -- same reasoning as the existing "no bulk cover photograph" call:
fetching arbitrary remote files into a document handed to a family is a decision this
project has already declined to make once, and nothing about a CSV column changes
that.

## Error handling

Three, and only three, per-row outcomes, matching CLAUDE.md's existing "no row
skipped silently" rule for batch processing:

- **generated** — both PDFs land in the archive, named `{slug}-book1.pdf` /
  `{slug}-book2.pdf` (same `slugOrDefault` the single-child zip path already uses).
- **special_care_stop** — no files; report names the condition and the provider's
  `mandatory_reviewer`, exactly what `engine.SpecialCareBlock` returns today.
- **rejected** — no files; report names the column and the reason (parse failure,
  missing required field, value outside a known vocabulary, unmapped special-care
  checkbox, special-care answer/detail-field mismatch).

Whole-job failure (`status=failed`) is reserved for the header row itself not
matching the seed -- a different failure mode from a bad individual row, and one that
means nothing in the file can be trusted, not just one line of it.

## Testing

- `internal/bulkintake/mapping_test.go` — table-driven over every field mapping in the
  table above; the state→region seed in both directions; diet/allergen/special-care
  keyword-to-code mapping; all three unmapped special-care checkboxes rejecting rather
  than falling through; the no/blank-vs-free-text-mismatch case (the real row) rejecting.
- `internal/bulkintake/csv_test.go` — malformed CSV, missing required column, header
  row present but wording drifted from the seed (must reject the whole file loudly).
- `internal/api/handlers/bulk_test.go` — job lifecycle queued→running→done; archive
  contains the right PDF count; report.csv accounts for every input row; a
  special-care row never appears in the archive.
- A unit test pinning that the HTTP handler's `renderSetWithPhoto` and the bulk
  worker's row path both call the same extracted core function — a regression guard
  against the two drifting into two different implementations of the safety gate.

No change to `internal/db/integrity_test.go`'s scope: `bulk_job` has no relationship
to the imported provider data.

## What this does not do

- Does not persist any child from a bulk run to `child_profile`. An operator who wants
  a child to have ongoing history re-enters them through the existing console flow.
- Does not build a mapping UI. A form-wording change means editing
  `internal/bulkintake/headers.go` and redeploying, the same recovery path
  `culture_region_map` already uses for the same kind of drift.
- Does not fetch any uploaded document, image, or report the form links to.
- Does not compute a z-score, a budget band, or a clinical-rule trigger the form
  doesn't cleanly supply. Every one of those stays an honest gap on the generated
  book, exactly as it does for a child entered by hand through the console today.
