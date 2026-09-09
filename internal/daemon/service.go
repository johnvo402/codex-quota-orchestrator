package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/codexquota"
	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/quota"
	"codex-desktop-quota-guard/internal/store"
)

type Service struct {
	cfg    config.Config
	store  *store.Store
	log    *slog.Logger
	policy quota.Policy
}

func NewService(cfg config.Config, st *store.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{cfg: cfg, store: st, log: log, policy: quota.Policy{SoftThreshold: cfg.SoftThresholdPercent, HardThreshold: cfg.HardThresholdPercent, ResumeThreshold: cfg.ResumeThresholdPercent}}
}

func (s *Service) Recover(ctx context.Context) error {
	recovered, err := s.store.RecoverStaleDeliveries(ctx, time.Now().UTC().Add(-60*time.Second))
	if err != nil {
		return fmt.Errorf("recover stale Desktop deliveries: %w", err)
	}
	if recovered > 0 {
		s.log.Warn("uncertain Desktop deliveries recovered", "count", recovered)
	}
	return nil
}

func (s *Service) RunMonitor(ctx context.Context) {
	s.refreshAndReconcile(ctx)
	ticker := time.NewTicker(s.cfg.PollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshAndReconcile(ctx)
		}
	}
}

func (s *Service) refreshAndReconcile(ctx context.Context) {
	if _, err := s.store.RecoverStaleDeliveries(ctx, time.Now().UTC().Add(-60*time.Second)); err != nil {
		s.log.Warn("stale Desktop delivery recovery failed", "error", err)
	}

	snap, err := s.readQuota(ctx)
	if err != nil {
		s.log.Warn("quota refresh failed", "error", err)
		return
	}
	if err := s.store.SaveQuota(ctx, snap); err != nil {
		s.log.Warn("save quota failed", "error", err)
		return
	}
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		return
	}
	for _, t := range tasks {
		_ = s.store.UpdateTaskQuota(ctx, t.ThreadID, snap)
	}

	if snap.SoftPause {
		running, _ := s.store.ListStates(ctx, domain.StateRunning)
		reason := snap.PauseReason
		if reason == "" {
			reason = "quota threshold"
		}
		for _, t := range running {
			if _, err := s.store.Transition(ctx, t.ThreadID, domain.StatePauseRequested, reason); err != nil {
				continue
			}
			_, _ = s.store.EnqueueAction(ctx, "pause_notice", t.ThreadID, pauseMessage(snap))
		}
		return
	}

	if s.policy.CanResume(snap) {
		pending, _ := s.store.ListStates(ctx, domain.StatePauseRequested)
		for _, t := range pending {
			_, err := s.store.Transition(ctx, t.ThreadID, domain.StateRunning, "quota recovered before cooperative pause completed")
			if err != nil {
				s.log.Warn("restore pause-requested task", "thread", t.ThreadID, "error", err)
			}
		}

		paused, _ := s.store.ListStates(ctx, domain.StatePausedQuota)
		for _, t := range paused {
			action, err := s.store.QueueResumeSafe(ctx, t.ThreadID, resumeMessage(t, snap))
			if err != nil {
				s.log.Error("queue Desktop resume", "thread", t.ThreadID, "error", err)
				continue
			}
			s.log.Info("Desktop resume queued", "thread", t.ThreadID, "actionId", action.ID, "fiveHourRemaining", snap.FiveHour.RemainingPercent, "weeklyRemaining", snap.Weekly.RemainingPercent)
		}

		s.ReconcileProjectQueues(ctx)
	}
}

