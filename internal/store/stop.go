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

const ActionKindStop = "stop"

// QueueStopSafe records a request to invoke Codex Desktop's real Stop control.
// The action message stores the expected turn id so claim/completion can fail
// closed if the Desktop thread has moved on to newer work.
func (s *Store) QueueStopSafe(ctx context.Context, threadID string) (Action, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return Action{}, errors.New("thread id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Action{}, err
	}
	defer tx.Rollback()

	var stateRaw, turnID string
	if err := tx.QueryRowContext(ctx, `SELECT state,turn_id FROM tasks WHERE thread_id=?`, threadID).Scan(&stateRaw, &turnID); err != nil {
		return Action{}, err
	}
	state := domain.TaskState(stateRaw)
	turnID = strings.TrimSpace(turnID)
	if state != domain.StateRunning {
		return Action{}, fmt.Errorf("Desktop Stop is only available for RUNNING tasks; task is %s", state)
	}
	if turnID == "" {
		return Action{}, errors.New("Desktop Stop requires a current turn id")
	}

	var existing Action
	var existingStatus, existingExpectedTurn string
	var createdAt int64
	err = tx.QueryRowContext(ctx, `
SELECT id,kind,thread_id,message,status,created_at
FROM actions
WHERE kind=? AND thread_id=? AND status IN ('pending','delivering')
ORDER BY id DESC
LIMIT 1
`, ActionKindStop, threadID).Scan(&existing.ID, &existing.Kind, &existing.ThreadID, &existingExpectedTurn, &existingStatus, &createdAt)
	if err == nil {
		existing.Message = existingExpectedTurn
		existing.CreatedAt = time.UnixMilli(createdAt)
		if strings.TrimSpace(existingExpectedTurn) == turnID {
			if err := tx.Commit(); err != nil {
				return Action{}, err
			}
			return existing, nil
		}
		if existingStatus == "delivering" {
			return Action{}, errors.New("an older Desktop Stop is already delivering; manual review is required before stopping a newer turn")
		}
		// A never-claimed stop for an older turn is safe to invalidate.
		if _, err := tx.ExecContext(ctx, `UPDATE actions SET status='cancelled',updated_at=? WHERE id=? AND status='pending'`, time.Now().UTC().UnixMilli(), existing.ID); err != nil {
			return Action{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Action{}, err
	}

	now := time.Now().UTC().UnixMilli()
	res, err := tx.ExecContext(ctx, `
INSERT INTO actions(kind,thread_id,message,status,created_at,updated_at)
VALUES(?,?,?,'pending',?,?)
`, ActionKindStop, threadID, turnID, now, now)
	if err != nil {
		return Action{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Action{}, err
	}
	if err := tx.Commit(); err != nil {
		return Action{}, err
	}
	return Action{ID: id, Kind: ActionKindStop, ThreadID: threadID, Message: turnID, CreatedAt: time.UnixMilli(now)}, nil
}

// validateStopClaimTx runs inside the same transaction that moves a stop action
// from pending to delivering. This closes the claim-vs-new-turn race at the
// durable state boundary.
func validateStopClaimTx(ctx context.Context, tx *sql.Tx, threadID, expectedTurnID string) error {
	var stateRaw, currentTurnID string
	if err := tx.QueryRowContext(ctx, `SELECT state,turn_id FROM tasks WHERE thread_id=?`, threadID).Scan(&stateRaw, &currentTurnID); err != nil {
		return err
	}
	if domain.TaskState(stateRaw) != domain.StateRunning {
		return fmt.Errorf("%w: Desktop task is %s", ErrActionNotPending, stateRaw)
	}
	if strings.TrimSpace(currentTurnID) == "" || strings.TrimSpace(currentTurnID) != strings.TrimSpace(expectedTurnID) {
		return fmt.Errorf("%w: Desktop turn changed before Stop could be claimed", ErrActionNotPending)
	}
	return nil
}

func completeStopActionTx(ctx context.Context, tx *sql.Tx, actionID int64, threadID string, success bool, errorText string, now int64) error {
	if !success {
		// The UI layer confirmed no Stop click was invoked. Keep the managed task
		// RUNNING so the user can retry after inspecting Desktop.
		return nil
	}

	var expectedTurnID string
	if err := tx.QueryRowContext(ctx, `SELECT message FROM actions WHERE id=?`, actionID).Scan(&expectedTurnID); err != nil {
		return err
	}
	var taskID, stateRaw, currentTurnID string
	if err := tx.QueryRowContext(ctx, `SELECT id,state,turn_id FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &stateRaw, &currentTurnID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	state := domain.TaskState(stateRaw)
	if isManagedTerminal(state) {
		return nil
	}
	if strings.TrimSpace(currentTurnID) != strings.TrimSpace(expectedTurnID) {
		reason := "Desktop Stop was invoked but the tracked turn changed before acknowledgement; inspect the current Desktop thread"
		if err := moveTaskToNeedsReviewTx(ctx, tx, taskID, threadID, state, reason, now); err != nil {
			return err
		}
		return markRunningProjectTasksReviewTx(ctx, tx, threadID, now)
	}

	if !domain.CanTransition(state, domain.StateCancelled) {
		return fmt.Errorf("cannot record Desktop Stop from managed task state %s", state)
	}
	reason := "Codex Desktop Stop invoked from dashboard"
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,pause_reason=?,updated_at=? WHERE thread_id=?`, domain.StateCancelled, reason, now, threadID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, state, domain.StateCancelled, reason, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE project_tasks
SET state='CANCELLED',updated_at=?,completed_at=?
WHERE target_thread_id=? AND state='RUNNING'
`, now, now, threadID); err != nil {
		return err
	}
	return nil
}

func markStopActionUncertainTx(ctx context.Context, tx *sql.Tx, actionID int64, threadID, reason string, now int64) error {
	if strings.TrimSpace(reason) == "" {
		reason = "Codex Desktop Stop outcome is uncertain"
	}
	var taskID, stateRaw string
	if err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &stateRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	state := domain.TaskState(stateRaw)
	if !isManagedTerminal(state) && state != domain.StateNeedsReview {
		if err := moveTaskToNeedsReviewTx(ctx, tx, taskID, threadID, state, reason, now); err != nil {
			return err
		}
	}
	return markRunningProjectTasksReviewTx(ctx, tx, threadID, now)
}

func moveTaskToNeedsReviewTx(ctx context.Context, tx *sql.Tx, taskID, threadID string, from domain.TaskState, reason string, now int64) error {
	if from == domain.StateNeedsReview {
		_, err := tx.ExecContext(ctx, `UPDATE tasks SET pause_reason=?,updated_at=? WHERE thread_id=?`, reason, now, threadID)
		return err
	}
	if !domain.CanTransition(from, domain.StateNeedsReview) {
		return fmt.Errorf("cannot move managed task %s to NEEDS_REVIEW after Desktop Stop", from)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,pause_reason=?,updated_at=? WHERE thread_id=?`, domain.StateNeedsReview, reason, now, threadID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, from, domain.StateNeedsReview, reason, now)
	return err
}

func markRunningProjectTasksReviewTx(ctx context.Context, tx *sql.Tx, threadID string, now int64) error {
	_, err := tx.ExecContext(ctx, `
UPDATE project_tasks
SET state='NEEDS_REVIEW',updated_at=?
WHERE target_thread_id=? AND state='RUNNING'
`, now, threadID)
	return err
}

func isManagedTerminal(state domain.TaskState) bool {
	return state == domain.StateCompleted || state == domain.StateFailed || state == domain.StateCancelled
}
