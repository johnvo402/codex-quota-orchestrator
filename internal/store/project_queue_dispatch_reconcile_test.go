package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codex-desktop-quota-guard/internal/domain"
)

func setupDispatchingProjectTask(t *testing.T) (*Store, context.Context, domain.ProjectTask, Action) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	managed, err := st.UpsertTask(ctx, "thread-1", "turn-1", "first", `D:\work\demo`)
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, managed.ID, `D:\work\demo`); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, "thread-1", domain.StateCompleted, "done"); err != nil {
		st.Close()
		t.Fatal(err)
	}
	item, err := st.CreateProjectTask(ctx, p.ID, "queued work", "")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	action, err := st.QueueProjectTaskDispatch(ctx, item.ID, "thread-1", "run queued work")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	item, err = st.GetProjectTask(ctx, item.ID)
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	return st, ctx, item, action
}

func TestReconcileProjectTaskDispatchLeavesPendingRetryable(t *testing.T) {
	st, ctx, item, _ := setupDispatchingProjectTask(t)
	defer st.Close()

	repaired, err := st.ReconcileProjectTaskDispatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 0 {
		t.Fatalf("repaired=%d want=0", repaired)
	}
	got, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.ProjectTaskDispatching {
		t.Fatalf("state=%s want=DISPATCHING", got.State)
	}
}

func TestReconcileProjectTaskDispatchRepairsDoneAction(t *testing.T) {
	st, ctx, item, action := setupDispatchingProjectTask(t)
	defer st.Close()
	now := time.Now().UTC().UnixMilli()
	if _, err := st.db.ExecContext(ctx, `UPDATE actions SET status='done',updated_at=? WHERE id=?`, now, action.ID); err != nil {
		t.Fatal(err)
	}

	repaired, err := st.ReconcileProjectTaskDispatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("repaired=%d want=1", repaired)
	}
	got, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.ProjectTaskRunning {
		t.Fatalf("state=%s want=RUNNING", got.State)
	}
	managed, err := st.GetByThread(ctx, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if managed.State != domain.StateRunning || managed.Objective != "queued work" {
		t.Fatalf("managed state=%s objective=%q", managed.State, managed.Objective)
	}
}

func TestReconcileProjectTaskDispatchRepairsUncertainAction(t *testing.T) {
	st, ctx, item, action := setupDispatchingProjectTask(t)
	defer st.Close()
	now := time.Now().UTC().UnixMilli()
	if _, err := st.db.ExecContext(ctx, `UPDATE actions SET status='uncertain',error_text='lost native response',updated_at=? WHERE id=?`, now, action.ID); err != nil {
		t.Fatal(err)
	}

	repaired, err := st.ReconcileProjectTaskDispatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("repaired=%d want=1", repaired)
	}
	got, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("state=%s want=NEEDS_REVIEW", got.State)
	}
}

func TestReconcileProjectTaskDispatchRepairsCancelledAction(t *testing.T) {
	st, ctx, item, action := setupDispatchingProjectTask(t)
	defer st.Close()
	now := time.Now().UTC().UnixMilli()
	if _, err := st.db.ExecContext(ctx, `UPDATE actions SET status='cancelled',updated_at=? WHERE id=?`, now, action.ID); err != nil {
		t.Fatal(err)
	}

	repaired, err := st.ReconcileProjectTaskDispatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("repaired=%d want=1", repaired)
	}
	got, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.ProjectTaskCancelled {
		t.Fatalf("state=%s want=CANCELLED", got.State)
	}
}

func TestReconcileProjectTaskDispatchMissingActionNeedsReview(t *testing.T) {
	st, ctx, item, action := setupDispatchingProjectTask(t)
	defer st.Close()
	if _, err := st.db.ExecContext(ctx, `DELETE FROM actions WHERE id=?`, action.ID); err != nil {
		t.Fatal(err)
	}

	repaired, err := st.ReconcileProjectTaskDispatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("repaired=%d want=1", repaired)
	}
	got, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("state=%s want=NEEDS_REVIEW", got.State)
	}
}
