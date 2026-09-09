package store

import (
	"context"
	"time"
)

// SetTaskDashboardMetadata applies the editable dashboard fields exactly.
// Empty projectID means unassigned and empty notes clears notes.
func (s *Store) SetTaskDashboardMetadata(ctx context.Context, id, objective, projectID, notes string, archived *bool) (DashboardTask, error) {
	if _, err := s.GetTask(ctx, id); err != nil {
		return DashboardTask{}, err
	}
	if objective != "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET objective=?,updated_at=? WHERE id=?`, objective, time.Now().UTC().UnixMilli(), id); err != nil {
			return DashboardTask{}, err
		}
	}
	current, err := s.GetDashboardTask(ctx, id)
	if err != nil {
		return DashboardTask{}, err
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
ON CONFLICT(task_id) DO UPDATE SET
 project_id=excluded.project_id,
 notes=excluded.notes,
 archived=excluded.archived,
 updated_at=excluded.updated_at
`, id, project, notes, boolInt(archivedValue), time.Now().UTC().UnixMilli())
	if err != nil {
		return DashboardTask{}, err
	}
	return s.GetDashboardTask(ctx, id)
}
