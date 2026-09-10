package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestProjectTaskRecoveryRetryAndConfirmRunning(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	project, err := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := st.UpsertTask(ctx, "thread-1", "turn-1", "finished task", project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, managed.ID, project.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, managed.ThreadID, domain.StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}

	item, err := st.CreateProjectTask(ctx, project.ID, "next objective", "acceptance criteria")
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.QueueProjectTaskDispatch(ctx, item.ID, managed.ThreadID, "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkActionUncertain(ctx, first.ID, "pipe outcome unknown"); err != nil {
		t.Fatal(err)
	}

	item, err = st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("state=%s want NEEDS_REVIEW", item.State)
	}
	info, err := st.GetActionInfo(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "uncertain" {
		t.Fatalf("action status=%s want uncertain", info.Status)
	}

	retry, err := st.RetryProjectTaskDispatch(ctx, item.ID, managed.ThreadID, "retry dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID == first.ID {
		t.Fatal("retry must create a fresh action")
	}
	item, err = st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != domain.ProjectTaskDispatching {
		t.Fatalf("retry state=%s want DISPATCHING", item.State)
	}
	if item.ActionID == nil || *item.ActionID != retry.ID {
		t.Fatalf("retry action id=%v want %d", item.ActionID, retry.ID)
	}

	if err := st.ClaimAction(ctx, retry.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkActionUncertain(ctx, retry.ID, "second outcome unknown"); err != nil {
		t.Fatal(err)
	}
	item, err = st.ConfirmProjectTaskRunning(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != domain.ProjectTaskRunning {
		t.Fatalf("confirmed state=%s want RUNNING", item.State)
	}
	managed, err = st.GetByThread(ctx, managed.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if managed.State != domain.StateRunning {
		t.Fatalf("managed state=%s want RUNNING", managed.State)
	}
	if managed.Objective != "next objective" {
		t.Fatalf("managed objective=%q want next objective", managed.Objective)
	}
}
