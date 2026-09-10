package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/domain"
)

var (
	// ErrDeliveryInProgress means cancellation raced with an already-claimed
	// Desktop delivery. The durable state is moved to NEEDS_REVIEW instead of
	// pretending the external side effect did not happen.
	ErrDeliveryInProgress = errors.New("Desktop delivery is already in progress")
	// ErrDeliveryAlreadyCompleted means the outbound message was already
	// confirmed delivered, so cancellation cannot undo that side effect.
	ErrDeliveryAlreadyCompleted = errors.New("Desktop delivery already completed")
)

// SafeCancelManagedTask cancels a managed task without allowing a pending
// resume action to be delivered afterwards. Pending non-project actions for the
// thread are invalidated in the same transaction.
//
// If a resume action has already been claimed, its external outcome is no
// longer knowable. That action is marked uncertain and the task is moved to
// NEEDS_REVIEW; callers receive ErrDeliveryInProgress after the transaction is
// committed.
func (s *Store) SafeCancelManagedTask(ctx context.Context, threadID string) (domain.Task, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return domain.Task{}, errors.New("thread id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()

	var taskID, stateRaw string
	if err := tx.QueryRowContext(ctx, `SELECT id,state FROM tasks WHERE thread_id=?`, threadID).Scan(&taskID, &stateRaw); err != nil {
		return domain.Task{}, err
	}
	state := domain.TaskState(stateRaw)
	if state == domain.StateCancelled {
		if err := tx.Commit(); err != nil {
			return domain.Task{}, err
		}
		return s.GetByThread(ctx, threadID)
	}

	now := time.Now().UTC().UnixMilli()
	if state == domain.StateResumeQueued {
		var actionID int64
		var actionStatus string
		err := tx.QueryRowContext(ctx, `
SELECT id,status
FROM actions
WHERE kind='resume' AND thread_id=?
ORDER BY id DESC
LIMIT 1
`, threadID).Scan(&actionID, &actionStatus)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Legacy/inconsistent RESUME_QUEUED without an action has no outbound
			// delivery left to invalidate, so normal cancellation is safe.
		case err != nil:
			return domain.Task{}, err
		default:
			switch actionStatus {
			case "pending":
				res, err := tx.ExecContext(ctx, `UPDATE actions SET status='cancelled',updated_at=? WHERE id=? AND status='pending'`, now, actionID)
				if err != nil {
					return domain.Task{}, err
				}
				if n, err := res.RowsAffected(); err != nil || n != 1 {
					if err != nil {
						return domain.Task{}, err
					}
					return domain.Task{}, errors.New("resume action changed while cancelling")
				}
			case "delivering":
				reason := "cancel requested while resume delivery was already in progress; delivery outcome is unknown"
				if _, err := tx.ExecContext(ctx, `UPDATE actions SET status='uncertain',updated_at=? WHERE id=? AND status='delivering'`, now, actionID); err != nil {
					return domain.Task{}, err
				}
				if err := moveManagedTaskToReviewTx(ctx, tx, taskID, threadID, state, reason, now); err != nil {
					return domain.Task{}, err
				}
				if err := tx.Commit(); err != nil {
					return domain.Task{}, err
				}
				updated, getErr := s.GetByThread(ctx, threadID)
				if getErr != nil {
					return domain.Task{}, getErr
				}
				return updated, fmt.Errorf("%w; task moved to NEEDS_REVIEW", ErrDeliveryInProgress)
			case "uncertain":
				reason := "cancel requested but resume delivery outcome is already uncertain"
				if err := moveManagedTaskToReviewTx(ctx, tx, taskID, threadID, state, reason, now); err != nil {
					return domain.Task{}, err
				}
				if err := tx.Commit(); err != nil {
					return domain.Task{}, err
				}
				updated, getErr := s.GetByThread(ctx, threadID)
				if getErr != nil {
					return domain.Task{}, getErr
				}
				return updated, fmt.Errorf("%w; task moved to NEEDS_REVIEW", ErrDeliveryInProgress)
			case "done":
				return domain.Task{}, fmt.Errorf("%w; inspect the Desktop thread before cancelling running work", ErrDeliveryAlreadyCompleted)
			case "failed", "cancelled":
				// No live outbound delivery remains.
			default:
				return domain.Task{}, fmt.Errorf("cannot safely cancel resume action %d from status %s", actionID, actionStatus)
			}
		}
	}

	if err := domain.ValidateTransition(state, domain.StateCancelled); err != nil {
		return domain.Task{}, err
	}

	// Invalidate queued pause/resume notices that have not been claimed yet.
	// Project-task dispatches are separate entities and must be cancelled via
	// SafeCancelProjectTask instead.
	if _, err := tx.ExecContext(ctx, `
UPDATE actions
SET status='cancelled',updated_at=?
WHERE thread_id=?
  AND status='pending'
  AND kind NOT LIKE 'project_task/%'
`, now, threadID); err != nil {
		return domain.Task{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,pause_reason=?,updated_at=? WHERE thread_id=?`, domain.StateCancelled, "cancelled from dashboard", now, threadID); err != nil {
		return domain.Task{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, state, domain.StateCancelled, "cancelled from dashboard; pending Desktop actions invalidated", now); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	return s.GetByThread(ctx, threadID)
}

// SafeCancelProjectTask cancels queued/review work and also handles the
// DISPATCHING race atomically with its linked action.
func (s *Store) SafeCancelProjectTask(ctx context.Context, itemID string) (domain.ProjectTask, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return domain.ProjectTask{}, errors.New("project task id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	defer tx.Rollback()

	var stateRaw, projectID string
	var actionID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT state,project_id,action_id FROM project_tasks WHERE id=?`, itemID).Scan(&stateRaw, &projectID, &actionID); err != nil {
		return domain.ProjectTask{}, err
	}
	state := domain.ProjectTaskState(stateRaw)
	if state == domain.ProjectTaskCancelled {
		if err := tx.Commit(); err != nil {
			return domain.ProjectTask{}, err
		}
		return s.GetProjectTask(ctx, itemID)
	}

	now := time.Now().UTC().UnixMilli()
	switch state {
	case domain.ProjectTaskQueued:
		res, err := tx.ExecContext(ctx, `UPDATE project_tasks SET state='CANCELLED',updated_at=?,completed_at=? WHERE id=? AND state='QUEUED'`, now, now, itemID)
		if err != nil {
			return domain.ProjectTask{}, err
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			if err != nil {
				return domain.ProjectTask{}, err
			}
			return domain.ProjectTask{}, errors.New("queue changed while cancelling project task")
		}
		if err := normalizeQueuedProjectTasksTx(ctx, tx, projectID); err != nil {
			return domain.ProjectTask{}, err
		}

	case domain.ProjectTaskNeedsReview:
		if _, err := tx.ExecContext(ctx, `UPDATE project_tasks SET state='CANCELLED',updated_at=?,completed_at=? WHERE id=? AND state='NEEDS_REVIEW'`, now, now, itemID); err != nil {
			return domain.ProjectTask{}, err
		}

	case domain.ProjectTaskDispatching:
		if !actionID.Valid {
			return domain.ProjectTask{}, errors.New("DISPATCHING project task has no linked action; manual review required")
		}
		var kind, status string
		if err := tx.QueryRowContext(ctx, `SELECT kind,status FROM actions WHERE id=?`, actionID.Int64).Scan(&kind, &status); err != nil {
			return domain.ProjectTask{}, err
		}
		if !isProjectTaskAction(kind) || projectTaskIDFromActionKind(kind) != itemID {
			return domain.ProjectTask{}, errors.New("project task action linkage is inconsistent; manual review required")
		}
		switch status {
		case "pending":
			res, err := tx.ExecContext(ctx, `UPDATE actions SET status='cancelled',updated_at=? WHERE id=? AND status='pending'`, now, actionID.Int64)
			if err != nil {
				return domain.ProjectTask{}, err
			}
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				if err != nil {
					return domain.ProjectTask{}, err
				}
				return domain.ProjectTask{}, errors.New("dispatch action changed while cancelling")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE project_tasks SET state='CANCELLED',updated_at=?,completed_at=? WHERE id=? AND state='DISPATCHING'`, now, now, itemID); err != nil {
				return domain.ProjectTask{}, err
			}
		case "delivering":
			reason := "cancel requested while project task delivery was already in progress; delivery outcome is unknown"
			res, err := tx.ExecContext(ctx, `UPDATE actions SET status='uncertain',updated_at=? WHERE id=? AND status='delivering'`, now, actionID.Int64)
			if err != nil {
				return domain.ProjectTask{}, err
			}
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				if err != nil {
					return domain.ProjectTask{}, err
				}
				return domain.ProjectTask{}, errors.New("dispatch action changed while cancelling")
			}
			if err := markProjectTaskActionUncertainTx(ctx, tx, kind, reason, now); err != nil {
				return domain.ProjectTask{}, err
			}
			if err := tx.Commit(); err != nil {
				return domain.ProjectTask{}, err
			}
			updated, getErr := s.GetProjectTask(ctx, itemID)
			if getErr != nil {
				return domain.ProjectTask{}, getErr
			}
			return updated, fmt.Errorf("%w; project task moved to NEEDS_REVIEW", ErrDeliveryInProgress)
		case "uncertain":
			reason := "cancel requested but project task delivery outcome is already uncertain"
			if err := markProjectTaskActionUncertainTx(ctx, tx, kind, reason, now); err != nil {
				return domain.ProjectTask{}, err
			}
			if err := tx.Commit(); err != nil {
				return domain.ProjectTask{}, err
			}
			updated, getErr := s.GetProjectTask(ctx, itemID)
			if getErr != nil {
				return domain.ProjectTask{}, getErr
			}
			return updated, fmt.Errorf("%w; project task moved to NEEDS_REVIEW", ErrDeliveryInProgress)
		case "done":
			return domain.ProjectTask{}, fmt.Errorf("%w; queued work may already be running", ErrDeliveryAlreadyCompleted)
		case "failed", "cancelled":
			if _, err := tx.ExecContext(ctx, `UPDATE project_tasks SET state='CANCELLED',updated_at=?,completed_at=? WHERE id=? AND state='DISPATCHING'`, now, now, itemID); err != nil {
				return domain.ProjectTask{}, err
			}
		default:
			return domain.ProjectTask{}, fmt.Errorf("cannot safely cancel project task action %d from status %s", actionID.Int64, status)
		}

	case domain.ProjectTaskRunning, domain.ProjectTaskDispatched:
		return domain.ProjectTask{}, fmt.Errorf("%w; project task is %s and requires a cooperative stop", ErrDeliveryAlreadyCompleted, state)
	case domain.ProjectTaskCompleted:
		return domain.ProjectTask{}, errors.New("completed project task cannot be cancelled")
	default:
		return domain.ProjectTask{}, fmt.Errorf("project task in state %s cannot be cancelled", state)
	}

	if err := tx.Commit(); err != nil {
		return domain.ProjectTask{}, err
	}
	return s.GetProjectTask(ctx, itemID)
}

func moveManagedTaskToReviewTx(ctx context.Context, tx *sql.Tx, taskID, threadID string, from domain.TaskState, reason string, now int64) error {
	if from == domain.StateNeedsReview {
		return nil
	}
	if err := domain.ValidateTransition(from, domain.StateNeedsReview); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state=?,pause_reason=?,updated_at=? WHERE thread_id=?`, domain.StateNeedsReview, reason, now, threadID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_events(task_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?)`, taskID, from, domain.StateNeedsReview, reason, now)
	return err
}
