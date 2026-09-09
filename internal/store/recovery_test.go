package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codex-desktop-quota-guard/internal/domain"
)

func pausedTaskForRecoveryTest(t *testing.T, st *Store, threadID string) domain.Task {
	t.Helper()
	ctx := context.Background()
	task, err := st.UpsertTask(ctx, threadID, "turn-test", "recovery test", `C:\test`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePauseRequested, "test"); err != nil {
		t.Fatal(err)
	}
	task, err = st.Transition(ctx, task.ThreadID, domain.StatePausedQuota, "test pause")
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestClaimedResumeRecoveryBecomesNeedsReview(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	task := pausedTaskForRecoveryTest(t, st, "thread-recovery")

	action, err := st.QueueResumeSafe(ctx, task.ThreadID, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}

	// Simulate a companion that died after claim. Make the delivery old enough
	// for startup recovery without sleeping in the test.
	if _, err := st.db.ExecContext(ctx, `UPDATE actions SET updated_at=? WHERE id=?`, time.Now().UTC().Add(-2*time.Minute).UnixMilli(), action.ID); err != nil {
		t.Fatal(err)
	}

	recovered, err := st.RecoverStaleDeliveries(ctx, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d, want 1", recovered)
	}

	current, err := st.GetByThread(ctx, task.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != domain.StateNeedsReview {
		t.Fatalf("state=%s, want %s", current.State, domain.StateNeedsReview)
	}

	var status string
	if err := st.db.QueryRowContext(ctx, `SELECT status FROM actions WHERE id=?`, action.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "uncertain" {
		t.Fatalf("action status=%s, want uncertain", status)
	}

	// NEEDS_REVIEW must not silently create another resume delivery.
	if _, err := st.QueueResumeSafe(ctx, task.ThreadID, "continue again"); err == nil {
		t.Fatal("expected QueueResumeSafe to reject NEEDS_REVIEW task")
	}
	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actions WHERE kind='resume' AND thread_id=?`, task.ThreadID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("resume action count=%d, want 1", count)
	}
}

func TestQueueResumeSafeReusesDeliveringAction(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	task := pausedTaskForRecoveryTest(t, st, "thread-idempotent")

	action, err := st.QueueResumeSafe(ctx, task.ThreadID, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}

	action2, err := st.QueueResumeSafe(ctx, task.ThreadID, "continue duplicate")
	if err != nil {
		t.Fatal(err)
	}
	if action2.ID != action.ID {
		t.Fatalf("action id=%d, want existing delivering action %d", action2.ID, action.ID)
	}
}

func TestClaimedResumeSuccessMovesTaskRunning(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	task := pausedTaskForRecoveryTest(t, st, "thread-success")

	action, err := st.QueueResumeSafe(ctx, task.ThreadID, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteClaimedAction(ctx, action.ID, true, ""); err != nil {
		t.Fatal(err)
	}

	current, err := st.GetByThread(ctx, task.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != domain.StateRunning {
		t.Fatalf("state=%s, want %s", current.State, domain.StateRunning)
	}
	var status string
	if err := st.db.QueryRowContext(ctx, `SELECT status FROM actions WHERE id=?`, action.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("action status=%s, want done", status)
	}
}
