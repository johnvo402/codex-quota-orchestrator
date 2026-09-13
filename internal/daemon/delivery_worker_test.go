package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/desktop"
	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/store"
)

type fakeNativeSender struct {
	sendErr error
	sends   int
}

func (f *fakeNativeSender) Available() bool     { return true }
func (f *fakeNativeSender) Description() string { return "fake native sender" }
func (f *fakeNativeSender) Diagnostics() desktop.NativeDiagnostics {
	return desktop.NativeDiagnostics{Available: true, Source: "test", ExecutorConfigured: true}
}
func (f *fakeNativeSender) Probe(context.Context) error { return nil }
func (f *fakeNativeSender) SendMessage(context.Context, string, string) error {
	f.sends++
	return f.sendErr
}

func setupDaemonDeliveryTest(t *testing.T) (config.Config, *store.Store, domain.ProjectTask, store.Action) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	st, err := store.Open(filepath.Join(cfg.DataDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := desktop.SaveRelay(cfg.RelayPath(), desktop.RelayConfig{ExecutorThreadID: "relay-thread"}); err != nil {
		st.Close()
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	managed, err := st.UpsertTask(ctx, "thread-1", "", "first", `D:\work\demo`)
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	if _, err := st.EnsureProjectForWorkspace(ctx, managed.ID, `D:\work\demo`); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, "thread-1", domain.StateCompleted, "done"); err != nil {
		st.Close()
		t.Fatal(err)
	}
	item, err := st.CreateProjectTask(ctx, p.ID, "next work", "")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	action, err := st.QueueProjectTaskDispatch(ctx, item.ID, "thread-1", "run next work")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	return cfg, st, item, action
}

func TestDaemonDeliveryWorkerCompletesPendingProjectDispatch(t *testing.T) {
	cfg, st, item, action := setupDaemonDeliveryTest(t)
	defer st.Close()
	fake := &fakeNativeSender{}
	oldFactory := newDesktopNativeSender
	newDesktopNativeSender = func(string) desktop.NativeSender { return fake }
	defer func() { newDesktopNativeSender = oldFactory }()

	svc := NewService(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.drainDesktopActions(context.Background())

	if fake.sends != 1 {
		t.Fatalf("send count=%d want=1", fake.sends)
	}
	status, err := st.GetActionStatus(context.Background(), action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "done" {
		t.Fatalf("action status=%s want=done", status.Status)
	}
	got, err := st.GetProjectTask(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.ProjectTaskRunning {
		t.Fatalf("project task state=%s want=RUNNING", got.State)
	}
}

func TestDaemonDeliveryWorkerMarksUncertainSendForReview(t *testing.T) {
	cfg, st, item, action := setupDaemonDeliveryTest(t)
	defer st.Close()
	fake := &fakeNativeSender{sendErr: errors.New("pipe response lost")}
	oldFactory := newDesktopNativeSender
	newDesktopNativeSender = func(string) desktop.NativeSender { return fake }
	defer func() { newDesktopNativeSender = oldFactory }()

	svc := NewService(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.drainDesktopActions(context.Background())

	if fake.sends != 1 {
		t.Fatalf("send count=%d want=1", fake.sends)
	}
	status, err := st.GetActionStatus(context.Background(), action.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "uncertain" {
		t.Fatalf("action status=%s want=uncertain", status.Status)
	}
	got, err := st.GetProjectTask(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.ProjectTaskNeedsReview {
		t.Fatalf("project task state=%s want=NEEDS_REVIEW", got.State)
	}
}
