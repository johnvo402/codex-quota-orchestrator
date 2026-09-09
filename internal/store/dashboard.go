package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/domain"
)

type DashboardTask struct {
	domain.Task
	ProjectID   string `json:"projectId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Archived    bool   `json:"archived"`
}

type TaskFilter struct {
	ProjectID string
	State     string
	Query     string
	Archived  *bool
}

func (s *Store) ensureDashboardSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS projects(
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 path TEXT NOT NULL DEFAULT '',
 description TEXT NOT NULL DEFAULT '',
 archived INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_path_nonempty
ON projects(path) WHERE path <> '';
CREATE INDEX IF NOT EXISTS idx_projects_archived_name
ON projects(archived,name);

CREATE TABLE IF NOT EXISTS task_metadata(
 task_id TEXT PRIMARY KEY,
 project_id TEXT,
 notes TEXT NOT NULL DEFAULT '',
 archived INTEGER NOT NULL DEFAULT 0,
 updated_at INTEGER NOT NULL,
 FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE,
 FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_task_metadata_project
ON task_metadata(project_id,archived);
`)
	return err
}

func normalizeWorkspacePath(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return filepath.Clean(v)
}

func defaultProjectName(path string) string {
	name := strings.TrimSpace(filepath.Base(path))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "Workspace"
	}
	return name
}

