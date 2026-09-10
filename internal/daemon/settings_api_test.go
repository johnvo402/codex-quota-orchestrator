package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"codex-desktop-quota-guard/internal/config"
)

func testSettingsServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(loaded, nil, nil)
	return NewServer(loaded.ListenAddr, svc, nil, nil), path
}

func TestSettingsGetReturnsPathAndConfig(t *testing.T) {
	srv, path := testSettingsServer(t)
	r := httptest.NewRequest(http.MethodGet, "/v1/settings", nil)
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out settingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ConfigPath != path {
		t.Fatalf("expected path %s, got %s", path, out.ConfigPath)
	}
	if !out.Config.AutoDispatch {
		t.Fatal("expected auto dispatch true")
	}
	if out.Config.FiveHourResumeThresholdPercent != 20 || out.Config.WeeklyResumeThresholdPercent != 5 {
		t.Fatalf("unexpected default resume thresholds: %#v", out.Config)
	}
}

func TestSettingsPutPersistsAndRequiresRestart(t *testing.T) {
	srv, path := testSettingsServer(t)
	body := []byte(`{
  "listenAddr":"127.0.0.1:47631",
  "pollIntervalSeconds":30,
  "companionPollSeconds":2,
  "softThresholdPercent":12,
  "hardThresholdPercent":6,
  "fiveHourResumeThresholdPercent":25,
  "weeklyResumeThresholdPercent":7,
  "requestTimeoutSeconds":10,
  "autoDispatch":false
}`)
	r := httptest.NewRequest(http.MethodPut, "/v1/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out settingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.RestartRequired {
		t.Fatal("expected restartRequired=true")
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AutoDispatch {
		t.Fatal("expected auto dispatch false after persistence")
	}
	if loaded.PollIntervalSeconds != 30 || loaded.HardThresholdPercent != 6 ||
		loaded.FiveHourResumeThresholdPercent != 25 || loaded.WeeklyResumeThresholdPercent != 7 {
		t.Fatalf("unexpected persisted config: %#v", loaded)
	}
}

func TestSettingsPutAcceptsLegacyResumeThreshold(t *testing.T) {
	srv, path := testSettingsServer(t)
	body := []byte(`{
  "listenAddr":"127.0.0.1:47631",
  "pollIntervalSeconds":30,
  "companionPollSeconds":2,
  "softThresholdPercent":12,
  "hardThresholdPercent":6,
  "resumeThresholdPercent":24,
  "requestTimeoutSeconds":10,
  "autoDispatch":true
}`)
	r := httptest.NewRequest(http.MethodPut, "/v1/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FiveHourResumeThresholdPercent != 24 || loaded.WeeklyResumeThresholdPercent != 24 {
		t.Fatalf("legacy API resume threshold should apply to both windows: %#v", loaded)
	}
}

func TestSettingsPutRejectsInvalidThresholds(t *testing.T) {
	srv, _ := testSettingsServer(t)
	body := []byte(`{
  "listenAddr":"127.0.0.1:47631",
  "pollIntervalSeconds":30,
  "companionPollSeconds":2,
  "softThresholdPercent":5,
  "hardThresholdPercent":10,
  "fiveHourResumeThresholdPercent":25,
  "weeklyResumeThresholdPercent":5,
  "requestTimeoutSeconds":10,
  "autoDispatch":true
}`)
	r := httptest.NewRequest(http.MethodPut, "/v1/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}
