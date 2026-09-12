package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/processjob"
)

const systemControlHeader = "X-CDQG-Control"
const restartControlHeader = systemControlHeader

const (
	companionIDHeader      = "X-CDQG-Companion-ID"
	companionStateHeader   = "X-CDQG-Companion-State"
	companionHostPIDHeader = "X-CDQG-Desktop-Host-PID"
)

var launchRestartChildFn = launchRestartChild
var processAliveFn = processjob.ProcessAlive

type companionRegistry struct {
	mu                  sync.Mutex
	seen                bool
	leases              map[string]time.Time
	hostPIDs            map[int]struct{}
	idleGeneration      uint64
	hostWatchGeneration uint64
	shutdownOnce        sync.Once
}

var companionRegistries sync.Map

func registryForServer(s *Server) *companionRegistry {
	candidate := &companionRegistry{
		leases:   make(map[string]time.Time),
		hostPIDs: make(map[int]struct{}),
	}
	actual, _ := companionRegistries.LoadOrStore(s, candidate)
	return actual.(*companionRegistry)
}

func (s *Server) systemRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	switch r.Header.Get(systemControlHeader) {
	case "restart":
		s.handleSystemRestart(w)
	case "shutdown":
		s.handleSystemShutdown(w)
	case "companion":
		s.handleCompanionLifecycle(w, r)
	default:
		httpErr(w, http.StatusForbidden, errors.New("system control header required"))
	}
}

func (s *Server) handleSystemRestart(w http.ResponseWriter) {
	configPath := s.service.cfg.ConfigPath()
	if _, err := os.Stat(configPath); err != nil {
		configPath = ""
	}
	targetCfg, err := config.Load(configPath)
	if err != nil {
		httpErr(w, http.StatusConflict, fmt.Errorf("saved config is not restartable: %w", err))
		return
	}
	if targetCfg.ListenAddr != s.service.cfg.ListenAddr {
		listener, err := net.Listen("tcp", targetCfg.ListenAddr)
		if err != nil {
			httpErr(w, http.StatusConflict, fmt.Errorf("new listen address %s is unavailable: %w", targetCfg.ListenAddr, err))
			return
		}
		_ = listener.Close()
	}

	if err := launchRestartChildFn(configPath, s.service.cfg.BaseURL()+"/healthz"); err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}

	jsonOut(w, http.StatusAccepted, map[string]any{
		"ok":         true,
		"restarting": true,
		"targetURL":  targetCfg.BaseURL(),
	})
	flushResponse(w)
	s.scheduleDaemonShutdown("restart")
}

func (s *Server) handleSystemShutdown(w http.ResponseWriter) {
	jsonOut(w, http.StatusAccepted, map[string]any{
		"ok":       true,
		"stopping": true,
	})
	flushResponse(w)
	s.scheduleDaemonShutdown("control request")
}

func (s *Server) handleCompanionLifecycle(w http.ResponseWriter, r *http.Request) {
	instanceID := strings.TrimSpace(r.Header.Get(companionIDHeader))
	stateName := strings.ToLower(strings.TrimSpace(r.Header.Get(companionStateHeader)))
	if instanceID == "" {
		httpErr(w, http.StatusBadRequest, errors.New("companion instance ID required"))
		return
	}
	if stateName == "" {
		stateName = "heartbeat"
	}

	hostPID := 0
	if raw := strings.TrimSpace(r.Header.Get(companionHostPIDHeader)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			s.log.Warn("ignoring invalid Codex Desktop host PID from companion", "value", raw)
		} else {
			hostPID = parsed
		}
	}

	registry := registryForServer(s)
	switch stateName {
	case "heartbeat":
		now := time.Now().UTC()
		registry.mu.Lock()
		registry.seen = true
		registry.leases[instanceID] = now
		if hostPID > 0 {
			registry.hostPIDs[hostPID] = struct{}{}
		}
		// Every heartbeat invalidates any pending idle shutdown or host watcher
		// that may have been armed by a previous short-lived MCP companion.
		registry.idleGeneration++
		registry.mu.Unlock()

		timeout := s.companionLeaseTimeout()
		time.AfterFunc(timeout, func() {
			s.expireCompanionLease(registry, instanceID, now, timeout)
		})
		jsonOut(w, http.StatusOK, map[string]any{"ok": true})
	case "disconnect":
		registry.mu.Lock()
		registry.seen = true
		if hostPID > 0 {
			registry.hostPIDs[hostPID] = struct{}{}
		}
		delete(registry.leases, instanceID)
		registry.idleGeneration++
		generation := registry.idleGeneration
		shouldArmIdleShutdown := len(registry.leases) == 0
		registry.mu.Unlock()

		jsonOut(w, http.StatusOK, map[string]any{"ok": true})
		flushResponse(w)
		if shouldArmIdleShutdown {
			// Codex Desktop may recycle the MCP companion during startup, tool
			// discovery, cancellation, or delivery. Give a replacement one full
			// lease window, then verify the Desktop host itself before stopping.
			grace := s.companionLeaseTimeout()
			s.log.Info("last Codex Desktop companion disconnected; waiting for replacement", "grace", grace)
			s.scheduleCompanionIdleShutdown(registry, generation, grace, "last Codex Desktop companion disconnected")
		}
	default:
		httpErr(w, http.StatusBadRequest, fmt.Errorf("unknown companion state %q", stateName))
	}
}

