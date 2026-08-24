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
