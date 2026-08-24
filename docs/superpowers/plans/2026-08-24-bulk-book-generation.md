# Bulk Book Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn one uploaded Google Form export CSV into a downloadable archive of per-child book1/book2 PDFs plus a report.csv accounting for every row, without persisting any child to the database.

**Architecture:** A new `internal/bulkintake` package maps CSV rows (via a hand-written, checked-in header-to-field seed) into `profile.Stored` values or explicit rejection reasons. A new `internal/bulkjob` package owns a Postgres-backed job queue (`bulk_job` table) and a single in-process worker goroutine that drains it sequentially — never in parallel, to respect the Chromium print's memory ceiling on the free Render instance. The worker reuses `book.AssembleSet`, `book.RenderHTML`, and `book.PrintPDFAll` directly — the same functions the existing `/api/books/generate*` endpoints call — plus one new small shared helper, `book.BlockedDetail`, that both the existing HTTP handler and the new worker use to extract a stop-gate's reviewer name, so there is exactly one implementation of "how a block is reported," not two.

**Tech Stack:** Go, `encoding/csv` (stdlib), Postgres via pgx/v5, chi router, existing `internal/book` and `internal/profile` packages unchanged in their public surface except the one new helper.

**Spec:** `docs/superpowers/specs/2026-08-24-bulk-book-generation-design.md`

## Global Constraints

- No child from a bulk run is ever written to `child_profile` or any child table — stateless per row, matching `POST /api/books/generate`.
- The worker processes rows strictly sequentially, never in parallel — the free Render instance's Chromium print already runs near its memory ceiling.
- No row is ever skipped silently — every row ends in exactly one of three outcomes: `generated`, `special_care_stop`, or `rejected`, all named in `report.csv`.
- No fetch of any URL found in the CSV (photo/prescription/report upload links) — read and discard, never fetched.
- A special-care condition with no matching `special_care_condition_gate` row (`Neuromuscular disorder`, `Dysphagia/aspiration risk`, `Tube/enteral feeding`, `Other`) is always `rejected`, never generated as a general-population book.
- Migration number: check `ls internal/db/migrations/` immediately before creating the new migration file — other sessions are landing migrations concurrently in this tree, and the number in this plan (0026) may already be taken by the time you implement.
- No mention of claude/anthropic/ai anywhere — code, comments, commit messages. No attribution trailers. No emojis.

---

## File Structure

```
internal/book/blocked.go                    NEW — BlockedDetail helper, shared by HTTP + worker
internal/api/handlers/books.go               MODIFY — writeBlocked becomes a thin wrapper
internal/db/migrations/0026_bulk_job.up.sql  NEW — job queue table
internal/db/migrations/0026_bulk_job.down.sql NEW
internal/bulkintake/headers.go               NEW — required header text constants
internal/bulkintake/csv.go                   NEW — CSV parsing + header validation
internal/bulkintake/region.go                NEW — state/UT + country -> region_culture
internal/bulkintake/allergen.go              NEW — free text -> allergen_group
internal/bulkintake/specialcare.go           NEW — checkbox -> condition_id, mismatch check
internal/bulkintake/mapping.go               NEW — MapRow ties the above together
internal/bulkjob/job.go                      NEW — bulk_job DB access (enqueue/claim/finish)
internal/bulkjob/worker.go                   NEW — poll loop + per-row processing + archive/report
internal/api/handlers/bulk.go                NEW — upload/status/download endpoints
internal/api/router.go                       MODIFY — register the three bulk routes
cmd/server/main.go                           MODIFY — start the worker goroutine
CLAUDE.md                                    MODIFY — blocker #6 status update
```

---

### Task 1: Shared block-detail helper, extracted from the HTTP handler

**Files:**
- Create: `internal/book/blocked.go`
- Create: `internal/book/blocked_test.go`
- Modify: `internal/api/handlers/books.go:96-121` (the existing `writeBlocked` method)

**Interfaces:**
- Produces: `book.BlockedDetail(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time, err error) (reason, reviewer string)` — used by Task 9 (worker) and by the modified `writeBlocked`.

- [ ] **Step 1: Write the failing test**

```go
// internal/book/blocked_test.go
package book_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/profile"
)

func testPool(t *testing.T) *pgxpool.Pool {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestBlockedDetailNamesTheSpecialCareReviewer(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	s := profile.Stored{
		ChildID:     "bd-test-1",
		DateOfBirth: time.Now().AddDate(-3, 0, 0),
		Conditions: []profile.ClinicalCondition{
			{TriggerField: "Special_Care_Condition", FlagValue: "SC-DS", Class: "chronic"},
		},
	}
	asOf := time.Now().UTC()
	err := errors.New(book.ErrBlocked.Error() + ": condition SC-DS is a STOP-REVIEW gate")

	reason, reviewer := book.BlockedDetail(ctx, pool, s, asOf, err)

	if reason != "condition SC-DS is a STOP-REVIEW gate" {
		t.Errorf("reason = %q, want the trimmed message", reason)
	}
	if reviewer == "" {
		t.Errorf("reviewer = %q, want the provider's mandatory_reviewer for SC-DS", reviewer)
	}
}

func TestBlockedDetailOmitsReviewerWhenNoSpecialCareCondition(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	s := profile.Stored{ChildID: "bd-test-2", DateOfBirth: time.Now().AddDate(-3, 0, 0)}
	asOf := time.Now().UTC()
	err := errors.New(book.ErrBlocked.Error() + ": clinical rule filter blocked this child")

	reason, reviewer := book.BlockedDetail(ctx, pool, s, asOf, err)

	if reason != "clinical rule filter blocked this child" {
		t.Errorf("reason = %q, want the trimmed message", reason)
	}
	if reviewer != "" {
		t.Errorf("reviewer = %q, want empty: this block carries no special-care condition", reviewer)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/book/... -run TestBlockedDetail -v`
Expected: FAIL with "undefined: book.BlockedDetail"

- [ ] **Step 3: Write the implementation**

```go
// internal/book/blocked.go
package book

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/profile"
)

// BlockedDetail extracts the human-facing pieces of a stop-gate block: the reason text and,
// when the block carries a special-care condition, the provider's own mandatory_reviewer for
// it.
//
// The reviewer comes from a fresh, direct read of special_care_condition_gate rather than
// from parsing err's text further than the ErrBlocked prefix -- the assembler's message is
// prose for a human, and the hard rule against inventing data applies just as much to a field
// pulled out of that prose by string matching as to one guessed outright. A block can also
// come from the clinical-rule filter rather than the special-care stop gate; that child
// carries no special-care condition id, there is nothing to look up, and reviewer is empty
// rather than filled with a guess.
//
// Shared by the HTTP handler's writeBlocked and the bulk worker's per-row processing, so there
// is exactly one implementation of "how a block is reported," not two that could drift.
func BlockedDetail(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time, err error) (reason, reviewer string) {
	reason = strings.TrimPrefix(err.Error(), ErrBlocked.Error()+": ")

	cp, _, cerr := s.ToChildProfile(asOf)
	if cerr != nil || cp.SpecialCareCondition == "" {
		return reason, ""
	}

	var rev string
	qerr := pool.QueryRow(ctx,
		`SELECT coalesce(mandatory_reviewer, '') FROM special_care_condition_gate WHERE condition_id = $1`,
		cp.SpecialCareCondition).Scan(&rev)
	if qerr == nil && rev != "" {
		reviewer = rev
	}
	return reason, reviewer
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/book/... -run TestBlockedDetail -v`
Expected: PASS

- [ ] **Step 5: Update `writeBlocked` to use the shared helper**

Replace the body of `writeBlocked` in `internal/api/handlers/books.go` (currently lines ~107-121):

```go
func (h *Handlers) writeBlocked(w http.ResponseWriter, r *http.Request, s profile.Stored, asOf time.Time, err error) {
	reason, reviewer := book.BlockedDetail(r.Context(), h.pool, s, asOf, err)
	body := map[string]string{"error": reason}
	if reviewer != "" {
		body["reviewer"] = reviewer
	}
	writeJSON(w, http.StatusConflict, body)
}
```

Remove the now-unused `strings` import from `books.go` if nothing else in the file uses it — check with `grep -n "strings\." internal/api/handlers/books.go` first, since `joinOmissions` also uses `strings.Join`.

