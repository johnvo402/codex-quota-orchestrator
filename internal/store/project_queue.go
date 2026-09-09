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

const projectTaskActionPrefix = "project_task/"

func (s *Store) ensureProjectQueueSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS project_tasks(
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL,
 objective TEXT NOT NULL,
 details TEXT NOT NULL DEFAULT '',
 position INTEGER NOT NULL,
 state TEXT NOT NULL,
 target_thread_id TEXT NOT NULL DEFAULT '',
 action_id INTEGER,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 started_at INTEGER,
 completed_at INTEGER,
 FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE,
 FOREIGN KEY(action_id) REFERENCES actions(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_project_tasks_queue
ON project_tasks(project_id,state,position,id);
CREATE INDEX IF NOT EXISTS idx_project_tasks_thread
ON project_tasks(target_thread_id,state);

CREATE TABLE IF NOT EXISTS project_queue_settings(
 project_id TEXT PRIMARY KEY,
 mode TEXT NOT NULL DEFAULT 'AUTO' CHECK(mode IN ('AUTO','MANUAL','PAUSED')),
 updated_at INTEGER NOT NULL,
 FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
);
`)
	return err
}

func (s *Store) CreateProjectTask(ctx context.Context, projectID, objective, details string) (domain.ProjectTask, error) {
	objective = strings.TrimSpace(objective)
	details = strings.TrimSpace(details)
	if objective == "" {
		return domain.ProjectTask{}, errors.New("objective is required")
	}
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return domain.ProjectTask{}, err
	}
	if err := s.NormalizeProjectQueue(ctx, projectID); err != nil {
		return domain.ProjectTask{}, err
	}
	var pos int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0)+1 FROM project_tasks WHERE project_id=? AND state='QUEUED'`, projectID).Scan(&pos); err != nil {
		return domain.ProjectTask{}, err
	}
	now := time.Now().UTC().UnixMilli()
	id := newID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_tasks(id,project_id,objective,details,position,state,created_at,updated_at) VALUES(?,?,?,?,?,'QUEUED',?,?)`, id, projectID, objective, details, pos, now, now)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	return s.GetProjectTask(ctx, id)
}

func (s *Store) GetProjectTask(ctx context.Context, id string) (domain.ProjectTask, error) {
	return scanProjectTask(s.db.QueryRowContext(ctx, `SELECT id,project_id,objective,details,position,state,target_thread_id,action_id,created_at,updated_at,started_at,completed_at FROM project_tasks WHERE id=?`, id))
}

func (s *Store) ListProjectTasks(ctx context.Context, projectID string) ([]domain.ProjectTask, error) {
	if err := s.NormalizeProjectQueue(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,objective,details,position,state,target_thread_id,action_id,created_at,updated_at,started_at,completed_at FROM project_tasks WHERE project_id=? ORDER BY CASE state WHEN 'RUNNING' THEN 0 WHEN 'DISPATCHING' THEN 1 WHEN 'DISPATCHED' THEN 2 WHEN 'NEEDS_REVIEW' THEN 3 WHEN 'QUEUED' THEN 4 ELSE 5 END, position, created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProjectTask{}
	for rows.Next() {
		v, err := scanProjectTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProjectTask(ctx context.Context, id, objective, details string, position *int64) (domain.ProjectTask, error) {
	v, err := s.GetProjectTask(ctx, id)
	if err != nil {
		return v, err
	}
	if v.State != domain.ProjectTaskQueued {
		return v, fmt.Errorf("only QUEUED project tasks can be edited")
	}
	if strings.TrimSpace(objective) != "" {
		v.Objective = strings.TrimSpace(objective)
	}
	v.Details = strings.TrimSpace(details)
	_, err = s.db.ExecContext(ctx, `UPDATE project_tasks SET objective=?,details=?,updated_at=? WHERE id=?`, v.Objective, v.Details, time.Now().UTC().UnixMilli(), id)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if position != nil {
		if err := s.MoveQueuedProjectTask(ctx, id, *position); err != nil {
			return domain.ProjectTask{}, err
		}
	}
	return s.GetProjectTask(ctx, id)
}

func (s *Store) DeleteProjectTask(ctx context.Context, id string) error {
	v, err := s.GetProjectTask(ctx, id)
	if err != nil {
		return err
	}
	if v.State != domain.ProjectTaskQueued && v.State != domain.ProjectTaskCancelled && v.State != domain.ProjectTaskCompleted {
		return fmt.Errorf("project task in state %s cannot be deleted", v.State)
	}
	if v.State != domain.ProjectTaskQueued {
		_, err = s.db.ExecContext(ctx, `DELETE FROM project_tasks WHERE id=?`, id)
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_tasks WHERE id=? AND state='QUEUED'`, id); err != nil {
		return err
	}
	if err := normalizeQueuedProjectTasksTx(ctx, tx, v.ProjectID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelProjectTask(ctx context.Context, id string) (domain.ProjectTask, error) {
	v, err := s.GetProjectTask(ctx, id)
	if err != nil {
		return v, err
	}
	if v.State != domain.ProjectTaskQueued && v.State != domain.ProjectTaskNeedsReview {
		return v, fmt.Errorf("project task in state %s cannot be cancelled", v.State)
	}
	now := time.Now().UTC().UnixMilli()
	if v.State == domain.ProjectTaskNeedsReview {
		_, err = s.db.ExecContext(ctx, `UPDATE project_tasks SET state='CANCELLED',updated_at=?,completed_at=? WHERE id=?`, now, now, id)
		if err != nil {
			return domain.ProjectTask{}, err
		}
		return s.GetProjectTask(ctx, id)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE project_tasks SET state='CANCELLED',updated_at=?,completed_at=? WHERE id=? AND state='QUEUED'`, now, now, id); err != nil {
		return domain.ProjectTask{}, err
	}
	if err := normalizeQueuedProjectTasksTx(ctx, tx, v.ProjectID); err != nil {
		return domain.ProjectTask{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ProjectTask{}, err
	}
	return s.GetProjectTask(ctx, id)
}

func (s *Store) ProjectHasBlockingTask(ctx context.Context, projectID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM project_tasks WHERE project_id=? AND state IN ('DISPATCHING','DISPATCHED','RUNNING','NEEDS_REVIEW')`, projectID).Scan(&n)
	return n > 0, err
}

func (s *Store) ProjectHasActiveManagedTask(ctx context.Context, projectID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM tasks t
JOIN task_metadata m ON m.task_id=t.id
WHERE m.project_id=? AND t.state IN ('RUNNING','PAUSE_REQUESTED','PAUSED_QUOTA','RESUME_QUEUED','NEEDS_REVIEW')
`, projectID).Scan(&n)
	return n > 0, err
}

func (s *Store) NextQueuedProjectTask(ctx context.Context, projectID string) (domain.ProjectTask, error) {
	return scanProjectTask(s.db.QueryRowContext(ctx, `SELECT id,project_id,objective,details,position,state,target_thread_id,action_id,created_at,updated_at,started_at,completed_at FROM project_tasks WHERE project_id=? AND state='QUEUED' ORDER BY position,created_at LIMIT 1`, projectID))
}

func (s *Store) ProjectDispatchThread(ctx context.Context, projectID string) (string, error) {
	var threadID, stateRaw string
	err := s.db.QueryRowContext(ctx, `
SELECT t.thread_id,t.state
FROM tasks t
JOIN task_metadata m ON m.task_id=t.id
WHERE m.project_id=?
ORDER BY t.updated_at DESC
LIMIT 1
`, projectID).Scan(&threadID, &stateRaw)
	if err != nil {
		return "", err
	}
	if domain.TaskState(stateRaw) != domain.StateCompleted {
		return "", fmt.Errorf("latest project task is %s; queue waits for COMPLETED", stateRaw)
	}
	return threadID, nil
}

func (s *Store) QueueProjectTaskDispatch(ctx context.Context, itemID, threadID, message string) (Action, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Action{}, err
	}
	defer tx.Rollback()
	var stateRaw, projectID string
	if err := tx.QueryRowContext(ctx, `SELECT state,project_id FROM project_tasks WHERE id=?`, itemID).Scan(&stateRaw, &projectID); err != nil {
		return Action{}, err
	}
	if domain.ProjectTaskState(stateRaw) != domain.ProjectTaskQueued {
		return Action{}, fmt.Errorf("project task %s is %s, not QUEUED", itemID, stateRaw)
	}
	now := time.Now().UTC().UnixMilli()
	res, err := tx.ExecContext(ctx, `INSERT INTO actions(kind,thread_id,message,status,created_at,updated_at) VALUES(?,?,?,'pending',?,?)`, projectTaskActionPrefix+itemID, threadID, message, now, now)
	if err != nil {
		return Action{}, err
	}
	actionID, err := res.LastInsertId()
	if err != nil {
		return Action{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_tasks SET state='DISPATCHING',target_thread_id=?,action_id=?,updated_at=? WHERE id=?`, threadID, actionID, now, itemID); err != nil {
		return Action{}, err
	}
	if err := normalizeQueuedProjectTasksTx(ctx, tx, projectID); err != nil {
		return Action{}, err
	}
	if err := tx.Commit(); err != nil {
		return Action{}, err
	}
	return Action{ID: actionID, Kind: projectTaskActionPrefix + itemID, ThreadID: threadID, Message: message, CreatedAt: time.UnixMilli(now)}, nil
}

