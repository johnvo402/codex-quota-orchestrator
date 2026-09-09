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

func completeProjectTaskActionTx(ctx context.Context, tx *sql.Tx, kind, threadID string, success bool, errorText string, now int64) error {
	itemID := projectTaskIDFromActionKind(kind)
	var stateRaw, objective string
	if err := tx.QueryRowContext(ctx, `SELECT state,objective FROM project_tasks WHERE id=?`, itemID).Scan(&stateRaw, &objective); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if domain.ProjectTaskState(stateRaw) != domain.ProjectTaskDispatching {
		return nil
	}
	if !success {
		reason := strings.TrimSpace(errorText)
		if reason == "" {
			reason = "Desktop delivery failed"
		}
		_, err := tx.ExecContext(ctx, `UPDATE project_tasks SET state='NEEDS_REVIEW',details=CASE WHEN details='' THEN ? ELSE details || char(10) || ? END,updated_at=? WHERE id=?`, reason, reason, now, itemID)
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE project_tasks SET state='RUNNING',target_thread_id=?,updated_at=?,started_at=COALESCE(started_at,?) WHERE id=?`, threadID, now, now, itemID); err != nil {
		return err
	}

	var taskID, taskState string
	if err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &taskState); err != nil {
		return err
	}
	from := domain.TaskState(taskState)
	switch from {
	case domain.StateCompleted, domain.StateFailed, domain.StateCancelled:
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,objective=?,pause_reason='',checkpoint='',pending='',last_test='',updated_at=? WHERE thread_id=?`, domain.StateRunning, objective, now, threadID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, from, domain.StateRunning, "project queue dispatched next task", now); err != nil {
			return err
		}
	case domain.StateRunning:
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET objective=?,updated_at=? WHERE thread_id=?`, objective, now, threadID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("cannot activate queued project task on managed task in state %s", from)
	}
	return nil
}

func markProjectTaskActionUncertainTx(ctx context.Context, tx *sql.Tx, kind string, reason string, now int64) error {
	itemID := projectTaskIDFromActionKind(kind)
	if strings.TrimSpace(reason) == "" {
		reason = "Desktop dispatch outcome is uncertain"
	}
	_, err := tx.ExecContext(ctx, `
UPDATE project_tasks
SET state='NEEDS_REVIEW',
    details=CASE WHEN details='' THEN ? ELSE details || char(10) || ? END,
    updated_at=?
WHERE id=? AND state='DISPATCHING'
`, reason, reason, now, itemID)
	return err
}

func projectTaskDispatchMessage(item domain.ProjectTask) string {
	text := "[Codex Task Queue] Start the next queued project task now.\n\nObjective: " + item.Objective
	if strings.TrimSpace(item.Details) != "" {
		text += "\n\nDetails:\n" + item.Details
	}
	text += "\n\nTreat this as a new managed task in the same project. Call desktop_quota_guard.desktop_task_register with this objective and the current workspace, then execute the work normally. When finished, call desktop_quota_guard.task_complete."
	return text
}

func projectTaskNow() int64 { return time.Now().UTC().UnixMilli() }
