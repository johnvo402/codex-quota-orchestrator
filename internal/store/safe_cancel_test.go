package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestSafeCancelManagedPendingResumeInvalidatesAction(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-resume-cancel", "turn-1", "resume me", `D:\work\resume`)
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

	cancelled, err := st.SafeCancelManagedTask(ctx, task.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != domain.StateCancelled {
		t.Fatalf("state=%s want CANCELLED", cancelled.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "cancelled" {
		t.Fatalf("action status=%s want cancelled", info.Status)
	}
	if err := st.ClaimAction(ctx, action.ID); !errors.Is(err, ErrActionNotPending) {
		t.Fatalf("claim after cancellation err=%v want ErrActionNotPending", err)
	}
}

func TestSafeCancelManagedClaimedResumeMovesToReview(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-resume-delivering", "turn-1", "resume me", `D:\work\resume`)
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

	updated, err := st.SafeCancelManagedTask(ctx, task.ThreadID)
	if !errors.Is(err, ErrDeliveryInProgress) {
		t.Fatalf("cancel err=%v want ErrDeliveryInProgress", err)
	}
	if updated.State != domain.StateNeedsReview {
		t.Fatalf("state=%s want NEEDS_REVIEW", updated.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "uncertain" {
		t.Fatalf("action status=%s want uncertain", info.Status)
	}
}

func TestSafeCancelProjectPendingDispatchInvalidatesAction(t *testing.T) {
	st, _, item, action := setupDispatchForCancelTest(t)
	ctx := context.Background()

	cancelled, err := st.SafeCancelProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != domain.ProjectTaskCancelled {
		t.Fatalf("state=%s want CANCELLED", cancelled.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "cancelled" {
		t.Fatalf("action status=%s want cancelled", info.Status)
	}
	if err := st.ClaimAction(ctx, action.ID); !errors.Is(err, ErrActionNotPending) {
		t.Fatalf("claim after project cancellation err=%v want ErrActionNotPending", err)
	}
}

func TestSafeCancelProjectClaimedDispatchMovesToReview(t *testing.T) {
	st, _, item, action := setupDispatchForCancelTest(t)
	ctx := context.Background()
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}

	updated, err := st.SafeCancelProjectTask(ctx, item.ID)
	if !errors.Is(err, ErrDeliveryInProgress) {
		t.Fatalf("cancel err=%v want ErrDeliveryInProgress", err)
	}
	if updated.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("state=%s want NEEDS_REVIEW", updated.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "uncertain" {
		t.Fatalf("action status=%s want uncertain", info.Status)
	}
}

func TestClaimVersusManagedCancelHasSingleSafeOutcome(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-race", "turn-1", "race", `D:\work\race`)
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

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var claimErr, cancelErr error
	go func() {
		defer wg.Done()
		<-start
		claimErr = st.ClaimAction(ctx, action.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, cancelErr = st.SafeCancelManagedTask(ctx, task.ThreadID)
	}()
	close(start)
	wg.Wait()

	got, err := st.GetByThread(ctx, task.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}

	switch {
	case claimErr == nil:
		if !errors.Is(cancelErr, ErrDeliveryInProgress) {
			t.Fatalf("claim won but cancel err=%v want ErrDeliveryInProgress", cancelErr)
		}
		if got.State != domain.StateNeedsReview || info.Status != "uncertain" {
			t.Fatalf("claim-won outcome task=%s action=%s", got.State, info.Status)
		}
	case errors.Is(claimErr, ErrActionNotPending):
		if cancelErr != nil {
			t.Fatalf("cancel won but err=%v", cancelErr)
		}
		if got.State != domain.StateCancelled || info.Status != "cancelled" {
			t.Fatalf("cancel-won outcome task=%s action=%s", got.State, info.Status)
		}
	default:
		t.Fatalf("unexpected claim err=%v cancel err=%v", claimErr, cancelErr)
	}
}

func setupDispatchForCancelTest(t *testing.T) (*Store, domain.Task, domain.ProjectTask, Action) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	project, err := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := st.UpsertTask(ctx, "thread-project-cancel", "turn-1", "previous", project.Path)
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
	return st, managed, item, action
}
