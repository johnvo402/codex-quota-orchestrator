package mcpserver

import (
	"context"
	"time"

	"codex-desktop-quota-guard/internal/desktop"
	"codex-desktop-quota-guard/internal/store"
)

func (s *Server) deliverClaimedAction(ctx context.Context, sender desktop.NativeSender, action store.Action) {
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
