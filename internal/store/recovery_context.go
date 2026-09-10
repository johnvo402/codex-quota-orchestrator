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

// ActionInfo is the non-payload action metadata exposed to recovery surfaces.
// Message bodies stay private to the delivery pipeline because recovery only
// needs identity, kind, status, destination thread, and timestamps.
type ActionInfo struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	ThreadID  string    `json:"threadId"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Store) GetActionInfo(ctx context.Context, id int64) (ActionInfo, error) {
	return scanActionInfo(s.db.QueryRowContext(ctx, `
SELECT id,kind,thread_id,status,created_at,updated_at
FROM actions
WHERE id=?
`, id))
}

func (s *Store) LatestActionInfo(ctx context.Context, threadID, kind string) (ActionInfo, error) {
	threadID = strings.TrimSpace(threadID)
	kind = strings.TrimSpace(kind)
	if threadID == "" {
		return ActionInfo{}, errors.New("thread id is required")
	}
	query := `
SELECT id,kind,thread_id,status,created_at,updated_at
FROM actions
WHERE thread_id=?`
	args := []any{threadID}
	if kind != "" {
		query += ` AND kind=?`
		args = append(args, kind)
	}
	query += ` ORDER BY id DESC LIMIT 1`
	return scanActionInfo(s.db.QueryRowContext(ctx, query, args...))
}

func scanActionInfo(s scanner) (ActionInfo, error) {
	var v ActionInfo
	var createdAt, updatedAt int64
	if err := s.Scan(&v.ID, &v.Kind, &v.ThreadID, &v.Status, &createdAt, &updatedAt); err != nil {
		return v, err
	}
	v.CreatedAt = time.UnixMilli(createdAt)
	v.UpdatedAt = time.UnixMilli(updatedAt)
	return v, nil
}

func (s *Store) ListProjectTasksNeedingReview(ctx context.Context) ([]domain.ProjectTask, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id,project_id,objective,details,position,state,target_thread_id,action_id,created_at,updated_at,started_at,completed_at
FROM project_tasks
WHERE state='NEEDS_REVIEW'
ORDER BY updated_at DESC,id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProjectTask{}
	for rows.Next() {
		item, err := scanProjectTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ProjectHasOtherBlockingTask(ctx context.Context, projectID, excludedItemID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM project_tasks
WHERE project_id=?
  AND id<>?
  AND state IN ('DISPATCHING','DISPATCHED','RUNNING','NEEDS_REVIEW')
`, projectID, excludedItemID).Scan(&n)
	return n > 0, err
}

// RetryProjectTaskDispatch records a fresh delivery attempt after the user has
// explicitly confirmed that the previous uncertain attempt did not arrive.
// The previous action remains UNCERTAIN as immutable history.
func (s *Store) RetryProjectTaskDispatch(ctx context.Context, itemID, threadID, message string) (Action, error) {
	itemID = strings.TrimSpace(itemID)
	threadID = strings.TrimSpace(threadID)
	if itemID == "" || threadID == "" {
		return Action{}, errors.New("project task id and thread id are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Action{}, err
	}
	defer tx.Rollback()

	var stateRaw string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM project_tasks WHERE id=?`, itemID).Scan(&stateRaw); err != nil {
		return Action{}, err
	}
	if domain.ProjectTaskState(stateRaw) != domain.ProjectTaskNeedsReview {
		return Action{}, fmt.Errorf("project task %s is %s, not NEEDS_REVIEW", itemID, stateRaw)
	}

	now := time.Now().UTC().UnixMilli()
	res, err := tx.ExecContext(ctx, `
INSERT INTO actions(kind,thread_id,message,status,created_at,updated_at)
VALUES(?,?,?,'pending',?,?)
`, projectTaskActionPrefix+itemID, threadID, message, now, now)
	if err != nil {
		return Action{}, err
	}
	actionID, err := res.LastInsertId()
	if err != nil {
		return Action{}, err
	}
	res, err = tx.ExecContext(ctx, `
UPDATE project_tasks
SET state='DISPATCHING',target_thread_id=?,action_id=?,updated_at=?
WHERE id=? AND state='NEEDS_REVIEW'
`, threadID, actionID, now, itemID)
	if err != nil {
		return Action{}, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return Action{}, err
		}
		return Action{}, errors.New("project task changed while retrying delivery")
	}
	if err := tx.Commit(); err != nil {
		return Action{}, err
	}
	return Action{ID: actionID, Kind: projectTaskActionPrefix + itemID, ThreadID: threadID, Message: message, CreatedAt: time.UnixMilli(now)}, nil
}

// ConfirmProjectTaskRunning resolves an uncertain project-task delivery after
// the user verifies that the message did arrive in Codex Desktop. It activates
// the queue item and managed Desktop task in one transaction.
func (s *Store) ConfirmProjectTaskRunning(ctx context.Context, itemID string) (domain.ProjectTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	defer tx.Rollback()

	var stateRaw, objective, threadID string
	if err := tx.QueryRowContext(ctx, `
SELECT state,objective,target_thread_id
FROM project_tasks
WHERE id=?
`, itemID).Scan(&stateRaw, &objective, &threadID); err != nil {
		return domain.ProjectTask{}, err
	}
	if domain.ProjectTaskState(stateRaw) != domain.ProjectTaskNeedsReview {
		return domain.ProjectTask{}, fmt.Errorf("project task is %s, not NEEDS_REVIEW", stateRaw)
	}
	if strings.TrimSpace(threadID) == "" {
		return domain.ProjectTask{}, errors.New("project task has no destination Desktop thread")
	}

	var taskID, managedStateRaw string
	if err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &managedStateRaw); err != nil {
		return domain.ProjectTask{}, err
	}
	managedState := domain.TaskState(managedStateRaw)
	now := time.Now().UTC().UnixMilli()
	switch managedState {
	case domain.StateCompleted, domain.StateFailed, domain.StateCancelled:
		if _, err := tx.ExecContext(ctx, `
UPDATE tasks
SET state=?,objective=?,pause_reason='',checkpoint='',pending='',last_test='',updated_at=?
WHERE thread_id=?
`, domain.StateRunning, objective, now, threadID); err != nil {
			return domain.ProjectTask{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO task_events(task_id,from_state,to_state,reason,created_at)
VALUES(?,?,?,?,?)
`, taskID, managedState, domain.StateRunning, "manual recovery: confirmed queued project task is already running", now); err != nil {
			return domain.ProjectTask{}, err
		}
	case domain.StateRunning:
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET objective=?,updated_at=? WHERE thread_id=?`, objective, now, threadID); err != nil {
			return domain.ProjectTask{}, err
		}
	default:
		return domain.ProjectTask{}, fmt.Errorf("managed Desktop task is %s; cannot confirm queued work as running", managedState)
	}

	res, err := tx.ExecContext(ctx, `
UPDATE project_tasks
SET state='RUNNING',updated_at=?,started_at=COALESCE(started_at,?)
WHERE id=? AND state='NEEDS_REVIEW'
`, now, now, itemID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return domain.ProjectTask{}, err
		}
		return domain.ProjectTask{}, errors.New("project task changed while confirming running state")
	}
	if err := tx.Commit(); err != nil {
		return domain.ProjectTask{}, err
	}
	return s.GetProjectTask(ctx, itemID)
}

var _ = sql.ErrNoRows
