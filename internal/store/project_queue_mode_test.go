package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestProjectQueueModeDefaultsToAutoAndPersists(t *testing.T) {
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

	settings, err := st.GetProjectQueueMode(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != domain.ProjectQueueAuto {
		t.Fatalf("default mode=%s want=AUTO", settings.Mode)
	}

	settings, err = st.SetProjectQueueMode(ctx, p.ID, domain.ProjectQueueManual)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != domain.ProjectQueueManual {
		t.Fatalf("set mode=%s want=MANUAL", settings.Mode)
	}
	loaded, err := st.GetProjectQueueMode(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mode != domain.ProjectQueueManual {
		t.Fatalf("loaded mode=%s want=MANUAL", loaded.Mode)
	}

	if _, err := st.SetProjectQueueMode(ctx, p.ID, domain.ProjectQueueMode("INVALID")); err == nil {
		t.Fatal("expected invalid queue mode to fail")
	}
}