// ReconcileProjectQueues starts at most one queued work item per project. A
// queue only advances after the latest managed task for that project completed
// successfully, and only while quota is healthy enough to resume work.
func (s *Service) ReconcileProjectQueues(ctx context.Context) {
	if !s.cfg.AutoDispatch {
		return
	}
	q, _, err := s.CurrentDecision(ctx)
	if err != nil || !s.policy.CanResume(q) || q.SoftPause {
		return
	}
	projects, err := s.store.ListProjects(ctx, false)
	if err != nil {
		s.log.Warn("list projects for queue reconciliation failed", "error", err)
		return
	}
	for _, p := range projects {
		blocking, err := s.store.ProjectHasBlockingTask(ctx, p.ID)
		if err != nil || blocking {
			continue
		}
		active, err := s.store.ProjectHasActiveManagedTask(ctx, p.ID)
		if err != nil || active {
			continue
		}
		item, err := s.store.NextQueuedProjectTask(ctx, p.ID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			s.log.Warn("read next queued project task failed", "project", p.ID, "error", err)
			continue
		}
		threadID, err := s.store.ProjectDispatchThread(ctx, p.ID)
		if err != nil {
			// No completed Desktop thread yet, or the latest project task ended in
			// a non-success terminal state. Keep the work item queued.
			continue
		}
		action, err := s.store.QueueProjectTaskDispatch(ctx, item.ID, threadID, queuedTaskMessage(p, item))
		if err != nil {
			s.log.Warn("queue project task dispatch failed", "project", p.ID, "projectTask", item.ID, "error", err)
			continue
		}
		s.log.Info("project task dispatch queued", "project", p.ID, "projectTask", item.ID, "thread", threadID, "actionId", action.ID)
	}
}

func queuedTaskMessage(p domain.Project, item domain.ProjectTask) string {
	var b strings.Builder
	b.WriteString("[Codex Task Queue] The previous project task completed. Start the next queued task now.\n\n")
	b.WriteString("Project: ")
	b.WriteString(p.Name)
	b.WriteString("\nObjective: ")
	b.WriteString(item.Objective)
	if strings.TrimSpace(item.Details) != "" {
		b.WriteString("\n\nDetails:\n")
		b.WriteString(item.Details)
	}
	b.WriteString("\n\nTreat this as a new managed task in the same project. Call desktop_quota_guard.desktop_task_register with this objective and the current workspace, then execute the work normally. When finished, call desktop_quota_guard.task_complete so the next queued task can start.")
	return b.String()
}

func (s *Service) readQuota(ctx context.Context) (quota.Snapshot, error) {
	c := codexquota.New(s.cfg.CodexCommand, s.cfg.RequestTimeout(), s.log)
	if err := c.Start(ctx); err != nil {
		return quota.Snapshot{}, err
	}
	defer c.Close()
	acct, err := c.ReadAccount(ctx)
	if err != nil {
		return quota.Snapshot{}, err
	}
	if acct.Account == nil || acct.Account.Type != "chatgpt" {
		return quota.Snapshot{}, fmt.Errorf("Codex is not signed in with ChatGPT")
	}
	r, err := c.ReadRateLimits(ctx)
	if err != nil {
		return quota.Snapshot{}, err
	}
	return quota.FromRateLimits(r, s.policy), nil
}

func pauseMessage(q quota.Snapshot) string {
	return fmt.Sprintf(
		"[Desktop Quota Guard] Quota is low "+
			"(5h=%s, weekly=%s, effective=%.0f%%, reason=%s). "+
			"Stop starting new work. Reach the next safe boundary, "+
			"call desktop_quota_guard.task_checkpoint, "+
			"then call desktop_quota_guard.task_mark_paused "+
			"and finish this turn.",
		windowRemaining(q.FiveHour),
		windowRemaining(q.Weekly),
		q.RemainingPercent,
		q.PauseReason,
	)
}

func resumeMessage(t domain.Task, q quota.Snapshot) string {
	extra := ""
	if t.Checkpoint != "" {
		extra = " Saved checkpoint: " + t.Checkpoint
	}
	return fmt.Sprintf(
		"[Desktop Quota Guard] Quota is available again "+
			"(5h=%s, weekly=%s). "+
			"Continue the previously paused task from the saved state.%s "+
			"Before another long phase, call desktop_quota_guard.quota_check.",
		windowRemaining(q.FiveHour),
		windowRemaining(q.Weekly),
		extra,
	)
}

func windowRemaining(w quota.Window) string {
	if !w.Available {
		return "unavailable"
	}
	return fmt.Sprintf("%.0f%%", w.RemainingPercent)
}

func (s *Service) CurrentDecision(ctx context.Context) (quota.Snapshot, quota.Decision, error) {
	q, err := s.store.LatestQuota(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return quota.Snapshot{}, quota.Decision{Action: "unknown", Reason: "quota has not been sampled yet"}, nil
		}
		return q, quota.Decision{}, err
	}
	return q, s.policy.Decide(q), nil
}

func (s *Service) DebugJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
