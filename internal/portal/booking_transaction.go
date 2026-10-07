package portal

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Scheduling and payment transitions share a lock because expiry can release
// another registration's slot. The database constraints remain the final guard.
func (s *Server) beginBookingTx(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(72641,43)`); err != nil {
		tx.Rollback(ctx)
		return nil, fmt.Errorf("lock booking transitions: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE app_private.appointment SET status='expired',updated_at=now()
		WHERE status='awaiting_payment' AND hold_expires_at<=now()`); err != nil {
		tx.Rollback(ctx)
		return nil, fmt.Errorf("expire booking holds: %w", err)
	}
	return tx, nil
}

func (s *Server) expireHolds(ctx context.Context) error {
	tx, err := s.beginBookingTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	return tx.Commit(ctx)
}

func guardianAllows(ctx context.Context, registrationID string) bool {
	g := CurrentGuardian(ctx)
	return g.RegistrationID == "" || g.RegistrationID == registrationID
}
