package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestQueuedContinuationWaitsForSelectedTaskAndUsesItsThread(t *testing.T) {
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
	selected, err := st.UpsertTask(ctx, "thread-selected", "", "selected work", `D:\work\demo`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, selected.ID, `D:\work\demo`); err != nil {
		t.Fatal(err)
	}

	other, err := st.UpsertTask(ctx, "thread-other", "", "other work", `D:\work\demo`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, other.ID, `D:\work\demo`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, other.ThreadID, domain.StateCompleted, "other done"); err != nil {
		t.Fatal(err)
	}

	item, err := st.CreateProjectTaskForTask(ctx, p.ID, selected.ID, "continue selected", "")
	if err != nil {
		t.Fatal(err)
	}
	if item.TargetThreadID != selected.ThreadID {
		t.Fatalf("target thread=%q want=%q", item.TargetThreadID, selected.ThreadID)
	}

	if thread, err := st.ProjectTaskDispatchThread(ctx, item.ID); err == nil {
		t.Fatalf("dispatch unexpectedly ready on thread %q while selected task is RUNNING", thread)
	}
	if _, err := st.Transition(ctx, selected.ThreadID, domain.StateCompleted, "selected done"); err != nil {
		t.Fatal(err)
	}
	thread, err := st.ProjectTaskDispatchThread(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if thread != selected.ThreadID {
		t.Fatalf("dispatch thread=%q want selected thread=%q", thread, selected.ThreadID)
	}
}

func TestQueuedContinuationRejectsTaskFromAnotherProject(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	p1, err := st.CreateProject(ctx, "One", `D:\work\one`, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateProject(ctx, "Two", `D:\work\two`, ""); err != nil {
		t.Fatal(err)
	}
	target, err := st.UpsertTask(ctx, "thread-two", "", "two", `D:\work\two`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, target.ID, `D:\work\two`); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CreateProjectTaskForTask(ctx, p1.ID, target.ID, "wrong project", ""); err == nil {
		t.Fatal("expected cross-project target task to be rejected")
	}
}

func TestLegacyUnboundQueuedItemDoesNotGuessAThread(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p, _ := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	task, _ := st.UpsertTask(ctx, "thread-1", "", "work", `D:\work\demo`)
	_, _ = st.EnsureProjectForWorkspace(ctx, task.ID, `D:\work\demo`)
	_, _ = st.Transition(ctx, task.ThreadID, domain.StateCompleted, "done")

	item, err := st.CreateProjectTask(ctx, p.ID, "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ProjectTaskDispatchThread(ctx, item.ID); err == nil {
		t.Fatal("legacy unbound queue item must not guess the latest project thread")
	}
}
