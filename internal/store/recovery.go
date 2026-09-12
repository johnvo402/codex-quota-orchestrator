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

var ErrActionNotPending = errors.New("action is not pending")

func (s *Store) QueueResumeSafe(ctx context.Context, threadID, message string) (Action, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Action{}, err
	}
	defer tx.Rollback()

	var taskID, stateRaw string
	if err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &stateRaw); err != nil {
		return Action{}, err
	}
	state := domain.TaskState(stateRaw)

	if state == domain.StateResumeQueued {
		var a Action
		var createdAt int64
		err := tx.QueryRowContext(ctx, `
SELECT id,kind,thread_id,message,created_at
FROM actions
WHERE kind='resume'
  AND thread_id=?
  AND status IN ('pending','delivering')
ORDER BY id DESC
LIMIT 1
`, threadID).Scan(&a.ID, &a.Kind, &a.ThreadID, &a.Message, &createdAt)
		if err == nil {
			a.CreatedAt = time.UnixMilli(createdAt)
			if err := tx.Commit(); err != nil {
				return Action{}, err
			}
			return a, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Action{}, err
		}
	}

	if state != domain.StatePausedQuota && state != domain.StateResumeQueued {
		return Action{}, fmt.Errorf("cannot queue resume for task in state %s", state)
	}

	now := time.Now().UTC().UnixMilli()
	result, err := tx.ExecContext(ctx, `
INSERT INTO actions(kind,thread_id,message,status,created_at,updated_at)
VALUES('resume',?,?,'pending',?,?)
`, threadID, message, now, now)
	if err != nil {
		return Action{}, err
	}
	actionID, err := result.LastInsertId()
	if err != nil {
		return Action{}, err
	}

	if state == domain.StatePausedQuota {
		if err := domain.ValidateTransition(state, domain.StateResumeQueued); err != nil {
			return Action{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,pause_reason=?,updated_at=? WHERE thread_id=?`, domain.StateResumeQueued, "quota recovered", now, threadID); err != nil {
			return Action{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, state, domain.StateResumeQueued, "quota recovered; resume delivery queued", now); err != nil {
			return Action{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Action{}, err
	}
	return Action{ID: actionID, Kind: "resume", ThreadID: threadID, Message: message, CreatedAt: time.UnixMilli(now)}, nil
}

func (s *Store) ClaimAction(ctx context.Context, actionID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var kind, threadID, message, status string
	if err := tx.QueryRowContext(ctx, `SELECT kind,thread_id,message,status FROM actions WHERE id=?`, actionID).Scan(&kind, &threadID, &message, &status); err != nil {
		return err
	}
	if status != "pending" {
		return ErrActionNotPending
	}

	now := time.Now().UTC().UnixMilli()
	if kind == ActionKindStop {
		if err := validateStopClaimTx(ctx, tx, threadID, message); err != nil {
			// The Stop request never crossed the external side-effect boundary, so
			// a stale request can be invalidated safely instead of becoming review.
			if _, updateErr := tx.ExecContext(ctx, `UPDATE actions SET status='cancelled',updated_at=? WHERE id=? AND status='pending'`, now, actionID); updateErr != nil {
				return updateErr
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return commitErr
			}
			return err
		}
	}

	result, err := tx.ExecContext(ctx, `
UPDATE actions
SET status='delivering', updated_at=?
WHERE id=? AND status='pending'
`, now, actionID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrActionNotPending
	}
	return tx.Commit()
}

func (s *Store) CompleteClaimedAction(ctx context.Context, actionID int64, success bool, errorText string) error {
	if err := s.ensureActionDiagnosticsSchema(ctx); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var kind, threadID, status string
	if err := tx.QueryRowContext(ctx, `SELECT kind,thread_id,status FROM actions WHERE id=?`, actionID).Scan(&kind, &threadID, &status); err != nil {
		return err
	}
	if status == "done" || status == "failed" {
		return tx.Commit()
	}
	if status != "delivering" {
		return fmt.Errorf("cannot complete action %d from status %s", actionID, status)
	}

	newStatus := "done"
	if !success {
		newStatus = "failed"
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE actions SET status=?,error_text=?,updated_at=? WHERE id=?`, newStatus, strings.TrimSpace(errorText), now, actionID); err != nil {
		return err
	}

	if kind == ActionKindStop {
		if err := completeStopActionTx(ctx, tx, actionID, threadID, success, errorText, now); err != nil {
			return err
		}
		return tx.Commit()
	}

	if isProjectTaskAction(kind) {
		if err := completeProjectTaskActionTx(ctx, tx, kind, threadID, success, errorText, now); err != nil {
			return err
		}
		return tx.Commit()
	}

	if kind != "resume" {
		return tx.Commit()
	}

	var taskID, stateRaw string
	if err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &stateRaw); err != nil {
		return err
	}
	from := domain.TaskState(stateRaw)
	if from != domain.StateResumeQueued {
		return tx.Commit()
	}

	target := domain.StateRunning
	reason := "resume message delivered to Desktop thread"
	pauseReason := ""
	if !success {
		target = domain.StatePausedQuota
		pauseReason = "resume_delivery_failed"
		reason = "resume delivery failed"
		if strings.TrimSpace(errorText) != "" {
			reason += ": " + errorText
		}
	}
	if err := domain.ValidateTransition(from, target); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,pause_reason=?,updated_at=? WHERE thread_id=?`, target, pauseReason, now, threadID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, from, target, reason, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkActionUncertain(ctx context.Context, actionID int64, reason string) error {
	if err := s.ensureActionDiagnosticsSchema(ctx); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var kind, threadID, status string
	if err := tx.QueryRowContext(ctx, `SELECT kind,thread_id,status FROM actions WHERE id=?`, actionID).Scan(&kind, &threadID, &status); err != nil {
		return err
	}
	if status == "uncertain" || status == "done" || status == "failed" {
		return tx.Commit()
	}
	if status != "delivering" {
		return fmt.Errorf("cannot mark action %d uncertain from status %s", actionID, status)
	}

	diagnosticReason := strings.TrimSpace(reason)
	if diagnosticReason == "" {
		diagnosticReason = "delivery outcome is uncertain"
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE actions SET status='uncertain',error_text=?,updated_at=? WHERE id=?`, diagnosticReason, now, actionID); err != nil {
		return err
	}

	if kind == ActionKindStop {
		if err := markStopActionUncertainTx(ctx, tx, actionID, threadID, reason, now); err != nil {
			return err
		}
		return tx.Commit()
	}

	if isProjectTaskAction(kind) {
		if err := markProjectTaskActionUncertainTx(ctx, tx, kind, reason, now); err != nil {
			return err
		}
		return tx.Commit()
	}

	if kind != "resume" {
		return tx.Commit()
	}

	var taskID, stateRaw string
	if err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &stateRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tx.Commit()
		}
		return err
	}
	from := domain.TaskState(stateRaw)
	if from != domain.StateResumeQueued {
		return tx.Commit()
	}
	if err := domain.ValidateTransition(from, domain.StateNeedsReview); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		reason = "resume delivery outcome is uncertain"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,pause_reason=?,updated_at=? WHERE thread_id=?`, domain.StateNeedsReview, reason, now, threadID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, from, domain.StateNeedsReview, reason, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecoverStaleDeliveries(ctx context.Context, olderThan time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id
FROM actions
WHERE status='delivering' AND updated_at<=?
ORDER BY id
`, olderThan.UTC().UnixMilli())
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	recovered := 0
	for _, id := range ids {
		if err := s.MarkActionUncertain(ctx, id, "delivery worker stopped before the Desktop action outcome was confirmed"); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}
