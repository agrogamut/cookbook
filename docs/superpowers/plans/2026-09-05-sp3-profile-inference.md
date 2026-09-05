# SP3: AI Profile Inference Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a doctor leaves a ranker-input field blank, infer it from what they did supply, grounded on the real corpus vocabularies, and never touch a field only the doctor can know.

**Architecture:** One new `Drafter` method, `InferProfile`, following the exact shape of the existing `DraftFoodGroupPriorities`: a prompt fed the real closed vocabularies, a `genai.Schema` constraining every field to an enum over those vocabularies, and a Go-side re-validation in `internal/book` that drops any field failing the check. It runs once in `AssembleSet` before either assembler, so both books are built against one inferred profile.

**Tech Stack:** Go 1.x, `google.golang.org/genai`, pgx/v5, Next.js.

**Spec:** `docs/superpowers/specs/2026-09-05-direct-generation-design.md`

**Depends on:** SP2 complete and merged. The field set has to be final before inference decides which fields it fills.

## Global Constraints

- **The never-inferred list is absolute.** `date_of_birth`, confirmed allergens, suspected allergens, growth measurements, name and identity, `SpecialCareCondition`, `ClinicalFlags`. Not "avoided" - structurally absent from the response type, so no task can set one by accident.
- **Grounding is required, not optional.** A field with no supplied source to reason from stays blank. Inference fires where there is something real to reason from and declines otherwise. Picking a plausible default with nothing behind it is the exact thing this project's hard rule forbids.
- **Inferred values never print in either book.** They changed which real recipes were selected. They are not facts about the child.
- Vocabularies are read live from the database on every request. No hardcoded region, cuisine, budget or time list anywhere in this sub-project.
- The whole feature is off when no `Drafter` is configured. `aidraft.Disabled` must produce byte-identical books to today.
- No emojis. No em dashes or en dashes in prose. No AI attribution anywhere.
- `go build ./...`, `go vet ./...`, `go test ./...` with `TEST_DATABASE_URL`; `npx tsc --noEmit`, `npm test`, `npm run build`.

## File Structure

| File | Responsibility |
|---|---|
| `internal/aidraft/types.go` | `InferenceRequest`, `InferredFields`, `InferredValue`. The never-inferred list is enforced by these types having no such field. |
| `internal/aidraft/prompt.go` | `inferProfileSchema(vocab)`, `buildInferProfilePrompt(req)`. |
| `internal/aidraft/draft.go` | `(*geminiClient).InferProfile`. |
| `internal/aidraft/client.go` | `Drafter` interface gains the method; `disabledClient` returns a zero result. |
| `internal/book/infer.go` (new) | Reads the live vocabularies, calls the drafter, re-validates every returned value, applies survivors to `profile.Stored`. |
| `internal/book/set.go` | Calls it once before either assembler. |
| `internal/api/handlers/book_set.go`, `book_generate.go` | Surface `inferred_fields` in the JSON. |
| `web/src/lib/types.ts`, `web/src/components/book-generator.tsx` | Operator panel listing what was inferred and from what. |

---

### Task 1: The request and response types