func (s *Store) EnsureProjectForWorkspace(ctx context.Context, taskID, workspace string) (domain.Project, error) {
	workspace = normalizeWorkspacePath(workspace)
	if workspace == "" {
		return domain.Project{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Project{}, err
	}
	defer tx.Rollback()

	var p domain.Project
	var archived int
	var created, updated int64
	err = tx.QueryRowContext(ctx, `SELECT id,name,path,description,archived,created_at,updated_at FROM projects WHERE path=?`, workspace).Scan(
		&p.ID, &p.Name, &p.Path, &p.Description, &archived, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		now := time.Now().UTC().UnixMilli()
		p = domain.Project{
			ID:        newID(),
			Name:      defaultProjectName(workspace),
			Path:      workspace,
			CreatedAt: time.UnixMilli(now),
			UpdatedAt: time.UnixMilli(now),
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,path,description,archived,created_at,updated_at) VALUES(?,?,?,'',0,?,?)`, p.ID, p.Name, p.Path, now, now); err != nil {
			return domain.Project{}, err
		}
	} else if err != nil {
		return domain.Project{}, err
	} else {
		p.Archived = archived != 0
		p.CreatedAt = time.UnixMilli(created)
		p.UpdatedAt = time.UnixMilli(updated)
	}

	now := time.Now().UTC().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO task_metadata(task_id,project_id,notes,archived,updated_at)
VALUES(?,?, '',0,?)
ON CONFLICT(task_id) DO UPDATE SET
 project_id=CASE WHEN task_metadata.project_id IS NULL OR task_metadata.project_id='' THEN excluded.project_id ELSE task_metadata.project_id END,
 updated_at=excluded.updated_at
`, taskID, p.ID, now); err != nil {
		return domain.Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Project{}, err
	}
	return p, nil
}

func (s *Store) CreateProject(ctx context.Context, name, path, description string) (domain.Project, error) {
	name = strings.TrimSpace(name)
	path = normalizeWorkspacePath(path)
	description = strings.TrimSpace(description)
	if name == "" {
		if path == "" {
			return domain.Project{}, errors.New("project name is required")
		}
		name = defaultProjectName(path)
	}
	now := time.Now().UTC().UnixMilli()
	p := domain.Project{ID: newID(), Name: name, Path: path, Description: description, CreatedAt: time.UnixMilli(now), UpdatedAt: time.UnixMilli(now)}
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,name,path,description,archived,created_at,updated_at) VALUES(?,?,?,?,0,?,?)`, p.ID, p.Name, p.Path, p.Description, now, now)
	if err != nil {
		return domain.Project{}, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

func (s *Store) GetProject(ctx context.Context, id string) (domain.Project, error) {
	var p domain.Project
	var archived int
	var created, updated int64
	err := s.db.QueryRowContext(ctx, `SELECT id,name,path,description,archived,created_at,updated_at FROM projects WHERE id=?`, id).Scan(
		&p.ID, &p.Name, &p.Path, &p.Description, &archived, &created, &updated,
	)
	if err != nil {
		return p, err
	}
	p.Archived = archived != 0
	p.CreatedAt = time.UnixMilli(created)
	p.UpdatedAt = time.UnixMilli(updated)
	return p, nil
}

func (s *Store) ListProjects(ctx context.Context, includeArchived bool) ([]domain.Project, error) {
	query := `SELECT id,name,path,description,archived,created_at,updated_at FROM projects`
	if !includeArchived {
		query += ` WHERE archived=0`
	}
	query += ` ORDER BY archived,name COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Project{}
	for rows.Next() {
		var p domain.Project
		var archived int
		var created, updated int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &archived, &created, &updated); err != nil {
			return nil, err
		}
		p.Archived = archived != 0
		p.CreatedAt = time.UnixMilli(created)
		p.UpdatedAt = time.UnixMilli(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProject(ctx context.Context, id, name, path, description string, archived *bool) (domain.Project, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return p, err
	}
	if strings.TrimSpace(name) != "" {
		p.Name = strings.TrimSpace(name)
	}
	if path != "" {
		p.Path = normalizeWorkspacePath(path)
	}
	p.Description = strings.TrimSpace(description)
	if archived != nil {
		p.Archived = *archived
	}
	now := time.Now().UTC().UnixMilli()
	_, err = s.db.ExecContext(ctx, `UPDATE projects SET name=?,path=?,description=?,archived=?,updated_at=? WHERE id=?`, p.Name, p.Path, p.Description, boolInt(p.Archived), now, id)
	if err != nil {
		return domain.Project{}, err
	}
	return s.GetProject(ctx, id)
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE task_metadata SET project_id=NULL,updated_at=? WHERE project_id=?`, time.Now().UTC().UnixMilli(), id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) ListDashboardTasks(ctx context.Context, f TaskFilter) ([]DashboardTask, error) {
	query := `
SELECT t.id,t.thread_id,t.turn_id,t.objective,t.workspace,t.state,t.pause_reason,t.checkpoint,t.pending,t.last_test,
       t.last_quota_remaining,t.last_quota_reset_at,t.created_at,t.updated_at,
       COALESCE(m.project_id,''),COALESCE(p.name,''),COALESCE(m.notes,''),COALESCE(m.archived,0)
FROM tasks t
LEFT JOIN task_metadata m ON m.task_id=t.id
LEFT JOIN projects p ON p.id=m.project_id
WHERE 1=1`
	args := []any{}
	if f.ProjectID != "" {
		query += ` AND m.project_id=?`
		args = append(args, f.ProjectID)
	}
	if f.State != "" {
		query += ` AND t.state=?`
		args = append(args, f.State)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		query += ` AND (t.objective LIKE ? OR t.workspace LIKE ? OR t.thread_id LIKE ? OR COALESCE(m.notes,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	if f.Archived != nil {
		query += ` AND COALESCE(m.archived,0)=?`
		args = append(args, boolInt(*f.Archived))
	}
	query += ` ORDER BY t.updated_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DashboardTask{}
	for rows.Next() {
		v, err := scanDashboardTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetDashboardTask(ctx context.Context, id string) (DashboardTask, error) {
	return scanDashboardTask(s.db.QueryRowContext(ctx, `
SELECT t.id,t.thread_id,t.turn_id,t.objective,t.workspace,t.state,t.pause_reason,t.checkpoint,t.pending,t.last_test,
       t.last_quota_remaining,t.last_quota_reset_at,t.created_at,t.updated_at,
       COALESCE(m.project_id,''),COALESCE(p.name,''),COALESCE(m.notes,''),COALESCE(m.archived,0)
FROM tasks t
LEFT JOIN task_metadata m ON m.task_id=t.id
LEFT JOIN projects p ON p.id=m.project_id
WHERE t.id=?`, id))
}

func (s *Store) UpdateTaskMetadata(ctx context.Context, id, objective, projectID, notes string, archived *bool) (DashboardTask, error) {
	t, err := s.GetTask(ctx, id)
	if err != nil {
		return DashboardTask{}, err
	}
	if strings.TrimSpace(objective) != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET objective=?,updated_at=? WHERE id=?`, strings.TrimSpace(objective), time.Now().UTC().UnixMilli(), id); err != nil {
			return DashboardTask{}, err
		}
	}
	current, err := s.GetDashboardTask(ctx, id)
	if err != nil {
		return DashboardTask{}, err
	}
	if projectID == "" {
		projectID = current.ProjectID
	}
	if notes == "" {
		notes = current.Notes
	}
	archivedValue := current.Archived
	if archived != nil {
		archivedValue = *archived
	}
	var project any
	if projectID != "" {
		project = projectID
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO task_metadata(task_id,project_id,notes,archived,updated_at)
VALUES(?,?,?,?,?)
ON CONFLICT(task_id) DO UPDATE SET project_id=excluded.project_id,notes=excluded.notes,archived=excluded.archived,updated_at=excluded.updated_at
`, t.ID, project, notes, boolInt(archivedValue), time.Now().UTC().UnixMilli())
	if err != nil {
		return DashboardTask{}, err
	}
	return s.GetDashboardTask(ctx, id)
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	t, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	switch t.State {
	case domain.StateCompleted, domain.StateFailed, domain.StateCancelled:
	default:
		return fmt.Errorf("task in state %s cannot be deleted; cancel or complete it first", t.State)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListTaskEvents(ctx context.Context, taskID string) ([]domain.TaskEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,from_state,to_state,reason,created_at FROM task_events WHERE task_id=? ORDER BY id DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.TaskEvent{}
	for rows.Next() {
		var e domain.TaskEvent
		var from, to string
		var created int64
		if err := rows.Scan(&e.ID, &e.TaskID, &from, &to, &e.Reason, &created); err != nil {
			return nil, err
		}
		e.FromState = domain.TaskState(from)
		e.ToState = domain.TaskState(to)
		e.CreatedAt = time.UnixMilli(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanDashboardTask(s scanner) (DashboardTask, error) {
	var v DashboardTask
	var state string
	var q sql.NullFloat64
	var reset sql.NullInt64
	var created, updated int64
	var archived int
	err := s.Scan(
		&v.ID, &v.ThreadID, &v.TurnID, &v.Objective, &v.Workspace, &state, &v.PauseReason, &v.Checkpoint, &v.Pending, &v.LastTest,
		&q, &reset, &created, &updated,
		&v.ProjectID, &v.ProjectName, &v.Notes, &archived,
	)
	if err != nil {
		return v, err
	}
	v.State = domain.TaskState(state)
	if q.Valid {
		n := q.Float64
		v.LastQuotaRemaining = &n
	}
	if reset.Valid {
		t := time.UnixMilli(reset.Int64)
		v.LastQuotaResetAt = &t
	}
	v.CreatedAt = time.UnixMilli(created)
	v.UpdatedAt = time.UnixMilli(updated)
	v.Archived = archived != 0
	return v, nil
}
