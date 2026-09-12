package daemon

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codex-desktop-quota-guard/internal/config"
)

func TestCompanionHeartbeatTracksDesktopHostPID(t *testing.T) {
	s := &Server{service: &Service{cfg: config.Default()}, log: slog.Default(), http: &http.Server{}}
	t.Cleanup(func() { companionRegistries.Delete(s) })

	r := httptest.NewRequest(http.MethodPost, "/v1/system/restart", nil)
	r.Header.Set(systemControlHeader, "companion")
	r.Header.Set(companionIDHeader, "companion-a")
	r.Header.Set(companionStateHeader, "heartbeat")
	r.Header.Set(companionHostPIDHeader, "4242")
	w := httptest.NewRecorder()
	s.systemRestart(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	registry := registryForServer(s)
	registry.mu.Lock()
	_, ok := registry.hostPIDs[4242]
	registry.mu.Unlock()
	if !ok {
		t.Fatal("expected Desktop host PID to be tracked")
	}
}

func TestDesktopHostStatusKeepsDaemonAliveForLiveHost(t *testing.T) {
	old := processAliveFn
	processAliveFn = func(pid int) (bool, error) { return pid == 4242, nil }
	t.Cleanup(func() { processAliveFn = old })

	s := &Server{service: &Service{cfg: config.Default()}, log: slog.Default(), http: &http.Server{}}
	registry := &companionRegistry{leases: map[string]time.Time{}, hostPIDs: map[int]struct{}{4242: {}}}
	tracked, keepAlive, livePID := s.desktopHostStatus(registry)
	if !tracked || !keepAlive || livePID != 4242 {
		t.Fatalf("unexpected host status: tracked=%v keepAlive=%v livePID=%d", tracked, keepAlive, livePID)
	}
}

func TestDesktopHostStatusAllowsShutdownAfterHostExit(t *testing.T) {
	old := processAliveFn
	processAliveFn = func(pid int) (bool, error) { return false, nil }
	t.Cleanup(func() { processAliveFn = old })

	s := &Server{service: &Service{cfg: config.Default()}, log: slog.Default(), http: &http.Server{}}
	registry := &companionRegistry{leases: map[string]time.Time{}, hostPIDs: map[int]struct{}{4242: {}}}
	tracked, keepAlive, livePID := s.desktopHostStatus(registry)
	if !tracked || keepAlive || livePID != 0 {
		t.Fatalf("unexpected host status: tracked=%v keepAlive=%v livePID=%d", tracked, keepAlive, livePID)
	}
	if len(registry.hostPIDs) != 0 {
		t.Fatal("confirmed-dead Desktop host PID should be retired")
	}
}

func TestDesktopHostStatusIsConservativeOnProbeError(t *testing.T) {
	old := processAliveFn
	processAliveFn = func(pid int) (bool, error) { return false, errors.New("access denied") }
	t.Cleanup(func() { processAliveFn = old })

	s := &Server{service: &Service{cfg: config.Default()}, log: slog.Default(), http: &http.Server{}}
	registry := &companionRegistry{leases: map[string]time.Time{}, hostPIDs: map[int]struct{}{4242: {}}}
	tracked, keepAlive, livePID := s.desktopHostStatus(registry)
	if !tracked || !keepAlive || livePID != 0 {
		t.Fatalf("probe errors must keep daemon alive: tracked=%v keepAlive=%v livePID=%d", tracked, keepAlive, livePID)
	}
}
