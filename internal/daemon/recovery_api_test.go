package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/store"
)

func TestDashboardRecoveryIncludesManagedAndProjectQueueItems(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	managedProject, err := st.CreateProject(ctx, "Managed", `D:\work\managed`, "")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := st.UpsertTask(ctx, "thread-managed", "turn-1", "resume me", managedProject.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, managed.ID, managedProject.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, managed.ThreadID, domain.StatePauseRequested, "quota"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, managed.ThreadID, domain.StatePausedQuota, "quota"); err != nil {
		t.Fatal(err)
	}
	resume, err := st.QueueResumeSafe(ctx, managed.ThreadID, "resume")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, resume.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkActionUncertain(ctx, resume.ID, "resume pipe outcome unknown"); err != nil {
		t.Fatal(err)
	}

	queueProject, err := st.CreateProject(ctx, "Queue", `D:\work\queue`, "")
	if err != nil {
		t.Fatal(err)
	}
	queueManaged, err := st.UpsertTask(ctx, "thread-queue", "turn-2", "finished", queueProject.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, queueManaged.ID, queueProject.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, queueManaged.ThreadID, domain.StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}
	queueItem, err := st.CreateProjectTask(ctx, queueProject.ID, "queued objective", "acceptance")
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := st.QueueProjectTaskDispatch(ctx, queueItem.ID, queueManaged.ThreadID, "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimAction(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkActionUncertain(ctx, dispatch.ID, "dispatch pipe outcome unknown"); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	svc := NewService(cfg, st, nil)
	srv := NewServer(cfg.ListenAddr, svc, st, nil)
	r := httptest.NewRequest(http.MethodGet, "/v1/dashboard/tasks/recovery", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var items []recoveryItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("recovery items=%d want 2: %s", len(items), w.Body.String())
	}
	seenManaged := false
	seenQueue := false
	for _, item := range items {
		if item.Action == nil || item.Action.Status != "uncertain" {
			t.Fatalf("missing uncertain action metadata: %#v", item)
		}
		switch item.Source {
		case "managed_task":
			seenManaged = item.ID == managed.ID && item.ThreadID == managed.ThreadID
		case "project_task":
			seenQueue = item.ID == queueItem.ID && item.ThreadID == queueManaged.ThreadID
		}
	}
	if !seenManaged || !seenQueue {
		t.Fatalf("sources missing: managed=%t queue=%t", seenManaged, seenQueue)
	}

	r = httptest.NewRequest(http.MethodGet, "/v1/dashboard", nil)
	w = httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", w.Code, w.Body.String())
	}
	var summary map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if got := int(summary["reviewCount"].(float64)); got != 2 {
		t.Fatalf("reviewCount=%d want 2", got)
	}
}

func TestProjectQueueRecoveryRunningEndpoint(t *testing.T) {
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
	if err := st.MarkActionUncertain(ctx, *started.ActionID, "unknown"); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	srv := NewServer(cfg.ListenAddr, svc, st, nil)
	r := httptest.NewRequest(http.MethodPost, "/v1/project-tasks/"+item.ID+"/running", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	loaded, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.ProjectTaskRunning {
		t.Fatalf("state=%s want RUNNING", loaded.State)
	}
}

func TestProjectQueueRecoveryRetryEndpointCreatesFreshAction(t *testing.T) {
	st, svc, _, item := setupQueueModeTest(t)
	ctx := context.Background()
	started, err := svc.StartProjectQueueItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.ActionID == nil {
		t.Fatal("expected dispatch action")
	}
	firstActionID := *started.ActionID
	if err := st.ClaimAction(ctx, firstActionID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkActionUncertain(ctx, firstActionID, "unknown"); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	srv := NewServer(cfg.ListenAddr, svc, st, nil)
	r := httptest.NewRequest(http.MethodPost, "/v1/project-tasks/"+item.ID+"/retry", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	loaded, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.ProjectTaskDispatching {
		t.Fatalf("state=%s want DISPATCHING", loaded.State)
	}
	if loaded.ActionID == nil || *loaded.ActionID == firstActionID {
		t.Fatalf("retry action=%v first=%d", loaded.ActionID, firstActionID)
	}
	info, err := st.GetActionInfo(ctx, firstActionID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "uncertain" {
		t.Fatalf("historical action status=%s want uncertain", info.Status)
	}
}
