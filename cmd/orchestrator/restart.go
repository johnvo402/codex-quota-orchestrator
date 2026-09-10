package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"codex-desktop-quota-guard/internal/config"
)

var errLegacyShutdownUnsupported = errors.New("daemon does not support graceful shutdown")

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

func stopDaemon(cfg config.Config) error {
	base, err := requestDaemonShutdown(cfg)
	if errors.Is(err, errLegacyShutdownUnsupported) {
		// v0.1.4 and older expose /v1/system/restart but reject the new shutdown
		// control value. Use runtime metadata for a one-time compatibility stop so
		// the v0.1.5 installer can replace the locked daemon binary. Future
		// upgrades use the graceful path above.
		runtimeInfo, runtimeErr := config.LoadRuntime(cfg.DataDir)
		if runtimeErr != nil {
			return fmt.Errorf("legacy daemon is running but runtime metadata is unavailable: %w", runtimeErr)
		}
		runtimeBase := "http://" + runtimeInfo.ListenAddr
		if base != runtimeBase || runtimeInfo.PID <= 0 || !healthyURL(runtimeBase+"/healthz") {
			return fmt.Errorf("refusing legacy force-stop because daemon identity could not be verified")
		}
		process, findErr := os.FindProcess(runtimeInfo.PID)
		if findErr != nil {
			return fmt.Errorf("find legacy daemon process %d: %w", runtimeInfo.PID, findErr)
		}
		if killErr := process.Kill(); killErr != nil {
			return fmt.Errorf("stop legacy daemon process %d: %w", runtimeInfo.PID, killErr)
		}
		if waitForDaemonDown(runtimeBase, 5*time.Second) {
			_ = config.RemoveRuntimeIfPID(cfg.DataDir, runtimeInfo.PID)
			fmt.Printf("Legacy daemon process %d stopped for upgrade compatibility.\n", runtimeInfo.PID)
			return nil
		}
		return fmt.Errorf("legacy daemon process %d did not stop", runtimeInfo.PID)
	}
	if err != nil {
		return err
	}
	if base == "" {
		fmt.Println("Daemon is not running.")
		return nil
	}
	if !waitForDaemonDown(base, 5*time.Second) {
		return fmt.Errorf("daemon shutdown was accepted but %s is still healthy", base)
	}
	fmt.Println("Daemon stopped.")
	return nil
}

func requestDaemonRestart(cfg config.Config) (string, error) {
	var lastErr error
	for _, base := range daemonControlCandidates(cfg) {
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

func requestDaemonShutdown(cfg config.Config) (string, error) {
	var hadResponse bool
	var lastErr error
	for _, base := range daemonControlCandidates(cfg) {
		req, err := http.NewRequest(http.MethodPost, base+"/v1/system/restart", nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("X-CDQG-Control", "shutdown")
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		hadResponse = true
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusAccepted {
			return base, nil
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
			return base, errLegacyShutdownUnsupported
		}
		lastErr = fmt.Errorf("shutdown endpoint at %s returned %s", base, resp.Status)
	}
	if !hadResponse {
		return "", nil
	}
	if lastErr == nil {
		lastErr = errors.New("daemon shutdown failed")
	}
	return "", fmt.Errorf("request daemon shutdown: %w", lastErr)
}

func daemonControlCandidates(cfg config.Config) []string {
	var candidates []string
	addCandidate := func(base string) {
		if base == "" {
			return
		}
		for _, existing := range candidates {
			if existing == base {
				return
			}
		}
		candidates = append(candidates, base)
	}

	// Persisted config can already point at the *next* port. Runtime metadata
	// tells the CLI where the predecessor is still listening right now.
	if runtimeInfo, err := config.LoadRuntime(cfg.DataDir); err == nil {
		addCandidate("http://" + runtimeInfo.ListenAddr)
	}
	addCandidate(cfg.BaseURL())
	addCandidate(config.Default().BaseURL())
	return candidates
}

func waitForDaemonDown(base string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !healthyURL(base + "/healthz") {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return !healthyURL(base + "/healthz")
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
