package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codex-desktop-quota-guard/internal/domain"
)

func TestPendingProjectActionCancelledWhenOwnerNoLongerDispatching(t *testing.T) {
	st, ctx, item, action := setupDispatchingProjectTask(t)
	defer st.Close()
	if _, err := st.db.ExecContext(ctx, `UPDATE project_tasks SET state='CANCELLED',updated_at=?,completed_at=? WHERE id=?`, time.Now().UTC().UnixMilli(), time.Now().UTC().UnixMilli(), item.ID); err != nil {
		t.Fatal(err)
	}

	ok, err := st.PendingActionDeliverable(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("stale project action must not be deliverable")
	}
	status, err := st.GetActionStatus(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "cancelled" {
		t.Fatalf("action status=%s want=cancelled", status.Status)
	}
}

func TestPendingResumeActionCancelledWhenTaskNoLongerResumeQueued(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.UpsertTask(ctx, "thread-1", "", "task", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, "thread-1", domain.StatePauseRequested, "quota"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, "thread-1", domain.StatePausedQuota, "quota"); err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueResumeSafe(ctx, "thread-1", "resume")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, "thread-1", domain.StateCancelled, "user cancelled"); err != nil {
		t.Fatal(err)
	}

	ok, err := st.PendingActionDeliverable(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("stale resume action must not be deliverable")
	}
	status, err := st.GetActionStatus(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "cancelled" {
		t.Fatalf("action status=%s want=cancelled", status.Status)
	}
}

func TestPendingPauseNoticeCancelledAfterQuotaRecovery(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.UpsertTask(ctx, "thread-1", "", "task", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, "thread-1", domain.StatePauseRequested, "quota"); err != nil {
		t.Fatal(err)
	}
	action, err := st.EnqueueAction(ctx, "pause_notice", "thread-1", "pause")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, "thread-1", domain.StateRunning, "quota recovered"); err != nil {
		t.Fatal(err)
	}

	ok, err := st.PendingActionDeliverable(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("stale pause notice must not be deliverable")
	}
	status, err := st.GetActionStatus(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "cancelled" {
		t.Fatalf("action status=%s want=cancelled", status.Status)
	}
}
