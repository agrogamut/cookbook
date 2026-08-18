package book

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
