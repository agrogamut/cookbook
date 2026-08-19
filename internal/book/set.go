package book

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/profile"
)

// Set is one generation run: both of a child's books, assembled from one stored profile at
// one instant.
//
// A run produces two books because a child's books are two halves of one deliverable -- the
// daily-life companion and the recipe book are handed over together, and a family holding
// one without the other has an incomplete answer. Generating them as a set rather than as
// two independent requests is also the only way they can be guaranteed to agree: two
// separate calls can straddle a birthday, a growth measurement or an allergy edit, and hand
// a family a Book 1 and a Book 2 that state different ages for the same child. AsOf is
// captured once here and passed to both assemblers for exactly that reason.
type Set struct {
	ChildID string    `json:"child_id"`
	AsOf    time.Time `json:"as_of"`
	Book1   Book1     `json:"book1"`
	Book2   Book2     `json:"book2"`

	// ProfileOmissions are the drops both books reported, which makes them facts about the
	// child's stored profile rather than about either book -- a suspected allergen, an
	// acute condition past its expiry. They are listed once here instead of twice, so an
	// operator reading the omissions is not left deciding whether a repeated line is one
	// finding or two.
	ProfileOmissions []string `json:"profile_omissions"`
	Book1Omissions   []string `json:"book1_omissions"`
	Book2Omissions   []string `json:"book2_omissions"`
}

// AssembleSet builds both books for one child.
//
// It returns ErrBlocked if either assembler does. The stop gate stops every artifact issued
// in the child's name, so a set is all-or-nothing: there is no partial run that hands over
// the daily-life book while the recipe book is withheld, which would read as though the
// clinician's stop applied only to food.
//
// Errors name which book failed. The two assemblers read different tables, so an operator
// chasing a failure needs to know which half of the run produced it.
func AssembleSet(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time) (Set, error) {
	b1, dropped1, err := AssembleBook1(ctx, pool, s, asOf)
	if err != nil {
		return Set{}, fmt.Errorf("book1: %w", err)
	}

	b2, dropped2, err := AssembleBook2(ctx, pool, s, asOf)
	if err != nil {
		return Set{}, fmt.Errorf("book2: %w", err)
	}

	shared, only1, only2 := partitionOmissions(dropped1, dropped2)

	return Set{
		ChildID:          s.ChildID,
		AsOf:             asOf,
		Book1:            b1,
		Book2:            b2,
		ProfileOmissions: shared,
		Book1Omissions:   only1,
		Book2Omissions:   only2,
	}, nil
}

// partitionOmissions splits two books' omission lists into the ones both reported and the
// ones each reported alone.
//
// A message in both lists is a profile-level drop by construction: block omissions carry
// omissionBlock, meal-category omissions carry omissionMealCategory, and neither assembler
// can emit the other's. The only text the two share is what ToChildProfile reported to both.
//
// Order is preserved from the first list, then the second, so an operator reading the set's
// omissions sees them in the order the assemblers produced them rather than sorted into a
// shape that hides which stage found what.
func partitionOmissions(dropped1, dropped2 []string) (shared, only1, only2 []string) {
	shared, only1, only2 = []string{}, []string{}, []string{}

	for _, d := range dropped1 {
		if slices.Contains(dropped2, d) {
			if !slices.Contains(shared, d) {
				shared = append(shared, d)
			}
			continue
		}
		only1 = append(only1, d)
	}
	for _, d := range dropped2 {
		if slices.Contains(dropped1, d) {
			continue
		}
		only2 = append(only2, d)
	}
	return shared, only1, only2
}
