package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type queuedPosition struct {
	id       string
	position int64
}

// NormalizeAllProjectQueues repairs legacy/duplicate QUEUED positions without
// changing the relative order users already see.
func (s *Store) NormalizeAllProjectQueues(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT project_id FROM project_tasks WHERE state='QUEUED' ORDER BY project_id`)
	if err != nil {
		return err
	}
	var projectIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		projectIDs = append(projectIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		if err := s.NormalizeProjectQueue(ctx, projectID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) NormalizeProjectQueue(ctx context.Context, projectID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := normalizeQueuedProjectTasksTx(ctx, tx, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

// ReorderQueuedProjectTasks replaces the complete QUEUED order for a project.
// The caller must provide every currently queued task exactly once. This makes
// stale drag/drop requests fail instead of silently losing or duplicating work.
func (s *Store) ReorderQueuedProjectTasks(ctx context.Context, projectID string, orderedIDs []string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errors.New("projectId required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM projects WHERE id=?`, projectID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return sql.ErrNoRows
	}

	current, err := queuedPositionsTx(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if len(orderedIDs) != len(current) {
		return fmt.Errorf("queue changed while reordering: got %d task ids, expected %d", len(orderedIDs), len(current))
	}

	allowed := make(map[string]struct{}, len(current))
	for _, item := range current {
		allowed[item.id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(orderedIDs))
	clean := make([]string, 0, len(orderedIDs))
	for _, raw := range orderedIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			return errors.New("taskIds cannot contain an empty id")
		}
		if _, ok := allowed[id]; !ok {
			return fmt.Errorf("project task %s is not currently QUEUED in this project", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("project task %s appears more than once in taskIds", id)
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}

	if err := writeQueuedOrderTx(ctx, tx, projectID, clean); err != nil {
		return err
	}
	return tx.Commit()
}

// MoveQueuedProjectTask keeps the legacy position-based API safe by translating
// a single move into a complete normalized queue order.
func (s *Store) MoveQueuedProjectTask(ctx context.Context, id string, targetPosition int64) error {
	item, err := s.GetProjectTask(ctx, id)
	if err != nil {
		return err
	}
	if item.State != "QUEUED" {
		return fmt.Errorf("only QUEUED project tasks can be reordered")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := queuedPositionsTx(ctx, tx, item.ProjectID)
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return errors.New("project has no queued tasks")
	}
	if targetPosition < 1 {
		targetPosition = 1
	}
	if targetPosition > int64(len(current)) {
		targetPosition = int64(len(current))
	}

	order := make([]string, 0, len(current))
	found := false
	for _, row := range current {
		if row.id == id {
			found = true
			continue
		}
		order = append(order, row.id)
	}
	if !found {
		return fmt.Errorf("project task %s is not currently QUEUED", id)
	}
	idx := int(targetPosition - 1)
	order = append(order, "")
	copy(order[idx+1:], order[idx:])
	order[idx] = id

	if err := writeQueuedOrderTx(ctx, tx, item.ProjectID, order); err != nil {
		return err
	}
	return tx.Commit()
}

func queuedPositionsTx(ctx context.Context, tx *sql.Tx, projectID string) ([]queuedPosition, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id,position
FROM project_tasks
WHERE project_id=? AND state='QUEUED'
ORDER BY position,created_at,id
`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []queuedPosition{}
	for rows.Next() {
		var v queuedPosition
		if err := rows.Scan(&v.id, &v.position); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func normalizeQueuedProjectTasksTx(ctx context.Context, tx *sql.Tx, projectID string) error {
	current, err := queuedPositionsTx(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return nil
	}
	order := make([]string, 0, len(current))
	alreadyNormalized := true
	for i, item := range current {
		order = append(order, item.id)
		if item.position != int64(i+1) {
			alreadyNormalized = false
		}
	}
	if alreadyNormalized {
		return nil
	}
	return writeQueuedOrderTx(ctx, tx, projectID, order)
}

func writeQueuedOrderTx(ctx context.Context, tx *sql.Tx, projectID string, orderedIDs []string) error {
	// Move every queued row into a unique negative slot first. This keeps the
	// transaction compatible with the partial UNIQUE(project_id,position)
	// invariant while swapping arbitrary rows.
	for i, id := range orderedIDs {
		res, err := tx.ExecContext(ctx, `
UPDATE project_tasks
SET position=?
WHERE id=? AND project_id=? AND state='QUEUED'
`, -(i + 1), id, projectID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("queue changed while reordering project task %s", id)
		}
	}

	now := time.Now().UTC().UnixMilli()
	for i, id := range orderedIDs {
		res, err := tx.ExecContext(ctx, `
UPDATE project_tasks
SET position=?,updated_at=?
WHERE id=? AND project_id=? AND state='QUEUED'
`, i+1, now, id, projectID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("queue changed while reordering project task %s", id)
		}
	}
	return nil
}