func projectTaskIDFromActionKind(kind string) string {
	return strings.TrimPrefix(kind, projectTaskActionPrefix)
}

func isProjectTaskAction(kind string) bool {
	return strings.HasPrefix(kind, projectTaskActionPrefix)
}

func (s *Store) CompleteRunningProjectTaskByThread(ctx context.Context, threadID string) error {
	now := time.Now().UTC().UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE project_tasks SET state='COMPLETED',updated_at=?,completed_at=? WHERE target_thread_id=? AND state='RUNNING'`, now, now, threadID)
	return err
}

func scanProjectTask(s scanner) (domain.ProjectTask, error) {
	var v domain.ProjectTask
	var state string
	var actionID, startedAt, completedAt sql.NullInt64
	var createdAt, updatedAt int64
	if err := s.Scan(&v.ID, &v.ProjectID, &v.Objective, &v.Details, &v.Position, &state, &v.TargetThreadID, &actionID, &createdAt, &updatedAt, &startedAt, &completedAt); err != nil {
		return v, err
	}
	v.State = domain.ProjectTaskState(state)
	v.CreatedAt = time.UnixMilli(createdAt)
	v.UpdatedAt = time.UnixMilli(updatedAt)
	if actionID.Valid {
		n := actionID.Int64
		v.ActionID = &n
	}
	if startedAt.Valid {
		t := time.UnixMilli(startedAt.Int64)
		v.StartedAt = &t
	}
	if completedAt.Valid {
		t := time.UnixMilli(completedAt.Int64)
		v.CompletedAt = &t
	}
	return v, nil
}
