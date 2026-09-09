package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestReorderQueuedProjectTasksNormalizesPositions(t *testing.T) {
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
	a, _ := st.CreateProjectTask(ctx, p.ID, "A", "")
	b, _ := st.CreateProjectTask(ctx, p.ID, "B", "")
	c, _ := st.CreateProjectTask(ctx, p.ID, "C", "")

	if err := st.ReorderQueuedProjectTasks(ctx, p.ID, []string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListProjectTasks(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	queued := onlyQueued(items)
	want := []string{c.ID, a.ID, b.ID}
	if len(queued) != len(want) {
		t.Fatalf("queued=%d want=%d", len(queued), len(want))
	}
	for i, item := range queued {
		if item.ID != want[i] {
			t.Fatalf("index %d id=%s want=%s", i, item.ID, want[i])
		}
		if item.Position != int64(i+1) {
			t.Fatalf("index %d position=%d want=%d", i, item.Position, i+1)
		}
	}
	next, err := st.NextQueuedProjectTask(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != c.ID {
		t.Fatalf("next=%s want=%s", next.ID, c.ID)
	}
}

func TestReorderQueuedProjectTasksRejectsStaleOrDuplicateOrder(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p, _ := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	a, _ := st.CreateProjectTask(ctx, p.ID, "A", "")
	b, _ := st.CreateProjectTask(ctx, p.ID, "B", "")

	if err := st.ReorderQueuedProjectTasks(ctx, p.ID, []string{a.ID}); err == nil {
		t.Fatal("expected stale partial order to fail")
	}
	if err := st.ReorderQueuedProjectTasks(ctx, p.ID, []string{a.ID, a.ID}); err == nil {
		t.Fatal("expected duplicate order to fail")
	}
	items, err := st.ListProjectTasks(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	queued := onlyQueued(items)
	if len(queued) != 2 || queued[0].ID != a.ID || queued[1].ID != b.ID {
		t.Fatalf("failed reorder mutated queue: %#v", queued)
	}
}

func TestQueuedPositionsRenumberAfterCancelAndDispatch(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p, _ := st.CreateProject(ctx, "Demo", `D:\work\demo`, "")
	managed, _ := st.UpsertTask(ctx, "thread-1", "", "first", `D:\work\demo`)
	_, _ = st.EnsureProjectForWorkspace(ctx, managed.ID, `D:\work\demo`)
	_, _ = st.Transition(ctx, "thread-1", domain.StateCompleted, "done")

	a, _ := st.CreateProjectTask(ctx, p.ID, "A", "")
	b, _ := st.CreateProjectTask(ctx, p.ID, "B", "")
	c, _ := st.CreateProjectTask(ctx, p.ID, "C", "")
	if _, err := st.CancelProjectTask(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	items, _ := st.ListProjectTasks(ctx, p.ID)
	queued := onlyQueued(items)
	if len(queued) != 2 || queued[0].ID != a.ID || queued[0].Position != 1 || queued[1].ID != c.ID || queued[1].Position != 2 {
		t.Fatalf("queue not normalized after cancel: %#v", queued)
	}

	if _, err := st.QueueProjectTaskDispatch(ctx, a.ID, "thread-1", "run A"); err != nil {
		t.Fatal(err)
	}
	items, _ = st.ListProjectTasks(ctx, p.ID)
	queued = onlyQueued(items)
	if len(queued) != 1 || queued[0].ID != c.ID || queued[0].Position != 1 {
		t.Fatalf("queue not normalized after dispatch: %#v", queued)
	}
}

func onlyQueued(items []domain.ProjectTask) []domain.ProjectTask {
	out := []domain.ProjectTask{}
	for _, item := range items {
		if item.State == domain.ProjectTaskQueued {
			out = append(out, item)
		}
	}
	return out
}
