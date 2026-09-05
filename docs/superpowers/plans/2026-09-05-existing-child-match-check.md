# Existing-Child Match Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Before an operator generates a book from inline inputs, let them see whether a
matching child profile already exists in the database, so they can reuse/update it instead
of every generation being anonymous by default and every "is this child already in our
records" question being answered by hand-typing a `child_id` and hoping it's right.

**Architecture:** A new read-only backend endpoint (`GET /api/profile-matches`) queries
`child_profile` for an exact `case_id` match, or an exact three-way match on
(case/whitespace-normalized) `display_name` + `date_of_birth` + `mother_name`, and returns
0+ candidates — never a similarity score, never applied automatically. `mother_name` does
not exist anywhere in this schema today and is added by this plan specifically because it's
a far stronger, more commonly-captured identifier than `case_id` (which nothing prompts an
operator to fill in) for telling apart two children who might share a name and birth date.
The console's generate form gets `Case ID` and `Mother's name` fields (neither exists
today) and a debounced check that, on a match, shows a non-blocking suggestion banner with
a "Load this record" button that prefills the form from the existing `getProfile` call.
Declining or ignoring the banner changes nothing: generation still stores nothing by
default, exactly as today.

**Tech Stack:** Go (chi, pgx/v5, golang-migrate), Next.js/React/TypeScript, no new
dependencies.

**Spec:** No separate spec document. This plan is synthesized directly from the user's
request ("check if this input is already in the DB... write this in a plans md file") and a
full investigation of the current codebase carried out in this session (cited inline below
by file:line). Treat the "Current state, verified" section as the spec's factual basis.

## Current state, verified this session (not assumed)

- **`child_id` is 100% operator-typed freeform text, never generated.** Routes
  (`internal/api/router.go:99-101`): `PUT /api/profiles/{childID}`, `GET
  /api/profiles/{childID}`, `GET /api/profiles/{childID}/engine-input` — the entire profiles
  API surface. No create/POST route exists. `PutProfile`
  (`internal/api/handlers/profiles.go:256`) takes `childID` from the URL path and rejects a
  body that disagrees with it. `profile.Save` (`internal/profile/profile.go:239`) is
  `INSERT ... ON CONFLICT (child_id) DO UPDATE` — pure upsert by whatever string the caller
  supplied. Nothing mints an id anywhere.
- **No list/search/match capability exists at all today.** `profile.Load`
  (`internal/profile/profile.go:340`) and every per-table loader are `WHERE child_id = $1`
  only. No `List`/`Search`/`Find`/`Match` function exists anywhere in
  `internal/api/handlers/` or `internal/profile/`.
- **`case_id` is a real column, completely unused for lookup.** `child_profile.case_id`
  (migration `0014_child_profile.up.sql:21`) has no UNIQUE constraint, no index, and is
  never read back as a query predicate anywhere — write-only today.
- **No uniqueness constraint exists on `case_id` or on `(display_name, date_of_birth)`.**
  Confirmed by reading migration `0014` in full. The only unique/PK constraints on the five
  `child_*` tables are `child_profile.child_id` (PK), `child_growth_measurement (child_id,
  measured_on)` (UNIQUE), and composite PKs on the other three.
- **The inline generate flow (`POST /api/books/generate*`,
  `internal/api/handlers/book_generate.go`) is a separate, non-persisting path** — takes a
  whole child inline, never touches `child_id`/`case_id`, never writes to the database. This
  plan does not change that default. `web/src/components/child-input-form.tsx` (the form
  feeding this path) has zero `child_id`/`case_id` fields today (confirmed by grep).
- **Directly relevant prior art: a considered rejection of dedup for a closely related
  feature.** The not-yet-built bulk book-generation design
  (`docs/superpowers/specs/2026-08-24-bulk-book-generation-design.md:26-30`) states
  generation is "stateless per row... No dedup/upsert logic, no history, no
  regenerate-later. If an operator wants a child to persist, they re-enter that child
  through the existing single-child console flow afterward." That punt assumed the
  single-child flow could find an existing child by re-entering it — but as shown above, that
  flow has zero ability to do that except by already knowing the exact `child_id` string.
  This plan closes that specific gap; it does not touch bulk intake itself.

## Decisions made and why

1. **Match on exact identity only — no fuzzy/similarity scoring, ever.** Two independent
   exact conditions, either one surfaces a candidate: (a) a non-empty `case_id` match, or
   (b) an exact `date_of_birth` match together with a case-insensitive, whitespace-trimmed
   match on both `display_name` and `mother_name`. This project's highest-priority rule is
   "never invent data," and a wrong match here is worse than that: attaching Child A's
   allergy or growth history to Child B because their names are similar is a pediatric
   feeding-safety hazard, not a cosmetic bug. A missed match costs an operator a few seconds
   of manual lookup; a wrong one silently attached is far more expensive. Fuzzy matching
   (trigram similarity, soundex, etc.) is deliberately out of scope for this reason, not
   because it would be hard to add.
2. **`mother_name` is a new field, added specifically to make condition (b) safe.** Name +
   date of birth alone is a real, plausible coincidence (twins, common names in one
   region). Requiring the mother's name to match too, on top of both of those, is what
   makes the fallback path trustworthy without needing `case_id` — which nothing prompts an
   operator to fill in today. This is a genuinely new column; nothing in this schema
   captures a parent/guardian name anywhere (confirmed by grep across
   `internal/db/migrations/`, `internal/profile/`, `internal/api/handlers/profiles.go`).
3. **Never applied automatically — always surfaced for a human to decide.** Matches this
   project's whole frontend philosophy (`CLAUDE.md`, "Frontend" section): operators see the
   machinery and confirm, nothing silent. A match is a suggestion banner with an explicit
   "Load this record" button; ignoring it changes nothing about how generation behaves
   today.
4. **Add `Case ID` and `Mother's name` fields to the generate form.** Today nothing prompts
   an operator to enter either, so both match signals are currently unreachable from the
   form that most needs them. These are the two new input fields this plan adds.
