package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestRegisterDesktopTaskReactivatesTerminalThreadOnNewTurn(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	first, err := st.RegisterDesktopTask(ctx, "thread-1", "turn-1", "first objective", `D:\work\demo`)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCheckpoint(ctx, first.ThreadID, "old checkpoint", "old pending", "old test"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, first.ThreadID, domain.StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}

	second, err := st.RegisterDesktopTask(ctx, first.ThreadID, "turn-2", "second objective", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("task id changed: first=%s second=%s", first.ID, second.ID)
	}
	if second.State != domain.StateRunning {
		t.Fatalf("state=%s want RUNNING", second.State)
	}
	if second.TurnID != "turn-2" {
		t.Fatalf("turn=%q want turn-2", second.TurnID)
	}
	if second.Objective != "second objective" {
		t.Fatalf("objective=%q want second objective", second.Objective)
	}
	if second.Workspace != `D:\work\demo` {
		t.Fatalf("workspace=%q want preserved workspace", second.Workspace)
	}
	if second.Checkpoint != "" || second.Pending != "" || second.LastTest != "" {
		t.Fatalf("old execution context leaked into new turn: checkpoint=%q pending=%q lastTest=%q", second.Checkpoint, second.Pending, second.LastTest)
	}

	var eventCount int
	if err := st.db.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM task_events
WHERE task_id=? AND from_state=? AND to_state=? AND reason='new Desktop turn registered after terminal task'
`, first.ID, domain.StateCompleted, domain.StateRunning).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("reactivation events=%d want 1", eventCount)
	}
}

func TestRegisterDesktopTaskSameTerminalTurnDoesNotReopen(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	first, err := st.RegisterDesktopTask(ctx, "thread-1", "turn-1", "first objective", `D:\work\demo`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, first.ThreadID, domain.StateCompleted, "done"); err != nil {
		t.Fatal(err)
	}

	got, err := st.RegisterDesktopTask(ctx, first.ThreadID, "turn-1", "should not replace completed objective", `D:\other`)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.StateCompleted {
		t.Fatalf("state=%s want COMPLETED", got.State)
	}
	if got.Objective != "first objective" {
		t.Fatalf("objective changed on duplicate terminal register: %q", got.Objective)
	}
	if got.Workspace != `D:\work\demo` {
		t.Fatalf("workspace changed on duplicate terminal register: %q", got.Workspace)
	}
}

func TestRegisterDesktopTaskDoesNotOverwriteNeedsReviewWithNewTurn(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	first, err := st.RegisterDesktopTask(ctx, "thread-1", "turn-1", "uncertain objective", `D:\work\demo`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(ctx, first.ThreadID, domain.StateNeedsReview, "delivery uncertain"); err != nil {
		t.Fatal(err)
	}

	got, err := st.RegisterDesktopTask(ctx, first.ThreadID, "turn-2", "new request", `D:\other`)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.StateNeedsReview {
		t.Fatalf("state=%s want NEEDS_REVIEW", got.State)
	}
	if got.TurnID != "turn-1" || got.Objective != "uncertain objective" || got.Workspace != `D:\work\demo` {
		t.Fatalf("review context was overwritten: turn=%q objective=%q workspace=%q", got.TurnID, got.Objective, got.Workspace)
	}
}
