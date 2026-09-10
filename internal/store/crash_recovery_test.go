package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestRecoverInterruptedDeliveriesMarksRecentResumeUncertain(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-resume", "turn-1", "resume me", `D:\work\resume`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePauseRequested, "quota low"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePausedQuota, "quota low"); err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueResumeSafe(ctx, task.ThreadID, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}

	recovered, err := st.RecoverInterruptedDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d want=1", recovered)
	}

	got, err := st.GetByThread(ctx, task.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.StateNeedsReview {
		t.Fatalf("task state=%s want NEEDS_REVIEW", got.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "uncertain" {
		t.Fatalf("action status=%s want uncertain", info.Status)
	}
}

func TestRecoverInterruptedDeliveriesLeavesPendingActionSafeToRetry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-pending", "turn-1", "pending resume", `D:\work\pending`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePauseRequested, "quota low"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePausedQuota, "quota low"); err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueResumeSafe(ctx, task.ThreadID, "resume")
	if err != nil {
		t.Fatal(err)
	}

	recovered, err := st.RecoverInterruptedDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 0 {
		t.Fatalf("recovered=%d want=0", recovered)
	}
	got, err := st.GetByThread(ctx, task.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.StateResumeQueued {
		t.Fatalf("task state=%s want RESUME_QUEUED", got.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "pending" {
		t.Fatalf("action status=%s want pending", info.Status)
	}
}

func TestRecoverInterruptedProjectDispatchBlocksBlindRetry(t *testing.T) {
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
	managed, err := st.UpsertTask(ctx, "thread-project", "turn-1", "previous task", project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, managed.ID, project.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, managed.ThreadID, domain.StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}
	item, err := st.CreateProjectTask(ctx, project.ID, "next task", "")
	if err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueProjectTaskDispatch(ctx, item.ID, managed.ThreadID, "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}

	recovered, err := st.RecoverInterruptedDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered=%d want=1", recovered)
	}
	item, err = st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("project task state=%s want NEEDS_REVIEW", item.State)
	}
	managed, err = st.GetByThread(ctx, managed.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if managed.State != domain.StateCompleted {
		t.Fatalf("managed task state=%s want COMPLETED", managed.State)
	}
}
