package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

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

// ensureActionDiagnosticsSchema upgrades existing installations lazily. The
// actions table predates durable error text, so older state.db files need one
// additive column before action errors can be surfaced in the dashboard.
func (s *Store) ensureActionDiagnosticsSchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(actions)`)
	if err != nil {
		return fmt.Errorf("inspect actions schema: %w", err)
	}
	defer rows.Close()

	hasErrorText := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("read actions schema: %w", err)
		}
		if strings.EqualFold(name, "error_text") {
			hasErrorText = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect actions schema rows: %w", err)
	}
	if hasErrorText {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `ALTER TABLE actions ADD COLUMN error_text TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add actions.error_text: %w", err)
	}
	return nil
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