**Files:**
- Modify: `internal/aidraft/types.go`
- Test: `internal/aidraft/draft_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `InferenceRequest`, `InferredFields`, `InferredValue`, all used by every later task.

- [ ] **Step 1: Write the failing test**

Add to `internal/aidraft/draft_test.go`:

```go
// The never-inferred list is enforced by the type, not by a runtime check. A test that
// asserted "the model did not return an allergen" would only be as good as the prompt;
// a struct with no field to put one in cannot be made to carry one by any prompt, any
// schema drift, or any future edit that forgets why the rule exists.
func TestInferredFieldsCannotCarryAFactOnlyTheDoctorKnows(t *testing.T) {
	forbidden := []string{
		"DateOfBirth", "Allergens", "SuspectedAllergens", "Growth",
		"DisplayName", "MotherName", "CaseID", "SpecialCareCondition", "ClinicalFlags",
	}
	ty := reflect.TypeOf(InferredFields{})
	for _, name := range forbidden {
		if _, ok := ty.FieldByName(name); ok {
			t.Errorf("InferredFields has a %s field: this is a fact only the doctor holds "+
				"and inferring it is forbidden by the spec", name)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/aidraft/ -run TestInferredFieldsCannot -v`
Expected: FAIL to compile, no such type.

- [ ] **Step 3: Add the types**

```go
// InferenceRequest asks the model to fill ranker-input fields the doctor left blank.
//
// Supplied holds what the doctor actually entered, so the model has something real to
// reason from. Vocab holds the complete, closed value sets it may choose from, read live
// from the corpus - the same discipline FoodGroupPriorityRequest.MacroGroups uses.
//
// There is deliberately no field here for a date of birth, an allergen, a growth
// measurement, a name or a clinical condition. Those are not missing information: they are
// facts only the doctor holds, and each fails badly in both directions. A guessed allergen
// that is wrong excludes safe food; a guessed absence of one serves unsafe food; a guessed
// birth date changes the age band every page in both books is written against.
type InferenceRequest struct {
	// Supplied maps a field name to the doctor's own value, e.g. "language_id" -> "bn".
	// Only non-empty entries appear.
	Supplied map[string]string

	// Vocab maps an inferable field name to its complete real value set, e.g.
	// "region_culture" -> the nine recipe_master.region_culture values. A field absent
	// from this map is not offered to the model at all.
	Vocab map[string][]string
}

// InferredValue is one filled field, with what it was reasoned from.
//
// GroundedOn names the Supplied key the model used. A value with no grounding is dropped by
// the caller rather than printed: inference fires where there is something real to reason
// from and declines otherwise, which is what separates this from picking a plausible
// default.
type InferredValue struct {
	Value      string
	GroundedOn string
	Confidence float64
}

// InferredFields is the complete set of fields this system will ever infer. Adding a field
// here is a spec change, not an implementation detail - see
// docs/superpowers/specs/2026-09-05-direct-generation-design.md.
type InferredFields struct {
	RegionCulture  *InferredValue
	CuisineCode    *InferredValue
	BudgetBand     *InferredValue
	MaxPrepTimeMin *InferredValue
	MaxCookTimeMin *InferredValue

	Source      string
	Model       string
	GeneratedAt time.Time
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/aidraft/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/aidraft/types.go internal/aidraft/draft_test.go
git commit -m "aidraft: inference request and response types, with the never-inferred list enforced structurally"
```

---

### Task 2: Prompt and schema

**Files:**
- Modify: `internal/aidraft/prompt.go`
- Test: `internal/aidraft/draft_test.go`

**Interfaces:**
- Consumes: Task 1's types.
- Produces: `inferProfileSchema(vocab map[string][]string) *genai.Schema`, `buildInferProfilePrompt(req InferenceRequest) string`.

- [ ] **Step 1: Write the failing test**

```go
func TestInferProfileSchemaConstrainsEveryFieldToTheRealVocabulary(t *testing.T) {
	vocab := map[string][]string{
		"region_culture": {"West Bengal / East India", "South India"},
		"budget_band":    {"Low", "Medium"},
	}
	s := inferProfileSchema(vocab)
	region, ok := s.Properties["region_culture"]
	if !ok {
		t.Fatal("region_culture missing from the schema")
	}
	if len(region.Properties["value"].Enum) != 2 {
		t.Fatalf("value must be an enum over the two real regions, got %v",
			region.Properties["value"].Enum)
	}
}

func TestInferProfilePromptNamesOnlySuppliedFieldsAsGrounding(t *testing.T) {
	req := InferenceRequest{
		Supplied: map[string]string{"language_id": "bn"},
		Vocab:    map[string][]string{"region_culture": {"West Bengal / East India"}},
	}
	p := buildInferProfilePrompt(req)
	if !strings.Contains(p, "language_id") {
		t.Error("the prompt must name the supplied fields the model may reason from")
	}
	if !strings.Contains(p, "West Bengal / East India") {
		t.Error("the prompt must carry the real vocabulary")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/aidraft/ -run TestInferProfile -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Write the schema**

Follow `foodGroupPrioritySchema`'s shape exactly. One object property per key in `vocab`, each an object with `value` (a string enum over that key's real values), `grounded_on` (a string enum over the supplied field names), and `confidence` (a number). Every property optional - the model must be able to decline a field.

- [ ] **Step 4: Write the prompt**

Follow `buildFoodGroupPriorityPrompt`'s structure. The prompt must state, in its own words:
- what the doctor supplied, field by field;
- the complete allowed value set per inferable field;
- that it may name nothing outside those sets;
- that every returned field must carry a `grounded_on` naming a supplied field it actually reasoned from, and that a field it cannot ground must be omitted rather than guessed;
- that it is filling *ranking preferences* for recipe selection, not stating facts about the child.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/aidraft/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/aidraft/prompt.go internal/aidraft/draft_test.go
git commit -m "aidraft: grounded inference prompt and enum-constrained response schema"
```

---

### Task 3: The client method

**Files:**
- Modify: `internal/aidraft/draft.go`, `internal/aidraft/client.go`
- Test: `internal/aidraft/draft_test.go`

**Interfaces:**
- Consumes: Tasks 1 and 2.
- Produces: `Drafter.InferProfile(ctx, InferenceRequest) (InferredFields, error)`; `disabledClient.InferProfile` returns a zero `InferredFields` and no error.

- [ ] **Step 1: Write the failing test**

```go
func TestDisabledDrafterInfersNothingAndDoesNotError(t *testing.T) {
	got, err := Disabled.InferProfile(context.Background(), InferenceRequest{})
	if err != nil {
		t.Fatalf("the disabled drafter must fail open, got %v", err)
	}
	if got.RegionCulture != nil {
		t.Error("the disabled drafter inferred a region")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/aidraft/ -run TestDisabledDrafterInfers -v`
Expected: FAIL, no such method.

- [ ] **Step 3: Implement**

Add `InferProfile` to the `Drafter` interface. Implement on `geminiClient` following `DraftFoodGroupPriorities` line for line: `context.WithTimeout(ctx, perCallTimeout)` first (this is the defect the 2026-08-26 amendment records - an unbounded call sat in IO wait for five minutes), `GenerateContent` with `ResponseMIMEType: "application/json"` and the schema, decode, wrap every failure in `ErrDraftingUnavailable`. Implement on `disabledClient` returning `InferredFields{}, nil`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/aidraft/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/aidraft/
git commit -m "aidraft: InferProfile, bounded per call like every other drafting method"
```

---

### Task 4: Vocabulary loading, re-validation and application

**Files:**
- Create: `internal/book/infer.go`
- Test: `internal/book/infer_test.go`

**Interfaces:**
- Consumes: Task 3's `Drafter.InferProfile`.
- Produces: `InferProfileFields(ctx, pool, drafter, s profile.Stored) (profile.Stored, []InferenceNote, error)` - returns the profile with survivors applied and one note per field, filled or dropped.

- [ ] **Step 1: Write the failing test**

```go
func TestAValueOutsideTheRealVocabularyIsDropped(t *testing.T) {
	pool := testPool(t)
	drafter := stubDrafter{infer: aidraft.InferredFields{
		RegionCulture: &aidraft.InferredValue{
			Value: "Atlantis", GroundedOn: "language_id", Confidence: 0.9},
	}}

	s := profile.Stored{DateOfBirth: someDOB, LanguageID: "bn"}
	out, notes, err := InferProfileFields(context.Background(), pool, drafter, s)
	if err != nil {
		t.Fatalf("InferProfileFields: %v", err)
	}
	if out.RegionCulture != "" {
		t.Fatalf("a region outside the real vocabulary reached the profile: %q", out.RegionCulture)
	}
	if len(notes) == 0 {
		t.Fatal("the drop must be recorded, not silent")
	}
}

func TestAFieldTheDoctorSuppliedIsNeverOverwritten(t *testing.T) {
	pool := testPool(t)
	drafter := stubDrafter{infer: aidraft.InferredFields{
		RegionCulture: &aidraft.InferredValue{
			Value: "South India", GroundedOn: "language_id", Confidence: 0.9},
	}}

	s := profile.Stored{DateOfBirth: someDOB, RegionCulture: "West Bengal / East India"}
	out, _, err := InferProfileFields(context.Background(), pool, drafter, s)
	if err != nil {
		t.Fatalf("InferProfileFields: %v", err)
	}
	if out.RegionCulture != "West Bengal / East India" {
		t.Fatalf("inference overwrote the doctor's own value: %q", out.RegionCulture)
	}
}

func TestAnUngroundedValueIsDropped(t *testing.T) {
	pool := testPool(t)
	drafter := stubDrafter{infer: aidraft.InferredFields{
		BudgetBand: &aidraft.InferredValue{Value: "Low", GroundedOn: "", Confidence: 0.9},
	}}

	s := profile.Stored{DateOfBirth: someDOB}
	out, _, err := InferProfileFields(context.Background(), pool, drafter, s)
	if err != nil {
		t.Fatalf("InferProfileFields: %v", err)
	}
	if out.BudgetBand != "" {
		t.Fatal("a value with nothing behind it is a plausible default, not an inference")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -run TestA.*Vocabulary -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Implement**

`internal/book/infer.go`:
- `loadInferenceVocab(ctx, pool) (map[string][]string, error)` reading `region_focus`, `cuisine_option`, `recipe_master.budget_band` distinct, and the distinct `prep_time_min` / `cook_time_min` value sets. Every one a live query.
- `suppliedFields(s profile.Stored) map[string]string` collecting only the non-empty entries the model may ground on: language, region, cuisine, diet, budget, sex, and the age in months. Never an allergen, never a growth row, never a name.
- `InferProfileFields` builds the request, calls the drafter, and for each returned field applies it **only if** all four hold: the corresponding `Stored` field is empty; `GroundedOn` names a key actually present in `suppliedFields`; `Value` is a member of that field's live vocabulary; and the drafter did not error. Anything failing any of the four is dropped with an `InferenceNote` saying which check failed.
- An `ErrDraftingUnavailable` from the drafter is not an error here: return the profile unchanged with one note. Inference is an enhancement, and a book that generates without it is correct.

- [ ] **Step 4: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/book/infer.go internal/book/infer_test.go
git commit -m "book: apply inferred profile fields only when grounded, in vocabulary, and not already set"
```

---

### Task 5: Run it once per set

**Files:**
- Modify: `internal/book/set.go`
- Test: `internal/book/set_test.go`

**Interfaces:**
- Consumes: Task 4's `InferProfileFields`.
- Produces: `SetResult` (or whatever `AssembleSet` returns) carrying `InferenceNotes []InferenceNote`.

- [ ] **Step 1: Write the failing test**

```go
func TestBothBooksAreBuiltFromTheSameInferredProfile(t *testing.T) {
	// One inference per run, for the same reason there is one asOf per run: two separate
	// inferences could land on different regions and hand a family a Book 1 and a Book 2
	// ranked against different cuisines for the same child.
	pool := testPool(t)
	drafter := countingDrafter{}
	s := profile.Stored{DateOfBirth: someDOB, LanguageID: "bn"}

	_, err := AssembleSet(context.Background(), pool, s, someTime, WithDrafter(&drafter))
	if err != nil {
		t.Fatalf("AssembleSet: %v", err)
	}
	if drafter.inferCalls != 1 {
		t.Fatalf("want exactly one inference call per set, got %d", drafter.inferCalls)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -run TestBothBooksAreBuiltFromTheSameInferredProfile -v`
Expected: FAIL, zero calls.

- [ ] **Step 3: Wire it in**

In `AssembleSet`, immediately after the profile is read and `asOf` is captured, and **before** either assembler runs, call `InferProfileFields` and pass the returned profile to both. Add `InferenceNotes` to the result. The per-book routes (`AssembleBook1` / `AssembleBook2` called directly) do not infer - state that in `set.go`'s doc comment, because a single-book route producing a differently-ranked book than the set route would be a real surprise and the reason must be findable.

- [ ] **Step 4: Run tests**

Run: `TEST_DATABASE_URL=$DATABASE_URL go test ./internal/book/ -v`
Expected: PASS, including any existing test asserting `aidraft.Disabled` produces the book it always did.

- [ ] **Step 5: Commit**

```bash
git add internal/book/set.go internal/book/set_test.go
git commit -m "book: infer once per set so both books rank against the same profile"
```

---

### Task 6: Surface inferred fields to the operator

**Files:**
- Modify: `internal/api/handlers/book_set.go`, `internal/api/handlers/book_generate.go`
- Modify: `web/src/lib/types.ts`, `web/src/lib/api.ts`, `web/src/components/book-generator.tsx`
- Test: `internal/api/handlers/book_generate_test.go`, `web/src/components/book-generator.test.tsx`

**Interfaces:**
- Consumes: Task 5's `InferenceNotes`.
- Produces: `inferred_fields` in the generate response JSON; an operator panel on `/books`.

- [ ] **Step 1: Write the failing tests**

Go side: assert the generate response carries `inferred_fields` and that each entry names the field, the value, and what it was grounded on.

Frontend: assert the panel renders one row per inferred field and does not render at all when the array is empty.

- [ ] **Step 2: Run to verify failure**

Run both suites. Expected: FAIL.

- [ ] **Step 3: Implement**

Serialize the notes in both handlers alongside the existing `profile_omissions` / `book1_omissions` / `book2_omissions`. On the frontend, add an `InferredField` type, read it in `generateBooks`, and render a panel beside the existing omissions panels, following their exact markup so the three read as one family. Show field, value, grounded-on and confidence, monospace, per the frontend rules - provenance is a column, never a footnote.

Nothing about this reaches either book's HTML. Assert that.

- [ ] **Step 4: Run the full suite**

```bash
go build ./... && go vet ./...
TEST_DATABASE_URL=$DATABASE_URL go test ./...
cd web && npx tsc --noEmit && npm test && npm run build
```

- [ ] **Step 5: Generate with a real key and read the result**

Set `GEMINI_API_KEY`, start the server, generate a book supplying only a date of birth and `language_id = bn`. Confirm: a region is inferred and grounded on the language; the books rank Bengali recipes first; the panel names the inference; neither printed book mentions it. Then generate supplying only a date of birth and confirm nothing is inferred at all - there is nothing to ground on, and declining is the correct behaviour.

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers/ web/src/
git commit -m "console: show what was inferred and what it was reasoned from"
```
