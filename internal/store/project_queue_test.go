package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestProjectTaskQueueLifecycle(t *testing.T) {
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
	managed, err := st.UpsertTask(ctx, "thread-1", "turn-1", "first task", `D:\work\demo`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, managed.ID, `D:\work\demo`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, "thread-1", domain.StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}

	b, err := st.CreateProjectTask(ctx, p.ID, "task B", "second")
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateProjectTask(ctx, p.ID, "task C", "third")
	if err != nil {
		t.Fatal(err)
	}
	if b.Position >= c.Position {
		t.Fatalf("queue positions not increasing: %d >= %d", b.Position, c.Position)
	}

	thread, err := st.ProjectDispatchThread(ctx, p.ID)
	if err != nil || thread != "thread-1" {
		t.Fatalf("dispatch thread=%q err=%v", thread, err)
	}
	a, err := st.QueueProjectTaskDispatch(ctx, b.ID, thread, "run B")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteClaimedAction(ctx, a.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	b, err = st.GetProjectTask(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.State != domain.ProjectTaskRunning {
		t.Fatalf("B state=%s", b.State)
	}
	managed, err = st.GetByThread(ctx, "thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if managed.State != domain.StateRunning || managed.Objective != "task B" {
		t.Fatalf("managed state=%s objective=%q", managed.State, managed.Objective)
	}

	blocking, err := st.ProjectHasBlockingTask(ctx, p.ID)
	if err != nil || !blocking {
		t.Fatalf("expected blocking running queue task, blocking=%v err=%v", blocking, err)
	}

	if _, err := st.Transition(ctx, "thread-1", domain.StateCompleted, "B done"); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteRunningProjectTaskByThread(ctx, "thread-1"); err != nil {
		t.Fatal(err)
	}
	b, _ = st.GetProjectTask(ctx, b.ID)
	if b.State != domain.ProjectTaskCompleted {
		t.Fatalf("B final state=%s", b.State)
	}
	next, err := st.NextQueuedProjectTask(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != c.ID {
		t.Fatalf("next=%s want=%s", next.ID, c.ID)
	}
}

func TestProjectQueueUncertainStopsAdvancement(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p, _ := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	managed, _ := st.UpsertTask(ctx, "thread-1", "", "first", `D:\work\demo`)
	_, _ = st.EnsureProjectForWorkspace(ctx, managed.ID, `D:\work\demo`)
	_, _ = st.Transition(ctx, "thread-1", domain.StateCompleted, "done")
	item, _ := st.CreateProjectTask(ctx, p.ID, "queued", "")
	a, err := st.QueueProjectTaskDispatch(ctx, item.ID, "thread-1", "run")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkActionUncertain(ctx, a.ID, "lost response"); err != nil {
		t.Fatal(err)
	}
	item, _ = st.GetProjectTask(ctx, item.ID)
	if item.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("state=%s", item.State)
	}
	blocking, err := st.ProjectHasBlockingTask(ctx, p.ID)
	if err != nil || !blocking {
		t.Fatalf("NEEDS_REVIEW must block queue, blocking=%v err=%v", blocking, err)
	}
}