- [ ] **Step 6: Run the full handlers test suite to confirm no regression**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/... ./internal/book/... -v`
Expected: PASS, including the existing special-care 409 tests in `internal/api/handlers/books_test.go` and `internal/book/*_test.go`.

- [ ] **Step 7: Commit**

```bash
git add internal/book/blocked.go internal/book/blocked_test.go internal/api/handlers/books.go
git commit -m "$(cat <<'EOF'
Extract the stop-gate reviewer lookup into book.BlockedDetail

Shared groundwork for the bulk book generation worker, which needs the
same reviewer lookup the HTTP handler already does, without an
http.ResponseWriter to write to.
EOF
)"
```

---

### Task 2: `bulk_job` migration

**Files:**
- Create: `internal/db/migrations/0026_bulk_job.up.sql` (verify 0026 is free first — see Global Constraints)
- Create: `internal/db/migrations/0026_bulk_job.down.sql`

**Interfaces:**
- Produces: table `bulk_job(job_id, status, uploaded_csv, total_rows, processed_rows, archive_zip, report_csv, error, created_at, finished_at)`, consumed by Task 8 (`internal/bulkjob/job.go`).

- [ ] **Step 1: Check the next free migration number**

Run: `ls internal/db/migrations/ | grep -oP '^\d+' | sort -un | tail -3`

If `0026` already exists, use the next free number instead and rename every reference to it throughout this task and Task 8.

- [ ] **Step 2: Write the up migration**

```sql
-- internal/db/migrations/0026_bulk_job.up.sql

-- Bulk book generation job queue.
--
-- One row per uploaded Google Form export. A job is processed by a single in-process worker
-- goroutine, never in parallel with another job or with a live single-child print request --
-- the free instance's Chromium print already runs near its memory ceiling (see render.yaml).
-- State lives here rather than in memory so a job survives a dyno restart mid-batch: the
-- worker claims a queued job with an atomic UPDATE, so a restart just means the claim never
-- happened and the job stays queued for the next poll.
--
-- No child produced by a job is ever written to child_profile -- this table is pure job
-- bookkeeping, not part of the imported provider dataset, and carries no foreign key into it.
CREATE TABLE bulk_job (
    job_id          bigserial PRIMARY KEY,
    status          text NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed'))
                        DEFAULT 'queued',

    -- The uploaded file itself, kept until the job finishes so the worker can be a separate
    -- goroutine that doesn't share memory with the HTTP handler that received the upload.
    uploaded_csv    bytea NOT NULL,

    total_rows      integer,
    processed_rows  integer NOT NULL DEFAULT 0,

    -- Both NULL until the job reaches 'done'. No object storage: bytea on this row is the
    -- whole persistence story, consistent with the in-process/same-Postgres decisions this
    -- feature is built on. A job is naturally ephemeral -- an operator downloads the archive
    -- once -- so there is no retention policy here beyond "don't grow forever," which is a
    -- later concern, not a v1 one.
    archive_zip     bytea,
    report_csv      bytea,

    -- Set only when status = 'failed', which is reserved for the header row itself not
    -- matching the required seed -- a different failure mode from a bad individual row (those
    -- are accounted for inside report_csv instead, never here).
    error           text,

    created_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz
);

COMMENT ON TABLE bulk_job IS
    'One row per bulk book generation run from an uploaded Google Form export. Stateless per '
    'child -- no row here ever produces a child_profile record. See CLAUDE.md blocker #6.';
```

- [ ] **Step 3: Write the down migration**

```sql
-- internal/db/migrations/0026_bulk_job.down.sql
DROP TABLE IF EXISTS bulk_job;
```

- [ ] **Step 4: Apply and verify**

Run:
```bash
scripts/dev_db.fish up
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/db/... -run TestNothing -v
```
(There is no `TestNothing`; this just forces the migration runner via `db.Connect`, which migrates on startup. Confirm no error, then:)

Run: `psql $(scripts/dev_db.fish url) -c '\d bulk_job'`
Expected: the table exists with the columns above.

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/0026_bulk_job.up.sql internal/db/migrations/0026_bulk_job.down.sql
git commit -m "$(cat <<'EOF'
Add the bulk_job table for CSV-driven bulk book generation

Pure job bookkeeping, no relationship to the imported provider data --
job state lives in Postgres so a batch survives a dyno restart mid-run.
EOF
)"
```

---

### Task 3: CSV parsing and header validation

**Files:**
- Create: `internal/bulkintake/headers.go`
- Create: `internal/bulkintake/csv.go`
- Test: `internal/bulkintake/csv_test.go`

**Interfaces:**
- Produces:
  - `type RawRow map[string]string`
  - `RequiredHeaders []string` (package var, the exact header text this system depends on)
  - `ParseRows(r io.Reader) ([]RawRow, error)` — returns an error naming every missing required header if the header row doesn't match, before returning any rows.
- Consumed by: Task 7 (`mapping.go`), Task 9 (worker).

- [ ] **Step 1: Write the failing tests**

```go
// internal/bulkintake/csv_test.go
package bulkintake

import (
	"strings"
	"testing"
)

func TestParseRowsReadsOneRowByHeaderName(t *testing.T) {
	csv := "Child Full Name,Date of Birth,Sex used for pediatric growth reference\n" +
		"Joyshree Debnath,03/05/2023,Female\n"
	rows, err := ParseRows(strings.NewReader(minimalHeaderCSV(csv)))
	if err != nil {
		t.Fatalf("ParseRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0]["Child Full Name"] != "Joyshree Debnath" {
		t.Errorf("Child Full Name = %q, want Joyshree Debnath", rows[0]["Child Full Name"])
	}
}

func TestParseRowsRejectsAFileMissingARequiredHeader(t *testing.T) {
	// Every required header except "Date of Birth".
	var cols []string
	for _, h := range RequiredHeaders {
		if h == "Date of Birth" {
			continue
		}
		cols = append(cols, h)
	}
	csv := strings.Join(cols, ",") + "\n"

	_, err := ParseRows(strings.NewReader(csv))
	if err == nil {
		t.Fatal("ParseRows: want error for missing required header, got nil")
	}
	if !strings.Contains(err.Error(), "Date of Birth") {
		t.Errorf("error %q does not name the missing header", err.Error())
	}
}

func TestParseRowsOnEmptyFile(t *testing.T) {
	_, err := ParseRows(strings.NewReader(""))
	if err == nil {
		t.Fatal("ParseRows: want error on an empty file, got nil")
	}
}

// minimalHeaderCSV pads a partial header+row pair with every other required header, empty,
// so tests can focus on the columns they care about without hand-writing all sixteen.
func minimalHeaderCSV(partial string) string {
	lines := strings.SplitN(partial, "\n", 2)
	givenHeader := strings.Split(lines[0], ",")
	given := map[string]bool{}
	for _, h := range givenHeader {
		given[h] = true
	}
	var extra []string
	for _, h := range RequiredHeaders {
		if !given[h] {
			extra = append(extra, h)
		}
	}
	fullHeader := append(givenHeader, extra...)
	row := lines[1]
	if row != "" {
		for range extra {
			row = strings.TrimRight(row, "\n") + ","
		}
	}
	return strings.Join(fullHeader, ",") + "\n" + row
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bulkintake/... -v`
Expected: FAIL — package `bulkintake` does not exist yet.

- [ ] **Step 3: Write `headers.go`**

```go
// internal/bulkintake/headers.go
package bulkintake

// RequiredHeaders is the exact question text this system depends on, copied verbatim from a
// real export of the live form (session of 2026-08-24). Google Forms exports the question
// text as the header row, and that text changes whenever the form is edited -- so a file
// whose header row doesn't contain every one of these, verbatim, is rejected whole rather
// than silently misaligning columns. See docs/superpowers/specs/2026-08-24-bulk-book-generation-design.md.
var RequiredHeaders = []string{
	"Child Full Name",
	"Date of Birth",
	"Sex used for pediatric growth reference",
	"State / Union Territory",
	"Country",
	"Household food practice",
	"Religious/cultural food restrictions",
	"Date of current measurement",
	"Current weight (kg)",
	"Current standing height / recumbent length",
	"Head circumference",
	"Known food allergy/intolerance?",
	"Known / suspected food allergens",
	"History of anaphylaxis?",
	"Does the child have any special-care diagnosis or feeding condition requiring additional review?",
	"If yes, which condition(s)?",
	// Free-text columns read only for the special-care mismatch check (Task 6) -- not mapped
	// to any profile field on their own.
	"Relevant family history (Select whichever is appropriate, ignore if none)",
	"Any developmental concern?",
	"Areas of concern",
}
```

- [ ] **Step 4: Write `csv.go`**

```go
// internal/bulkintake/csv.go
package bulkintake

import (
	"encoding/csv"
	"fmt"
	"io"
)

// RawRow is one submission, keyed by the form's own question text. Looking up by name rather
// than by column position is what makes this tolerant of the provider reordering columns
// (adding a question in the middle of the form) without a code change -- only a renamed or
// removed column, which changes the text itself, is a breaking change, and that is exactly
// the case ParseRows rejects loudly.
type RawRow map[string]string

// ParseRows reads a Google Form CSV export and returns one RawRow per submission.
//
// The header row is validated against RequiredHeaders before any row is read: a form edited
// since this system's header seed was written must fail the whole file, not silently produce
// rows with some fields missing. That mirrors the existing rule for the rest of this project's
// batch processing -- a bad file is reported, not partially trusted.
func ParseRows(r io.Reader) ([]RawRow, error) {
	cr := csv.NewReader(r)
	// The form's free-text answers can legitimately contain commas inside quoted fields;
	// encoding/csv already handles RFC 4180 quoting by default, so no FieldsPerRecord
	// override is needed here beyond leaving it at -1 while validating.
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("bulkintake: empty file, no header row")
	}
	if err != nil {
		return nil, fmt.Errorf("bulkintake: read header row: %w", err)
	}

	present := make(map[string]bool, len(header))
	for _, h := range header {
		present[h] = true
	}
	var missing []string
	for _, want := range RequiredHeaders {
		if !present[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"bulkintake: form header does not match the required seed -- missing column(s): %v. "+
				"The form was likely edited; update internal/bulkintake/headers.go to match the "+
				"current export and redeploy", missing)
	}

	var rows []RawRow
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bulkintake: read row %d: %w", len(rows)+2, err)
		}
		row := make(RawRow, len(header))
		for i, h := range header {
			if i < len(record) {
				row[h] = record[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/bulkintake/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/bulkintake/headers.go internal/bulkintake/csv.go internal/bulkintake/csv_test.go
git commit -m "$(cat <<'EOF'
Parse a Google Form export CSV, rejecting the whole file on header drift

Header lookup is by exact question text, not column position, so a
reordered question is tolerated and a renamed or removed one fails
loudly instead of silently misaligning columns.
EOF
)"
```

---

### Task 4: State/UT + country to region_culture mapping

**Files:**
- Create: `internal/bulkintake/region.go`
- Test: `internal/bulkintake/region_test.go`

**Interfaces:**
- Produces: `RegionForRow(state, country string) (regionCulture string, ok bool)`.
- Consumed by: Task 7 (`mapping.go`).

Context: `region_culture` on `child_profile` is validated by `internal/api/handlers/profiles.go`'s `profileVocabularies` against `SELECT region_culture FROM region_focus` — exactly 8 values: `West Bengal / East India`, `South India`, `North India`, `West India`, `Bangladesh`, `Northeast India`, `Central / Tribal India`, `Himalayan India` (see CLAUDE.md's "Region focus" table). Any state text this seed doesn't recognize must not produce an unrecognized `region_culture` value — it must be a mapping failure the row is rejected for, not a raw pass-through of the state name.

- [ ] **Step 1: Write the failing test**

```go
// internal/bulkintake/region_test.go
package bulkintake

import "testing"

func TestRegionForRowBangladeshShortCircuitsOnCountry(t *testing.T) {
	got, ok := RegionForRow("Dhaka", "Bangladesh")
	if !ok || got != "Bangladesh" {
		t.Errorf("got %q, %v; want Bangladesh, true", got, ok)
	}
}

func TestRegionForRowWestBengalMapsToEastIndiaTier(t *testing.T) {
	got, ok := RegionForRow("West Bengal", "India")
	if !ok || got != "West Bengal / East India" {
		t.Errorf("got %q, %v; want \"West Bengal / East India\", true", got, ok)
	}
}

func TestRegionForRowTamilNaduMapsToSouthIndia(t *testing.T) {
	got, ok := RegionForRow("Tamil Nadu", "India")
	if !ok || got != "South India" {
		t.Errorf("got %q, %v; want South India, true", got, ok)
	}
}

func TestRegionForRowUnrecognisedStateFails(t *testing.T) {
	_, ok := RegionForRow("Atlantis", "India")
	if ok {
		t.Error("want ok=false for an unrecognised state, got true")
	}
}

func TestEveryRegionSeedValueIsAKnownRegionFocusRow(t *testing.T) {
	known := map[string]bool{
		"West Bengal / East India": true, "South India": true, "North India": true,
		"West India": true, "Bangladesh": true, "Northeast India": true,
		"Central / Tribal India": true, "Himalayan India": true,
	}
	for state, region := range stateRegionSeed {
		if !known[region] {
			t.Errorf("state %q maps to %q, which is not one of region_focus's 8 values", state, region)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bulkintake/... -run TestRegion -v`
Expected: FAIL — undefined `RegionForRow`, `stateRegionSeed`.

- [ ] **Step 3: Write the implementation**

```go
// internal/bulkintake/region.go
package bulkintake

import "strings"

// stateRegionSeed maps an Indian state/union territory, as a doctor would type it on the
// intake form, to one of region_focus's 8 region_culture values. Hand-written from the
// standard list of Indian states and union territories against the same India/state/zone
// groupings CLAUDE.md's "Region focus" table already documents -- not derived, not fuzzy,
// same discipline as culture_region_map.
//
// Values are lower-cased for lookup; RegionForRow does the case-folding.
var stateRegionSeed = map[string]string{
	// West Bengal / East India
	"west bengal": "West Bengal / East India",
	"odisha":      "West Bengal / East India",
	"orissa":      "West Bengal / East India",
	"bihar":       "West Bengal / East India",
	"jharkhand":   "West Bengal / East India",

	// South India
	"tamil nadu":     "South India",
	"kerala":         "South India",
	"karnataka":      "South India",
	"andhra pradesh": "South India",
	"telangana":      "South India",
	"puducherry":     "South India",

	// North India
	"punjab":      "North India",
	"haryana":     "North India",
	"uttar pradesh": "North India",
	"delhi":       "North India",
	"rajasthan":   "North India",
	"chandigarh":  "North India",

	// West India
	"gujarat":     "West India",
	"maharashtra": "West India",
	"goa":         "West India",

	// Northeast India
	"assam":       "Northeast India",
	"sikkim":      "Northeast India",
	"meghalaya":   "Northeast India",
	"manipur":     "Northeast India",
	"mizoram":     "Northeast India",
	"nagaland":    "Northeast India",
	"tripura":     "Northeast India",
	"arunachal pradesh": "Northeast India",

	// Central / Tribal India
	"madhya pradesh": "Central / Tribal India",
	"chhattisgarh":   "Central / Tribal India",

	// Himalayan India
	"jammu and kashmir": "Himalayan India",
	"jammu & kashmir":   "Himalayan India",
	"ladakh":            "Himalayan India",
	"himachal pradesh":  "Himalayan India",
	"uttarakhand":       "Himalayan India",
}

// RegionForRow resolves a form row's state/UT and country into one of region_focus's 8
// region_culture values.
//
// Country is checked first and short-circuits: a Bangladeshi respondent's "state" field is
// meaningless for this project's region tiers regardless of what they typed there, since
// Bangladesh is its own single-tier region with no further subdivision in region_focus.
func RegionForRow(state, country string) (regionCulture string, ok bool) {
	c := strings.ToLower(strings.TrimSpace(country))
	if c == "bangladesh" {
		return "Bangladesh", true
	}

	s := strings.ToLower(strings.TrimSpace(state))
	region, found := stateRegionSeed[s]
	return region, found
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bulkintake/... -run TestRegion -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bulkintake/region.go internal/bulkintake/region_test.go
git commit -m "$(cat <<'EOF'
Add the state/UT-to-region_culture seed for bulk intake

Hand-written, same discipline as culture_region_map: an unrecognised
state fails the lookup rather than passing raw text through to a
region_culture the engine's vocabulary check would reject anyway.
EOF
)"
```

---

### Task 5: Allergen free-text mapping

**Files:**
- Create: `internal/bulkintake/allergen.go`
- Test: `internal/bulkintake/allergen_test.go`

**Interfaces:**
- Produces: `MapAllergens(text string) (groups []string, ok bool)` — `ok` is false when the text matches no known group.
- Consumed by: Task 7 (`mapping.go`).

Context: the 11-value `allergen_group` vocabulary is `Egg, Fish, Milk, Peanut, Sesame, Soy, Wheat, Crustacean/Mollusc, Mustard, Sulphites, Tree nuts` (from `allergen_tag_vocabulary`, migration `0011`). `child_allergen.status` is set to `'confirmed'` unconditionally by the caller (Task 7) per the spec's stated safe-direction default — this task only extracts which groups a free-text answer names.

- [ ] **Step 1: Write the failing test**

```go
// internal/bulkintake/allergen_test.go
package bulkintake

import (
	"reflect"
	"testing"
)

func TestMapAllergensPeanutBeforeGenericNut(t *testing.T) {
	got, ok := MapAllergens("child reacts to peanuts and walnuts")
	if !ok {
		t.Fatal("want ok=true")
	}
	want := []string{"Peanut", "Tree nuts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMapAllergensMilkAndEgg(t *testing.T) {
	got, ok := MapAllergens("Milk and egg allergy confirmed by pediatrician")
	if !ok {
		t.Fatal("want ok=true")
	}
	want := []string{"Egg", "Milk"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMapAllergensShellfish(t *testing.T) {
	got, ok := MapAllergens("shrimp and crab")
	if !ok {
		t.Fatal("want ok=true")
	}
	want := []string{"Crustacean/Mollusc"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMapAllergensUnrecognisedTextFails(t *testing.T) {
	_, ok := MapAllergens("rash after eating outside food, cause unclear")
	if ok {
		t.Error("want ok=false: no known allergen group is named in this text")
	}
}

func TestMapAllergensEmptyTextFails(t *testing.T) {
	_, ok := MapAllergens("")
	if ok {
		t.Error("want ok=false for empty text")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bulkintake/... -run TestMapAllergens -v`
Expected: FAIL — undefined `MapAllergens`.

- [ ] **Step 3: Write the implementation**

```go
// internal/bulkintake/allergen.go
package bulkintake

import "strings"

// allergenKeywords maps a lower-case keyword found in the form's free-text allergen answer to
// one of allergen_tag_vocabulary's 11 allergen_group values (migration 0011). Ordered so a
// caller checking "peanut" before "nut" never mis-files a peanut allergy under Tree nuts --
// see the ordered check in MapAllergens, not map iteration order, which Go does not guarantee.
var allergenKeywordOrder = []struct {
	keyword string
	group   string
}{
	{"peanut", "Peanut"},
	{"groundnut", "Peanut"},
	{"egg", "Egg"},
	{"fish", "Fish"},
	{"milk", "Milk"},
	{"dairy", "Milk"},
	{"sesame", "Sesame"},
	{"til", "Sesame"},
	{"soy", "Soy"},
	{"soya", "Soy"},
	{"wheat", "Wheat"},
	{"gluten", "Wheat"},
	{"shellfish", "Crustacean/Mollusc"},
	{"prawn", "Crustacean/Mollusc"},
	{"shrimp", "Crustacean/Mollusc"},
	{"crab", "Crustacean/Mollusc"},
	{"mollusc", "Crustacean/Mollusc"},
	{"mustard", "Mustard"},
	{"sulphite", "Sulphites"},
	{"sulfite", "Sulphites"},
	{"almond", "Tree nuts"},
	{"cashew", "Tree nuts"},
	{"walnut", "Tree nuts"},
	{"pistachio", "Tree nuts"},
	{"tree nut", "Tree nuts"},
	// Bare "nut" last: it would otherwise match inside "peanut" and "groundnut" before
	// those more specific entries get a chance, mis-filing a peanut allergy as Tree nuts.
	{"nut", "Tree nuts"},
}

// MapAllergens extracts every allergen_group named in a free-text answer.
//
// ok is false when nothing recognisable is found, which the caller (Task 7's MapRow) treats
// as a rejection when the row separately declares a known food allergy -- silently producing
// a book with no allergen filter for a child whose family reported one is the dangerous
// direction, so an unmatched declaration is reported for manual review rather than dropped.
func MapAllergens(text string) (groups []string, ok bool) {
	lower := strings.ToLower(text)
	seen := map[string]bool{}
	var found []string
	for _, kw := range allergenKeywordOrder {
		if strings.Contains(lower, kw.keyword) && !seen[kw.group] {
			seen[kw.group] = true
			found = append(found, kw.group)
		}
	}
	if len(found) == 0 {
		return nil, false
	}
	// Stable, alphabetical output regardless of keyword-match order, so callers and tests
	// don't depend on allergenKeywordOrder's incidental ordering.
	sortStrings(found)
	return found, true
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bulkintake/... -run TestMapAllergens -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bulkintake/allergen.go internal/bulkintake/allergen_test.go
git commit -m "$(cat <<'EOF'
Add free-text-to-allergen_group mapping for bulk intake

Peanut and tree-nut keywords are checked in an explicit order so a
peanut allergy is never mis-filed under Tree nuts by a bare "nut"
substring match.
EOF
)"
```

---

### Task 6: Special-care checkbox mapping and mismatch detection

**Files:**
- Create: `internal/bulkintake/specialcare.go`
- Test: `internal/bulkintake/specialcare_test.go`

**Interfaces:**
- Produces:
  - `MapSpecialCareCheckbox(label string) (conditionID string, ok bool)`
  - `FreeTextMentionsSpecialCare(fields ...string) bool`
- Consumed by: Task 7 (`mapping.go`).

Context: the form's "If yes, which condition(s)?" is a fixed checkbox list (confirmed from a screenshot of the live form). Six of its nine possible values map to real `special_care_condition_gate.condition_id` rows; three plus "Other" do not and must never fall through to a normal book.

- [ ] **Step 1: Write the failing test**

```go
// internal/bulkintake/specialcare_test.go
package bulkintake

import "testing"

func TestMapSpecialCareCheckboxKnownConditions(t *testing.T) {
	cases := map[string]string{
		"Down Syndrome":                  "SC-DS",
		"Cerebral palsy":                 "SC-CP",
		"Congenital heart disease":       "SC-CHD",
		"Cleft lip/palate":               "SC-CLP",
		"ASD with feeding selectivity/sensory issue": "SC-ASD",
		"Intellectual disability/global developmental delay with feeding issue": "SC-ID",
	}
	for label, want := range cases {
		got, ok := MapSpecialCareCheckbox(label)
		if !ok || got != want {
			t.Errorf("MapSpecialCareCheckbox(%q) = %q, %v; want %q, true", label, got, ok, want)
		}
	}
}

func TestMapSpecialCareCheckboxUnmappedOptionsFail(t *testing.T) {
	for _, label := range []string{
		"Neuromuscular disorder", "Dysphagia/aspiration risk", "Tube/enteral feeding",
		"Other", "None", "",
	} {
		if _, ok := MapSpecialCareCheckbox(label); ok {
			t.Errorf("MapSpecialCareCheckbox(%q): want ok=false, this option has no gate row", label)
		}
	}
}

func TestFreeTextMentionsSpecialCareFindsAutism(t *testing.T) {
	if !FreeTextMentionsSpecialCare("Autism/neurodevelopmental condition", "") {
		t.Error("want true: family history names autism")
	}
}

func TestFreeTextMentionsSpecialCareFindsNothingOrdinary(t *testing.T) {
	if FreeTextMentionsSpecialCare("Allergic rhinitis in father", "picky eating") {
		t.Error("want false: nothing here names a special-care condition")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bulkintake/... -run "TestMapSpecialCareCheckbox|TestFreeTextMentionsSpecialCare" -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write the implementation**

```go
// internal/bulkintake/specialcare.go
package bulkintake

import "strings"

// specialCareCheckboxSeed maps the live form's "If yes, which condition(s)?" checkbox labels,
// copied verbatim from a screenshot of the form taken 2026-08-24, to special_care_condition_
// gate.condition_id. Only 6 of the form's 9 checkbox options have a gate row; the other 3 and
// "Other" are deliberately absent from this map, and MapSpecialCareCheckbox reports ok=false
// for them rather than inventing a condition_id.
var specialCareCheckboxSeed = map[string]string{
	"Down Syndrome":                  "SC-DS",
	"Cerebral palsy":                 "SC-CP",
	"Congenital heart disease":       "SC-CHD",
	"Cleft lip/palate":               "SC-CLP",
	"ASD with feeding selectivity/sensory issue":                             "SC-ASD",
	"Intellectual disability/global developmental delay with feeding issue": "SC-ID",
}

// MapSpecialCareCheckbox resolves one checked box's label to a condition_id.
//
// ok is false for "Neuromuscular disorder", "Dysphagia/aspiration risk", "Tube/enteral
// feeding", "Other", "None", or anything unrecognised -- the caller (Task 7) rejects the row
// rather than either forcing an invalid condition_id through the engine's stop gate (which
// would just error) or silently generating a normal book for a declared feeding-safety
// concern the engine has no filter for.
func MapSpecialCareCheckbox(label string) (conditionID string, ok bool) {
	id, found := specialCareCheckboxSeed[strings.TrimSpace(label)]
	return id, found
}

// specialCareFreeTextKeywords are the same six conditions' names, used only to catch a
// mismatch: a row whose dedicated special-care question says No but whose free-text fields
// describe one of these conditions anyway. This is never used to set a condition_id -- only
// to trigger a rejection for manual review, which is the safe direction for an ambiguous
// signal the way blocking already is for a confirmed one.
var specialCareFreeTextKeywords = []string{
	"down syndrome", "cerebral palsy", "congenital heart disease", "cleft lip", "cleft palate",
	"autism", "asd", "intellectual disability", "global developmental delay",
}

// FreeTextMentionsSpecialCare reports whether any of the given free-text fields names one of
// the six known special-care conditions.
func FreeTextMentionsSpecialCare(fields ...string) bool {
	for _, f := range fields {
		lower := strings.ToLower(f)
		for _, kw := range specialCareFreeTextKeywords {
			if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bulkintake/... -run "TestMapSpecialCareCheckbox|TestFreeTextMentionsSpecialCare" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bulkintake/specialcare.go internal/bulkintake/specialcare_test.go
git commit -m "$(cat <<'EOF'
Map the special-care checkbox list and add a free-text mismatch check

Only 6 of the form's 9 checkbox options have a special_care_condition_
gate row; the rest report ok=false rather than inventing a condition_id
or falling through to a normal book.
EOF
)"
```

---

### Task 7: `MapRow` — tie the mapping together

**Files:**
- Create: `internal/bulkintake/mapping.go`
- Test: `internal/bulkintake/mapping_test.go`

**Interfaces:**
- Consumes: `RawRow` (Task 3), `RegionForRow` (Task 4), `MapAllergens` (Task 5), `MapSpecialCareCheckbox` / `FreeTextMentionsSpecialCare` (Task 6), `profile.Stored` / `profile.GrowthMeasurement` / `profile.DeclaredAllergen` / `profile.ClinicalCondition` (existing, `internal/profile/profile.go`).
- Produces:
  - `type Outcome int` with `OutcomeGenerate`, `OutcomeSpecialCareStop`, `OutcomeReject`
  - `type MappedRow struct { Outcome Outcome; Stored profile.Stored; RejectReason string; SpecialCareConditionID string }`
  - `MapRow(row RawRow) MappedRow`
- Consumed by: Task 9 (worker).

- [ ] **Step 1: Write the failing tests**

```go
// internal/bulkintake/mapping_test.go
package bulkintake

import "testing"

func baseRow() RawRow {
	return RawRow{
		"Child Full Name":                          "Test Child",
		"Date of Birth":                             "03/05/2023",
		"Sex used for pediatric growth reference":   "Female",
		"State / Union Territory":                   "West Bengal",
		"Country":                                   "India",
		"Household food practice":                   "Vegetarian + Egg",
		"Religious/cultural food restrictions":       "Hindu",
		"Date of current measurement":                "08/08/2026",
		"Current weight (kg)":                        "12 KG",
		"Current standing height / recumbent length": "73 centimetres",
		"Head circumference":                         "N/A",
		"Known food allergy/intolerance?":             "No",
		"Known / suspected food allergens":            "",
		"History of anaphylaxis?":                     "No",
		"Does the child have any special-care diagnosis or feeding condition requiring additional review?": "No",
		"If yes, which condition(s)?": "",
		"Relevant family history (Select whichever is appropriate, ignore if none)": "",
		"Any developmental concern?": "No",
		"Areas of concern":           "",
	}
}

func TestMapRowGeneratesAnOrdinaryChild(t *testing.T) {
	m := MapRow(baseRow())
	if m.Outcome != OutcomeGenerate {
		t.Fatalf("outcome = %v, want OutcomeGenerate (reason: %s)", m.Outcome, m.RejectReason)
	}
	if m.Stored.DisplayName != "Test Child" {
		t.Errorf("DisplayName = %q", m.Stored.DisplayName)
	}
	if m.Stored.Sex != "female" {
		t.Errorf("Sex = %q, want female", m.Stored.Sex)
	}
	if m.Stored.RegionCulture != "West Bengal / East India" {
		t.Errorf("RegionCulture = %q", m.Stored.RegionCulture)
	}
	if m.Stored.DietType != "Eggetarian" {
		t.Errorf("DietType = %q, want Eggetarian", m.Stored.DietType)
	}
	if len(m.Stored.Growth) != 1 {
		t.Fatalf("Growth = %v, want 1 measurement", m.Stored.Growth)
	}
	g := m.Stored.Growth[0]
	if g.WeightKg == nil || *g.WeightKg != 12 {
		t.Errorf("WeightKg = %v, want 12", g.WeightKg)
	}
	if g.HeightCm == nil || *g.HeightCm != 73 {
		t.Errorf("HeightCm = %v, want 73", g.HeightCm)
	}
	if g.HeadCircumferenceCm != nil {
		t.Errorf("HeadCircumferenceCm = %v, want nil for N/A", g.HeadCircumferenceCm)
	}
}

func TestMapRowRejectsUnparseableDateOfBirth(t *testing.T) {
	row := baseRow()
	row["Date of Birth"] = "not a date"
	m := MapRow(row)
	if m.Outcome != OutcomeReject {
		t.Fatalf("outcome = %v, want OutcomeReject", m.Outcome)
	}
	if m.RejectReason == "" {
		t.Error("want a non-empty reject reason")
	}
}

func TestMapRowRejectsUnrecognisedRegion(t *testing.T) {
	row := baseRow()
	row["State / Union Territory"] = "Atlantis"
	row["Country"] = "India"
	m := MapRow(row)
	if m.Outcome != OutcomeReject {
		t.Fatalf("outcome = %v, want OutcomeReject", m.Outcome)
	}
}

func TestMapRowConfirmedAllergyMapsAndDefaultsToConfirmedStatus(t *testing.T) {
	row := baseRow()
	row["Known food allergy/intolerance?"] = "Yes"
	row["Known / suspected food allergens"] = "peanut"
	m := MapRow(row)
	if m.Outcome != OutcomeGenerate {
		t.Fatalf("outcome = %v, want OutcomeGenerate (reason: %s)", m.Outcome, m.RejectReason)
	}
	if len(m.Stored.Allergens) != 1 || m.Stored.Allergens[0].Group != "Peanut" {
		t.Fatalf("Allergens = %v", m.Stored.Allergens)
	}
	if m.Stored.Allergens[0].Status != "confirmed" {
		t.Errorf("Status = %q, want confirmed", m.Stored.Allergens[0].Status)
	}
}

func TestMapRowRejectsDeclaredAllergyWithNoMatchableGroup(t *testing.T) {
	row := baseRow()
	row["Known food allergy/intolerance?"] = "Yes"
	row["Known / suspected food allergens"] = "reacts to something, unclear"
	m := MapRow(row)
	if m.Outcome != OutcomeReject {
		t.Fatalf("outcome = %v, want OutcomeReject", m.Outcome)
	}
}

func TestMapRowAnaphylaxisSetsSystemicSeverity(t *testing.T) {
	row := baseRow()
	row["Known food allergy/intolerance?"] = "Yes"
	row["Known / suspected food allergens"] = "milk"
	row["History of anaphylaxis?"] = "Yes"
	m := MapRow(row)
	if m.Stored.Allergens[0].Severity != "systemic" {
		t.Errorf("Severity = %q, want systemic", m.Stored.Allergens[0].Severity)
	}
}

func TestMapRowMappedSpecialCareConditionStops(t *testing.T) {
	row := baseRow()
	row["Does the child have any special-care diagnosis or feeding condition requiring additional review?"] = "Yes"
	row["If yes, which condition(s)?"] = "Down Syndrome"
	m := MapRow(row)
	if m.Outcome != OutcomeSpecialCareStop {
		t.Fatalf("outcome = %v, want OutcomeSpecialCareStop (reason: %s)", m.Outcome, m.RejectReason)
	}
	if m.SpecialCareConditionID != "SC-DS" {
		t.Errorf("SpecialCareConditionID = %q, want SC-DS", m.SpecialCareConditionID)
	}
}

func TestMapRowUnmappedSpecialCareCheckboxRejects(t *testing.T) {
	row := baseRow()
	row["Does the child have any special-care diagnosis or feeding condition requiring additional review?"] = "Yes"
	row["If yes, which condition(s)?"] = "Dysphagia/aspiration risk"
	m := MapRow(row)
	if m.Outcome != OutcomeReject {
		t.Fatalf("outcome = %v, want OutcomeReject", m.Outcome)
	}
}

func TestMapRowUnsureAnswerIsACandidateStop(t *testing.T) {
	row := baseRow()
	row["Does the child have any special-care diagnosis or feeding condition requiring additional review?"] = "Unsure"
	m := MapRow(row)
	if m.Outcome != OutcomeReject {
		t.Fatalf("outcome = %v, want OutcomeReject (no checked box to route through the gate)", m.Outcome)
	}
}

// TestMapRowNoButFreeTextDescribesAutismRejects is the real row this feature was designed
// against: the dedicated special-care question says No, but family-history free text
// describes autism anyway.
func TestMapRowNoButFreeTextDescribesAutismRejects(t *testing.T) {
	row := baseRow()
	row["Does the child have any special-care diagnosis or feeding condition requiring additional review?"] = "No"
	row["If yes, which condition(s)?"] = ""
	row["Relevant family history (Select whichever is appropriate, ignore if none)"] = "Autism/neurodevelopmental condition"
	m := MapRow(row)
	if m.Outcome != OutcomeReject {
		t.Fatalf("outcome = %v, want OutcomeReject (mismatch)", m.Outcome)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bulkintake/... -run TestMapRow -v`
Expected: FAIL — undefined `MapRow`, `OutcomeGenerate`, etc.

- [ ] **Step 3: Write the implementation**

```go
// internal/bulkintake/mapping.go
package bulkintake

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/madamgy/recipie/internal/profile"
)

// Outcome is what a mapped row is destined for, decided entirely at mapping time so the
// worker (Task 9) never has to re-derive it from Stored's shape.
type Outcome int

const (
	OutcomeGenerate Outcome = iota
	OutcomeSpecialCareStop
	OutcomeReject
)

// MappedRow is one form row's mapping result.
type MappedRow struct {
	DisplayName             string // kept even on reject/stop, for the report -- see Task 9
	Outcome                 Outcome
	Stored                  profile.Stored
	RejectReason            string
	SpecialCareConditionID  string // set only when Outcome == OutcomeSpecialCareStop
}

var leadingNumber = regexp.MustCompile(`-?\d+(\.\d+)?`)

// parseLeadingFloat extracts the first number in a string like "12 KG" or "73 centimetres".
// "N/A", empty, and text with no leading number all return ok=false, which the caller treats
// as an honest absence -- never a zero.
func parseLeadingFloat(s string) (float64, bool) {
	m := leadingNumber.FindString(s)
	if m == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// parseFormDate parses the form's DD/MM/YYYY date fields.
func parseFormDate(s string) (time.Time, error) {
	return time.Parse("02/01/2006", strings.TrimSpace(s))
}

func mapSex(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "female":
		return "female"
	case "male":
		return "male"
	default:
		return "" // left unset rather than guessed -- sex only selects the growth reference
	}
}

// mapDietType maps the form's free-text household food practice to recipe_master.diet_type's
// exact vocabulary. Checked most-restrictive first, since "vegetarian" is a substring
// consideration doesn't apply here (no substring relationship), but "egg" must be checked
// before assuming plain vegetarian for text like "Vegetarian + Egg".
func mapDietType(s string) (dietType string, vegan bool) {
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "vegan"):
		return "Vegetarian", true // vegan is a stricter subset of Vegetarian, see child_profile.vegan
	case strings.Contains(lower, "non-vegetarian") || strings.Contains(lower, "non vegetarian"):
		return "Non-vegetarian", false
	case strings.Contains(lower, "egg"):
		return "Eggetarian", false
	case strings.Contains(lower, "vegetarian"):
		return "Vegetarian", false
	default:
		return "", false // honest gap: dietFilter is a no-op when DietType is empty
	}
}

// MapRow turns one raw CSV row into a MappedRow, deciding Generate/SpecialCareStop/Reject.
//
// Field-by-field mapping follows docs/superpowers/specs/2026-08-24-bulk-book-generation-
// design.md's "Scope" table exactly: only fields with a clean, non-invented path into
// profile.Stored are set; everything else (budget, prep/cook time, cuisine, non-special-care
// clinical conditions, uploaded document links) is left unset, matching how an honest gap
// already prints on a hand-entered console profile.
func MapRow(row RawRow) MappedRow {
	name := strings.TrimSpace(row["Child Full Name"])

	dob, err := parseFormDate(row["Date of Birth"])
	if err != nil {
		return MappedRow{DisplayName: name, Outcome: OutcomeReject,
			RejectReason: fmt.Sprintf("Date of Birth %q is not a DD/MM/YYYY date", row["Date of Birth"])}
	}

	region, ok := RegionForRow(row["State / Union Territory"], row["Country"])
	if !ok {
		return MappedRow{DisplayName: name, Outcome: OutcomeReject,
			RejectReason: fmt.Sprintf("State/UT %q, Country %q did not map to a known region_culture",
				row["State / Union Territory"], row["Country"])}
	}

	dietType, vegan := mapDietType(row["Household food practice"])

	s := profile.Stored{
		DisplayName:          name,
		DateOfBirth:          dob,
		Sex:                  mapSex(row["Sex used for pediatric growth reference"]),
		RegionCulture:        region,
		DietType:             dietType,
		Vegan:                vegan,
		ReligiousRestriction: strings.TrimSpace(row["Religious/cultural food restrictions"]),
		CreatedBy:            "bulk-intake",
	}

	if measuredOn, err := parseFormDate(row["Date of current measurement"]); err == nil {
		gm := profile.GrowthMeasurement{MeasuredOn: measuredOn, MeasuredBy: "bulk-intake"}
		if w, ok := parseLeadingFloat(row["Current weight (kg)"]); ok {
			gm.WeightKg = &w
		}
		if h, ok := parseLeadingFloat(row["Current standing height / recumbent length"]); ok {
			gm.HeightCm = &h
		}
		if hc, ok := parseLeadingFloat(row["Head circumference"]); ok {
			gm.HeadCircumferenceCm = &hc
		}
		s.Growth = append(s.Growth, gm)
	}

	// Allergens. "No" or blank means nothing to map. "Yes" with unmatchable free text is a
	// reject: silently producing a book with no allergen filter for a declared allergy is the
	// dangerous direction (see allergen.go's MapAllergens doc comment).
	if strings.EqualFold(strings.TrimSpace(row["Known food allergy/intolerance?"]), "yes") {
		groups, ok := MapAllergens(row["Known / suspected food allergens"])
		if !ok {
			return MappedRow{DisplayName: name, Outcome: OutcomeReject,
				RejectReason: fmt.Sprintf(
					"food allergy declared but %q names no recognised allergen group -- needs manual review",
					row["Known / suspected food allergens"])}
		}
		severity := "mild"
		if strings.EqualFold(strings.TrimSpace(row["History of anaphylaxis?"]), "yes") {
			severity = "systemic"
		}
		for _, g := range groups {
			s.Allergens = append(s.Allergens, profile.DeclaredAllergen{
				Group: g, Status: "confirmed", Severity: severity, Source: "parent_reported",
				EnteredBy: "bulk-intake",
			})
		}
	}

	// Special-care condition. Any Yes/Unsure answer is a candidate stop, whether or not a
	// checked box maps to a real gate row -- see specialcare.go.
	primaryAnswer := strings.ToLower(strings.TrimSpace(row[
		"Does the child have any special-care diagnosis or feeding condition requiring additional review?"]))
	checkedBox := strings.TrimSpace(row["If yes, which condition(s)?"])

	if primaryAnswer == "yes" || primaryAnswer == "unsure" {
		conditionID, mapped := MapSpecialCareCheckbox(checkedBox)
		if !mapped {
			return MappedRow{DisplayName: name, Outcome: OutcomeReject,
				RejectReason: fmt.Sprintf(
					"special-care condition declared (%q, checkbox %q) but not covered by the "+
						"engine's stop-gate table -- manual clinical review needed", primaryAnswer, checkedBox)}
		}
		return MappedRow{DisplayName: name, Outcome: OutcomeSpecialCareStop,
			SpecialCareConditionID: conditionID}
	}

	// The primary question said No, but other free-text fields on the row describe a
	// special-care condition anyway. Never fall through to a normal book on that mismatch.
	if FreeTextMentionsSpecialCare(
		row["Relevant family history (Select whichever is appropriate, ignore if none)"],
		row["Any developmental concern?"],
		row["Areas of concern"],
	) {
		return MappedRow{DisplayName: name, Outcome: OutcomeReject,
			RejectReason: "special-care question answered No but other fields describe a " +
				"special-care condition -- mismatch, needs manual review"}
	}

	return MappedRow{DisplayName: name, Outcome: OutcomeGenerate, Stored: s}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bulkintake/... -v`
Expected: PASS, all tests in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/bulkintake/mapping.go internal/bulkintake/mapping_test.go
git commit -m "$(cat <<'EOF'
Add MapRow, tying the header/region/allergen/special-care mappings
together into one CSV-row-to-outcome decision

Three outcomes only: generate, special-care stop, or reject with a
named reason -- no row falls through silently.
EOF
)"
```

---

### Task 8: `bulk_job` DB access layer

**Files:**
- Create: `internal/bulkjob/job.go`
- Test: `internal/bulkjob/job_test.go`

**Interfaces:**
- Produces:
  - `type Job struct { JobID int64; Status string; TotalRows, ProcessedRows int; Error string }`
  - `Enqueue(ctx context.Context, pool *pgxpool.Pool, csvBytes []byte) (jobID int64, error)`
  - `Claim(ctx context.Context, pool *pgxpool.Pool) (jobID int64, csvBytes []byte, found bool, error)`
  - `SetTotalRows(ctx context.Context, pool *pgxpool.Pool, jobID int64, total int) error`
  - `IncrementProcessed(ctx context.Context, pool *pgxpool.Pool, jobID int64) error`
  - `Finish(ctx context.Context, pool *pgxpool.Pool, jobID int64, archiveZip, reportCSV []byte) error`
  - `Fail(ctx context.Context, pool *pgxpool.Pool, jobID int64, reason string) error`
  - `Get(ctx context.Context, pool *pgxpool.Pool, jobID int64) (Job, bool, error)`
  - `GetArchive(ctx context.Context, pool *pgxpool.Pool, jobID int64) (archiveZip []byte, found bool, error)`
- Consumed by: Task 9 (worker), Task 10 (HTTP handlers).

- [ ] **Step 1: Write the failing tests**

```go
// internal/bulkjob/job_test.go
package bulkjob

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestEnqueueThenClaim(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	jobID, err := Enqueue(ctx, pool, []byte("a,b\n1,2\n"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	claimedID, csvBytes, found, err := Claim(ctx, pool)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !found {
		t.Fatal("Claim: found=false, want a queued job to claim")
	}
	if claimedID != jobID {
		t.Errorf("claimedID = %d, want %d", claimedID, jobID)
	}
	if string(csvBytes) != "a,b\n1,2\n" {
		t.Errorf("csvBytes = %q", csvBytes)
	}

	// A second claim finds nothing: the first claim already moved status to 'running'.
	_, _, found2, err := Claim(ctx, pool)
	if err != nil {
		t.Fatalf("Claim (second): %v", err)
	}
	if found2 {
		t.Error("second Claim: found=true, want false -- job already running")
	}
}

func TestFinishAndGet(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	jobID, _ := Enqueue(ctx, pool, []byte("x"))
	_, _, _, _ = Claim(ctx, pool)

	if err := SetTotalRows(ctx, pool, jobID, 3); err != nil {
		t.Fatalf("SetTotalRows: %v", err)
	}
	if err := IncrementProcessed(ctx, pool, jobID); err != nil {
		t.Fatalf("IncrementProcessed: %v", err)
	}
	if err := Finish(ctx, pool, jobID, []byte("zip-bytes"), []byte("report-bytes")); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	job, found, err := Get(ctx, pool, jobID)
	if err != nil || !found {
		t.Fatalf("Get: found=%v, err=%v", found, err)
	}
	if job.Status != "done" {
		t.Errorf("Status = %q, want done", job.Status)
	}
	if job.TotalRows != 3 || job.ProcessedRows != 1 {
		t.Errorf("TotalRows=%d ProcessedRows=%d, want 3, 1", job.TotalRows, job.ProcessedRows)
	}

	archive, found, err := GetArchive(ctx, pool, jobID)
	if err != nil || !found {
		t.Fatalf("GetArchive: found=%v, err=%v", found, err)
	}
	if string(archive) != "zip-bytes" {
		t.Errorf("archive = %q", archive)
	}
}

func TestFail(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	jobID, _ := Enqueue(ctx, pool, []byte("x"))
	_, _, _, _ = Claim(ctx, pool)

	if err := Fail(ctx, pool, jobID, "header row missing Date of Birth"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	job, _, _ := Get(ctx, pool, jobID)
	if job.Status != "failed" {
		t.Errorf("Status = %q, want failed", job.Status)
	}
	if job.Error != "header row missing Date of Birth" {
		t.Errorf("Error = %q", job.Error)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/bulkjob/... -v`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/bulkjob/job.go
package bulkjob

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Job is one bulk_job row's status, without the large blob columns -- those are fetched
// separately (GetArchive) so a status poll never pulls a multi-megabyte zip over the wire.
type Job struct {
	JobID         int64
	Status        string
	TotalRows     int
	ProcessedRows int
	Error         string
}

// Enqueue stores an uploaded CSV as a new queued job and returns its id.
func Enqueue(ctx context.Context, pool *pgxpool.Pool, csvBytes []byte) (int64, error) {
	var jobID int64
	err := pool.QueryRow(ctx,
		`INSERT INTO bulk_job (uploaded_csv) VALUES ($1) RETURNING job_id`, csvBytes).Scan(&jobID)
	if err != nil {
		return 0, fmt.Errorf("bulkjob: enqueue: %w", err)
	}
	return jobID, nil
}

// Claim atomically takes the oldest queued job, if any, moving it to 'running' in the same
// statement so two worker ticks (or a worker surviving a near-simultaneous restart) can never
// both claim the same job.
func Claim(ctx context.Context, pool *pgxpool.Pool) (jobID int64, csvBytes []byte, found bool, err error) {
	row := pool.QueryRow(ctx, `
		UPDATE bulk_job SET status = 'running'
		WHERE job_id = (
			SELECT job_id FROM bulk_job WHERE status = 'queued' ORDER BY created_at LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING job_id, uploaded_csv`)
	err = row.Scan(&jobID, &csvBytes)
	if err == pgx.ErrNoRows {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, fmt.Errorf("bulkjob: claim: %w", err)
	}
	return jobID, csvBytes, true, nil
}

// SetTotalRows records the row count once the CSV has been parsed, so a status poll can show
// progress as processed/total.
func SetTotalRows(ctx context.Context, pool *pgxpool.Pool, jobID int64, total int) error {
	_, err := pool.Exec(ctx, `UPDATE bulk_job SET total_rows = $2 WHERE job_id = $1`, jobID, total)
	if err != nil {
		return fmt.Errorf("bulkjob: set total rows: %w", err)
	}
	return nil
}

// IncrementProcessed advances the row counter by one. Called once per row regardless of that
// row's outcome (generated, special-care stop, or rejected), since all three count as
// "processed" for progress-reporting purposes.
func IncrementProcessed(ctx context.Context, pool *pgxpool.Pool, jobID int64) error {
	_, err := pool.Exec(ctx,
		`UPDATE bulk_job SET processed_rows = processed_rows + 1 WHERE job_id = $1`, jobID)
	if err != nil {
		return fmt.Errorf("bulkjob: increment processed: %w", err)
	}
	return nil
}

// Finish marks a job done and stores its two output blobs.
func Finish(ctx context.Context, pool *pgxpool.Pool, jobID int64, archiveZip, reportCSV []byte) error {
	_, err := pool.Exec(ctx, `
		UPDATE bulk_job
		SET status = 'done', archive_zip = $2, report_csv = $3, finished_at = now()
		WHERE job_id = $1`, jobID, archiveZip, reportCSV)
	if err != nil {
		return fmt.Errorf("bulkjob: finish: %w", err)
	}
	return nil
}

// Fail marks a job failed with a whole-file reason -- reserved for the header row not
// matching the required seed, a different failure mode from any individual row's rejection
// (those go in report_csv instead via Finish, not here).
func Fail(ctx context.Context, pool *pgxpool.Pool, jobID int64, reason string) error {
	_, err := pool.Exec(ctx,
		`UPDATE bulk_job SET status = 'failed', error = $2, finished_at = now() WHERE job_id = $1`,
		jobID, reason)
	if err != nil {
		return fmt.Errorf("bulkjob: fail: %w", err)
	}
	return nil
}

// Get reads a job's status fields, without its blob columns.
func Get(ctx context.Context, pool *pgxpool.Pool, jobID int64) (Job, bool, error) {
	var j Job
	var errText, status *string
	err := pool.QueryRow(ctx,
		`SELECT job_id, status, coalesce(total_rows, 0), processed_rows, coalesce(error, '')
		 FROM bulk_job WHERE job_id = $1`, jobID,
	).Scan(&j.JobID, &status, &j.TotalRows, &j.ProcessedRows, &errText)
	if err == pgx.ErrNoRows {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("bulkjob: get: %w", err)
	}
	j.Status = *status
	j.Error = *errText
	return j, true, nil
}

// GetArchive reads a finished job's zip archive. found is false for a job that doesn't exist
// or hasn't reached 'done' yet -- both look the same to a caller deciding whether to offer a
// download.
func GetArchive(ctx context.Context, pool *pgxpool.Pool, jobID int64) ([]byte, bool, error) {
	var archive []byte
	err := pool.QueryRow(ctx,
		`SELECT archive_zip FROM bulk_job WHERE job_id = $1 AND status = 'done'`, jobID,
	).Scan(&archive)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("bulkjob: get archive: %w", err)
	}
	if archive == nil {
		return nil, false, nil
	}
	return archive, true, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/bulkjob/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bulkjob/job.go internal/bulkjob/job_test.go
git commit -m "$(cat <<'EOF'
Add the bulk_job DB access layer: enqueue, atomic claim, finish, fail

Claim uses SELECT ... FOR UPDATE SKIP LOCKED so a restart-induced
double-tick of the worker poll can never claim the same job twice.
EOF
)"
```

---

### Task 9: Worker — per-row processing and the poll loop

**Files:**
- Create: `internal/bulkjob/worker.go`
- Test: `internal/bulkjob/worker_test.go`

**Interfaces:**
- Consumes: `bulkintake.ParseRows`, `bulkintake.MapRow`, `bulkintake.MappedRow` + `Outcome*` (Task 3, Task 7); `book.AssembleSet`, `book.RenderHTML`, `book.PrintPDFAll`, `book.PrintJob`, `book.ErrBlocked`, `book.BlockedDetail`, `book.Kind1`, `book.Kind2` (existing `internal/book` package + Task 1); `bulkjob.Claim`, `SetTotalRows`, `IncrementProcessed`, `Finish`, `Fail` (Task 8).
- Produces:
  - `RunWorker(ctx context.Context, pool *pgxpool.Pool, pollInterval time.Duration)` — blocks forever, call in its own goroutine.
  - `processOneJob(ctx context.Context, pool *pgxpool.Pool) (didWork bool, err error)` — one poll tick, exported as a lowercase helper the test calls directly to avoid sleeping through `pollInterval`.

- [ ] **Step 1: Write the failing test**

This test needs a running Chromium (same conditions as `internal/book/pdf_test.go`) and a migrated test database. It skips cleanly without either, following the existing project convention.

```go
// internal/bulkjob/worker_test.go
package bulkjob

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/book"
)

func testPool(t *testing.T) *pgxpool.Pool {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func requireChromium(t *testing.T) {
	if err := book.WarmUp(); err != nil {
		t.Skipf("chromium unavailable: %v", err)
	}
}

// twoRowCSV builds a minimal valid export with one ordinary child (generates) and one
// special-care child (Down Syndrome, stops).
func twoRowCSV(t *testing.T) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := append([]string{}, requiredHeadersForTest()...)
	if err := w.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	rowFor := func(name, specialCare, checkbox string) []string {
		vals := make(map[string]string, len(header))
		for _, h := range header {
			vals[h] = ""
		}
		vals["Child Full Name"] = name
		vals["Date of Birth"] = "03/05/2020"
		vals["Sex used for pediatric growth reference"] = "Female"
		vals["State / Union Territory"] = "West Bengal"
		vals["Country"] = "India"
		vals["Household food practice"] = "Vegetarian"
		vals["Known food allergy/intolerance?"] = "No"
		vals["Does the child have any special-care diagnosis or feeding condition requiring additional review?"] = specialCare
		vals["If yes, which condition(s)?"] = checkbox
		out := make([]string, len(header))
		for i, h := range header {
			out[i] = vals[h]
		}
		return out
	}
	_ = w.Write(rowFor("Ordinary Child", "No", ""))
	_ = w.Write(rowFor("Stopped Child", "Yes", "Down Syndrome"))
	w.Flush()
	return buf.Bytes()
}

func TestProcessOneJobGeneratesAndStopsCorrectly(t *testing.T) {
	pool := testPool(t)
	requireChromium(t)
	ctx := context.Background()

	jobID, err := Enqueue(ctx, pool, twoRowCSV(t))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	didWork, err := processOneJob(ctx, pool)
	if err != nil {
		t.Fatalf("processOneJob: %v", err)
	}
	if !didWork {
		t.Fatal("processOneJob: didWork=false, want true -- a job was queued")
	}

	job, found, err := Get(ctx, pool, jobID)
	if err != nil || !found {
		t.Fatalf("Get: found=%v, err=%v", found, err)
	}
	if job.Status != "done" {
		t.Fatalf("Status = %q, want done (error: %s)", job.Status, job.Error)
	}
	if job.ProcessedRows != 2 {
		t.Errorf("ProcessedRows = %d, want 2", job.ProcessedRows)
	}

	archive, found, err := GetArchive(ctx, pool, jobID)
	if err != nil || !found {
		t.Fatalf("GetArchive: found=%v, err=%v", found, err)
	}

	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	var names []string
	var reportContent string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == "report.csv" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			reportContent = string(b)
		}
	}
	// Ordinary Child generated two PDFs; Stopped Child generated none; report.csv is always
	// present.
	pdfCount := 0
	for _, n := range names {
		if strings.HasSuffix(n, ".pdf") {
			pdfCount++
		}
	}
	if pdfCount != 2 {
		t.Errorf("pdfCount = %d, want 2 (ordinary child's book1+book2 only)", pdfCount)
	}
	if !strings.Contains(reportContent, "Ordinary Child") || !strings.Contains(reportContent, "generated") {
		t.Errorf("report.csv missing the generated row: %q", reportContent)
	}
	if !strings.Contains(reportContent, "Stopped Child") || !strings.Contains(reportContent, "special_care_stop") {
		t.Errorf("report.csv missing the special-care-stop row: %q", reportContent)
	}
}

func TestProcessOneJobWithNoQueuedJobDoesNothing(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	didWork, err := processOneJob(ctx, pool)
	if err != nil {
		t.Fatalf("processOneJob: %v", err)
	}
	if didWork {
		t.Error("didWork=true, want false -- nothing was queued")
	}
}

func TestProcessOneJobFailsWholeJobOnBadHeader(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	jobID, err := Enqueue(ctx, pool, []byte("not,the,right,header\n1,2,3,4\n"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if _, err := processOneJob(ctx, pool); err != nil {
		t.Fatalf("processOneJob: %v", err)
	}

	job, _, _ := Get(ctx, pool, jobID)
	if job.Status != "failed" {
		t.Errorf("Status = %q, want failed", job.Status)
	}
	if job.Error == "" {
		t.Error("want a non-empty Error naming the missing header")
	}
}
```

Add the missing `io` import and a small test-only header helper at the top of the file:

```go
import (
	"io"
	// ... (the imports above)
)

// requiredHeadersForTest re-exports bulkintake.RequiredHeaders under a local name so this
// test file's single import block stays alphabetical; see the real import below.
func requiredHeadersForTest() []string {
	return bulkintakeRequiredHeaders
}
```

(Replace that helper with a direct `bulkintake.RequiredHeaders` reference and drop the alias — this was written out only to flag that the test file imports `github.com/madamgy/recipie/internal/bulkintake` for `bulkintake.RequiredHeaders`, `bulkintake.ParseRows` is not needed in the test itself. Use `bulkintake.RequiredHeaders` directly in `twoRowCSV`'s header line instead of the indirection above.)

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/bulkjob/... -run TestProcessOneJob -v`
Expected: FAIL — undefined `processOneJob`.

- [ ] **Step 3: Write the implementation**

```go
// internal/bulkjob/worker.go
package bulkjob

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/bulkintake"
)

// RunWorker polls for a queued job every pollInterval and processes it to completion before
// polling again. It never processes two jobs at once and never processes rows within a job in
// parallel -- book.PrintPDFAll's underlying Chromium print already runs close to the memory
// ceiling of the free Render instance this project deploys to (see render.yaml), and adding
// concurrent prints on top of live single-child requests would risk both.
//
// Call this in its own goroutine from cmd/server/main.go. It runs for the life of the process;
// there is no cancellation path today because the server itself has none.
func RunWorker(ctx context.Context, pool *pgxpool.Pool, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := processOneJob(ctx, pool); err != nil {
				log.Printf("bulkjob: worker tick failed: %v", err)
			}
		}
	}
}

// processOneJob claims at most one queued job and runs it to completion (or to a whole-file
// failure). didWork is false when nothing was queued, which callers use in tests to avoid
// sleeping through pollInterval.
func processOneJob(ctx context.Context, pool *pgxpool.Pool) (didWork bool, err error) {
	jobID, csvBytes, found, err := Claim(ctx, pool)
	if err != nil {
		return false, fmt.Errorf("bulkjob: process: claim: %w", err)
	}
	if !found {
		return false, nil
	}

	rows, err := bulkintake.ParseRows(bytes.NewReader(csvBytes))
	if err != nil {
		if failErr := Fail(ctx, pool, jobID, err.Error()); failErr != nil {
			return true, fmt.Errorf("bulkjob: process: fail after bad header: %w", failErr)
		}
		return true, nil
	}

	if err := SetTotalRows(ctx, pool, jobID, len(rows)); err != nil {
		return true, fmt.Errorf("bulkjob: process: set total rows: %w", err)
	}

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	var report [][]string
	report = append(report, []string{"name", "outcome", "detail"})

	for _, row := range rows {
		mapped := bulkintake.MapRow(row)
		outcome, detail, pdfErr := processRow(ctx, pool, mapped, zw)
		if pdfErr != nil {
			// A render/print failure is about this row's engine run, not the file -- record
			// it as a rejection rather than failing the whole job, so one bad child does not
			// cost every other family in the batch their books.
			outcome, detail = "rejected", "generation failed: "+pdfErr.Error()
		}
		report = append(report, []string{mapped.DisplayName, outcome, detail})

		if err := IncrementProcessed(ctx, pool, jobID); err != nil {
			return true, fmt.Errorf("bulkjob: process: increment: %w", err)
		}
	}

	reportBuf := &bytes.Buffer{}
	cw := csv.NewWriter(reportBuf)
	if err := cw.WriteAll(report); err != nil {
		return true, fmt.Errorf("bulkjob: process: write report: %w", err)
	}

	rf, err := zw.Create("report.csv")
	if err != nil {
		return true, fmt.Errorf("bulkjob: process: zip create report: %w", err)
	}
	if _, err := rf.Write(reportBuf.Bytes()); err != nil {
		return true, fmt.Errorf("bulkjob: process: zip write report: %w", err)
	}
	if err := zw.Close(); err != nil {
		return true, fmt.Errorf("bulkjob: process: zip close: %w", err)
	}

	if err := Finish(ctx, pool, jobID, archive.Bytes(), reportBuf.Bytes()); err != nil {
		return true, fmt.Errorf("bulkjob: process: finish: %w", err)
	}
	return true, nil
}

// processRow handles one mapped row: reject and special-care-stop outcomes need no engine
// run at all; a generate outcome runs the same AssembleSet/RenderHTML/PrintPDFAll sequence
// the live /api/books/generate.zip endpoint uses, and writes both PDFs straight into the
// job's archive under this child's slug.
func processRow(ctx context.Context, pool *pgxpool.Pool, mapped bulkintake.MappedRow, zw *zip.Writer) (outcome, detail string, err error) {
	switch mapped.Outcome {
	case bulkintake.OutcomeReject:
		return "rejected", mapped.RejectReason, nil

	case bulkintake.OutcomeSpecialCareStop:
		var reviewer string
		qerr := pool.QueryRow(ctx,
			`SELECT coalesce(mandatory_reviewer, '') FROM special_care_condition_gate WHERE condition_id = $1`,
			mapped.SpecialCareConditionID).Scan(&reviewer)
		if qerr != nil {
			reviewer = ""
		}
		return "special_care_stop", fmt.Sprintf("condition %s, reviewer: %s",
			mapped.SpecialCareConditionID, reviewer), nil

	case bulkintake.OutcomeGenerate:
		return generateAndArchive(ctx, pool, mapped, zw)

	default:
		return "rejected", "unrecognised mapping outcome", nil
	}
}

func generateAndArchive(ctx context.Context, pool *pgxpool.Pool, mapped bulkintake.MappedRow, zw *zip.Writer) (outcome, detail string, err error) {
	asOf := time.Now().UTC()
	set, err := book.AssembleSet(ctx, pool, mapped.Stored, asOf)
	if err != nil {
		if errors.Is(err, book.ErrBlocked) {
			reason, reviewer := book.BlockedDetail(ctx, pool, mapped.Stored, asOf, err)
			return "special_care_stop", fmt.Sprintf("%s, reviewer: %s", reason, reviewer), nil
		}
		return "", "", fmt.Errorf("assemble: %w", err)
	}

	var buf1, buf2 bytes.Buffer
	if err := book.RenderHTML(&buf1, book.Kind1, set.Book1.Metadata, set.Book1); err != nil {
		return "", "", fmt.Errorf("render book1: %w", err)
	}
	if err := book.RenderHTML(&buf2, book.Kind2, set.Book2.Metadata, set.Book2); err != nil {
		return "", "", fmt.Errorf("render book2: %w", err)
	}

	pdfs, err := book.PrintPDFAll(ctx, []book.PrintJob{
		{Name: "book1", HTML: buf1.Bytes(), Metadata: set.Book1.Metadata},
		{Name: "book2", HTML: buf2.Bytes(), Metadata: set.Book2.Metadata},
	})
	if err != nil {
		return "", "", fmt.Errorf("print: %w", err)
	}

	slug := slugify(mapped.DisplayName)
	for i, which := range []string{"book1", "book2"} {
		f, err := zw.Create(fmt.Sprintf("%s-%s.pdf", slug, which))
		if err != nil {
			return "", "", fmt.Errorf("zip create %s: %w", which, err)
		}
		if _, err := f.Write(pdfs[i]); err != nil {
			return "", "", fmt.Errorf("zip write %s: %w", which, err)
		}
	}
	return "generated", fmt.Sprintf("%s-book1.pdf, %s-book2.pdf", slug, slug), nil
}

// slugify mirrors internal/api/handlers/book_generate.go's slugOrDefault -- a separate copy
// rather than an import, since that function is unexported and this package intentionally
// does not depend on internal/api/handlers (the worker has no HTTP concerns).
func slugify(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "child"
	}
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	return out
}
```

Fix the test file's import block: replace the placeholder `requiredHeadersForTest` helper with a direct reference. In `worker_test.go`, add `"github.com/madamgy/recipie/internal/bulkintake"` to the imports and change:

```go
header := append([]string{}, requiredHeadersForTest()...)
```
to:
```go
header := append([]string{}, bulkintake.RequiredHeaders...)
```
and delete the `requiredHeadersForTest` helper function entirely.

- [ ] **Step 4: Run test to verify it passes**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/bulkjob/... -v`
Expected: PASS. `TestProcessOneJobGeneratesAndStopsCorrectly` skips if Chromium isn't installed (matching `internal/book/pdf_test.go`'s existing convention) — confirm the other two tests still pass either way.

- [ ] **Step 5: Commit**

```bash
git add internal/bulkjob/worker.go internal/bulkjob/worker_test.go
git commit -m "$(cat <<'EOF'
Add the bulk job worker: sequential per-row processing into one archive

Reuses book.AssembleSet/RenderHTML/PrintPDFAll directly -- the same
functions the live generate.zip endpoint calls -- plus book.BlockedDetail
for the special-care reviewer lookup, so there is one implementation of
each, not a second copy for the batch path.
EOF
)"
```

---

### Task 10: HTTP endpoints — upload, status, download

**Files:**
- Create: `internal/api/handlers/bulk.go`
- Test: `internal/api/handlers/bulk_test.go`
- Modify: `internal/api/router.go`

**Interfaces:**
- Consumes: `bulkjob.Enqueue`, `bulkjob.Get`, `bulkjob.GetArchive` (Task 8).
- Produces: `(h *Handlers) BulkUpload`, `(h *Handlers) BulkStatus`, `(h *Handlers) BulkArchive` — registered as HTTP handlers.

- [ ] **Step 1: Write the failing tests**

```go
// internal/api/handlers/bulk_test.go
package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/bulkjob"
)

func testPool(t *testing.T) *pgxpool.Pool {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func multipartCSVBody(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func TestBulkUploadReturnsAJobID(t *testing.T) {
	pool := testPool(t)
	h := New(pool)

	body, contentType := multipartCSVBody(t, "export.csv", []byte("a,b\n1,2\n"))
	req := httptest.NewRequest(http.MethodPost, "/api/bulk/jobs", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	h.BulkUpload(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkStatusForAnUnknownJobIs404(t *testing.T) {
	pool := testPool(t)
	h := New(pool)

	req := httptest.NewRequest(http.MethodGet, "/api/bulk/jobs/999999999", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", "999999999")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.BulkStatus(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestBulkArchiveBeforeDoneIs409(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	jobID, err := bulkjob.Enqueue(ctx, pool, []byte("a,b\n1,2\n"))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	h := New(pool)

	req := httptest.NewRequest(http.MethodGet, "/api/bulk/jobs/x/archive.zip", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", itoa(jobID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.BulkArchive(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (job not done yet)", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/handlers/... -run TestBulk -v`
Expected: FAIL — undefined `BulkUpload`, `itoa`, etc.

- [ ] **Step 3: Write the implementation**

```go
// internal/api/handlers/bulk.go
package handlers

import (
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/madamgy/recipie/internal/bulkjob"
)

// maxBulkUploadBody caps an uploaded CSV. Generous -- a real Google Form export with a few
// hundred rows and a few dozen columns is well under a megabyte of text -- but bounded so an
// unbounded upload cannot exhaust memory before the row count is even known.
const maxBulkUploadBody = 32 << 20

// BulkUpload accepts a multipart CSV upload and enqueues it for the background worker.
//
// Returns immediately with a job id: printing a batch takes roughly 19 seconds per child, so
// any batch past a handful of children would exceed an HTTP timeout if this endpoint waited
// for the result. The operator polls BulkStatus and downloads via BulkArchive once done.
func (h *Handlers) BulkUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBulkUploadBody)

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing \"file\" form field: "+err.Error())
		return
	}
	defer file.Close()

	csvBytes, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "reading uploaded file: "+err.Error())
		return
	}

	jobID, err := bulkjob.Enqueue(r.Context(), h.pool, csvBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "status": "queued"})
}

// BulkStatus reports a job's progress: status, and processed/total row counts once the CSV
// has been parsed.
func (h *Handlers) BulkStatus(w http.ResponseWriter, r *http.Request) {
	jobID, ok := parseJobID(w, r)
	if !ok {
		return
	}

	job, found, err := bulkjob.Get(r.Context(), h.pool, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "status lookup failed: "+err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no bulk job with that id")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"job_id":         job.JobID,
		"status":         job.Status,
		"total_rows":     job.TotalRows,
		"processed_rows": job.ProcessedRows,
		"error":          job.Error,
	})
}

// BulkArchive serves a finished job's zip archive: book1.pdf/book2.pdf per generated child
// plus report.csv, naming every input row's outcome.
func (h *Handlers) BulkArchive(w http.ResponseWriter, r *http.Request) {
	jobID, ok := parseJobID(w, r)
	if !ok {
		return
	}

	archive, found, err := bulkjob.GetArchive(r.Context(), h.pool, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "archive lookup failed: "+err.Error())
		return
	}
	if !found {
		// Same response whether the job doesn't exist or simply isn't done yet -- an
		// operator polling BulkStatus already knows which case they're in, and 409 reads
		// as "try again shortly" rather than the permanent 404 a genuinely unknown id gets.
		job, statusFound, _ := bulkjob.Get(r.Context(), h.pool, jobID)
		if !statusFound {
			writeError(w, http.StatusNotFound, "no bulk job with that id")
			return
		}
		writeError(w, http.StatusConflict, "job status is "+job.Status+", not done yet")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"bulk-books.zip\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

func parseJobID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "jobID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return 0, false
	}
	return id, true
}
```

`itoa` used only in the test file — add it as a small unexported test helper at the bottom of `bulk_test.go`:

```go
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
```

(add `"strconv"` to `bulk_test.go`'s imports.)

- [ ] **Step 4: Register the routes**

In `internal/api/router.go`, inside `NewRouter`, alongside the other `/api/books/...` registrations:

```go
r.Post("/api/bulk/jobs", h.BulkUpload)
r.Get("/api/bulk/jobs/{jobID}", h.BulkStatus)
r.Get("/api/bulk/jobs/{jobID}/archive.zip", h.BulkArchive)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/... -v`
Expected: PASS, including the pre-existing handler tests (no regression).

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers/bulk.go internal/api/handlers/bulk_test.go internal/api/router.go
git commit -m "$(cat <<'EOF'
Add the bulk upload/status/download HTTP endpoints

Upload returns immediately with a job id -- a real batch takes minutes
to print, far past any request timeout -- and the operator polls status
before downloading the finished archive.
EOF
)"
```

---

### Task 11: Start the worker in `cmd/server/main.go`

**Files:**
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `bulkjob.RunWorker(ctx, pool, pollInterval)` (Task 9).

- [ ] **Step 1: Add the worker goroutine**

In `cmd/server/main.go`, after the existing `book.WarmUp()` block and before `srv := &http.Server{...}`:

```go
	// The bulk book generation worker. One goroutine, polling for at most one queued job at
	// a time -- see internal/bulkjob/worker.go's doc comment for why this never runs two
	// jobs, or two rows within a job, concurrently with itself or with a live print request.
	go bulkjob.RunWorker(ctx, pool, 5*time.Second)
```

Add the import:

```go
	"github.com/madamgy/recipie/internal/bulkjob"
```

- [ ] **Step 2: Verify it builds and starts**

Run:
```bash
go build ./...
go vet ./...
```
Expected: both succeed with no errors.

Run (manual smoke check, not automated — confirm the service still starts and serves `/healthz`):
```bash
scripts/dev_db.fish up
DATABASE_URL=$(scripts/dev_db.fish url) go run ./cmd/server &
sleep 2
curl -sf http://localhost:8080/healthz
kill %1
```
Expected: `{"status":"ok"}`, then the background process is killed cleanly.

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "$(cat <<'EOF'
Start the bulk book generation worker alongside the API server

One background goroutine, no separate Render service -- see the design
spec's job-runner decision.
EOF
)"
```

---

### Task 12: Full verification pass and CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md` (blocker #6 section and the "How each blocker is handled in Phase 1" table)

- [ ] **Step 1: Run the full verification suite**

```bash
go build ./...
go vet ./...
scripts/dev_db.fish up
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./...
cd web && npm test && cd ..
```
Expected: everything green. If `internal/book`'s Chromium-dependent tests skip (no browser installed in this environment), that's consistent with the project's existing documented behavior — not a failure.

- [ ] **Step 2: Update CLAUDE.md**

In the "Blockers" section, find blocker #6 ("Bulk book generation is blocked on one real Google Form export"). Add a short status note directly under its heading (do not remove the existing text — it documents real decisions still in force, like the no-cover-photo and job-queue-need calls):

```markdown
### 6. Bulk book generation is blocked on one real Google Form export

**Status: built.** A real export landed 2026-08-24 (session "plan 1"), including a
screenshot of the live form confirming the special-care question is a fixed checkbox
list rather than free text. `internal/bulkintake` maps the header-to-field seed
(hand-written, matching this section's option 1); `internal/bulkjob` runs a
Postgres-backed job queue with a single in-process worker; `POST /api/bulk/jobs`,
`GET /api/bulk/jobs/{id}`, and `GET /api/bulk/jobs/{id}/archive.zip` are the
operator-facing surface. Design: `docs/superpowers/specs/2026-08-24-bulk-book-generation-design.md`.
Every row is stateless (never written to `child_profile`), and a declared special-care
condition outside the six known `special_care_condition_gate` rows is always rejected
for manual review rather than silently generated.

[... existing text below unchanged ...]
```

In "How each blocker is handled in Phase 1" table, update the blocker 6 row:

```markdown
| 6. Bulk input mapping | Built. `internal/bulkintake` + `internal/bulkjob`, hand-written header seed, stateless per row. See blocker 6 above. |
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "$(cat <<'EOF'
Record that bulk book generation (blocker #6) is built

A real Google Form export landed and the hand-written mapping,
job queue, and archive/report endpoints are implemented and tested.
EOF
)"
```

---

## Self-Review Notes

- **Spec coverage:** every section of the design doc maps to a task — mapping mechanism (Tasks 3-7), persistence (stateless throughout, no task writes `child_profile`), job runner (Tasks 8-9, in-process/sequential), rejection accounting (Task 9's `report.csv`), special-care checkbox + mismatch (Task 6-7), HTTP surface (Task 10), worker startup (Task 11).
- **Placeholder scan:** no TBD/TODO; every step carries real code. Task 9's Step 1 has one deliberate piece of scaffolding text (the `requiredHeadersForTest` note) that Step 3 explicitly resolves by naming the exact edit to make — left visible rather than silently correct so an executor understands why the first draft of that test file needs one fix before it compiles.
- **Type consistency:** `profile.Stored`, `profile.GrowthMeasurement`, `profile.DeclaredAllergen`, `profile.ClinicalCondition` field names match `internal/profile/profile.go` exactly, verified by reading the file directly rather than from memory. `book.AssembleSet`, `book.RenderHTML`, `book.PrintPDFAll`, `book.PrintJob`, `book.Kind1`/`Kind2`, `book.ErrBlocked` all match `internal/book`'s actual exported signatures, likewise verified. `bulkintake.MappedRow`/`Outcome*` are used identically across Task 7 (producer) and Task 9 (consumer).
- **Scope check:** one subsystem, one plan — matches the spec's own single-subsystem scope (the spec explicitly separated this from the photo pipeline and Plan 4, both owned by a different session).
