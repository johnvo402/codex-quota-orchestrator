package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/domain"
)

// PendingActionDeliverable validates that a pending action is still owned by
// the durable state that created it. This prevents a stale queued action from
// being sent after its project task or managed task has already moved on.
// Stale pending actions are cancelled atomically and are never claimed.
func (s *Store) PendingActionDeliverable(ctx context.Context, actionID int64) (bool, error) {
	if err := s.ensureActionDiagnosticsSchema(ctx); err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var kind, threadID, status string
	if err := tx.QueryRowContext(ctx, `SELECT kind,thread_id,status FROM actions WHERE id=?`, actionID).Scan(&kind, &threadID, &status); err != nil {
		return false, err
	}
	if status != "pending" {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	reason := ""
	switch {
	case isProjectTaskAction(kind):
		itemID := projectTaskIDFromActionKind(kind)
		var stateRaw string
		var linkedAction sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT state,action_id FROM project_tasks WHERE id=?`, itemID).Scan(&stateRaw, &linkedAction)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			reason = "project task no longer exists"
		case err != nil:
			return false, err
		case domain.ProjectTaskState(stateRaw) != domain.ProjectTaskDispatching:
			reason = "project task is no longer DISPATCHING"
		case !linkedAction.Valid || linkedAction.Int64 != actionID:
			reason = "project task is linked to a different Desktop action"
		}
	case kind == "resume":
		var stateRaw string
		err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE thread_id=?`, threadID).Scan(&stateRaw)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			reason = "managed task no longer exists"
		case err != nil:
			return false, err
		case domain.TaskState(stateRaw) != domain.StateResumeQueued:
			reason = "managed task is no longer RESUME_QUEUED"
		}
	case kind == "pause_notice":
		var stateRaw string
		err := tx.QueryRowContext(ctx, `SELECT state FROM tasks WHERE thread_id=?`, threadID).Scan(&stateRaw)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			reason = "managed task no longer exists"
		case err != nil:
			return false, err
		case domain.TaskState(stateRaw) != domain.StatePauseRequested:
			reason = "managed task is no longer PAUSE_REQUESTED"
		}
	}

	if reason == "" {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}

	reason = fmt.Sprintf("pending Desktop action cancelled before delivery: %s", strings.TrimSpace(reason))
	res, err := tx.ExecContext(ctx, `
UPDATE actions
SET status='cancelled', error_text=?, updated_at=?
WHERE id=? AND status='pending'
`, reason, time.Now().UTC().UnixMilli(), actionID)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, err
	} else if n != 1 {
		return false, ErrActionNotPending
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return false, nil
}