func (s *Server) companionLeaseTimeout() time.Duration {
	interval := s.service.cfg.CompanionPollInterval()
	if interval <= 0 {
		interval = 5 * time.Second
	}
	timeout := 3 * interval
	if timeout < 10*time.Second {
		timeout = 10 * time.Second
	}
	return timeout
}

func (s *Server) expireCompanionLease(registry *companionRegistry, instanceID string, observedAt time.Time, timeout time.Duration) {
	registry.mu.Lock()
	last, ok := registry.leases[instanceID]
	if !ok || !last.Equal(observedAt) {
		registry.mu.Unlock()
		return
	}
	if time.Since(last) < timeout {
		registry.mu.Unlock()
		return
	}
	delete(registry.leases, instanceID)
	registry.idleGeneration++
	generation := registry.idleGeneration
	shouldStop := registry.seen && len(registry.leases) == 0
	registry.mu.Unlock()

	if shouldStop {
		s.scheduleCompanionIdleShutdown(registry, generation, 0, "Codex Desktop companion heartbeat expired")
	}
}

func companionIdleAtGeneration(registry *companionRegistry, generation uint64) bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.seen && len(registry.leases) == 0 && registry.idleGeneration == generation
}

// desktopHostStatus returns whether a Desktop host identity has been tracked,
// whether the daemon should conservatively stay alive, and one confirmed live
// PID when available. Query errors keep the daemon alive and are retried; a
// transient permission failure must never be interpreted as Desktop exit.
func (s *Server) desktopHostStatus(registry *companionRegistry) (tracked bool, keepAlive bool, livePID int) {
	registry.mu.Lock()
	pids := make([]int, 0, len(registry.hostPIDs))
	for pid := range registry.hostPIDs {
		pids = append(pids, pid)
	}
	registry.mu.Unlock()
	if len(pids) == 0 {
		return false, false, 0
	}

	dead := make([]int, 0, len(pids))
	queryFailed := false
	for _, pid := range pids {
		alive, err := processAliveFn(pid)
		if err != nil {
			queryFailed = true
			s.log.Warn("cannot query Codex Desktop host liveness; keeping daemon alive", "pid", pid, "error", err)
			continue
		}
		if alive {
			return true, true, pid
		}
		dead = append(dead, pid)
	}

	if len(dead) > 0 {
		registry.mu.Lock()
		for _, pid := range dead {
			delete(registry.hostPIDs, pid)
		}
		registry.mu.Unlock()
	}
	if queryFailed {
		return true, true, 0
	}
	return true, false, 0
}

func markDesktopHostWatch(registry *companionRegistry, generation uint64) bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if !registry.seen || len(registry.leases) != 0 || registry.idleGeneration != generation {
		return false
	}
	if registry.hostWatchGeneration == generation {
		return false
	}
	registry.hostWatchGeneration = generation
	return true
}

func (s *Server) scheduleCompanionIdleShutdown(registry *companionRegistry, generation uint64, delay time.Duration, reason string) {
	time.AfterFunc(delay, func() {
		// Give a just-arriving replacement heartbeat a final opportunity to
		// invalidate this generation before shutdown becomes irreversible.
		time.Sleep(75 * time.Millisecond)
		if !companionIdleAtGeneration(registry, generation) {
			return
		}

		tracked, keepAlive, livePID := s.desktopHostStatus(registry)
		if tracked && keepAlive {
			if markDesktopHostWatch(registry, generation) {
				if livePID > 0 {
					s.log.Info("Codex Desktop host still running; keeping daemon alive while companions are absent", "desktopHostPid", livePID, "reason", reason)
				} else {
					s.log.Warn("Codex Desktop host liveness is temporarily unknown; keeping daemon alive while companions are absent", "reason", reason)
				}
				go s.watchDesktopHostExit(registry, generation, reason)
			}
			return
		}

		// Older companions do not send host identity. Preserve the previous
		// fallback for them; with v0.2.5 companions, shutdown only happens after
		// the tracked Desktop host is confirmed dead.
		s.shutdownForCompanionIdle(registry, reason)
	})
}

func (s *Server) watchDesktopHostExit(registry *companionRegistry, generation uint64, reason string) {
	interval := s.companionLeaseTimeout()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if !companionIdleAtGeneration(registry, generation) {
			return
		}
		tracked, keepAlive, _ := s.desktopHostStatus(registry)
		if tracked && keepAlive {
			continue
		}
		s.shutdownForCompanionIdle(registry, reason+"; Codex Desktop host exited")
		return
	}
}

func (s *Server) shutdownForCompanionIdle(registry *companionRegistry, reason string) {
	registry.shutdownOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			s.log.Warn("daemon shutdown failed", "reason", reason, "error", err)
		} else {
			s.log.Info("daemon shutdown requested", "reason", reason)
		}
		companionRegistries.Delete(s)
	})
}

func (s *Server) scheduleDaemonShutdown(reason string) {
	registry := registryForServer(s)
	registry.shutdownOnce.Do(func() {
		go func() {
			// Give the accepted response a moment to leave the socket before
			// gracefully closing listeners and SQLite/log handles.
			time.Sleep(75 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.Shutdown(ctx); err != nil {
				s.log.Warn("daemon shutdown failed", "reason", reason, "error", err)
			} else {
				s.log.Info("daemon shutdown requested", "reason", reason)
			}
			companionRegistries.Delete(s)
		}()
	})
}

func flushResponse(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
