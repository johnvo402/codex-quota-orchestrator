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

// ProjectTaskTarget validates that targetTaskID is a managed Desktop task that
// belongs to projectID. A managed task is unique per Desktop thread, so binding
// a queue item to its thread is also a stable binding to that task.
func (s *Store) ProjectTaskTarget(ctx context.Context, projectID, targetTaskID string) (domain.Task, error) {
	projectID = strings.TrimSpace(projectID)
	targetTaskID = strings.TrimSpace(targetTaskID)
	if projectID == "" {
		return domain.Task{}, errors.New("project id is required")
	}
	if targetTaskID == "" {
		return domain.Task{}, errors.New("target task is required")
	}

	t, err := s.GetTask(ctx, targetTaskID)
	if err != nil {
		return domain.Task{}, err
	}
	var linkedProject sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM task_metadata WHERE task_id=?`, targetTaskID).Scan(&linkedProject); err != nil {
		return domain.Task{}, err
	}
	if !linkedProject.Valid || strings.TrimSpace(linkedProject.String) != projectID {
		return domain.Task{}, errors.New("target task does not belong to this project")
	}
	if strings.TrimSpace(t.ThreadID) == "" {
		return domain.Task{}, errors.New("target task has no Desktop thread")
	}
	return t, nil
}

// CreateProjectTaskForTask creates a queued continuation that is permanently
// aimed at the selected managed task's Desktop thread. The prompt is not sent
// until that selected task reaches COMPLETED.
func (s *Store) CreateProjectTaskForTask(ctx context.Context, projectID, targetTaskID, objective, details string) (domain.ProjectTask, error) {
	target, err := s.ProjectTaskTarget(ctx, projectID, targetTaskID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	item, err := s.CreateProjectTask(ctx, projectID, objective, details)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	now := time.Now().UTC().UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE project_tasks SET target_thread_id=?,updated_at=? WHERE id=? AND state='QUEUED'`, target.ThreadID, now, item.ID)
	if err != nil {
		_ = s.DeleteProjectTask(ctx, item.ID)
		return domain.ProjectTask{}, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		_ = s.DeleteProjectTask(ctx, item.ID)
		if err != nil {
			return domain.ProjectTask{}, err
		}
		return domain.ProjectTask{}, errors.New("queue changed while binding target task")
	}
	return s.GetProjectTask(ctx, item.ID)
}

// SetQueuedProjectTaskTarget changes the selected continuation task while the
// queue item is still QUEUED. This also repairs legacy queue items that were
// created before explicit task selection existed.
func (s *Store) SetQueuedProjectTaskTarget(ctx context.Context, itemID, targetTaskID string) (domain.ProjectTask, error) {
	item, err := s.GetProjectTask(ctx, itemID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if item.State != domain.ProjectTaskQueued {
		return domain.ProjectTask{}, fmt.Errorf("only QUEUED project tasks can change target task")
	}
	target, err := s.ProjectTaskTarget(ctx, item.ProjectID, targetTaskID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	now := time.Now().UTC().UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE project_tasks SET target_thread_id=?,updated_at=? WHERE id=? AND state='QUEUED'`, target.ThreadID, now, itemID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return domain.ProjectTask{}, err
		}
		return domain.ProjectTask{}, errors.New("queue changed while updating target task")
	}
	return s.GetProjectTask(ctx, itemID)
}

// ProjectTaskDispatchThread returns the exact Desktop thread selected when this
// queue item was created/edited. It never falls back to the latest project task.
// Dispatch is eligible only after that selected managed task is COMPLETED.
func (s *Store) ProjectTaskDispatchThread(ctx context.Context, itemID string) (string, error) {
	item, err := s.GetProjectTask(ctx, itemID)
	if err != nil {
		return "", err
	}
	threadID := strings.TrimSpace(item.TargetThreadID)
	if threadID == "" {
		return "", errors.New("queued task has no selected target task; edit it and choose a task to continue")
	}

	t, err := s.GetByThread(ctx, threadID)
	if err != nil {
		return "", fmt.Errorf("selected target task is unavailable: %w", err)
	}
	var linkedProject sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM task_metadata WHERE task_id=?`, t.ID).Scan(&linkedProject); err != nil {
		return "", err
	}
	if !linkedProject.Valid || strings.TrimSpace(linkedProject.String) != item.ProjectID {
		return "", errors.New("selected target task no longer belongs to this project")
	}
	if t.State != domain.StateCompleted {
		return "", fmt.Errorf("selected target task is %s; queue waits for COMPLETED", t.State)
	}
	return threadID, nil
}
