package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

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
