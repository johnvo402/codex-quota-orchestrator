//go:build windows

package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	mbRetryCancel = 0x00000005
	idRetry       = 4
)

func normalizedVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "dev"
	}
	return strings.TrimPrefix(v, "v")
}

func installedVersion() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	v, _, err := key.GetStringValue("DisplayVersion")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func installPrompt() string {
	current := installedVersion()
	target := normalizedVersion(version)
	if current == "" {
		return fmt.Sprintf("Install Codex Desktop Quota Guard %s for the current Windows user?\n\nThis installs the orch command, Codex Desktop MCP integration, background daemon, and dashboard.", target)
	}
	if strings.EqualFold(current, target) {
		return fmt.Sprintf("Repair/reinstall Codex Desktop Quota Guard %s?\n\nYour local task history and SQLite state will be preserved.", target)
	}
	return fmt.Sprintf("Upgrade Codex Desktop Quota Guard %s → %s?\n\nYour local task history and SQLite state will be preserved.", current, target)
}

func waitForCompanionExit(silent bool) error {
	for {
		running, err := imageRunning("desktop-companion.exe")
		if err != nil {
			// Do not block setup solely because tasklist could not be queried.
			return nil
		}
		if !running {
			return nil
		}
		if silent {
			return errors.New("Codex Desktop is still using desktop-companion.exe; fully quit Codex Desktop before installing or upgrading")
		}
		result := messageBox(
			"Codex Desktop is still running the Quota Guard companion.\n\nFully quit Codex Desktop, then click Retry. This prevents Windows from locking desktop-companion.exe during the upgrade.",
			appName+" "+normalizedVersion(version),
			mbRetryCancel|mbIconQuestion,
		)
		if result != idRetry {
			return errors.New("installation cancelled because Codex Desktop is still using the installed companion")
		}
	}
}

func imageRunning(image string) (bool, error) {
	out, err := runHidden("tasklist.exe", "/FI", "IMAGENAME eq "+image, "/FO", "CSV", "/NH")
	if err != nil {
		return false, err
	}
	needle := `"` + strings.ToLower(image) + `"`
	return strings.Contains(strings.ToLower(out), needle), nil
}

func waitForDaemonHealth(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 750 * time.Millisecond}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://127.0.0.1:47631/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				return nil
			}
			lastErr = fmt.Errorf("health endpoint returned %s", resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("health endpoint did not respond")
	}
	return lastErr
}
