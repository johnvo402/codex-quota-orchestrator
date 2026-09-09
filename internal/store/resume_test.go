package store

import (
	"context"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/domain"
)

func TestResumeQueueLifecycle(
	t *testing.T,
) {
	dbPath := filepath.Join(
		t.TempDir(),
		"state.db",
	)

	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()

	task, err := st.UpsertTask(
		ctx,
		"thread-test",
		"turn-test",
		"test objective",
		`C:\test`,
	)

	if err != nil {
		t.Fatal(err)
	}

	_, err = st.Transition(
		ctx,
		task.ThreadID,
		domain.StatePauseRequested,
		"test",
	)

	if err != nil {
		t.Fatal(err)
	}

	_, err = st.Transition(
		ctx,
		task.ThreadID,
		domain.StatePausedQuota,
		"test quota pause",
	)

	if err != nil {
		t.Fatal(err)
	}

	action, err := st.QueueResume(
		ctx,
		task.ThreadID,
		"continue test",
	)

	if err != nil {
		t.Fatal(err)
	}

	current, err := st.GetByThread(
		ctx,
		task.ThreadID,
	)

	if err != nil {
		t.Fatal(err)
	}

	if current.State !=
		domain.StateResumeQueued {
		t.Fatalf(
			"state=%s, want %s",
			current.State,
			domain.StateResumeQueued,
		)
	}

	// Queue lại không được tạo duplicate.
	action2, err := st.QueueResume(
		ctx,
		task.ThreadID,
		"continue test",
	)

	if err != nil {
		t.Fatal(err)
	}

	if action.ID != action2.ID {
		t.Fatalf(
			"duplicate resume action: %d vs %d",
			action.ID,
			action2.ID,
		)
	}

	if err := st.CompleteAction(
		ctx,
		action.ID,
		true,
		"",
	); err != nil {
		t.Fatal(err)
	}

	current, err = st.GetByThread(
		ctx,
		task.ThreadID,
	)

	if err != nil {
		t.Fatal(err)
	}

	if current.State != domain.StateRunning {
		t.Fatalf(
			"state=%s, want RUNNING",
			current.State,
		)
	}
}
