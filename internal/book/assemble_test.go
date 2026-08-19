package book

import (
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
// interpret. The prototype's wording is the provider's, so it is used verbatim.
func TestAllergyStatusNamesTheEmptyCase(t *testing.T) {
	if got := allergyStatus(nil); got != "No known food allergy reported" {
		t.Fatalf("empty allergy status = %q", got)
	}
	if got := allergyStatus([]string{"Peanut"}); got == "No known food allergy reported" {
		t.Fatalf("a declared allergen must not render as none: %q", got)
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
			if strings.HasPrefix(s, "block ") {
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
// book, at least one of Rows, Cards or Callout must be non-empty.
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
			if len(sec.Rows) == 0 && len(sec.Cards) == 0 && sec.Callout == nil {
				t.Fatalf("at %d months: block %s (%s) was rendered with no rows, no cards "+
					"and no callout -- a heading over an empty table, not a book",
					months, sec.BlockID, sec.TemplateID)
			}
		}
	}
}

// The single most important property of this package. A special-care condition stops the
// engine, and a recipe book is exactly the artifact that would override a clinician's
// judgement if it were produced anyway.
func TestBlockedEngineProducesNoBook(t *testing.T) {
	pool := testPool(t)
	s := profile.Stored{
		ChildID:     "BOOK-TEST-003",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
		Conditions: []profile.ClinicalCondition{{
			TriggerField: "Special_Care_Condition", FlagValue: "SC-CP", Class: "congenital",
		}},
	}
	_, _, err := AssembleBook2(context.Background(), pool, s,
		time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("a blocked engine result must not become a book, got err = %v", err)
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

	// Only entries prefixed "meal category " are counted, because skipped also carries
	// profile-level drops from ToChildProfile, which are not meal categories.
	categorySkips := 0
	for _, sk := range skipped {
		if strings.HasPrefix(sk, "meal category ") {
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
