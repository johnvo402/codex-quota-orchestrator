package store

import (
	"context"
	"database/sql"
	"errors"
)

// QueueResume is kept for compatibility with older callers/tests. New code
// should use QueueResumeSafe directly.
func (s *Store) QueueResume(ctx context.Context, threadID, message string) (Action, error) {
	return s.QueueResumeSafe(ctx, threadID, message)
}

// CompleteAction preserves the pre-claim API while delegating to the crash-safe
// action lifecycle. Pending actions are claimed first; already-delivering
// actions can be completed directly. Terminal actions remain idempotent.
func (s *Store) CompleteAction(ctx context.Context, actionID int64, success bool, errorText string) error {
	var status string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM actions WHERE id=?`, actionID).Scan(&status); err != nil {
		return err
	}

	switch status {
	case "pending":
		if err := s.ClaimAction(ctx, actionID); err != nil && !errors.Is(err, ErrActionNotPending) {
			return err
		}
	case "delivering":
		// Already claimed by a compatibility caller; complete below.
	case "done", "failed", "uncertain":
		return nil
	default:
		return sql.ErrNoRows
	}

	return s.CompleteClaimedAction(ctx, actionID, success, errorText)
}
