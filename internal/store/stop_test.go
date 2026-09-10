package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func openStopTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func registerStopTestTask(t *testing.T, st *Store, threadID, turnID string) domain.Task {
	t.Helper()
	task, err := st.UpsertTask(context.Background(), threadID, turnID, "work", `D:\work`)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestQueueStopSafeDeduplicatesCurrentTurn(t *testing.T) {
	st := openStopTestStore(t)
	ctx := context.Background()
	registerStopTestTask(t, st, "thread-stop", "turn-1")

	first, err := st.QueueStopSafe(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.QueueStopSafe(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("stop action duplicated: first=%d second=%d", first.ID, second.ID)
	}
	if first.Message != "turn-1" {
		t.Fatalf("expected turn payload=%q want turn-1", first.Message)
	}
}

func TestStopClaimRejectsChangedTurnBeforeSideEffect(t *testing.T) {
	st := openStopTestStore(t)
	ctx := context.Background()
	registerStopTestTask(t, st, "thread-stop", "turn-1")
	action, err := st.QueueStopSafe(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE tasks SET turn_id='turn-2' WHERE thread_id='thread-stop'`); err != nil {
		t.Fatal(err)
	}

	if err := st.ClaimAction(ctx, action.ID); !errors.Is(err, ErrActionNotPending) {
		t.Fatalf("claim error=%v want ErrActionNotPending", err)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "cancelled" {
		t.Fatalf("action status=%s want cancelled", info.Status)
	}
	loaded, err := st.GetByThread(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.StateRunning || loaded.TurnID != "turn-2" {
		t.Fatalf("task changed unexpectedly: state=%s turn=%s", loaded.State, loaded.TurnID)
	}
}

func TestCompletedDesktopStopCancelsManagedAndQueueWork(t *testing.T) {
	st := openStopTestStore(t)
	ctx := context.Background()
	registerStopTestTask(t, st, "thread-stop", "turn-1")

	project, err := st.CreateProject(ctx, "Stop project", `D:\work`, "")
	if err != nil {
		t.Fatal(err)
	}
	item, err := st.CreateProjectTask(ctx, project.ID, "queued work", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE project_tasks SET state='RUNNING',target_thread_id=? WHERE id=?`, "thread-stop", item.ID); err != nil {
		t.Fatal(err)
	}

	action, err := st.QueueStopSafe(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteClaimedAction(ctx, action.ID, true, ""); err != nil {
		t.Fatal(err)
	}

	managed, err := st.GetByThread(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if managed.State != domain.StateCancelled {
		t.Fatalf("managed state=%s want CANCELLED", managed.State)
	}
	queued, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != domain.ProjectTaskCancelled {
		t.Fatalf("queue state=%s want CANCELLED", queued.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "done" {
		t.Fatalf("action status=%s want done", info.Status)
	}
}

func TestDesktopStopFailureBeforeClickLeavesTaskRunning(t *testing.T) {
	st := openStopTestStore(t)
	ctx := context.Background()
	registerStopTestTask(t, st, "thread-stop", "turn-1")
	action, err := st.QueueStopSafe(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteClaimedAction(ctx, action.ID, false, "button not found"); err != nil {
		t.Fatal(err)
	}
	managed, err := st.GetByThread(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if managed.State != domain.StateRunning {
		t.Fatalf("managed state=%s want RUNNING", managed.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "failed" {
		t.Fatalf("action status=%s want failed", info.Status)
	}
}

func TestUncertainDesktopStopMovesManagedAndQueueWorkToReview(t *testing.T) {
	st := openStopTestStore(t)
	ctx := context.Background()
	registerStopTestTask(t, st, "thread-stop", "turn-1")
	project, err := st.CreateProject(ctx, "Stop project", `D:\work`, "")
	if err != nil {
		t.Fatal(err)
	}
	item, err := st.CreateProjectTask(ctx, project.ID, "queued work", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE project_tasks SET state='RUNNING',target_thread_id=? WHERE id=?`, "thread-stop", item.ID); err != nil {
		t.Fatal(err)
	}

	action, err := st.QueueStopSafe(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkActionUncertain(ctx, action.ID, "Stop click outcome unknown"); err != nil {
		t.Fatal(err)
	}

	managed, err := st.GetByThread(ctx, "thread-stop")
	if err != nil {
		t.Fatal(err)
	}
	if managed.State != domain.StateNeedsReview {
		t.Fatalf("managed state=%s want NEEDS_REVIEW", managed.State)
	}
	queued, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("queue state=%s want NEEDS_REVIEW", queued.State)
	}
}
