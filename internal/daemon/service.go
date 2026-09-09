package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
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
		running, _ := s.store.ListStates(
			ctx,
			domain.StateRunning,
		)

		reason := snap.PauseReason

		if reason == "" {
			reason = "quota threshold"
		}

		for _, t := range running {
			if _, err := s.store.Transition(
				ctx,
				t.ThreadID,
				domain.StatePauseRequested,
				reason,
			); err != nil {
				continue
			}

			msg := pauseMessage(snap)

			_, _ = s.store.EnqueueAction(
				ctx,
				"pause_notice",
				t.ThreadID,
				msg,
			)
		}

		return
	}
	if s.policy.CanResume(snap) {
		pending, _ := s.store.ListStates(ctx, domain.StatePauseRequested)
		for _, t := range pending {
			_, _ = s.store.Transition(ctx, t.ThreadID, domain.StateRunning, "quota recovered before pause")
		}
		paused, _ := s.store.ListStates(ctx, domain.StatePausedQuota)
		for _, t := range paused {
			if _, err := s.store.Transition(ctx, t.ThreadID, domain.StateResumeQueued, "quota recovered"); err != nil {
				continue
			}
			_, _ = s.store.EnqueueAction(ctx, "resume", t.ThreadID, resumeMessage(t, snap))
		}
	}
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

func resumeMessage(
	t domain.Task,
	q quota.Snapshot,
) string {
	extra := ""

	if t.Checkpoint != "" {
		extra = " Saved checkpoint: " + t.Checkpoint
	}

	return fmt.Sprintf(
		"[Desktop Quota Guard] Quota is available again "+
			"(5h=%s, weekly=%s). "+
			"Continue the previously paused task from the saved state.%s "+
			"Before another long phase, "+
			"call desktop_quota_guard.quota_check.",
		windowRemaining(q.FiveHour),
		windowRemaining(q.Weekly),
		extra,
	)
}

func windowRemaining(w quota.Window) string {
	if !w.Available {
		return "unavailable"
	}

	return fmt.Sprintf(
		"%.0f%%",
		w.RemainingPercent,
	)
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
