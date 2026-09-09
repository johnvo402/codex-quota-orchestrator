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
