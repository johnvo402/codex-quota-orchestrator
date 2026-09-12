package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const desktopStopRemovedReason = "Desktop Stop was removed in v0.2.0"

// ActionStatus exposes durable delivery state for UI/diagnostics without
// changing the semantic task state machine.
type ActionStatus struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	ThreadID  string    `json:"threadId"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ensureActionDiagnosticsSchema upgrades existing installations. The actions
// table predates durable error text, so older state.db files need one additive
// column before action errors can be surfaced in the dashboard. v0.2.0 also
// retires any live Desktop Stop action left by an older installation.
func (s *Store) ensureActionDiagnosticsSchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(actions)`)
	if err != nil {
		return fmt.Errorf("inspect actions schema: %w", err)
	}

	hasErrorText := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read actions schema: %w", err)
		}
		if strings.EqualFold(name, "error_text") {
			hasErrorText = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("inspect actions schema rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close actions schema cursor: %w", err)
	}
	if !hasErrorText {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE actions ADD COLUMN error_text TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add actions.error_text: %w", err)
		}
	}
	return s.retireLegacyDesktopStopActions(ctx)
}

func (s *Store) retireLegacyDesktopStopActions(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
SELECT DISTINCT thread_id
FROM actions
WHERE kind='stop' AND status='delivering'
`)
	if err != nil {
		return err
	}
	var deliveringThreads []string
	for rows.Next() {
		var threadID string
		if err := rows.Scan(&threadID); err != nil {
			_ = rows.Close()
			return err
		}
		deliveringThreads = append(deliveringThreads, threadID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	now := time.Now().UTC().UnixMilli()
	if _, err := tx.ExecContext(ctx, `
UPDATE actions
SET status='cancelled', error_text=?, updated_at=?
WHERE kind='stop' AND status='pending'
`, desktopStopRemovedReason, now); err != nil {
		return err
	}
	uncertainReason := desktopStopRemovedReason + "; previous delivery outcome is unknown"
	if _, err := tx.ExecContext(ctx, `
UPDATE actions
SET status='uncertain', error_text=?, updated_at=?
WHERE kind='stop' AND status='delivering'
`, uncertainReason, now); err != nil {
		return err
	}

	for _, threadID := range deliveringThreads {
		var taskID, state string
		err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &state)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if state == "COMPLETED" || state == "FAILED" || state == "CANCELLED" || state == "NEEDS_REVIEW" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state='NEEDS_REVIEW',pause_reason=?,updated_at=? WHERE thread_id=?`, uncertainReason, now, threadID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, state, "NEEDS_REVIEW", uncertainReason, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetActionStatus(ctx context.Context, id int64) (ActionStatus, error) {
	if err := s.ensureActionDiagnosticsSchema(ctx); err != nil {
		return ActionStatus{}, err
	}

	var out ActionStatus
	var createdAt, updatedAt int64
	err := s.db.QueryRowContext(ctx, `
SELECT id,kind,thread_id,status,error_text,created_at,updated_at
FROM actions
WHERE id=?
`, id).Scan(&out.ID, &out.Kind, &out.ThreadID, &out.Status, &out.Error, &createdAt, &updatedAt)
	if err != nil {
		return ActionStatus{}, err
	}
	out.CreatedAt = time.UnixMilli(createdAt)
	out.UpdatedAt = time.UnixMilli(updatedAt)
	return out, nil
}
