package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/quota"
	"codex-desktop-quota-guard/internal/store"
)

func setupQueueModeTest(t *testing.T) (*store.Store, *Service, domain.Project, domain.ProjectTask) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
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
	item, err := st.CreateProjectTask(ctx, p.ID, "next task", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveQuota(ctx, quota.Snapshot{
		RemainingPercent: 80,
		CanResume:        true,
		ObservedAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	return st, NewService(cfg, st, nil), p, item
}

func TestManualProjectQueueDoesNotAutoDispatchButCanStart(t *testing.T) {
	st, svc, p, item := setupQueueModeTest(t)
	ctx := context.Background()
	if _, err := st.SetProjectQueueMode(ctx, p.ID, domain.ProjectQueueManual); err != nil {
		t.Fatal(err)
	}

	svc.ReconcileProjectQueues(ctx)
	loaded, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.ProjectTaskQueued {
		t.Fatalf("manual reconcile state=%s want=QUEUED", loaded.State)
	}

	started, err := svc.StartProjectQueueItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.State != domain.ProjectTaskDispatching {
		t.Fatalf("manual start state=%s want=DISPATCHING", started.State)
	}
}

func TestPausedProjectQueueRejectsManualStart(t *testing.T) {
	st, svc, p, item := setupQueueModeTest(t)
	ctx := context.Background()
	if _, err := st.SetProjectQueueMode(ctx, p.ID, domain.ProjectQueuePaused); err != nil {
		t.Fatal(err)
	}

	svc.ReconcileProjectQueues(ctx)
	if _, err := svc.StartProjectQueueItem(ctx, item.ID); err == nil {
		t.Fatal("expected PAUSED queue to reject manual start")
	}
	loaded, err := st.GetProjectTask(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != domain.ProjectTaskQueued {
		t.Fatalf("paused queue state=%s want=QUEUED", loaded.State)
	}
}
