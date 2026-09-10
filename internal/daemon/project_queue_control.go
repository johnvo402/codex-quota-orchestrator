package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"codex-desktop-quota-guard/internal/domain"
)

// StartProjectQueueItem manually dispatches the next queued item for a project.
// It bypasses only automatic-dispatch policy; quota, ordering, project blocking,
// and active managed-task safety checks remain mandatory.
func (s *Service) StartProjectQueueItem(ctx context.Context, itemID string) (domain.ProjectTask, error) {
	item, err := s.store.GetProjectTask(ctx, itemID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if item.State != domain.ProjectTaskQueued {
		return domain.ProjectTask{}, fmt.Errorf("project task is %s, not QUEUED", item.State)
	}

	settings, err := s.store.GetProjectQueueMode(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if settings.Mode == domain.ProjectQueuePaused {
		return domain.ProjectTask{}, errors.New("project queue is PAUSED; change the project queue mode before starting work")
	}

	next, err := s.store.NextQueuedProjectTask(ctx, item.ProjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ProjectTask{}, errors.New("project has no queued task to start")
	}
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if next.ID != item.ID {
		return domain.ProjectTask{}, errors.New("only the next queued task can be started")
	}

	q, _, err := s.CurrentDecision(ctx)
	if err != nil {
		return domain.ProjectTask{}, fmt.Errorf("read quota: %w", err)
	}
	if q.SoftPause || !s.policy.CanResume(q) {
		return domain.ProjectTask{}, errors.New("quota is not healthy enough to start queued work")
	}

	blocking, err := s.store.ProjectHasBlockingTask(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if blocking {
		return domain.ProjectTask{}, errors.New("project queue is blocked by active or NEEDS_REVIEW work")
	}
	active, err := s.store.ProjectHasActiveManagedTask(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if active {
		return domain.ProjectTask{}, errors.New("project already has an active managed Desktop task")
	}

	p, err := s.store.GetProject(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	threadID, err := s.store.ProjectDispatchThread(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if _, err := s.store.QueueProjectTaskDispatch(ctx, item.ID, threadID, queuedTaskMessage(p, item)); err != nil {
		return domain.ProjectTask{}, err
	}
	return s.store.GetProjectTask(ctx, item.ID)
}

// RetryProjectQueueItem retries a NEEDS_REVIEW dispatch only after the user has
// verified that the previous uncertain Desktop delivery did not arrive. The
// retry is still subject to quota, queue-mode, and one-active-task safeguards.
func (s *Service) RetryProjectQueueItem(ctx context.Context, itemID string) (domain.ProjectTask, error) {
	item, err := s.store.GetProjectTask(ctx, itemID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if item.State != domain.ProjectTaskNeedsReview {
		return domain.ProjectTask{}, fmt.Errorf("project task is %s, not NEEDS_REVIEW", item.State)
	}

	settings, err := s.store.GetProjectQueueMode(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if settings.Mode == domain.ProjectQueuePaused {
		return domain.ProjectTask{}, errors.New("project queue is PAUSED; change the project queue mode before retrying delivery")
	}

	q, _, err := s.CurrentDecision(ctx)
	if err != nil {
		return domain.ProjectTask{}, fmt.Errorf("read quota: %w", err)
	}
	if q.SoftPause || !s.policy.CanResume(q) {
		return domain.ProjectTask{}, errors.New("quota is not healthy enough to retry queued work")
	}

	blocking, err := s.store.ProjectHasOtherBlockingTask(ctx, item.ProjectID, item.ID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if blocking {
		return domain.ProjectTask{}, errors.New("project has another active or NEEDS_REVIEW queue item")
	}
	active, err := s.store.ProjectHasActiveManagedTask(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if active {
		return domain.ProjectTask{}, errors.New("project already has an active managed Desktop task")
	}

	p, err := s.store.GetProject(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	threadID, err := s.store.ProjectDispatchThread(ctx, item.ProjectID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if item.TargetThreadID != "" && item.TargetThreadID != threadID {
		return domain.ProjectTask{}, errors.New("project Desktop thread changed since the uncertain delivery; review the project before retrying")
	}
	action, err := s.store.RetryProjectTaskDispatch(ctx, item.ID, threadID, queuedTaskMessage(p, item))
	if err != nil {
		return domain.ProjectTask{}, err
	}
	s.log.Info("project task recovery delivery queued", "project", item.ProjectID, "projectTask", item.ID, "thread", threadID, "actionId", action.ID)
	return s.store.GetProjectTask(ctx, item.ID)
}

// ConfirmProjectQueueItemRunning resolves a NEEDS_REVIEW dispatch after the user
// verifies that the uncertain message actually arrived and Codex is already
// executing it. No new Desktop message is sent.
func (s *Service) ConfirmProjectQueueItemRunning(ctx context.Context, itemID string) (domain.ProjectTask, error) {
	item, err := s.store.GetProjectTask(ctx, itemID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if item.State != domain.ProjectTaskNeedsReview {
		return domain.ProjectTask{}, fmt.Errorf("project task is %s, not NEEDS_REVIEW", item.State)
	}
	blocking, err := s.store.ProjectHasOtherBlockingTask(ctx, item.ProjectID, item.ID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	if blocking {
		return domain.ProjectTask{}, errors.New("project has another active or NEEDS_REVIEW queue item")
	}
	updated, err := s.store.ConfirmProjectTaskRunning(ctx, item.ID)
	if err != nil {
		return domain.ProjectTask{}, err
	}
	s.log.Info("project task recovery confirmed running", "project", item.ProjectID, "projectTask", item.ID, "thread", updated.TargetThreadID)
	return updated, nil
}
