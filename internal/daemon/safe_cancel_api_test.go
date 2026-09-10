package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/store"
)

func TestManagedCancelEndpointInvalidatesPendingResume(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-managed-cancel-api", "turn-1", "resume", `D:\work\cancel`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePauseRequested, "quota"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePausedQuota, "quota"); err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueResumeSafe(ctx, task.ThreadID, "resume")
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	srv := NewServer(cfg.ListenAddr, NewService(cfg, st, nil), st, nil)
	r := httptest.NewRequest(http.MethodPost, "/v1/dashboard/tasks/"+task.ID+"/cancel", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	loaded, err := st.GetByThread(ctx, task.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.StateCancelled {
		t.Fatalf("state=%s want CANCELLED", loaded.State)
	}
	info, err := st.GetActionInfo(ctx, action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "cancelled" {
		t.Fatalf("action status=%s want cancelled", info.Status)
	}
}

func TestManagedCancelEndpointClaimedResumeReturnsConflictAndReview(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-managed-cancel-race-api", "turn-1", "resume", `D:\work\cancel`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePauseRequested, "quota"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StatePausedQuota, "quota"); err != nil {
		t.Fatal(err)
	}
	action, err := st.QueueResumeSafe(ctx, task.ThreadID, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, action.ID); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	srv := NewServer(cfg.ListenAddr, NewService(cfg, st, nil), st, nil)
	r := httptest.NewRequest(http.MethodPost, "/v1/dashboard/tasks/"+task.ID+"/cancel", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	loaded, err := st.GetByThread(ctx, task.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.StateNeedsReview {
		t.Fatalf("state=%s want NEEDS_REVIEW", loaded.State)
	}
}

func TestProjectCancelEndpointInvalidatesPendingDispatch(t *testing.T) {
	st, svc, _, item := setupQueueModeTest(t)
	ctx := context.Background()
	started, err := svc.StartProjectQueueItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.ActionID == nil {
		t.Fatal("expected dispatch action")
	}
	actionID := *started.ActionID

	cfg := config.Default()
	srv := NewServer(cfg.ListenAddr, svc, st, nil)
	r := httptest.NewRequest(http.MethodPost, "/v1/project-tasks/"+item.ID+"/cancel", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	loaded, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.ProjectTaskCancelled {
		t.Fatalf("state=%s want CANCELLED", loaded.State)
	}
	info, err := st.GetActionInfo(ctx, actionID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "cancelled" {
		t.Fatalf("action status=%s want cancelled", info.Status)
	}
}

func TestProjectCancelEndpointClaimedDispatchReturnsConflictAndReview(t *testing.T) {
	st, svc, _, item := setupQueueModeTest(t)
	ctx := context.Background()
	started, err := svc.StartProjectQueueItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.ActionID == nil {
		t.Fatal("expected dispatch action")
	}
	if err := st.ClaimAction(ctx, *started.ActionID); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	srv := NewServer(cfg.ListenAddr, svc, st, nil)
	r := httptest.NewRequest(http.MethodPost, "/v1/project-tasks/"+item.ID+"/cancel", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	loaded, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("state=%s want NEEDS_REVIEW", loaded.State)
	}
}
