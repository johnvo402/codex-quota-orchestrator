package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/desktop"
	"codex-desktop-quota-guard/internal/quota"
)

type Server struct {
	cfg    config.Config
	log    *slog.Logger
	daemon *daemonClient
	next   atomic.Int64
}

func New(cfg config.Config, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{cfg: cfg, log: log, daemon: newDaemonClient(cfg.BaseURL())}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Meta      map[string]any `json:"_meta,omitempty"`
}

func (s *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	go s.actionPump(ctx)
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	for scan.Scan() {
		var req request
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			continue
		}
		if req.Method == "notifications/initialized" || req.Method == "initialized" {
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		result, err := s.handle(ctx, req)
		resp := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID)}
		if err != nil {
			resp["error"] = map[string]any{"code": -32603, "message": err.Error()}
		} else {
			resp["result"] = result
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scan.Err()
}

func (s *Server) handle(ctx context.Context, req request) (any, error) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{"protocolVersion": p.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "desktop-quota-guard", "version": "0.2.0"}}, nil
	case "tools/list":
		return map[string]any{"tools": toolList()}, nil
	case "tools/call":
		var p callParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, err
		}
		return s.callTool(ctx, p)
	case "ping":
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("method not found: %s", req.Method)
	}
}

func toolList() []any {
	return []any{
		tool("desktop_task_register", "Register the current Codex Desktop task with the quota guard. Call near the beginning of substantial work.", map[string]any{"type": "object", "properties": map[string]any{"objective": map[string]any{"type": "string"}, "workspace": map[string]any{"type": "string"}}, "required": []string{"objective"}}),
		tool("quota_check", "Check the current Codex quota policy before a substantial new phase or at a safe boundary.", map[string]any{"type": "object", "properties": map[string]any{}}),
		tool("task_checkpoint", "Persist a compact checkpoint before pausing or after a meaningful milestone.", map[string]any{"type": "object", "properties": map[string]any{"summary": map[string]any{"type": "string"}, "pending": map[string]any{"type": "string"}, "lastTest": map[string]any{"type": "string"}}, "required": []string{"summary"}}),
		tool("task_mark_paused", "Mark the current Desktop task paused for quota. Call only after reaching a safe boundary; then finish the current turn.", map[string]any{"type": "object", "properties": map[string]any{"reason": map[string]any{"type": "string"}}}),
		tool("task_complete", "Mark the managed Desktop task complete.", map[string]any{"type": "object", "properties": map[string]any{"summary": map[string]any{"type": "string"}}}),
		tool("desktop_guard_status", "Show guard/daemon/native delivery status for the current Desktop task.", map[string]any{"type": "object", "properties": map[string]any{}}),
		tool("desktop_native_test_send", "Send a test message to a Codex Desktop thread through the native Desktop tools pipe. Debug only.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"targetThreadId": map[string]any{"type": "string", "description": "Destination Codex Desktop thread ID."},
				"message":        map[string]any{"type": "string", "description": "Optional test message."},
			},
			"required": []string{"targetThreadId"},
		}),
	}
}

func tool(name, desc string, schema any) any {
	return map[string]any{"name": name, "description": desc, "inputSchema": schema}
}

