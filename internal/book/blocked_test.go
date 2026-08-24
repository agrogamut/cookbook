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
