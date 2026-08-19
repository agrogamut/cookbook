package book

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/profile"
)

// A run produces two books, both populated. The requirement this pins is a product decision
// -- a child's books are handed over together -- so a run that returns one book and an empty
// shell for the other has failed even though nothing errored.
func TestASetRunProducesBothBooks(t *testing.T) {
	pool := testPool(t)
	asOf := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	s := profile.Stored{
		ChildID:       "BOOK-TEST-SET",
		DisplayName:   "Set Test Child",
		DateOfBirth:   time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		RegionCulture: "West Bengal / East India",
		DietType:      "Vegetarian",
	}

	set, err := AssembleSet(context.Background(), pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleSet: %v", err)
	}

	if len(set.Book1.Sections) == 0 {
		t.Fatal("book 1 has no sections; a run must produce a populated book 1")
	}
	if len(set.Book2.MealSections) == 0 {
		t.Fatal("book 2 has no meal sections; a run must produce a populated book 2")
	}
	if set.ChildID != s.ChildID {
		t.Fatalf("set child id = %q, want %q", set.ChildID, s.ChildID)
	}
}

// Both books must describe the same child at the same instant. Assembled separately they can
// straddle a birthday or a profile edit, and the family is handed a Book 1 and a Book 2 that
// state different ages -- which is the defect the set exists to make impossible.
func TestASetDescribesOneChildAtOneInstant(t *testing.T) {
	pool := testPool(t)
	asOf := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	// A date of birth that lands the child one day short of a birthday, so an asOf that
	// slipped by a day between the two assemblies would change the printed age.
	s := profile.Stored{
		ChildID:     "BOOK-TEST-SET-ASOF",
		DisplayName: "Boundary Child",
		DateOfBirth: time.Date(2022, 8, 20, 0, 0, 0, 0, time.UTC),
	}

	set, err := AssembleSet(context.Background(), pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleSet: %v", err)
	}

	if set.Book1.Child.AgeMonths != set.Book2.Child.AgeMonths {
		t.Fatalf("book 1 says %d months and book 2 says %d; both books must describe the "+
			"same child at the same instant",
			set.Book1.Child.AgeMonths, set.Book2.Child.AgeMonths)
	}
	if !set.Book1.Metadata.GenerationDate.Equal(set.Book2.Metadata.GenerationDate) {
		t.Fatalf("generation dates differ: book1 %s, book2 %s",
			set.Book1.Metadata.GenerationDate, set.Book2.Metadata.GenerationDate)
	}
	if !set.AsOf.Equal(asOf) {
		t.Fatalf("set as_of = %s, want the asOf it was given (%s)", set.AsOf, asOf)
	}
}

