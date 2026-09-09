package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestProjectAndTaskMetadataCRUD(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-ui", "turn-ui", "Build dashboard", `D:\work\quota-guard`)
	if err != nil {
		t.Fatal(err)
	}
	project, err := st.EnsureProjectForWorkspace(ctx, task.ID, task.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "quota-guard" {
		t.Fatalf("project name=%q", project.Name)
	}

	view, err := st.GetDashboardTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ProjectID != project.ID {
		t.Fatalf("project id=%q, want %q", view.ProjectID, project.ID)
	}

	view, err = st.SetTaskDashboardMetadata(ctx, task.ID, "Build local dashboard", project.ID, "first note", nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.Objective != "Build local dashboard" || view.Notes != "first note" {
		t.Fatalf("unexpected task metadata: %+v", view)
	}

	view, err = st.SetTaskDashboardMetadata(ctx, task.ID, view.Objective, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if view.ProjectID != "" || view.Notes != "" {
		t.Fatalf("expected project and notes cleared: %+v", view)
	}

	archived := true
	updated, err := st.UpdateProject(ctx, project.ID, "Quota Guard", project.Path, "local dashboard", &archived)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Archived || updated.Name != "Quota Guard" {
		t.Fatalf("unexpected project update: %+v", updated)
	}

	if err := st.DeleteProject(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteTaskRequiresTerminalState(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	task, err := st.UpsertTask(ctx, "thread-delete", "", "Delete me", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteTask(ctx, task.ID); err == nil {
		t.Fatal("expected running task delete to fail")
	}
	if _, err := st.Transition(ctx, task.ThreadID, domain.StateCancelled, "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
}
