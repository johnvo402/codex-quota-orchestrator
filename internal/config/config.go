package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	DataDir                string  `json:"dataDir"`
	ListenAddr             string  `json:"listenAddr"`
	CodexCommand           string  `json:"codexCommand"`
	PollIntervalSeconds    int     `json:"pollIntervalSeconds"`
	CompanionPollSeconds   int     `json:"companionPollSeconds"`
	SoftThresholdPercent   float64 `json:"softThresholdPercent"`
	HardThresholdPercent   float64 `json:"hardThresholdPercent"`
	ResumeThresholdPercent float64 `json:"resumeThresholdPercent"`
	RequestTimeoutSeconds  int     `json:"requestTimeoutSeconds"`
}

func Default() Config {
	home, _ := os.UserHomeDir()
	return Config{
		DataDir:                filepath.Join(home, ".codex-desktop-quota-guard"),
		ListenAddr:             "127.0.0.1:47631",
		CodexCommand:           "codex",
		PollIntervalSeconds:    60,
		CompanionPollSeconds:   5,
		SoftThresholdPercent:   10,
		HardThresholdPercent:   5,
		ResumeThresholdPercent: 20,
		RequestTimeoutSeconds:  20,
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = os.Getenv("CDQG_CONFIG")
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config: %w", err)
		}
	}
	if v := os.Getenv("CDQG_CODEX_COMMAND"); v != "" {
		cfg.CodexCommand = v
	}
	if v := os.Getenv("CDQG_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.DataDir == "" || c.ListenAddr == "" || c.CodexCommand == "" {
		return errors.New("dataDir, listenAddr and codexCommand are required")
	}
	if c.PollIntervalSeconds < 10 {
		return errors.New("pollIntervalSeconds must be >= 10")
	}
	if c.CompanionPollSeconds < 1 {
		return errors.New("companionPollSeconds must be >= 1")
	}
	if c.HardThresholdPercent < 0 || c.SoftThresholdPercent <= c.HardThresholdPercent || c.ResumeThresholdPercent <= c.SoftThresholdPercent || c.ResumeThresholdPercent > 100 {
		return errors.New("thresholds must satisfy 0 <= hard < soft < resume <= 100")
	}
	return nil
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
