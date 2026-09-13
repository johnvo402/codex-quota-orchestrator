package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"codex-desktop-quota-guard/internal/desktop"
	"codex-desktop-quota-guard/internal/store"
)

const desktopDeliveryPollInterval = time.Second

var newDesktopNativeSender = desktop.NewNativeSender

type deliveryRuntime struct {
	wake chan struct{}
}

var deliveryRuntimes sync.Map

func deliveryRuntimeForService(s *Service) *deliveryRuntime {
	candidate := &deliveryRuntime{wake: make(chan struct{}, 1)}
	actual, _ := deliveryRuntimes.LoadOrStore(s, candidate)
	return actual.(*deliveryRuntime)
}

// RegisterDesktopNativePipe gives the long-lived daemon the native Codex
// Desktop pipe discovered by a short-lived MCP companion. The daemon process
// then owns action delivery, so companion recycling cannot strand pending work.
func (s *Service) RegisterDesktopNativePipe(raw string) error {
	pipe := strings.TrimSpace(raw)
	pipe = strings.ReplaceAll(pipe, "/", `\`)
	if pipe == "" {
		return nil
	}
	if len(pipe) > 1024 || strings.ContainsAny(pipe, "\r\n\x00") {
		return errors.New("invalid Desktop native pipe path")
	}
	if !strings.HasPrefix(strings.ToLower(pipe), `\\.\pipe\`) {
		return fmt.Errorf("Desktop native pipe must be local: %q", pipe)
	}

	// NewNativeSender prefers CODEX_APP_TOOLS_PIPE_PATH, so update both the
	// normal Desktop variable and the explicit guard override. This only changes
	// the daemon process environment; it never mutates Codex Desktop itself.
	if err := os.Setenv("CODEX_APP_TOOLS_PIPE_PATH", pipe); err != nil {
		return err
	}
	if err := os.Setenv("CDQG_DESKTOP_NATIVE_PIPE", pipe); err != nil {
		return err
	}
	s.WakeDesktopDelivery()
	return nil
}

func (s *Service) WakeDesktopDelivery() {
	runtime := deliveryRuntimeForService(s)
	select {
	case runtime.wake <- struct{}{}:
	default:
	}
}

// RunDeliveryWorker is the single durable owner of outbound Desktop actions.
// The daemon outlives transient MCP companions, so pending actions continue to
// drain even when Codex recycles the companion immediately after task_complete.
func (s *Service) RunDeliveryWorker(ctx context.Context) {
	runtime := deliveryRuntimeForService(s)
	defer deliveryRuntimes.Delete(s)

	// Repair persisted mismatches and drain restart-safe pending work before the
	// first timer tick.
	s.drainDesktopActions(ctx)

	ticker := time.NewTicker(desktopDeliveryPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-runtime.wake:
			s.drainDesktopActions(ctx)
		case <-ticker.C:
			s.drainDesktopActions(ctx)
		}
	}
}

func (s *Service) drainDesktopActions(ctx context.Context) {
	if repaired, err := s.store.ReconcileProjectTaskDispatches(ctx); err != nil {
		s.log.Warn("project dispatch reconciliation failed", "error", err)
	} else if repaired > 0 {
		s.log.Warn("orphaned project dispatch states reconciled", "count", repaired)
	}

	for batch := 0; batch < 10; batch++ {
		actions, err := s.store.PendingActions(ctx, 20)
		if err != nil {
			s.log.Warn("list pending Desktop actions failed", "error", err)
			return
		}
		if len(actions) == 0 {
			return
		}

		deliverable := make([]store.Action, 0, len(actions))
		for _, action := range actions {
			ok, err := s.store.PendingActionDeliverable(ctx, action.ID)
			if err != nil {
				s.log.Warn("validate pending Desktop action failed", "action", action.ID, "error", err)
				continue
			}
			if ok {
				deliverable = append(deliverable, action)
			} else {
				s.log.Info("stale pending Desktop action skipped", "action", action.ID, "kind", action.Kind)
			}
		}

		if len(deliverable) == 0 {
			if len(actions) < 20 {
				return
			}
			continue
		}

		relay, err := desktop.LoadRelay(s.cfg.RelayPath())
		if err != nil {
			s.log.Debug("pending Desktop actions blocked: relay unavailable", "count", len(deliverable), "error", err)
			return
		}
		sender := newDesktopNativeSender(relay.ExecutorThreadID)
		if !sender.Available() {
			s.log.Debug("pending Desktop actions blocked: native sender unavailable", "count", len(deliverable), "native", sender.Description())
			return
		}

		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		probeErr := sender.Probe(probeCtx)
		cancel()
		if probeErr != nil {
			s.log.Warn("Desktop native probe failed; pending actions remain retryable", "count", len(deliverable), "error", probeErr)
			return
		}

		for _, action := range deliverable {
			if ctx.Err() != nil {
				return
			}
			if err := s.store.ClaimAction(ctx, action.ID); err != nil {
				if !errors.Is(err, store.ErrActionNotPending) {
					s.log.Warn("claim Desktop action failed", "action", action.ID, "error", err)
				}
				continue
			}

			sendCtx, sendCancel := context.WithTimeout(ctx, 20*time.Second)
			err := sender.SendMessage(sendCtx, action.ThreadID, action.Message)
			sendCancel()
			if err != nil {
				s.log.Warn("Desktop native delivery outcome uncertain", "action", action.ID, "kind", action.Kind, "thread", action.ThreadID, "error", err)
				reason := "native send was attempted but the outcome could not be confirmed: " + err.Error()
				if markErr := s.store.MarkActionUncertain(ctx, action.ID, reason); markErr != nil {
					s.log.Error("mark Desktop action uncertain failed", "action", action.ID, "error", markErr)
				}
				continue
			}

			if err := s.store.CompleteClaimedAction(ctx, action.ID, true, ""); err != nil {
				// The native response confirmed acceptance. Never resend merely
				// because the durable acknowledgement failed; stale-delivery
				// recovery will move it to manual review.
				s.log.Error("Desktop action delivered but durable ack failed; leaving delivery for recovery", "action", action.ID, "error", err)
				continue
			}
			s.log.Info("Desktop action delivered", "action", action.ID, "kind", action.Kind, "thread", action.ThreadID)
		}

		if len(actions) < 20 {
			return
		}
	}
}