5. **New route is `GET /api/profile-matches`, not `GET /api/profiles/match`.** chi already
   registers `GET /api/profiles/{childID}` — placing a second static route at the same path
   depth risks depending on router-precedence behavior nobody should have to reason about
   to add a query endpoint. A distinct top-level path removes the ambiguity entirely.
6. **Indexes, not a uniqueness constraint.** A plain (non-unique) index on `case_id` and a
   plain composite index on `(lower(display_name), date_of_birth, lower(mother_name))` make
   the lookup cheap without asserting something false: two children can legitimately share
   all three fields by coincidence, and an operator can legitimately mistype a `case_id` for
   two different children — the database should not reject either case, only help find
   them.
7. **`FindMatches` returns nothing when called with no usable signal.** Called with no
   `case_id` and no complete `display_name`+`date_of_birth`+`mother_name` triple, it returns
   zero rows rather than the whole table — the frontend never fires the check on an
   empty/partial form, and a partial name+DOB entry (mother's name not yet typed) never
   triggers condition (b) on its own.

## Global Constraints

- No fuzzy/similarity matching of any kind — exact `case_id`, or exact
  `date_of_birth` + case/whitespace-normalized `display_name`, and nothing looser.
- No automatic merge, overwrite, or silent application of a match. Every match is
  surfaced to the operator; loading one is always an explicit click.
- Inline generation's default behavior (nothing persisted unless the operator explicitly
  loads or saves a profile) does not change.
- No new dependencies, frontend or backend.
- Follow this codebase's established conventions exactly: `fmt.Errorf("...: %w", err)`
  wrapping, `coalesce(...)` on nullable columns in SELECTs, table-driven Go tests,
  `writeJSON`/`writeError` for HTTP responses, shadcn/ui components on the frontend, all
  API calls through `web/src/lib/api.ts`.

---

### Task 1: Migration — `mother_name` column and match-lookup indexes

**Files:**
- Create: `internal/db/migrations/0028_profile_match.up.sql`
- Create: `internal/db/migrations/0028_profile_match.down.sql`

**Interfaces:**
- Produces: `child_profile.mother_name text` (new, nullable column) and two indexes.
  Task 2's `Stored` struct and SQL, and every later task, depend on this column existing
  under this exact name.

- [ ] **Step 1: Write the up migration**

```sql
-- mother_name: a new field, added specifically to make the name+date-of-birth match path
-- in profile.FindMatches (internal/profile/profile.go) safe. Name + date of birth alone is
-- a real, plausible coincidence (twins, common names in one region); requiring the
-- mother's name to match too is what makes that fallback trustworthy without depending on
-- case_id, which nothing prompts an operator to fill in today. Nullable, like every other
-- optional profile field here -- not every consultation captures it, and an absent value
-- means that match path simply does not fire for this row, not that the row is invalid.
ALTER TABLE child_profile ADD COLUMN mother_name text
    CHECK (mother_name IS NULL OR length(mother_name) <= 100);

COMMENT ON COLUMN child_profile.mother_name IS
    'Family-declared. Used, together with display_name and date_of_birth, as an exact-match '
    'signal for surfacing a possible existing profile to an operator -- see '
    'profile.FindMatches. Never fuzzy-matched, never used to auto-apply a match.';

-- Deliberately NOT unique constraints -- see the plan's "Decisions made and why" #6: two
-- children can legitimately share a case_id typo or a name+dob+mother-name coincidence, and
-- the database should not reject either. These only make an existing, honest exact-match
-- lookup cheap.
CREATE INDEX child_profile_case_id_idx
    ON child_profile (case_id)
    WHERE case_id IS NOT NULL;

CREATE INDEX child_profile_name_dob_mother_idx
    ON child_profile (lower(display_name), date_of_birth, lower(mother_name))
    WHERE display_name IS NOT NULL AND mother_name IS NOT NULL;
```

- [ ] **Step 2: Write the down migration**

```sql
DROP INDEX IF EXISTS child_profile_name_dob_mother_idx;
DROP INDEX IF EXISTS child_profile_case_id_idx;
ALTER TABLE child_profile DROP COLUMN IF EXISTS mother_name;
```

- [ ] **Step 3: Apply and verify**

