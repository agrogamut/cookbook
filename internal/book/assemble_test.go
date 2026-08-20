package book

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
	"github.com/madamgy/recipie/internal/profile"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
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

// storedProfileAged returns a minimal profile.Stored whose derived age is approximately
// months old as of time.Now(). Tests that only need a particular age band, not a particular
// child, share this rather than each hand-rolling a DateOfBirth.
func storedProfileAged(months int) profile.Stored {
	return profile.Stored{
		ChildID:     "BOOK-TEST-AGE",
		DisplayName: "Test",
		DateOfBirth: time.Now().AddDate(0, -months, 0),
	}
}

func TestAgeLabel(t *testing.T) {
	for _, c := range []struct {
		months int
		want   string
	}{
		{8, "8 months"},
		{23, "23 months"},
		{24, "2 years"},
		{51, "4 years 3 months"}, // the prototype's own child, Aarav
	} {
		if got := ageLabel(c.months); got != c.want {
			t.Fatalf("ageLabel(%d) = %q, want %q", c.months, got, c.want)
		}
	}
}

// An unrecorded allergy must read as a recorded negative, not as a blank the reader has to
// interpret. The prototype's wording is the provider's, so it is used verbatim -- but only
// for a child with nothing recorded at all.
//
// The suspected-only row is the regression. A child with a clinician-documented suspected
// peanut allergy got a book reading "No known food allergy reported" while AS-002 correctly
// left peanut recipes in the ranked set, so the page denied the allergy and the book could
// serve it. The only disclosure was an HTTP header that never reaches the printed page.
func TestAllergyStatusNamesEveryRecordedState(t *testing.T) {
	for _, c := range []struct {
		name            string
		confirmed       []string
		suspected       []string
		wantNone        bool
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:     "nothing recorded",
			wantNone: true,
		},
		{
			name:            "confirmed only",
			confirmed:       []string{"Peanut"},
			wantContains:    []string{"Peanut", "Confirmed"},
			wantNotContains: []string{"AS-002"},
		},
		{
			name:         "suspected only",
			suspected:    []string{"Peanut"},
			wantContains: []string{"Peanut", "Suspected", "AS-002", "not filtered out"},
		},
		{
			name:         "both",
			confirmed:    []string{"Milk"},
			suspected:    []string{"Peanut"},
			wantContains: []string{"Milk", "Peanut", "Confirmed", "Suspected", "AS-002"},
		},
		{
			// A resolved allergen never reaches either list: ToChildProfile keeps it out
			// of both and reports it in the omissions instead, which
			// TestResolvedAllergenIsReportedAsAnOmission pins.
			name:     "resolved only",
			wantNone: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := allergyStatus(c.confirmed, c.suspected)
			const none = "No known food allergy reported"
			if c.wantNone {
				if got != none {
					t.Fatalf("allergy status = %q, want %q", got, none)
				}
				return
			}
			if strings.Contains(got, none) {
				t.Fatalf("a recorded allergen must never render as %q, got %q", none, got)
			}
			for _, want := range c.wantContains {
				if !strings.Contains(got, want) {
					t.Fatalf("allergy status %q must contain %q", got, want)
				}
			}
			for _, unwanted := range c.wantNotContains {
				if strings.Contains(got, unwanted) {
					t.Fatalf("allergy status %q must not contain %q", got, unwanted)
				}
			}
		})
	}
}

