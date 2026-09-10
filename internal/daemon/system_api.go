package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"codex-desktop-quota-guard/internal/config"
)

const systemControlHeader = "X-CDQG-Control"
const restartControlHeader = systemControlHeader

const (
	companionIDHeader    = "X-CDQG-Companion-ID"
	companionStateHeader = "X-CDQG-Companion-State"
)

var launchRestartChildFn = launchRestartChild

type companionRegistry struct {
	mu           sync.Mutex
	seen         bool
	leases       map[string]time.Time
	shutdownOnce sync.Once
}

var companionRegistries sync.Map

func registryForServer(s *Server) *companionRegistry {
	candidate := &companionRegistry{leases: make(map[string]time.Time)}
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

	registry := registryForServer(s)
	switch stateName {
	case "heartbeat":
		now := time.Now().UTC()
		registry.mu.Lock()
		registry.seen = true
		registry.leases[instanceID] = now
		registry.mu.Unlock()

		timeout := s.companionLeaseTimeout()
		time.AfterFunc(timeout, func() {
			s.expireCompanionLease(registry, instanceID, now, timeout)
		})
		jsonOut(w, http.StatusOK, map[string]any{"ok": true})
	case "disconnect":
		registry.mu.Lock()
		registry.seen = true
		delete(registry.leases, instanceID)
		shouldStop := len(registry.leases) == 0
		registry.mu.Unlock()

		jsonOut(w, http.StatusOK, map[string]any{"ok": true})
		flushResponse(w)
		if shouldStop {
			s.scheduleDaemonShutdown("last Codex Desktop companion disconnected")
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
	shouldStop := registry.seen && len(registry.leases) == 0
	registry.mu.Unlock()

	if shouldStop {
		s.scheduleDaemonShutdown("Codex Desktop companion heartbeat expired")
	}
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