Run: `scripts/dev_db.fish up` then `TEST_DATABASE_URL=(scripts/dev_db.fish url) go run ./cmd/import` (migrations run automatically on connect, per `internal/db/db.go`'s `Connect`).
Verify: `psql (scripts/dev_db.fish url) -c "\d child_profile"` shows `mother_name` and both new indexes.

- [ ] **Step 4: Commit**

```bash
git add internal/db/migrations/0028_profile_match.up.sql internal/db/migrations/0028_profile_match.down.sql
git commit -m "db: add child_profile.mother_name, index for exact case_id and name+dob+mother lookups"
```

---

### Task 2: `MotherName` round-trip + `profile.FindMatches`

**Files:**
- Modify: `internal/profile/profile.go`
- Test: `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: `child_profile.mother_name` (Task 1).
- Produces: `Stored.MotherName string` (so `mother_name` round-trips through `Save`/`Load`
  like every other profile field), `type MatchCandidate struct { ChildID, CaseID,
  DisplayName string; DateOfBirth, LastTouched time.Time }`, and `func FindMatches(ctx
  context.Context, pool *pgxpool.Pool, caseID, displayName, motherName string, dateOfBirth
  time.Time) ([]MatchCandidate, error)`. Task 3's handler calls this exactly. (Note:
  `MatchCandidate` itself does not carry `MotherName` — a match result names *which* child
  it might be, not a re-statement of the query that found it.)

- [ ] **Step 1: Add the `strings` import**

`internal/profile/profile.go` does not import `strings` yet (confirmed: current imports are
`context`, `errors`, `fmt`, `time`, `pgx/v5`, `pgx/v5/pgxpool`, `internal/models`). Add it to
the import block.

- [ ] **Step 2: Add `MotherName` to `Stored`**

In the `Stored` struct, add one field directly below `CaseID`:

```go
	MotherName           string
```

- [ ] **Step 3: Wire `MotherName` through `Save`**

`Save`'s upsert currently has 15 columns/placeholders. Replace the whole `INSERT`
statement and its argument list with this (the only changes are `mother_name` added as
column 3, and every placeholder from the old `$3` onward shifted up by one):

```go
	_, err = tx.Exec(ctx, `
		INSERT INTO child_profile (child_id, case_id, mother_name, display_name, date_of_birth,
			sex, language_id, region_culture, cuisine_code, diet_type, vegan,
			religious_restriction, budget_band, max_prep_time_min, max_cook_time_min, created_by)
		VALUES ($1,$2,nullif($3,''),nullif($4,''),$5,nullif($6,''),nullif($7,''),nullif($8,''),
			nullif($9,''),nullif($10,''),$11,nullif($12,''),nullif($13,''),nullif($14,0),
			nullif($15,0),$16)
		ON CONFLICT (child_id) DO UPDATE SET
			case_id = excluded.case_id,
			mother_name = excluded.mother_name,
			display_name = excluded.display_name,
			date_of_birth = excluded.date_of_birth,
			sex = excluded.sex,
			language_id = excluded.language_id,
			region_culture = excluded.region_culture,
			cuisine_code = excluded.cuisine_code,
			diet_type = excluded.diet_type,
			vegan = excluded.vegan,
			religious_restriction = excluded.religious_restriction,
			budget_band = excluded.budget_band,
			max_prep_time_min = excluded.max_prep_time_min,
			max_cook_time_min = excluded.max_cook_time_min,
			updated_by = excluded.created_by,
			updated_at = now()`,
		s.ChildID, nullString(s.CaseID), s.MotherName, s.DisplayName, s.DateOfBirth, s.Sex,
		s.LanguageID, s.RegionCulture, s.CuisineCode, s.DietType, s.Vegan,
		s.ReligiousRestriction, s.BudgetBand, s.MaxPrepTimeMin, s.MaxCookTimeMin, s.CreatedBy)
```

- [ ] **Step 4: Wire `MotherName` through `Load`**

Replace `Load`'s `SELECT`/`Scan` pair (the profile-row one, not the four per-table loaders
below it) with this — `coalesce(mother_name,'')` added as the third selected column, scanned
into `&s.MotherName` in the matching position:

```go
	err := pool.QueryRow(ctx, `
		SELECT child_id, coalesce(case_id,''), coalesce(mother_name,''),
		       coalesce(display_name,''), date_of_birth,
		       coalesce(sex,''), coalesce(language_id,''), coalesce(region_culture,''),
		       coalesce(cuisine_code,''), coalesce(diet_type,''), vegan,
		       coalesce(religious_restriction,''), coalesce(budget_band,''),
		       coalesce(max_prep_time_min,0), coalesce(max_cook_time_min,0), created_by
		FROM child_profile WHERE child_id = $1`, childID).
		Scan(&s.ChildID, &s.CaseID, &s.MotherName, &s.DisplayName, &s.DateOfBirth, &s.Sex,
			&s.LanguageID, &s.RegionCulture, &s.CuisineCode, &s.DietType, &s.Vegan,
			&s.ReligiousRestriction, &s.BudgetBand, &s.MaxPrepTimeMin, &s.MaxCookTimeMin,
			&s.CreatedBy)
```

- [ ] **Step 5: Write the failing tests**

Append to `internal/profile/profile_test.go` (uses the existing `testPool(t)` and `date(s
string)` helpers already in this file):

```go
func TestFindMatchesByExactCaseID(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM child_profile WHERE child_id IN ('MATCH-A', 'MATCH-B')`)
	})

	if err := Save(ctx, pool, Stored{
		ChildID: "MATCH-A", CaseID: "CASE-100", DisplayName: "Aarav Sen",
		DateOfBirth: date("2023-01-01"), CreatedBy: "test",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := Save(ctx, pool, Stored{
		ChildID: "MATCH-B", CaseID: "CASE-200", DisplayName: "Someone Else",
		DateOfBirth: date("2020-01-01"), CreatedBy: "test",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := FindMatches(ctx, pool, "CASE-100", "", "", time.Time{})
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if len(got) != 1 || got[0].ChildID != "MATCH-A" {
		t.Fatalf("want exactly MATCH-A, got %+v", got)
	}
}

func TestFindMatchesByNameDateOfBirthAndMotherNameIsCaseAndWhitespaceInsensitive(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM child_profile WHERE child_id = 'MATCH-C'`)
	})

	if err := Save(ctx, pool, Stored{
		ChildID: "MATCH-C", DisplayName: "  Priya Das  ", MotherName: "  Ananya Das  ",
		DateOfBirth: date("2022-06-15"), CreatedBy: "test",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := FindMatches(ctx, pool, "", "priya das", "ananya das", date("2022-06-15"))
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if len(got) != 1 || got[0].ChildID != "MATCH-C" {
		t.Fatalf("want exactly MATCH-C on a case/whitespace-insensitive match, got %+v", got)
	}
}

func TestFindMatchesRequiresMotherNameTooNotJustNameAndDateOfBirth(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM child_profile WHERE child_id = 'MATCH-E'`)
	})

	if err := Save(ctx, pool, Stored{
		ChildID: "MATCH-E", DisplayName: "Common Name", MotherName: "Real Mother",
		DateOfBirth: date("2022-01-01"), CreatedBy: "test",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Same name and date of birth, but a mismatched mother's name -- this is exactly the
	// coincidence mother_name exists to rule out (see the plan's "Decisions made and why" #2).
	got, err := FindMatches(ctx, pool, "", "Common Name", "A Different Mother", date("2022-01-01"))
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a mismatched mother's name must prevent the match even with name+dob agreeing, got %+v", got)
	}

	// No mother's name supplied at all -- must not match on name+dob alone.
	got, err = FindMatches(ctx, pool, "", "Common Name", "", date("2022-01-01"))
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("name+dob alone, with no mother's name, must not match, got %+v", got)
	}
}

func TestFindMatchesReturnsNothingOnADifferentDateOfBirth(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM child_profile WHERE child_id = 'MATCH-D'`)
	})

	if err := Save(ctx, pool, Stored{
		ChildID: "MATCH-D", DisplayName: "Ravi Kumar", MotherName: "Sita Kumar",
		DateOfBirth: date("2021-03-10"), CreatedBy: "test",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := FindMatches(ctx, pool, "", "Ravi Kumar", "Sita Kumar", date("2021-03-11"))
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a one-day-off date of birth must not match, got %+v", got)
	}
}

