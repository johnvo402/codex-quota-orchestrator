package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/domain"
)

// RegisterDesktopTask registers activity for the current Codex Desktop turn.
//
// A Desktop thread is a stable delivery destination, but it can host multiple
// sequential user objectives over time. If the previous managed task on the
// thread is terminal and Codex supplies a different turn id, the same durable
// thread slot is re-armed as RUNNING for the new objective. This keeps native
// resume targeting stable without leaving continued work stuck in COMPLETED.
//
// Re-registering the same terminal turn is intentionally idempotent and does
// not reopen it. NEEDS_REVIEW and quota-paused states are also never bypassed
// by registration.
func (s *Store) RegisterDesktopTask(ctx context.Context, threadID, turnID, objective, workspace string) (domain.Task, error) {
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	objective = strings.TrimSpace(objective)
	workspace = strings.TrimSpace(workspace)
	if threadID == "" {
		return domain.Task{}, errors.New("thread id is required")
	}
	if objective == "" {
		objective = "Desktop task"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()

	var taskID, currentTurnID, currentStateRaw, currentObjective, currentWorkspace string
	err = tx.QueryRowContext(ctx, `
SELECT id,turn_id,state,objective,workspace
FROM tasks
WHERE thread_id=?
`, threadID).Scan(&taskID, &currentTurnID, &currentStateRaw, &currentObjective, &currentWorkspace)
	if errors.Is(err, sql.ErrNoRows) {
		now := time.Now().UTC().UnixMilli()
		taskID = newID()
		if _, err := tx.ExecContext(ctx, `
INSERT INTO tasks(id,thread_id,turn_id,objective,workspace,state,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)
`, taskID, threadID, turnID, objective, workspace, domain.StateRunning, now, now); err != nil {
			return domain.Task{}, err
		}
		if err := tx.Commit(); err != nil {
			return domain.Task{}, err
		}
		return s.GetByThread(ctx, threadID)
	}
	if err != nil {
		return domain.Task{}, err
	}

	currentState := domain.TaskState(currentStateRaw)
	terminal := currentState == domain.StateCompleted || currentState == domain.StateFailed || currentState == domain.StateCancelled
	newTurn := turnID != "" && turnID != strings.TrimSpace(currentTurnID)
	now := time.Now().UTC().UnixMilli()

	if terminal {
		// A repeated register from the same completed turn must not resurrect the
		// task. A distinct turn id is the proof that the user continued the chat.
		if !newTurn {
			if err := tx.Commit(); err != nil {
				return domain.Task{}, err
			}
			return s.GetByThread(ctx, threadID)
		}

		nextWorkspace := currentWorkspace
		if workspace != "" {
			nextWorkspace = workspace
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE tasks
SET turn_id=?,objective=?,workspace=?,state=?,pause_reason='',checkpoint='',pending='',last_test='',
    last_quota_remaining=NULL,last_quota_reset_at=NULL,created_at=?,updated_at=?
WHERE id=?
`, turnID, objective, nextWorkspace, domain.StateRunning, now, now, taskID); err != nil {
			return domain.Task{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO task_events(task_id,from_state,to_state,reason,created_at)
VALUES(?,?,?,?,?)
`, taskID, currentState, domain.StateRunning, "new Desktop turn registered after terminal task", now); err != nil {
			return domain.Task{}, err
		}
		if err := tx.Commit(); err != nil {
			return domain.Task{}, err
		}
		return s.GetByThread(ctx, threadID)
	}

	// Existing non-terminal behavior: refresh metadata but never use register as
	// a way to escape quota pause or NEEDS_REVIEW.
	nextTurnID := currentTurnID
	if turnID != "" {
		nextTurnID = turnID
	}
	nextObjective := currentObjective
	if objective != "" {
		nextObjective = objective
	}
	nextWorkspace := currentWorkspace
	if workspace != "" {
		nextWorkspace = workspace
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE tasks
SET turn_id=?,objective=?,workspace=?,updated_at=?
WHERE id=?
`, nextTurnID, nextObjective, nextWorkspace, now, taskID); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	return s.GetByThread(ctx, threadID)
}
