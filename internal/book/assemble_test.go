package book

import (
	"context"
	"os"
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
	s := profile.Stored{
		ChildID:     "BOOK-TEST-001",
		DisplayName: "Test",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	b, _, err := AssembleBook1(context.Background(), pool, s, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
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
	s := profile.Stored{
		ChildID:     "BOOK-TEST-002",
		DateOfBirth: time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	_, skipped, err := AssembleBook1(context.Background(), pool, s, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("AssembleBook1: %v", err)
	}
	// 32 blocks exist and blockTemplate maps 6, so a real database must report skips.
	if len(skipped) == 0 {
		t.Fatal("no skipped blocks reported; with 6 of 32 blocks mapped this cannot be right")
	}
}
