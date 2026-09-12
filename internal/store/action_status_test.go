package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenEagerlyMigratesLegacyActionDiagnosticsSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`
CREATE TABLE actions(
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 kind TEXT NOT NULL,
 thread_id TEXT NOT NULL,
 message TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending',
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
`); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer st.Close()

	rows, err := st.db.Query(`PRAGMA table_info(actions)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	hasErrorText := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "error_text" {
			hasErrorText = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !hasErrorText {
		t.Fatal("Open must migrate actions.error_text before startup recovery can run")
	}
}

func TestActionStatusPersistsFailureDetails(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	if _, err := st.UpsertTask(ctx, "thread-action-status", "turn-action-status", "working", `D:\work`); err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueStopSafe(ctx, "thread-action-status")
	if err != nil {
		t.Fatal(err)
	}

	pending, err := st.GetActionStatus(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != "pending" || pending.Error != "" {
		t.Fatalf("pending action=%#v", pending)
	}

	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteClaimedAction(ctx, action.ID, false, "Codex Desktop Stop button was not found after navigation"); err != nil {
		t.Fatal(err)
	}

	failed, err := st.GetActionStatus(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" {
		t.Fatalf("status=%q want failed", failed.Status)
	}
	if !strings.Contains(failed.Error, "Stop button was not found") {
		t.Fatalf("error=%q", failed.Error)
	}
	if failed.UpdatedAt.Before(failed.CreatedAt) {
		t.Fatalf("updatedAt=%v before createdAt=%v", failed.UpdatedAt, failed.CreatedAt)
	}
}
