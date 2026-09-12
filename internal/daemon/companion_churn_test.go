package daemon

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"codex-desktop-quota-guard/internal/config"
)

func TestCompanionReplacementInvalidatesPendingIdleShutdown(t *testing.T) {
	s := &Server{
		service: &Service{cfg: config.Default()},
		log:     slog.Default(),
		http:    &http.Server{},
	}

	signal := func(id, state string) {
		r := httptest.NewRequest(http.MethodPost, "/v1/system/restart", nil)
		r.Header.Set(systemControlHeader, "companion")
		r.Header.Set(companionIDHeader, id)
		r.Header.Set(companionStateHeader, state)
		w := httptest.NewRecorder()
		s.systemRestart(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s: expected 200, got %d: %s", state, id, w.Code, w.Body.String())
		}
	}

	signal("companion-a", "heartbeat")
	signal("companion-a", "disconnect")

	registry := registryForServer(s)
	registry.mu.Lock()
	disconnectGeneration := registry.idleGeneration
	leasesAfterDisconnect := len(registry.leases)
	registry.mu.Unlock()
	if leasesAfterDisconnect != 0 {
		t.Fatalf("expected no leases immediately after disconnect, got %d", leasesAfterDisconnect)
	}
	if !companionIdleAtGeneration(registry, disconnectGeneration) {
		t.Fatal("disconnect generation should initially be eligible for idle shutdown")
	}

	// Codex Desktop may replace an MCP companion a few seconds after the old
	// stdio process exits. A new heartbeat must invalidate the old shutdown
	// generation so the already-running daemon survives the handoff.
	signal("companion-b", "heartbeat")
	if companionIdleAtGeneration(registry, disconnectGeneration) {
		t.Fatal("replacement heartbeat must invalidate the pending idle shutdown")
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if len(registry.leases) != 1 {
		t.Fatalf("expected replacement companion lease, got %d", len(registry.leases))
	}
	if _, ok := registry.leases["companion-b"]; !ok {
		t.Fatal("replacement companion lease was not registered")
	}
}
