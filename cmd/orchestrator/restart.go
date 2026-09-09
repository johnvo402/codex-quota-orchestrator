package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"codex-desktop-quota-guard/internal/config"
)

func restartDaemon(cfg config.Config) error {
	oldBase, err := requestDaemonRestart(cfg)
	if err != nil {
		return err
	}

	// Wait until the old daemon is actually gone so we do not mistake its
	// still-live health response for a completed restart.
	downDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(downDeadline) {
		if !healthyURL(oldBase + "/healthz") {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	path := existingConfigPath(cfg)
	newCfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("reload saved config: %w", err)
	}
	upDeadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(upDeadline) {
		if healthyURL(newCfg.BaseURL() + "/healthz") {
			fmt.Printf("Daemon restarted at %s\n", newCfg.BaseURL())
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("daemon restart was requested but %s did not become healthy", newCfg.BaseURL())
}

func requestDaemonRestart(cfg config.Config) (string, error) {
	candidates := []string{cfg.BaseURL()}
	defaultBase := config.Default().BaseURL()
	if defaultBase != cfg.BaseURL() {
		candidates = append(candidates, defaultBase)
	}

	var lastErr error
	for _, base := range candidates {
		req, err := http.NewRequest(http.MethodPost, base+"/v1/system/restart", nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("X-CDQG-Control", "restart")
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusAccepted {
			return base, nil
		}
		lastErr = fmt.Errorf("restart endpoint at %s returned %s", base, resp.Status)
	}
	if lastErr == nil {
		lastErr = errors.New("daemon is not reachable")
	}
	return "", fmt.Errorf("request daemon restart: %w", lastErr)
}

func existingConfigPath(cfg config.Config) string {
	path := cfg.ConfigPath()
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func healthyURL(url string) bool {
	client := &http.Client{Timeout: 700 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode/100 == 2
}
