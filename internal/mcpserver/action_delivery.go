package mcpserver

import (
	"context"
	"time"

	"codex-desktop-quota-guard/internal/desktop"
	"codex-desktop-quota-guard/internal/store"
)

func (s *Server) deliverClaimedAction(ctx context.Context, sender desktop.NativeSender, action store.Action) {
	if action.Kind == store.ActionKindStop {
		// Desktop Stop can perform an accessibility InvokePattern attempt, verify
		// the backend turn through read_thread, and then make one guarded physical
		// click fallback if the exact same turn is still inProgress. Give that
		// bounded verification flow enough time to finish before classifying it.
		stopCtx, cancel := context.WithTimeout(ctx, 24*time.Second)
		attempted, err := desktop.NavigateAndStop(stopCtx, sender, action.ThreadID, action.Message)
		cancel()
		if err != nil {
			if attempted {
				s.log.Warn("Codex Desktop Stop outcome uncertain", "action", action.ID, "thread", action.ThreadID, "error", err)
				if markErr := s.daemon.uncertain(ctx, action.ID, "Desktop Stop was attempted but the backend turn was not confirmed interrupted: "+err.Error()); markErr != nil {
					s.log.Error("mark Desktop Stop uncertain failed", "action", action.ID, "error", markErr)
				}
				return
			}

			// Navigation, exact-turn verification, or UI discovery failed before
			// any Stop side effect was attempted. Keep the managed task RUNNING so
			// the user can inspect Desktop and retry.
			s.log.Warn("Codex Desktop Stop failed before invocation", "action", action.ID, "thread", action.ThreadID, "error", err)
			if ackErr := s.daemon.ack(ctx, action.ID, false, err.Error()); ackErr != nil {
				s.log.Error("record failed Desktop Stop action failed", "action", action.ID, "error", ackErr)
			}
			return
		}

		if err := s.daemon.ack(ctx, action.ID, true, ""); err != nil {
			// The backend turn was confirmed interrupted, but durable acknowledgement
			// did not complete. Do not attempt Stop again: restart recovery will move
			// the delivering action to NEEDS_REVIEW.
			s.log.Error("Desktop Stop verified but durable ack failed; leaving delivery for recovery", "action", action.ID, "thread", action.ThreadID, "error", err)
			return
		}
		s.log.Info("Codex Desktop Stop verified", "action", action.ID, "thread", action.ThreadID, "turn", action.Message)
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err := sender.SendMessage(sendCtx, action.ThreadID, action.Message)
	cancel()
	if err != nil {
		s.log.Warn("Desktop native delivery outcome uncertain", "action", action.ID, "thread", action.ThreadID, "error", err)
		// Once SendMessage has been attempted, retrying automatically is unsafe:
		// Desktop may have accepted the message while the response was lost.
		if markErr := s.daemon.uncertain(ctx, action.ID, "native send was attempted but the outcome could not be confirmed: "+err.Error()); markErr != nil {
			s.log.Error("mark Desktop action uncertain failed", "action", action.ID, "error", markErr)
		}
		return
	}

	if err := s.daemon.ack(ctx, action.ID, true, ""); err != nil {
		// The message was definitely accepted according to the native response,
		// but durable acknowledgement failed. Do not resend it. The action remains
		// delivering and startup/periodic recovery will move it to NEEDS_REVIEW.
		s.log.Error("Desktop action delivered but durable ack failed; leaving delivery for recovery", "action", action.ID, "error", err)
		return
	}

	s.log.Info("Desktop action delivered", "action", action.ID, "kind", action.Kind, "thread", action.ThreadID)
}