// The printed page, not just the helper: a suspected allergen must be named on the book's
// own profile page, in both books, because the HTTP omissions header never reaches paper.
func TestSuspectedAllergenReachesBothPrintedBooks(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	asOf := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	s := profile.Stored{
		ChildID:     "BOOK-TEST-SUSPECT",
		DisplayName: "Suspected Allergen Child",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		DietType:    "Vegetarian",
		Allergens: []profile.DeclaredAllergen{
			{Group: "Peanut", Status: "suspected", Source: "clinician"},
		},
	}

	b1, _, err := AssembleBook1(ctx, pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	b2, _, err := AssembleBook2(ctx, pool, s, asOf)
	if err != nil {
		t.Fatalf("AssembleBook2: %v", err)
	}

	for _, c := range []struct {
		book   string
		status string
	}{{"book1", b1.Child.AllergyStatus}, {"book2", b2.Child.AllergyStatus}} {
		if strings.Contains(c.status, "No known food allergy reported") {
			t.Fatalf("%s prints %q for a child with a suspected peanut allergy", c.book, c.status)
		}
		if !strings.Contains(c.status, "Peanut") {
			t.Fatalf("%s must name the suspected group, got %q", c.book, c.status)
		}
	}
	if b1.Child.AllergyStatus != b2.Child.AllergyStatus {
		t.Fatalf("the two books disagree about the same child: %q vs %q",
			b1.Child.AllergyStatus, b2.Child.AllergyStatus)
	}

	// And it reaches the rendered page, not only the model.
	var buf bytes.Buffer
	if err := RenderHTML(&buf, Kind1, b1.Metadata, b1); err != nil {
		t.Fatalf("render book1: %v", err)
	}
	if !strings.Contains(buf.String(), "Peanut") {
		t.Fatal("the suspected group must appear on the rendered Book 1 profile page")
	}
}

// A resolved allergen prints nowhere and is reported everywhere: it excludes nothing, so
// naming it as a current status would be wrong, and dropping it silently would hide a
// recorded clinical fact from the reviewer.
func TestResolvedAllergenIsReportedAsAnOmission(t *testing.T) {
	pool := testPool(t)
	s := profile.Stored{
		ChildID:     "BOOK-TEST-RESOLVED",
		DisplayName: "Resolved Allergen Child",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		Allergens: []profile.DeclaredAllergen{
			{Group: "Egg", Status: "resolved", Source: "clinician"},
		},
	}
	b, skipped, err := AssembleBook1(context.Background(), pool, s,
		time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	if b.Child.AllergyStatus != "No known food allergy reported" {
		t.Fatalf("a resolved allergen excludes nothing and must not print as a current "+
			"status, got %q", b.Child.AllergyStatus)
	}
	found := false
	for _, s := range skipped {
		if strings.Contains(s, "Egg") && strings.Contains(s, "resolved") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a resolved allergen must be named in the omissions, got %v", skipped)
	}
}

// A missing measurement renders as a writing line, never as zero. This is the assembler half
// of that contract: the pointer stays nil rather than becoming "0.0 kg".
func TestMissingGrowthStaysNil(t *testing.T) {
	pool := testPool(t)
	s := storedProfileAged(51)
	b, _, err := AssembleBook1(context.Background(), pool, s, time.Now())
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	if b.Child.WeightKg != nil || b.Child.HeightCm != nil {
		t.Fatalf("a child with no growth measurement must carry nil, got %v / %v",
			b.Child.WeightKg, b.Child.HeightCm)
	}
	if b.Metadata.ReviewStatus == "" {
		t.Fatal("every assembled book must carry the provider's review status")
	}
}

// Blocks with no template mapping must be reported, not silently dropped. A reviewer needs
// to know the book is missing sections.
func TestUnmappedBlocksAreReported(t *testing.T) {
	pool := testPool(t)
	s := storedProfileAged(51)
	_, skipped, err := AssembleBook1(context.Background(), pool, s, time.Now())
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	// 32 blocks exist and blockTemplate maps 6, so a real database must report skips.
	if len(skipped) == 0 {
		t.Fatal("no skipped blocks reported; with 6 of 32 blocks mapped this cannot be right")
	}
}

// Every one of the 32 content blocks is either rendered or named in the skip list. This is
// the honest-gap rule made checkable: a book that quietly contains 30 of 32 blocks looks
// exactly like a book that contains all of them.
//
// Only entries prefixed "block " are counted, because skipped also carries profile-level
// drops from ToChildProfile, which are not blocks.
func TestEveryBlockIsEitherRenderedOrReported(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM book1_content_block`).Scan(&total); err != nil {
		t.Fatalf("count blocks: %v", err)
	}

	// Two ages either side of the corpus, so age exclusion is exercised in both directions.
	for _, months := range []int{7, 51, 200} {
		b, skipped, err := AssembleBook1(ctx, pool, storedProfileAged(months), time.Now())
		if err != nil {
			t.Fatalf("assemble at %d months: %v", months, err)
		}
		blockSkips := 0
		for _, s := range skipped {
			if strings.HasPrefix(s, omissionBlock) {
				blockSkips++
			}
		}
		if got := len(b.Sections) + blockSkips; got != total {
			t.Fatalf("at %d months: %d rendered + %d reported skips = %d, want %d; "+
				"a block that is neither rendered nor reported is silently absent",
				months, len(b.Sections), blockSkips, got, total)
		}
	}
}

// A rendered section must carry content. TestEveryBlockIsEitherRenderedOrReported proves
// every block is rendered or reported; it says nothing about whether a rendered block has
// anything in it, which is exactly how a heading over an empty table shipped once already
// on this branch (D1). This is the other half: for every section AssembleBook1 puts in the
// book, at least one of Rows, Growth or Callout must be non-empty.
//
// B1-END-01 (block B1-022) is the one deliberate exception. Its template reads
// Book1.Metadata directly -- book_version, release_id, generation_date, review_status --
// because those are release-level facts with nowhere else to live, not per-section content,
// so the section it renders under legitimately carries none of the three.
func TestRenderedSectionsAreNotEmpty(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	for _, months := range []int{7, 51, 200} {
		b, _, err := AssembleBook1(ctx, pool, storedProfileAged(months), time.Now())
		if err != nil {
			t.Fatalf("assemble at %d months: %v", months, err)
		}
		for _, sec := range b.Sections {
			if sec.TemplateID == "B1-END-01" {
				continue
			}
			// SectionHasContent rather than a hand-written list of the shapes a section
			// can carry. The hand-written version listed rows, growth and callout, and went
			// on passing when four more content kinds were added -- a test that checks a
			// subset of the ways a page can be empty is a test that stops noticing.
			if !SectionHasContent(sec) {
				t.Fatalf("at %d months: block %s (%s) was rendered carrying nothing -- "+
					"a heading over an empty area, not a book",
					months, sec.BlockID, sec.TemplateID)
			}
		}
	}
}

// The single most important property of this package, and it covers both books because the
// provider's rule is "Condition is a STOP GATE, not a simple recipe filter". A special-care
// condition stops generation, and a book issued in the child's name -- a recipe book, or a
// Book 1 of general-population milestone tables -- is exactly the artifact that would
// override a clinician's judgement if it were produced anyway.
//
// Every condition id is read from special_care_condition_gate rather than hardcoding one:
// the gate covers six conditions, and a test that exercises one of them proves nothing about
// the other five.
func TestBlockedEngineProducesNoBook(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	asOf := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	rows, err := pool.Query(ctx, `SELECT condition_id FROM special_care_condition_gate ORDER BY condition_id`)
	if err != nil {
		t.Fatalf("load special-care condition ids: %v", err)
	}
	var conditionIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan condition id: %v", err)
		}
		conditionIDs = append(conditionIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("condition id rows: %v", err)
	}
	if len(conditionIDs) == 0 {
		t.Fatal("special_care_condition_gate is empty; the stop gate cannot be exercised")
	}

	for _, conditionID := range conditionIDs {
		s := profile.Stored{
			ChildID:     "BOOK-TEST-003",
			DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
			Conditions: []profile.ClinicalCondition{{
				TriggerField: "Special_Care_Condition", FlagValue: conditionID, Class: "congenital",
			}},
		}

		_, _, err := AssembleBook2(ctx, pool, s, asOf)
		if !errors.Is(err, ErrBlocked) {
			t.Fatalf("%s: a blocked result must not become a recipe book, got err = %v",
				conditionID, err)
		}

		b1, _, err := AssembleBook1(ctx, pool, s, asOf)
		if !errors.Is(err, ErrBlocked) {
			t.Fatalf("%s: a blocked result must not become a Book 1 either, got err = %v "+
				"with %d sections", conditionID, err, len(b1.Sections))
		}
		// The provider's own stop text, not a sentence composed here, is what the caller
		// gets to show the operator.
		var reviewer string
		if err := pool.QueryRow(ctx,
			`SELECT coalesce(mandatory_reviewer, '') FROM special_care_condition_gate WHERE condition_id = $1`,
			conditionID).Scan(&reviewer); err != nil {
			t.Fatalf("%s: reviewer lookup: %v", conditionID, err)
		}
		if reviewer != "" && !strings.Contains(err.Error(), reviewer) {
			t.Fatalf("%s: the block reason must quote the provider's mandatory reviewer %q, got %q",
				conditionID, reviewer, err.Error())
		}
	}
}

// An empty chapter is omitted and reported, never rendered as a heading with nothing under
// it. Four of seven categories have no mapped recipes today.
//
// The assertion is the conservation invariant, not just "some skip happened": every one of
// the 7 rows in meal_category_target must be either rendered as a chapter or named in the
// skipped list. A weaker len(skipped) == 0 check already let a real defect ship once in this
// plan, where content rows vanished from both the document and the skip list while the test
// stayed green -- counting only meal-category skip entries and reconciling against the total
// is what catches that class of bug.
func TestEmptyChaptersAreOmittedAndReported(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM meal_category_target`).Scan(&total); err != nil {
		t.Fatalf("count meal categories: %v", err)
	}

	s := profile.Stored{
		ChildID:     "BOOK-TEST-004",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		DietType:    "Vegetarian",
	}
	b, skipped, err := AssembleBook2(ctx, pool, s,
		time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AssembleBook2: %v", err)
	}
	for _, sec := range b.MealSections {
		if len(sec.Recipes) == 0 {
			t.Fatalf("chapter %s rendered with no recipes; it should have been omitted",
				sec.MealCategoryID)
		}
	}

	// Only omissionMealCategory entries are counted, because skipped also carries
	// profile-level drops from ToChildProfile, which are not meal categories.
	categorySkips := 0
	for _, sk := range skipped {
		if strings.HasPrefix(sk, omissionMealCategory) {
			categorySkips++
		}
	}
	if got := len(b.MealSections) + categorySkips; got != total {
		t.Fatalf("%d rendered + %d reported skips = %d, want %d (all rows of "+
			"meal_category_target); a category that is neither rendered nor reported is "+
			"silently absent", len(b.MealSections), categorySkips, got, total)
	}
}

// The property under test: for the ids handed to loadRecipeCards, every id ends up either as
// a rendered card or named in the skip slice, never neither. recipe_method_card joins
// recipe_master 1:1 across all 940 recipes today, so a genuine join miss cannot be produced by
// asking AssembleBook2 for a real child -- it is fabricated directly here by asking for one
// real recipe id alongside one that does not exist in recipe_method_card, exercising the exact
// path that is otherwise unreachable on current data. This is the same defect class that
// already shipped once on this branch for Book 1 content, where a dropped row left the
// rendered count and the skip list both short and the test stayed green.
func TestLoadRecipeCardsReportsAJoinMiss(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var realID string
	if err := pool.QueryRow(ctx, `SELECT recipe_id FROM recipe_method_card LIMIT 1`).Scan(&realID); err != nil {
		t.Fatalf("find a real recipe id: %v", err)
	}
	const missingID = "MG-R-99999"

	ids := []string{realID, missingID}
	cards, skipped, err := loadRecipeCards(ctx, pool, ids, "MC-TEST", "v-test",
		map[string]bool{}, map[string][]string{}, map[string]string{}, map[string]models.RankedRecipe{},
		models.ChildProfile{}, models.EngineResult{})
	if err != nil {
		t.Fatalf("loadRecipeCards: %v", err)
	}

	if len(cards)+len(skipped) != len(ids) {
		t.Fatalf("%d ids requested, %d cards + %d skips = %d; every id must be either "+
			"rendered or reported, never neither",
			len(ids), len(cards), len(skipped), len(cards)+len(skipped))
	}
	if len(cards) != 1 || cards[0].RecipeID != realID {
		t.Fatalf("the real id must still be rendered, got %+v", cards)
	}
	found := false
	for _, s := range skipped {
		if strings.Contains(s, missingID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("recipe id %s has no method card row; it must be named in the skip list, got %v",
			missingID, skipped)
	}
}

// TestOnlyTheZScoreBlockIsUnmapped is the assertion conservation accounting cannot make.
//
// "Rendered plus reported equals total" held at seven-plus-twenty-five exactly as well as it
// holds at twenty-nine-plus-three, which is how this book shipped nearly empty with a green
// suite: twenty-four blocks reported "no template mapping" while the provider's content for
// them sat in four tables nothing queried. Counting cannot tell those two states apart. This
// names which block may be missing, and B1-004 is the only answer.
//
// B1-004 is growth trend interpretation. Its declared input is a z-score/percentile engine,
// which means computing against the WHO reference tables this project does not carry, and a
// trend stated without them is a clinical finding with no source. If it ever appears in a
// book, that happened here and not in a clinic.
func TestOnlyTheZScoreBlockIsUnmapped(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Fifty-one months: inside the declared age range of every block except the adolescent
	// one, and the prototype's own child.
	_, omissions, err := AssembleBook1(ctx, pool, storedProfileAged(51), time.Now())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	var unmapped []string
	for _, o := range omissions {
		if strings.Contains(o, "no template mapping") {
			unmapped = append(unmapped, o)
		}
	}
	if len(unmapped) != 1 {
		t.Fatalf("want exactly one unmapped block, got %d:\n  %s",
			len(unmapped), strings.Join(unmapped, "\n  "))
	}
	if !strings.Contains(unmapped[0], "B1-004") {
		t.Errorf("the unmapped block is not B1-004: %s", unmapped[0])
	}
}

// TestBookOneIsNotThin holds the floor this whole change exists to raise.
//
// A count, and a weak assertion on its own -- twenty-five sections carrying one row each
// would pass it. It is paired with TestRenderedSectionsAreNotEmpty, which holds every one of
// those sections to carrying something, and with reading the printed pages, which is the only
// check that sees a page whose content is present, correct and useless.
func TestBookOneIsNotThin(t *testing.T) {
	pool := testPool(t)
	b, _, err := AssembleBook1(context.Background(), pool, storedProfileAged(51), time.Now())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(b.Sections) < 25 {
		t.Errorf("a four-year-old's Book 1 has %d sections; the provider supplies content "+
			"for at least 25 at this age", len(b.Sections))
	}
}

// TestTheFourFeedingBlocksDoNotPrintTheSamePage: B1-005 to B1-008 share one template, and the
// first version of that template ignored which block it was rendering -- so a book carried the
// same twenty-row feeding table four times in a row. Every row was the provider's own text and
// every test passed; it still read as padding, which is what a reader calls filler regardless
// of where the sentences came from.
func TestTheFourFeedingBlocksDoNotPrintTheSamePage(t *testing.T) {
	pool := testPool(t)
	// Thirty months, not the prototype's four-year-old: B1-008 declares 6-36 months, so at
	// fifty-one it is correctly age-excluded and only three of the four render.
	b, _, err := AssembleBook1(context.Background(), pool, storedProfileAged(30), time.Now())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	facets := map[string]string{}
	for _, sec := range b.Sections {
		if sec.TemplateID != "B1-STAGE-01" {
			continue
		}
		if sec.Stage == nil || sec.Stage.Facet == "" {
			t.Fatalf("%s renders B1-STAGE-01 with no facet, so it prints the whole stage "+
				"table like its three neighbours", sec.BlockID)
		}
		if other, dup := facets[sec.Stage.Facet]; dup {
			t.Errorf("%s and %s both render the %q facet", other, sec.BlockID, sec.Stage.Facet)
		}
		facets[sec.Stage.Facet] = sec.BlockID
	}
	if len(facets) != 4 {
		t.Errorf("want four distinct feeding facets, got %d: %v", len(facets), facets)
	}
}

// TestTheTwoMilestoneBlocksDoNotPrintTheSamePage is the same defect on the other shared
// template. B1-011 (surveillance, 0-72 months) and B1-014 (detailed comparison, 0-216) both
// map to B1-DEV-01 and both called milestoneRows, so one book printed the identical
// forty-row table twice.
func TestTheTwoMilestoneBlocksDoNotPrintTheSamePage(t *testing.T) {
	pool := testPool(t)
	b, _, err := AssembleBook1(context.Background(), pool, storedProfileAged(51), time.Now())
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}

	var surveillance, detailed []Row
	for _, sec := range b.Sections {
		switch sec.BlockID {
		case "B1-011":
			surveillance = sec.Rows
		case "B1-014":
			detailed = sec.Rows
		}
	}
	if len(surveillance) == 0 || len(detailed) == 0 {
		t.Fatalf("want both milestone blocks rendered, got B1-011=%d rows B1-014=%d rows",
			len(surveillance), len(detailed))
	}
	if len(surveillance) >= len(detailed) {
		t.Errorf("B1-011 (current checkpoint) has %d rows and B1-014 (every checkpoint to "+
			"date) has %d. Surveillance is the checkpoint the child is at, not the whole "+
			"history, so it must be the shorter of the two.", len(surveillance), len(detailed))
	}
}

// TestBook1SectionsFlowRatherThanTakingASheetEach.
//
// Book 1 broke before every one of its thirty-one blocks, so a block holding three writing lines
// took a whole sheet. Breaking once per provider part was tried next and measured better; letting
// the sections flow and breaking only for the four full-page forms measured better again. The
// three counts are in markSheetStarts.
func TestBook1SectionsFlowRatherThanTakingASheetEach(t *testing.T) {
	pool := testPool(t)
	b, _, err := AssembleBook1(context.Background(), pool, storedProfileAged(53), time.Now())
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	if len(b.Sections) < 10 {
		t.Fatalf("only %d sections rendered; this guard would prove little", len(b.Sections))
	}

	starts := 0
	for _, s := range b.Sections {
		if s.StartsPart {
			starts++
		}
	}
	// One for the first section, plus one per full-page form, and nothing else.
	want := 1
	for _, s := range b.Sections[1:] {
		if MustStartASheet(s.BlockID) {
			want++
		}
	}
	if starts != want {
		t.Errorf("%d of %d sections start a sheet, want %d (the first, plus the full-page forms)",
			starts, len(b.Sections), want)
	}
	if !b.Sections[0].StartsPart {
		t.Error("the first section must start a sheet; the contents page precedes it")
	}
}

// TestFullPageFormsStillStartASheet holds the four exceptions in pagepolicy.go. Each is a form a
// parent fills in over weeks, where a break landing mid-form costs the column headings on one of
// the two halves.
func TestFullPageFormsStillStartASheet(t *testing.T) {
	pool := testPool(t)
	b, _, err := AssembleBook1(context.Background(), pool, storedProfileAged(53), time.Now())
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	found := 0
	for _, s := range b.Sections {
		if !MustStartASheet(s.BlockID) {
			continue
		}
		found++
		if !s.StartsPart {
			t.Errorf("%s is a declared full-page form and must start a sheet", s.BlockID)
		}
	}
	if found == 0 {
		t.Fatal("no declared full-page form rendered; the guard proved nothing")
	}
}

// A section that is neither the first nor a full-page form never forces a break, whatever part it
// sits in. A unit test, because it is a statement about the rule rather than about one child.
func TestOnlyTheFirstSectionAndFullPageFormsBreak(t *testing.T) {
	sections := []Section{
		{BlockID: "B1-002", Part: "A"},
		{BlockID: "B1-003", Part: "B"},
		{BlockID: "B1-019", Part: "L"}, // a declared full-page form
		{BlockID: "B1-021", Part: "N"},
	}
	markSheetStarts(sections)
	for i, want := range []bool{true, false, true, false} {
		if sections[i].StartsPart != want {
			t.Errorf("section %d (%s): StartsPart = %v, want %v",
				i, sections[i].BlockID, sections[i].StartsPart, want)
		}
	}
}
