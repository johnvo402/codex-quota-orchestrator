package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestProjectHasBlockingTaskRepairsCompletedManagedTask(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	p, err := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := st.UpsertTask(ctx, "thread-1", "turn-1", "seed", `D:\work\demo`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, managed.ID, `D:\work\demo`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, managed.ThreadID, domain.StateCompleted, "seed done"); err != nil {
		t.Fatal(err)
	}

	runningItem, err := st.CreateProjectTask(ctx, p.ID, "queued A", "")
	if err != nil {
		t.Fatal(err)
	}
	nextItem, err := st.CreateProjectTask(ctx, p.ID, "queued B", "")
	if err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueProjectTaskDispatch(ctx, runningItem.ID, managed.ThreadID, "run A")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteClaimedAction(ctx, action.ID, true, ""); err != nil {
		t.Fatal(err)
	}

	// Simulate the crash/older-build window: the managed task completes, but
	// CompleteRunningProjectTaskByThread is never called, leaving the queue row
	// incorrectly RUNNING.
	if _, err := st.Transition(ctx, managed.ThreadID, domain.StateCompleted, "A done"); err != nil {
		t.Fatal(err)
	}
	stale, err := st.GetProjectTask(ctx, runningItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.State != domain.ProjectTaskRunning {
		t.Fatalf("precondition state=%s want RUNNING", stale.State)
	}

	blocking, err := st.ProjectHasBlockingTask(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blocking {
		t.Fatal("completed managed task must not leave project queue blocked")
	}

	repaired, err := st.GetProjectTask(ctx, runningItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.State != domain.ProjectTaskCompleted || repaired.CompletedAt == nil {
		t.Fatalf("repaired state=%s completedAt=%v", repaired.State, repaired.CompletedAt)
	}
	next, err := st.NextQueuedProjectTask(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != nextItem.ID {
		t.Fatalf("next=%s want=%s", next.ID, nextItem.ID)
	}
}
