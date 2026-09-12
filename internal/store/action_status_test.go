package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	action, err := st.EnqueueAction(ctx, "pause_notice", "thread-action-status", "pause safely")
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
	if err := st.CompleteClaimedAction(ctx, action.ID, false, "native delivery failed"); err != nil {
		t.Fatal(err)
	}

	failed, err := st.GetActionStatus(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != "failed" {
		t.Fatalf("status=%q want failed", failed.Status)
	}
	if !strings.Contains(failed.Error, "native delivery failed") {
		t.Fatalf("error=%q", failed.Error)
	}
	if failed.UpdatedAt.Before(failed.CreatedAt) {
		t.Fatalf("updatedAt=%v before createdAt=%v", failed.UpdatedAt, failed.CreatedAt)
	}
}

func TestOpenRetiresLegacyDesktopStopActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := st.UpsertTask(ctx, "thread-legacy-stop", "turn-legacy-stop", "legacy stop", `D:\work`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := st.db.ExecContext(ctx, `INSERT INTO actions(kind,thread_id,message,status,created_at,updated_at) VALUES('stop','thread-pending-stop','turn-pending','pending',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `INSERT INTO actions(kind,thread_id,message,status,created_at,updated_at) VALUES('stop','thread-legacy-stop','turn-legacy-stop','delivering',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("reopen v0.1.x database: %v", err)
	}
	defer st.Close()

	var pendingStatus, pendingError string
	if err := st.db.QueryRowContext(ctx, `SELECT status,error_text FROM actions WHERE kind='stop' AND thread_id='thread-pending-stop'`).Scan(&pendingStatus, &pendingError); err != nil {
		t.Fatal(err)
	}
	if pendingStatus != "cancelled" || !strings.Contains(pendingError, "removed in v0.2.0") {
		t.Fatalf("pending legacy stop status=%q error=%q", pendingStatus, pendingError)
	}

	var deliveringStatus, deliveringError string
	if err := st.db.QueryRowContext(ctx, `SELECT status,error_text FROM actions WHERE kind='stop' AND thread_id='thread-legacy-stop'`).Scan(&deliveringStatus, &deliveringError); err != nil {
		t.Fatal(err)
	}
	if deliveringStatus != "uncertain" || !strings.Contains(deliveringError, "outcome is unknown") {
		t.Fatalf("delivering legacy stop status=%q error=%q", deliveringStatus, deliveringError)
	}

	task, err := st.GetByThread(ctx, "thread-legacy-stop")
	if err != nil {
		t.Fatal(err)
	}
	if string(task.State) != "NEEDS_REVIEW" {
		t.Fatalf("legacy delivering Stop task state=%q want NEEDS_REVIEW", task.State)
	}

	actions, err := st.PendingActions(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if action.Kind == "stop" {
			t.Fatalf("legacy Stop remained pending: %#v", action)
		}
	}
}