// The stop gate stops every artifact issued in the child's name. A set is all-or-nothing:
// there is no partial run handing over the daily-life book while the recipe book is withheld,
// which would read as though the clinician's stop applied only to food.
func TestASetIsBlockedWholeOrNotAtAll(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	asOf := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	rows, err := pool.Query(ctx, `SELECT condition_id FROM special_care_condition_gate`)
	if err != nil {
		t.Fatalf("load condition ids: %v", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan condition id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("condition id rows: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("special_care_condition_gate is empty; the stop gate cannot be exercised")
	}

	for _, id := range ids {
		s := profile.Stored{
			ChildID:     "BOOK-TEST-SET-BLOCKED",
			DisplayName: "Blocked Child",
			DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
			Conditions: []profile.ClinicalCondition{{
				TriggerField: "Special_Care_Condition", FlagValue: id, Class: "chronic",
			}},
		}

		set, err := AssembleSet(ctx, pool, s, asOf)
		if err == nil {
			t.Fatalf("%s: a special-care child must get no set, got one with %d book-1 "+
				"sections and %d book-2 chapters",
				id, len(set.Book1.Sections), len(set.Book2.MealSections))
		}
		if !strings.Contains(err.Error(), ErrBlocked.Error()) {
			t.Fatalf("%s: a stop must be ErrBlocked, got %v", id, err)
		}
		if len(set.Book1.Sections) != 0 || len(set.Book2.MealSections) != 0 {
			t.Fatalf("%s: a blocked run must return no book content", id)
		}
	}
}

// An omission both books reported is a fact about the child's stored profile, not about
// either book. Listing it twice leaves an operator deciding whether a repeated line is one
// finding or two, so the set reports it once.
func TestSharedOmissionsAreReportedOnceAsProfileFacts(t *testing.T) {
	pool := testPool(t)
	asOf := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	// A suspected allergen is reported by both assemblers, because it comes from
	// ToChildProfile rather than from either book's own corpus coverage.
	s := profile.Stored{
		ChildID:     "BOOK-TEST-SET-OMIT",
		DisplayName: "Suspected Allergen Child",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		Allergens: []profile.DeclaredAllergen{{
			Group: "Peanut", Status: "suspected", Source: "parent_reported",
		}},
	}

	set, err := AssembleSet(context.Background(), pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleSet: %v", err)
	}

	var suspected string
	for _, o := range set.ProfileOmissions {
		if strings.Contains(o, "Peanut") {
			suspected = o
		}
	}
	if suspected == "" {
		t.Fatalf("the suspected allergen must be a profile omission, got %v",
			set.ProfileOmissions)
	}
	if slices.Contains(set.Book1Omissions, suspected) ||
		slices.Contains(set.Book2Omissions, suspected) {
		t.Fatal("a profile omission must not also be listed against a book")
	}

	// The partition must not lose anything: every omission either book reported is still
	// reachable from the set.
	for name, reported := range map[string][]string{
		"book1": set.Book1Omissions, "book2": set.Book2Omissions,
	} {
		for _, o := range reported {
			if slices.Contains(set.ProfileOmissions, o) {
				t.Fatalf("%s omission %q is also listed as a profile omission", name, o)
			}
		}
	}
}

// The partition is pure list logic and is worth pinning without a database: a message in both
// lists is shared by construction, and everything else stays with the book that reported it.
func TestPartitionOmissions(t *testing.T) {
	shared, only1, only2 := partitionOmissions(
		[]string{"profile drop", "[block] B1-002 unmapped"},
		[]string{"profile drop", "[meal category] MC-04 empty"},
	)

	if !slices.Equal(shared, []string{"profile drop"}) {
		t.Fatalf("shared = %v", shared)
	}
	if !slices.Equal(only1, []string{"[block] B1-002 unmapped"}) {
		t.Fatalf("only1 = %v", only1)
	}
	if !slices.Equal(only2, []string{"[meal category] MC-04 empty"}) {
		t.Fatalf("only2 = %v", only2)
	}

	// Never nil, so a client renders the lists without a null check.
	for name, got := range map[string][]string{"shared": shared, "only1": only1, "only2": only2} {
		if got == nil {
			t.Fatalf("%s must be an empty slice, not nil", name)
		}
	}
	s, o1, o2 := partitionOmissions(nil, nil)
	if s == nil || o1 == nil || o2 == nil {
		t.Fatal("empty input must still produce empty slices, not nil")
	}
}

// The child's own recorded facts must reach the page they are recorded for. Date of birth,
// sex and language are stored on child_profile and named by B1-001's own
// personalization_inputs, and each was stored but unprinted -- a book that knows a child's
// birth date and prints only a derived age gives a parent no way to check the book is about
// their child.
func TestIdentityFactsReachTheProfilePage(t *testing.T) {
	pool := testPool(t)
	asOf := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	s := profile.Stored{
		ChildID:     "BOOK-TEST-IDENTITY",
		DisplayName: "Identity Child",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		Sex:         "female",
		LanguageID:  "bn",
	}

	b, _, err := AssembleBook1(context.Background(), pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}

	var out strings.Builder
	if err := RenderHTML(&out, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	page := out.String()

	for label, want := range map[string]string{
		"date of birth": "2022-05-01",
		"sex":           "female",
		"language":      "bn",
		"name":          "Identity Child",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the profile page must print the child's %s (%q)", label, want)
		}
	}
}

// A dated growth record must print as the dates and values a clinician entered, and a
// measurement not taken at a visit must print as a line to write on -- never a zero, which
// reads as a measured value of nothing.
func TestGrowthTablePrintsWhatWasMeasuredAndOnlyThat(t *testing.T) {
	pool := testPool(t)
	asOf := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	weight, height, head := 14.2, 96.5, 49.1
	z := -0.4
	s := profile.Stored{
		ChildID:     "BOOK-TEST-GROWTH",
		DisplayName: "Growth Child",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		Growth: []profile.GrowthMeasurement{
			// Newest first, the order profile.Load returns.
			{
				MeasuredOn: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				WeightKg:   &weight, HeightCm: &height,
				WeightForAgeZ: &z, Interpretation: "tracking along the same centile",
				MeasuredBy: "Dr Sen",
			},
			{
				// An earlier visit that recorded head circumference but no weight: the
				// weight cell must be a writing line, not a zero.
				MeasuredOn:          time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
				HeadCircumferenceCm: &head,
			},
		},
	}

	b, _, err := AssembleBook1(context.Background(), pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}

	var growth *Section
	for i := range b.Sections {
		if b.Sections[i].TemplateID == "B1-GROWTH-01" {
			growth = &b.Sections[i]
		}
	}
	if growth == nil {
		t.Fatal("a child with recorded measurements must get the growth monitoring block")
	}
	if len(growth.Growth) != 2 {
		t.Fatalf("growth table has %d rows, want 2", len(growth.Growth))
	}

	// Oldest first: a monitoring table reads as a trend down the page.
	if growth.Growth[0].MeasuredOn != "2026-01-05" {
		t.Fatalf("first row is %s, want the oldest visit", growth.Growth[0].MeasuredOn)
	}
	if growth.Growth[0].WeightKg != "" {
		t.Fatalf("a visit that recorded no weight must leave it empty, got %q",
			growth.Growth[0].WeightKg)
	}
	// Nothing is carried forward from the later visit to fill the gap.
	if strings.Contains(growth.Growth[0].HeadCircumCm, "14.2") {
		t.Fatal("a measurement must never be carried between visits")
	}
	if growth.Growth[1].ZScores != "weight-for-age -0.4" {
		t.Fatalf("recorded z-score = %q", growth.Growth[1].ZScores)
	}
	// No z-score is computed: the earlier visit recorded none and must show none.
	if growth.Growth[0].ZScores != "" {
		t.Fatalf("an unrecorded z-score must stay empty, got %q", growth.Growth[0].ZScores)
	}

	var out strings.Builder
	if err := RenderHTML(&out, Kind1, b.Metadata, b); err != nil {
		t.Fatalf("render: %v", err)
	}
	page := out.String()
	for _, want := range []string{"2026-01-05", "2026-07-01", "14.2 kg", "49.1 cm", "Dr Sen"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the printed growth table must contain %q", want)
		}
	}
	if strings.Contains(page, "0.0 kg") {
		t.Fatal("an unrecorded measurement must never print as zero")
	}
}

// Both books print from one browser. Launching Chromium is most of the cost of a print, and
// paying it twice is what pushed a set past the request budget in production while every
// local test passed -- startup is a fraction of a second on a developer's machine and about
// twenty on a small instance.
func TestPrintPDFAllPrintsEveryDocument(t *testing.T) {
	if !BrowserAvailable() {
		t.Skip("no chromium on PATH")
	}
	meta := Metadata{
		Title: "Set", BookVersion: "V1", ReviewStatus: "Draft",
		GenerationDate: time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC), Language: "en",
	}

	out, err := PrintPDFAll(context.Background(), []PrintJob{
		{Name: "book1", HTML: []byte("<html><body><h1>Book one</h1></body></html>"), Metadata: meta},
		{Name: "book2", HTML: []byte("<html><body><h1>Book two</h1></body></html>"), Metadata: meta},
	})
	if err != nil {
		t.Fatalf("PrintPDFAll: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d documents, want 2", len(out))
	}
	for i, pdf := range out {
		if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Fatalf("document %d is not a PDF: %.16q", i, pdf)
		}
	}
	// Two different documents, so a batch cannot quietly print the first one twice.
	if bytes.Equal(out[0], out[1]) {
		t.Fatal("both documents are byte-identical; each job must print its own HTML")
	}
}

// An empty batch is not an error and must not launch a browser.
func TestPrintPDFAllOnNoDocuments(t *testing.T) {
	out, err := PrintPDFAll(context.Background(), nil)
	if err != nil || out != nil {
		t.Fatalf("empty batch: got %v, %v; want nil, nil", out, err)
	}
}