func (s *Server) callTool(ctx context.Context, p callParams) (any, error) {
	thread, turn := extractIdentity(p.Meta)
	if v := strArg(p.Arguments, "threadId"); thread == "" {
		thread = v
	}
	if thread == "" {
		return toolError("Codex did not provide threadId metadata. This tool must run inside a Codex Desktop task."), nil
	}

	switch p.Name {
	case "desktop_task_register":
		objective := strArg(p.Arguments, "objective")
		if objective == "" {
			objective = "Desktop task"
		}
		workspace := strArg(p.Arguments, "workspace")
		t, err := s.daemon.register(ctx, thread, turn, objective, workspace)
		if err != nil {
			return toolError("Guard daemon unavailable: " + err.Error()), nil
		}
		return toolOK(fmt.Sprintf("Task registered. thread=%s state=%s. Call quota_check before substantial new phases.", t.ThreadID, t.State), map[string]any{"task": t}), nil

	case "quota_check":
		q, d, err := s.daemon.quota(ctx)
		if err != nil {
			return toolError("Guard daemon unavailable: " + err.Error()), nil
		}
		text := fmt.Sprintf("%s; action=%s; reason=%s", describeQuota(q), d.Action, d.Reason)
		if d.Action == "pause" {
			text += ". Reach a safe boundary, checkpoint, call task_mark_paused, then finish this turn."
		}
		return toolOK(text, map[string]any{"quota": q, "decision": d}), nil

	case "task_checkpoint":
		if err := s.daemon.checkpoint(ctx, thread, strArg(p.Arguments, "summary"), strArg(p.Arguments, "pending"), strArg(p.Arguments, "lastTest")); err != nil {
			return toolError(err.Error()), nil
		}
		return toolOK("Checkpoint saved.", nil), nil

	case "task_mark_paused":
		if err := s.daemon.paused(ctx, thread, first(strArg(p.Arguments, "reason"), "quota")); err != nil {
			return toolError(err.Error()), nil
		}
		return toolOK("Task is PAUSED_QUOTA. Finish this turn now; the companion will resume this Desktop thread when quota recovers.", nil), nil

	case "task_complete":
		if err := s.daemon.complete(ctx, thread, strArg(p.Arguments, "summary")); err != nil {
			return toolError(err.Error()), nil
		}
		return toolOK("Task marked completed.", nil), nil

	case "desktop_guard_status":
		q, d, err := s.daemon.quota(ctx)
		if err != nil {
			return toolError(err.Error()), nil
		}
		relay, relayErr := desktop.LoadRelay(s.cfg.RelayPath())
		var sender desktop.NativeSender
		if relayErr == nil {
			sender = desktop.NewNativeSender(relay.ExecutorThreadID)
		}
		nativeConfigured := false
		nativeReachable := false
		nativeDescription := "relay not configured"
		nativeProbeError := ""
		var diagnostics any
		if sender != nil {
			nativeConfigured = sender.Available()
			nativeDescription = sender.Description()
			diagnostics = sender.Diagnostics()
			if nativeConfigured {
				probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				probeErr := sender.Probe(probeCtx)
				cancel()
				if probeErr == nil {
					nativeReachable = true
				} else {
					nativeProbeError = probeErr.Error()
				}
			}
		}
		return toolOK(
			fmt.Sprintf("%s; action=%s; nativeConfigured=%v; nativeReachable=%v; %s", describeQuota(q), d.Action, nativeConfigured, nativeReachable, nativeDescription),
			map[string]any{
				"quota": q, "decision": d,
				"nativeAvailable": nativeReachable, "nativeConfigured": nativeConfigured, "nativeReachable": nativeReachable,
				"nativeDescription": nativeDescription, "nativeProbeError": nativeProbeError, "nativeDiagnostics": diagnostics,
			},
		), nil

	case "desktop_native_test_send":
		targetThreadID, _ := p.Arguments["targetThreadId"].(string)
		targetThreadID = strings.TrimSpace(targetThreadID)
		if targetThreadID == "" {
			return toolError("targetThreadId is required"), nil
		}
		message, _ := p.Arguments["message"].(string)
		message = strings.TrimSpace(message)
		if message == "" {
			message = "[Desktop Quota Guard] Native delivery test succeeded. This message was sent through the Codex Desktop native tools pipe."
		}
		relay, err := desktop.LoadRelay(s.cfg.RelayPath())
		if err != nil {
			return toolError("relay unavailable: " + err.Error()), nil
		}
		sender := desktop.NewNativeSender(relay.ExecutorThreadID)
		if !sender.Available() {
			return toolError("native Desktop delivery unavailable: " + sender.Description()), nil
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = sender.Probe(probeCtx)
		cancel()
		if err != nil {
			return toolError("native Desktop probe failed: " + err.Error()), nil
		}
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = sender.SendMessage(sendCtx, targetThreadID, message)
		cancel()
		if err != nil {
			return toolError("native Desktop send failed: " + err.Error()), nil
		}
		return toolOK("Native Desktop test message sent successfully.", map[string]any{"success": true, "targetThreadId": targetThreadID, "native": sender.Diagnostics()}), nil

	default:
		return toolError("unknown tool " + p.Name), nil
	}
}

func (s *Server) actionPump(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.CompanionPollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.deliverPending(ctx)
		}
	}
}

func (s *Server) deliverPending(ctx context.Context) {
	relay, err := desktop.LoadRelay(s.cfg.RelayPath())
	if err != nil {
		return
	}
	sender := desktop.NewNativeSender(relay.ExecutorThreadID)
	if !sender.Available() {
		return
	}
	actions, err := s.daemon.actions(ctx)
	if err != nil {
		return
	}

	for _, action := range actions {
		// Probe happens before claim. If Desktop is unavailable, the action stays
		// pending and can be attempted safely on a later poll.
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		probeErr := sender.Probe(probeCtx)
		cancel()
		if probeErr != nil {
			s.log.Warn("Desktop native probe failed", "action", action.ID, "error", probeErr)
			continue
		}

		// Claim atomically before the external side effect. A second companion
		// will receive conflict and must not send the same action.
		if err := s.daemon.claim(ctx, action.ID); err != nil {
			s.log.Debug("Desktop action already claimed", "action", action.ID, "error", err)
			continue
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
			continue
		}

		if err := s.daemon.ack(ctx, action.ID, true, ""); err != nil {
			// The message was definitely accepted according to the native response,
			// but durable acknowledgement failed. Do not resend it. The action remains
			// delivering and startup/periodic recovery will move it to NEEDS_REVIEW.
			s.log.Error("resume delivered but durable ack failed; leaving delivery for recovery", "action", action.ID, "error", err)
			continue
		}

		s.log.Info("Desktop action delivered", "action", action.ID, "kind", action.Kind, "thread", action.ThreadID)
	}
}

func extractIdentity(meta map[string]any) (thread, turn string) {
	if meta == nil {
		return "", ""
	}
	thread = asString(meta["threadId"])
	turn = asString(meta["turnId"])
	raw := meta["x-codex-turn-metadata"]
	var m map[string]any
	switch v := raw.(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &m)
	case map[string]any:
		m = v
	}
	if thread == "" {
		thread = asString(m["thread_id"])
	}
	if turn == "" {
		turn = asString(m["turn_id"])
	}
	return
}

func asString(v any) string { s, _ := v.(string); return s }

func strArg(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(asString(m[k]))
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func toolOK(text string, structured any) any {
	r := map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": false}
	if structured != nil {
		r["structuredContent"] = structured
	}
	return r
}

func toolError(text string) any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": true}
}

var _ = errors.New
var _ = os.Stderr

func describeQuota(q quota.Snapshot) string {
	return fmt.Sprintf("5h=%s; weekly=%s; effective=%.0f%%", describeWindow(q.FiveHour), describeWindow(q.Weekly), q.RemainingPercent)
}

func describeWindow(w quota.Window) string {
	if !w.Available {
		return "unavailable"
	}
	return fmt.Sprintf("%.0f%%", w.RemainingPercent)
}
