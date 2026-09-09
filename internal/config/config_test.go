package config

import (
	"os"
	"path/filepath"
	"testing"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CDQG_CONFIG", "")
	t.Setenv("CDQG_DATA_DIR", "")
	t.Setenv("CDQG_CODEX_COMMAND", "")
}

func TestLoadMissingDefaultUsesDefaults(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("CDQG_DATA_DIR", t.TempDir())

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoDispatch {
		t.Fatal("default autoDispatch should be true")
	}
	if cfg.ConfigPath() != filepath.Join(cfg.DataDir, "config.json") {
		t.Fatalf("unexpected config path: %s", cfg.ConfigPath())
	}
}

func TestLegacyConfigWithoutAutoDispatchKeepsDefault(t *testing.T) {
	clearConfigEnv(t)
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
  "dataDir": "C:/tmp/cdqg",
  "listenAddr": "127.0.0.1:47631",
  "codexCommand": "codex",
  "pollIntervalSeconds": 60,
  "companionPollSeconds": 5,
  "softThresholdPercent": 10,
  "hardThresholdPercent": 5,
  "resumeThresholdPercent": 20,
  "requestTimeoutSeconds": 20
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoDispatch {
		t.Fatal("legacy config should inherit autoDispatch=true")
	}
	if cfg.ConfigPath() != path {
		t.Fatalf("expected source path %s, got %s", path, cfg.ConfigPath())
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	cfg := Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.ListenAddr = "127.0.0.1:48700"
	cfg.HardThresholdPercent = 7
	cfg.SoftThresholdPercent = 12
	cfg.ResumeThresholdPercent = 25
	cfg.PollIntervalSeconds = 30
	cfg.CompanionPollSeconds = 3
	cfg.RequestTimeoutSeconds = 15
	cfg.AutoDispatch = false

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ListenAddr != cfg.ListenAddr ||
		loaded.HardThresholdPercent != cfg.HardThresholdPercent ||
		loaded.SoftThresholdPercent != cfg.SoftThresholdPercent ||
		loaded.ResumeThresholdPercent != cfg.ResumeThresholdPercent ||
		loaded.PollIntervalSeconds != cfg.PollIntervalSeconds ||
		loaded.CompanionPollSeconds != cfg.CompanionPollSeconds ||
		loaded.RequestTimeoutSeconds != cfg.RequestTimeoutSeconds ||
		loaded.AutoDispatch != cfg.AutoDispatch {
		t.Fatalf("round-trip mismatch: %#v", loaded)
	}
	if loaded.ConfigPath() != path {
		t.Fatalf("expected config path %s, got %s", path, loaded.ConfigPath())
	}
}

func TestExplicitMissingConfigFails(t *testing.T) {
	clearConfigEnv(t)
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected explicit missing config to fail")
	}
}

func TestValidateRejectsNonLoopbackListenAddr(t *testing.T) {
	cfg := Default()
	cfg.ListenAddr = "0.0.0.0:47631"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected non-loopback listen address to be rejected")
	}
}

func TestValidateAcceptsLoopbackHosts(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:47631", "localhost:47631", "[::1]:47631"} {
		cfg := Default()
		cfg.ListenAddr = addr
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected %s to be valid: %v", addr, err)
		}
	}
}
