package store

import (
	"context"
	"time"
)

// ReconcileCompletedProjectTasks repairs queue rows that are still RUNNING even
// though their owning managed Desktop task is already COMPLETED. Normally the
// task_complete endpoint updates both records in one request, but older builds,
// crashes, or a partial write path may leave the queue row stale. Without this
// repair ProjectHasBlockingTask would block AUTO advancement forever.
func (s *Store) ReconcileCompletedProjectTasks(ctx context.Context, projectID string) (int64, error) {
	now := time.Now().UTC().UnixMilli()
	res, err := s.db.ExecContext(ctx, `
UPDATE project_tasks
SET state='COMPLETED', updated_at=?, completed_at=COALESCE(completed_at,?)
WHERE project_id=?
  AND state='RUNNING'
  AND target_thread_id<>''
  AND EXISTS (
    SELECT 1
    FROM tasks t
    WHERE t.thread_id=project_tasks.target_thread_id
      AND t.state='COMPLETED'
  )
`, now, now, projectID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
