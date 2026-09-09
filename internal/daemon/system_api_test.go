package daemon

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"codex-desktop-quota-guard/internal/config"
)

func TestSystemRestartRequiresControlHeader(t *testing.T) {
	called := false
	old := launchRestartChildFn
	launchRestartChildFn = func(configPath, waitURL string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { launchRestartChildFn = old })

	s := &Server{
		service: &Service{cfg: config.Default()},
		log:     slog.Default(),
		http:    &http.Server{},
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/system/restart", nil)
	w := httptest.NewRecorder()

	s.systemRestart(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if called {
		t.Fatal("restart child must not launch without control header")
	}
}

func TestSystemRestartLaunchesReplacementWithoutMissingConfigArg(t *testing.T) {
	var gotConfig, gotWaitURL string
	old := launchRestartChildFn
	launchRestartChildFn = func(configPath, waitURL string) error {
		gotConfig = configPath
		gotWaitURL = waitURL
		return nil
	}
	t.Cleanup(func() { launchRestartChildFn = old })

	cfg := config.Default()
	s := &Server{
		service: &Service{cfg: cfg},
		log:     slog.Default(),
		http:    &http.Server{},
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/system/restart", nil)
	r.Header.Set(restartControlHeader, "restart")
	w := httptest.NewRecorder()

	s.systemRestart(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if gotConfig != "" {
		t.Fatalf("missing default config must not be passed as explicit --config: %q", gotConfig)
	}
	if gotWaitURL != cfg.BaseURL()+"/healthz" {
		t.Fatalf("unexpected wait URL: %q", gotWaitURL)
	}
}