func TestFindMatchesReturnsNothingWithNoUsableInput(t *testing.T) {
	pool := testPool(t)
	got, err := FindMatches(context.Background(), pool, "", "", "", time.Time{})
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("no case_id and no complete name+dob+mother triple must return nothing, got %+v", got)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/profile/... -run TestFindMatches -v`
Expected: FAIL with `undefined: FindMatches`.

- [ ] **Step 7: Implement `MatchCandidate` and `FindMatches`**

Add to `internal/profile/profile.go`, after the existing `Load` function:

```go
// MatchCandidate is one possible existing profile surfaced to an operator, never applied
// automatically. Deliberately narrower than Stored -- a candidate is something to look at
// and decide about, not a profile ready to use as-is.
type MatchCandidate struct {
	ChildID     string
	CaseID      string
	DisplayName string
	DateOfBirth time.Time
	LastTouched time.Time
}

// FindMatches looks for an existing child_profile row that might be the same child as the
// one described by caseID/displayName/motherName/dateOfBirth, matched on exact identity
// only -- never a similarity score. A wrong match in a pediatric feeding-safety context
// (attaching one child's allergy or growth history to another) is a worse failure than
// finding nothing, so this deliberately returns zero rows rather than a "close enough" one.
//
// Two independent match conditions, either of which surfaces a row:
//   - an exact, non-empty case_id match -- the strongest signal when the operator has one
//   - an exact date_of_birth match together with a case-insensitive, whitespace-trimmed
//     match on BOTH display_name and mother_name -- name+dob alone is a real coincidence
//     (twins, common names), and mother_name is what makes this fallback trustworthy
//     without depending on case_id, which nothing prompts an operator for today
//
// Called with no case_id and no complete display_name+motherName+dateOfBirth triple, this
// returns no rows rather than every profile in the table.
func FindMatches(ctx context.Context, pool *pgxpool.Pool, caseID, displayName, motherName string, dateOfBirth time.Time) ([]MatchCandidate, error) {
	caseID = strings.TrimSpace(caseID)
	displayName = strings.TrimSpace(displayName)
	motherName = strings.TrimSpace(motherName)
	if caseID == "" && (displayName == "" || motherName == "" || dateOfBirth.IsZero()) {
		return nil, nil
	}

	rows, err := pool.Query(ctx, `
		SELECT child_id, coalesce(case_id,''), coalesce(display_name,''), date_of_birth,
		       coalesce(updated_at, created_at)
		FROM child_profile
		WHERE (nullif($1,'') IS NOT NULL AND case_id = $1)
		   OR (nullif($2,'') IS NOT NULL AND nullif($3,'') IS NOT NULL AND $4::date IS NOT NULL
		       AND lower(trim(display_name)) = lower(trim($2))
		       AND lower(trim(mother_name)) = lower(trim($3))
		       AND date_of_birth = $4)
		ORDER BY coalesce(updated_at, created_at) DESC`,
		caseID, displayName, motherName, nullDate(dateOfBirth))
	if err != nil {
		return nil, fmt.Errorf("profile: find matches: %w", err)
	}
	defer rows.Close()

	var out []MatchCandidate
	for rows.Next() {
		var m MatchCandidate
		if err := rows.Scan(&m.ChildID, &m.CaseID, &m.DisplayName, &m.DateOfBirth, &m.LastTouched); err != nil {
			return nil, fmt.Errorf("profile: scan match: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("profile: match rows: %w", err)
	}
	return out, nil
}

// nullDate returns nil for a zero time.Time so an absent date of birth reaches Postgres as
// SQL NULL rather than as 0001-01-01, which would otherwise be a real (wrong) date to
// compare against instead of "no date supplied".
func nullDate(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/profile/... -run "TestFindMatches|TestSaveAndLoadRoundTrip" -v`
Expected: all `TestFindMatches*` PASS, and `TestSaveAndLoadRoundTrip` still passes (confirms
`mother_name` didn't break the existing round-trip test — that test doesn't set
`MotherName`, so it round-trips as `""`, same as any other unset optional field).

- [ ] **Step 9: Commit**

```bash
git add internal/profile/profile.go internal/profile/profile_test.go
git commit -m "profile: round-trip mother_name, add FindMatches (exact case_id, or exact name+dob+mother)"
```

---

### Task 3: `GET /api/profile-matches` handler

**Files:**
- Modify: `internal/api/handlers/profiles.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/handlers/profiles_test.go`

**Interfaces:**
- Consumes: `profile.FindMatches` from Task 2 (exact 4-arg signature above).
- Produces: `GET /api/profile-matches?case_id=&display_name=&mother_name=&date_of_birth=`
  returning `[]matchCandidateDTO` (JSON array, never null — Task 5's frontend renders it
  directly). Also: `profileDTO.MotherName`, so a saved profile's `mother_name` round-trips
  through `PUT`/`GET /api/profiles/{childID}` like every other field.

- [ ] **Step 1: Add `MotherName` to `profileDTO`, `toDTO`, and `fromDTO`**

In `internal/api/handlers/profiles.go`, add one field to `profileDTO` directly below
`CaseID`:

```go
	MotherName           string               `json:"mother_name,omitempty"`
```

In `toDTO`, add to the struct literal directly below `CaseID: s.CaseID,`:

```go
		MotherName: s.MotherName,
```

In `fromDTO`, add to the struct literal directly below `ChildID: d.ChildID, CaseID:
d.CaseID,`:

```go
		MotherName: d.MotherName,
```

- [ ] **Step 2: Update `profileRouter` in the test file to register the new route**

In `internal/api/handlers/profiles_test.go`, add one line to `profileRouter`:

```go
func profileRouter(t *testing.T) (*chi.Mux, *Handlers) {
	t.Helper()
	h := New(testPool(t), aidraft.Disabled)
	r := chi.NewRouter()
	r.Put("/api/profiles/{childID}", h.PutProfile)
	r.Get("/api/profiles/{childID}", h.GetProfile)
	r.Get("/api/profiles/{childID}/engine-input", h.GetProfileEngineInput)
	r.Get("/api/profile-matches", h.MatchProfiles)
	return r, h
}
```

- [ ] **Step 3: Write the failing tests**

Append to `internal/api/handlers/profiles_test.go`:

```go
func TestMatchProfilesByCaseID(t *testing.T) {
	r, h := profileRouter(t)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			`DELETE FROM child_profile WHERE child_id = 'TEST-CHILD-001'`)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/profiles/TEST-CHILD-001",
		bytes.NewReader([]byte(putProfileBody))))
	if rec.Code != 200 {
		t.Fatalf("seed put: got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/profile-matches?case_id=TEST-CASE-001", nil))
	if rec.Code != 200 {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		ChildID string `json:"child_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ChildID != "TEST-CHILD-001" {
		t.Fatalf("want exactly TEST-CHILD-001, got %+v", got)
	}
}

// putProfileBodyWithMother is putProfileBody plus mother_name, under a distinct child_id
// so it can seed a test independently of putProfileBody's own fixture and cleanup.
const putProfileBodyWithMother = `{
  "child_id": "TEST-CHILD-002",
  "display_name": "Test Child Two",
  "mother_name": "Test Mother",
  "date_of_birth": "2022-03-10",
  "created_by": "integration-test",
  "allergens": []
}`

func TestMatchProfilesByNameDateOfBirthAndMotherName(t *testing.T) {
	r, h := profileRouter(t)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			`DELETE FROM child_profile WHERE child_id = 'TEST-CHILD-002'`)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/profiles/TEST-CHILD-002",
		bytes.NewReader([]byte(putProfileBodyWithMother))))
	if rec.Code != 200 {
		t.Fatalf("seed put: got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET",
		"/api/profile-matches?display_name=test+child+two&mother_name=test+mother&date_of_birth=2022-03-10", nil))
	if rec.Code != 200 {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		ChildID string `json:"child_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ChildID != "TEST-CHILD-002" {
		t.Fatalf("want exactly TEST-CHILD-002, got %+v", got)
	}

	// Same name and date of birth, no mother_name in the query at all -- must not match.
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET",
		"/api/profile-matches?display_name=test+child+two&date_of_birth=2022-03-10", nil))
	if rec.Code != 200 {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("name+dob alone, with no mother_name, must not match, got %q", rec.Body.String())
	}
}

