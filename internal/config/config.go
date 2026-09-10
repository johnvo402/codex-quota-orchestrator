package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DataDir                        string  `json:"dataDir"`
	ListenAddr                     string  `json:"listenAddr"`
	CodexCommand                   string  `json:"codexCommand"`
	PollIntervalSeconds            int     `json:"pollIntervalSeconds"`
	CompanionPollSeconds           int     `json:"companionPollSeconds"`
	SoftThresholdPercent           float64 `json:"softThresholdPercent"`
	HardThresholdPercent           float64 `json:"hardThresholdPercent"`
	FiveHourResumeThresholdPercent float64 `json:"fiveHourResumeThresholdPercent"`
	WeeklyResumeThresholdPercent   float64 `json:"weeklyResumeThresholdPercent"`
	RequestTimeoutSeconds          int     `json:"requestTimeoutSeconds"`
	AutoDispatch                   bool    `json:"autoDispatch"`

	// Deprecated internal compatibility alias. It mirrors the 5h resume
	// threshold and is intentionally omitted from newly saved JSON.
	ResumeThresholdPercent float64 `json:"-"`

	sourcePath string
}

func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		DataDir:                        filepath.Join(home, ".codex-desktop-quota-guard"),
		ListenAddr:                     "127.0.0.1:47631",
		CodexCommand:                   "codex",
		PollIntervalSeconds:            60,
		CompanionPollSeconds:           5,
		SoftThresholdPercent:           10,
		HardThresholdPercent:           5,
		FiveHourResumeThresholdPercent: 20,
		WeeklyResumeThresholdPercent:   5,
		RequestTimeoutSeconds:          20,
		AutoDispatch:                   true,
		ResumeThresholdPercent:         20,
	}
}

// DefaultPath is the shared config file used by the daemon, companion, and CLI
// when no explicit --config/CDQG_CONFIG path is supplied.
func DefaultPath() string {
	cfg := Default()
	if v := strings.TrimSpace(os.Getenv("CDQG_DATA_DIR")); v != "" {
		cfg.DataDir = v
	}
	return filepath.Join(cfg.DataDir, "config.json")
}

func ResolvePath(path string) string {
	if v := strings.TrimSpace(path); v != "" {
		return filepath.Clean(v)
	}
	if v := strings.TrimSpace(os.Getenv("CDQG_CONFIG")); v != "" {
		return filepath.Clean(v)
	}
	return DefaultPath()
}

func Load(path string) (Config, error) {
	cfg := Default()
	resolved := ResolvePath(path)
	explicit := strings.TrimSpace(path) != "" || strings.TrimSpace(os.Getenv("CDQG_CONFIG")) != ""

	b, err := os.ReadFile(resolved)
	switch {
	case err == nil:
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", resolved, err)
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", resolved, err)
		}
		// Before per-window resume thresholds existed, one
		// resumeThresholdPercent value controlled both quota windows. Preserve
		// that behavior for existing config files until Settings is saved with
		// the new fields.
		if legacyRaw, ok := raw["resumeThresholdPercent"]; ok {
			var legacy float64
			if err := json.Unmarshal(legacyRaw, &legacy); err != nil {
				return Config{}, fmt.Errorf("parse legacy resumeThresholdPercent in %s: %w", resolved, err)
			}
			if _, present := raw["fiveHourResumeThresholdPercent"]; !present {
				cfg.FiveHourResumeThresholdPercent = legacy
			}
			if _, present := raw["weeklyResumeThresholdPercent"]; !present {
				cfg.WeeklyResumeThresholdPercent = legacy
			}
		}
	case errors.Is(err, os.ErrNotExist) && !explicit:
		// The shared default file is optional. Existing installations continue
		// to use defaults until Settings is saved for the first time.
	case err != nil:
		return Config{}, fmt.Errorf("read config %s: %w", resolved, err)
	}

	if v := os.Getenv("CDQG_CODEX_COMMAND"); v != "" {
		cfg.CodexCommand = v
	}
	if v := os.Getenv("CDQG_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	cfg.ResumeThresholdPercent = cfg.FiveHourResumeThresholdPercent
	cfg.sourcePath = resolved
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	resolved := strings.TrimSpace(path)
	if resolved == "" {
		resolved = cfg.ConfigPath()
	}
	resolved = ResolvePath(resolved)
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(resolved), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tmpPath, resolved); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func (c Config) Validate() error {
	if c.DataDir == "" || c.ListenAddr == "" || c.CodexCommand == "" {
		return errors.New("dataDir, listenAddr and codexCommand are required")
	}
	if err := validateListenAddr(c.ListenAddr); err != nil {
		return err
	}
	if c.PollIntervalSeconds < 10 {
		return errors.New("pollIntervalSeconds must be >= 10")
	}
	if c.CompanionPollSeconds < 1 {
		return errors.New("companionPollSeconds must be >= 1")
	}
	if c.RequestTimeoutSeconds < 1 {
		return errors.New("requestTimeoutSeconds must be >= 1")
	}
	if c.HardThresholdPercent < 0 || c.SoftThresholdPercent <= c.HardThresholdPercent || c.SoftThresholdPercent > 100 {
		return errors.New("pause thresholds must satisfy 0 <= hard < soft <= 100")
	}
	if c.FiveHourResumeThresholdPercent < 0 || c.FiveHourResumeThresholdPercent > 100 ||
		c.WeeklyResumeThresholdPercent < 0 || c.WeeklyResumeThresholdPercent > 100 {
		return errors.New("resume thresholds must each be between 0 and 100")
	}
	return nil
}

func validateListenAddr(addr string) error {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("listenAddr must be host:port: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("listenAddr port must be between 1 and 65535")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("listenAddr must use a loopback host (127.0.0.1, localhost, or ::1)")
	}
	return nil
}

func (c Config) ConfigPath() string {
	if strings.TrimSpace(c.sourcePath) != "" {
		return c.sourcePath
	}
	return ResolvePath("")
}

func (c Config) DBPath() string    { return filepath.Join(c.DataDir, "state.db") }
func (c Config) RelayPath() string { return filepath.Join(c.DataDir, "relay.json") }
func (c Config) PollInterval() time.Duration {
	return time.Duration(c.PollIntervalSeconds) * time.Second
}
func (c Config) CompanionPollInterval() time.Duration {
	return time.Duration(c.CompanionPollSeconds) * time.Second
}
func (c Config) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutSeconds) * time.Second
}
func (c Config) BaseURL() string { return "http://" + c.ListenAddr }