func TestMatchProfilesReturnsEmptyArrayNotNullWithNoQuery(t *testing.T) {
	r, _ := profileRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/profile-matches", nil))
	if rec.Code != 200 {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("want an empty JSON array, got %q", rec.Body.String())
	}
}

func TestMatchProfilesRejectsAMalformedDate(t *testing.T) {
	r, _ := profileRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/profile-matches?date_of_birth=01-05-2022", nil))
	if rec.Code != 400 {
		t.Fatalf("want 400 for a malformed date, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/handlers/... -run TestMatchProfiles -v`
Expected: FAIL to compile — `h.MatchProfiles` undefined.

- [ ] **Step 5: Implement the DTO and handler**

Add to `internal/api/handlers/profiles.go`, after `GetProfileEngineInput`:

```go
type matchCandidateDTO struct {
	ChildID     string `json:"child_id"`
	CaseID      string `json:"case_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	DateOfBirth string `json:"date_of_birth"`
	LastTouched string `json:"last_touched"`
}

// MatchProfiles looks for an existing stored profile that might already be this child,
// surfaced for an operator to look at and decide about -- never applied automatically. See
// profile.FindMatches for the exact-match-only rule this never relaxes: no fuzzy or
// similarity matching, ever.
func (h *Handlers) MatchProfiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	caseID := q.Get("case_id")
	displayName := q.Get("display_name")
	motherName := q.Get("mother_name")

	var dob time.Time
	if raw := q.Get("date_of_birth"); raw != "" {
		parsed, err := time.Parse(dateLayout, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "date_of_birth "+raw+" is not a YYYY-MM-DD date")
			return
		}
		dob = parsed
	}

	matches, err := profile.FindMatches(r.Context(), h.pool, caseID, displayName, motherName, dob)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profile match failed: "+err.Error())
		return
	}

	// Never nil: a client rendering "N possible matches" should not need a null check to
	// show zero.
	out := make([]matchCandidateDTO, 0, len(matches))
	for _, m := range matches {
		out = append(out, matchCandidateDTO{
			ChildID: m.ChildID, CaseID: m.CaseID, DisplayName: m.DisplayName,
			DateOfBirth: m.DateOfBirth.UTC().Format(dateLayout),
			LastTouched: m.LastTouched.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 6: Register the route**

In `internal/api/router.go`, add one line beside the other `/api/profiles/...` routes:

```go
		r.Put("/api/profiles/{childID}", h.PutProfile)
		r.Get("/api/profiles/{childID}", h.GetProfile)
		r.Get("/api/profiles/{childID}/engine-input", h.GetProfileEngineInput)
		r.Get("/api/profile-matches", h.MatchProfiles)
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./internal/api/handlers/... -run "TestMatchProfiles|TestProfileRoundTripsThroughHTTP" -v`
Expected: all `TestMatchProfiles*` PASS, and `TestProfileRoundTripsThroughHTTP` still passes
(confirms adding `MotherName` to `profileDTO` didn't break the existing round-trip test).

- [ ] **Step 8: Commit**

```bash
git add internal/api/handlers/profiles.go internal/api/handlers/profiles_test.go internal/api/router.go
git commit -m "api: add GET /api/profile-matches, an exact-match-only existing-child check"
```

---

### Task 4: Frontend API client and types

**Files:**
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`

**Interfaces:**
- Produces: `MatchCandidate` type and `matchProfiles(query)` function. Task 5 calls this
  exactly.

- [ ] **Step 1: Add the type**

In `web/src/lib/types.ts`, add near `StoredProfile`:

```typescript
export interface MatchCandidate {
  child_id: string;
  case_id?: string;
  display_name?: string;
  date_of_birth: string;
  last_touched: string;
}
```

Also add one field to the existing `StoredProfile` interface in this file, directly below
its `case_id?: string;` line, so `mother_name` round-trips through `getProfile`/`putProfile`
like every other profile field (Task 5's `loadMatch` reads `p.mother_name` back out):

```typescript
  mother_name?: string;
```

- [ ] **Step 2: Add the API function**

In `web/src/lib/api.ts`, add after `getProfileEngineInput`:

```typescript
/** Looks for an existing stored profile that might already be this child -- exact case_id,
 *  or exact date_of_birth plus a case/whitespace-insensitive match on BOTH display_name and
 *  mother_name. Never a fuzzy or similarity match. Returns an empty array, never throws,
 *  when nothing is a plausible query (matching the server's own "nothing to search for"
 *  behavior) so callers don't need a special case for an empty form. */
export async function matchProfiles(query: {
  case_id?: string; display_name?: string; mother_name?: string; date_of_birth?: string;
}): Promise<MatchCandidate[]> {
  const params = new URLSearchParams();
  if (query.case_id) params.set("case_id", query.case_id);
  if (query.display_name) params.set("display_name", query.display_name);
  if (query.mother_name) params.set("mother_name", query.mother_name);
  if (query.date_of_birth) params.set("date_of_birth", query.date_of_birth);
  return request<MatchCandidate[]>(`/api/profile-matches?${params.toString()}`);
}
```

Add `MatchCandidate` to the existing `import type { ... } from "./types"` block at the top
of the file.

- [ ] **Step 3: Verify it compiles**

Run: `cd web && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/types.ts web/src/lib/api.ts
git commit -m "web: add matchProfiles API client function"
```

---

### Task 5: Console UI — case_id + mother's name fields, match check, suggestion banner

**Files:**
- Modify: `web/src/components/child-input-form.tsx`

**Interfaces:**
- Consumes: `matchProfiles` and `getProfile` (already exists, `web/src/lib/api.ts:97`) from
  Task 4 and the existing client.
- Produces: nothing new for another task to consume — this is the leaf of the chain.

- [ ] **Step 1: Add state**

In `ChildInputForm`, alongside the existing `name`/`dob`/... state declarations, add:

```typescript
  const [caseId, setCaseId] = useState("");
  const [motherName, setMotherName] = useState("");
  const [matches, setMatches] = useState<MatchCandidate[]>([]);
  const [dismissedMatch, setDismissedMatch] = useState(false);
```

Add `MatchCandidate` to the existing `import type {...} from "@/lib/types"` block. The
current `import {...} from "@/lib/api"` line in this file is:

```typescript
import {
  GenerateInput, getRegions, getCuisines, getAllergens, getEnums, getSpecialCareConditions,
} from "@/lib/api";
```

Add `matchProfiles` and `getProfile` to it — neither is imported by this file today.

- [ ] **Step 2: Add the debounced match-check effect**

Add after the existing reference-data `useEffect`:

```typescript
  // Debounced: fires 500ms after the operator stops typing, so a check doesn't fire on
  // every keystroke. Fires on a case id alone, or once name+date-of-birth+mother's-name are
  // ALL filled in -- matching profile.FindMatches's own rule that name+dob alone is never
  // enough. Cleared and never fired again once the operator explicitly dismisses a
  // suggestion for the current inputs, so accepting "this is a new child" does not re-show
  // the same banner on every keystroke afterward.
  useEffect(() => {
    setDismissedMatch(false);
    if (!caseId && !(name && dob && motherName)) {
      setMatches([]);
      return;
    }
    const handle = setTimeout(() => {
      matchProfiles({
        case_id: caseId || undefined,
        display_name: name || undefined,
        mother_name: motherName || undefined,
        date_of_birth: dob || undefined,
      })
        .then(setMatches)
        .catch(() => setMatches([])); // A failed check is not itself an error worth surfacing --
        // it only means the suggestion banner does not appear, and generation proceeds exactly
        // as it does when there genuinely is no match.
    }, 500);
    return () => clearTimeout(handle);
  }, [caseId, name, dob, motherName]);
```

- [ ] **Step 3: Add the "load this record" handler**

```typescript
  async function loadMatch(childID: string) {
    const p = await getProfile(childID);
    setName(p.display_name ?? "");
    setDob(p.date_of_birth);
    setSex(p.sex ?? "");
    setLanguage(p.language_id ?? "");
    setRegion(p.region_culture ?? "");
    setCuisine(p.cuisine_code ?? "");
    setDiet(p.diet_type ?? "");
    setBudget(p.budget_band ?? "");
    setCaseId(p.case_id ?? "");
    setMotherName(p.mother_name ?? "");
    setConfirmed(p.allergens.filter((a) => a.status === "confirmed").map((a) => a.group));
    setSuspected(p.allergens.filter((a) => a.status === "suspected").map((a) => a.group));
    setMatches([]);
    setDismissedMatch(true);
  }
```

- [ ] **Step 4: Add the Case ID and Mother's name fields to the Child section**

Replace the "Child" section's grid (currently `sm:grid-cols-2 lg:grid-cols-4` holding
Name/Date of birth/Sex/Language) with this — the only changes are the grid's column count
and the two new fields; Name/DOB/Sex/Language are copied verbatim from the current file:

```tsx
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-6">
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
          <div className={field}>
            <Label htmlFor="g-mother">Mother&apos;s name</Label>
            <Input id="g-mother" value={motherName} onChange={(e) => setMotherName(e.target.value)}
                   placeholder="for matching an existing record" />
          </div>
        </div>
```

- [ ] **Step 5: Add the suggestion banner**

Immediately after the Child section, before "Food practice and place":

```tsx
      {matches.length > 0 && !dismissedMatch && (
        <Alert>
          <AlertTitle>
            {matches.length === 1 ? "This may already be a saved child" : `${matches.length} possible matches found`}
          </AlertTitle>
          <AlertDescription className="space-y-2">
            {matches.map((m) => (
              <div key={m.child_id} className="flex items-center justify-between gap-3">
                <span className="font-mono text-xs">
                  {m.child_id} {m.case_id && `· case ${m.case_id}`} · {m.display_name} · DOB {m.date_of_birth}
                </span>
                <Button type="button" size="sm" variant="outline" onClick={() => loadMatch(m.child_id)}>
                  Load this record
                </Button>
              </div>
            ))}
            <Button type="button" size="sm" variant="ghost" onClick={() => setDismissedMatch(true)}>
              This is a different child
            </Button>
          </AlertDescription>
        </Alert>
      )}
```

`Alert`/`AlertTitle`/`AlertDescription` are already used elsewhere in this codebase
(`web/src/components/book-generator.tsx`) — import from `@/components/ui/alert`.

- [ ] **Step 6: Include `case_id` and `mother_name` in the generate submission**

In `web/src/lib/api.ts`, add two fields to the `GenerateInput` interface, directly below the
existing `display_name?: string;` line:

```typescript
  case_id?: string;
  mother_name?: string;
```

In `child-input-form.tsx`'s `submit()`, add two fields to the `GenerateInput` object
literal, directly below the existing `display_name: name || undefined,` line:

```typescript
      case_id: caseId || undefined,
      mother_name: motherName || undefined,
```

This is metadata only: `internal/api/handlers/book_generate.go`'s `generateRequest` embeds
`profileDTO`, which after Task 3 has both a `case_id` field (`profiles.go:36`) and a
`mother_name` field, so this reaches the backend for free — no backend change needed in
this step. It does not change generation behavior (the inline path still stores nothing),
it just lets a request carry the identity facts an operator already confirmed via the match
check, for whatever downstream use needs them later (e.g. an eventual explicit "save this
profile" action, out of scope for this plan).

- [ ] **Step 7: Manual verification**

Run the app (`go run ./cmd/server` + `cd web && npm run dev`), open `/books`:
1. Type a case id that does not exist yet -> no banner appears.
2. Save a profile via the existing profile-form screen (or `PUT /api/profiles/{id}` by
   hand) with a known case_id, name, mother's name, and date of birth.
3. On the generate form, type that same case_id -> banner appears within ~500ms, naming the
   right `child_id`.
4. Click "Load this record" -> form fields populate from the stored profile, including
   Mother's name.
5. Clear the case id, type the same name + date of birth + mother's name instead -> banner
   appears again via that path.
6. Type the same name + date of birth but a different mother's name -> no banner.
7. Type the same name + date of birth with mother's name left blank -> no banner.

- [ ] **Step 8: Commit**

```bash
git add web/src/components/child-input-form.tsx web/src/lib/api.ts
git commit -m "web: surface possible existing-child matches on the generate form, never applied automatically"
```

---

### Task 6: Frontend test

**Files:**
- Modify: `web/src/components/child-input-form.test.tsx` if it exists, else create it
  following the existing pattern in `web/src/components/book-generator.test.tsx`.

- [ ] **Step 1: Write a test that a match banner appears and loading it prefills the form**

Mock `matchProfiles` and `getProfile` from `@/lib/api` (vitest `vi.mock`, matching however
`book-generator.test.tsx` already mocks the api module — check that file's mocking pattern
first and follow it exactly rather than introducing a new one). Assert: typing a case id
that the mock resolves to one candidate renders the candidate's `child_id`; clicking "Load
this record" calls the mocked `getProfile` and the Name input's value updates to the
mocked profile's `display_name`.

- [ ] **Step 2: Run it**

Run: `cd web && npm test`
Expected: new test passes, all existing tests still pass (30 today).

- [ ] **Step 3: Commit**

```bash
git add web/src/components/child-input-form.test.tsx
git commit -m "web: test that an existing-child match surfaces and loads correctly"
```

---

## Verification (whole plan)

```bash
go build ./...
go vet ./...
scripts/dev_db.fish up
TEST_DATABASE_URL=$(scripts/dev_db.fish url) go test ./...
cd web && npx tsc --noEmit && npm test
```

Then the manual walkthrough in Task 5 Step 7, end to end, against a real running server —
this is a UI feature, and this project's own standing practice is that layout/UI defects are
found by actually looking at the running page, not only by tests.
