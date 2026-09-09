package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codex-desktop-quota-guard/internal/config"
)

const (
	agentsBlockStart = "<!-- desktop-quota-guard:start -->"
	agentsBlockEnd   = "<!-- desktop-quota-guard:end -->"
)

func setupCodexIntegration(cfg config.Config) error {
	companion, err := installedCompanionPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(companion); err != nil {
		return fmt.Errorf("desktop companion not found at %s", companion)
	}

	_, _ = runCodexQuiet("mcp", "remove", "desktop-quota-guard")
	out, err := runCodexQuiet("mcp", "add", "desktop-quota-guard", "--", companion)
	if err != nil {
		return fmt.Errorf("register Codex MCP companion: %w\n%s", err, out)
	}
	if err := installAgentInstructions(); err != nil {
		return fmt.Errorf("install Codex AGENTS instructions: %w", err)
	}

	if _, err := os.Stat(cfg.RelayPath()); errors.Is(err, os.ErrNotExist) {
		if err := relayInit(cfg); err != nil {
			return fmt.Errorf("initialize relay: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("check relay config: %w", err)
	}

	fmt.Println("Codex integration configured.")
	fmt.Println("MCP: desktop-quota-guard")
	fmt.Println("Companion:", companion)
	return nil
}

func teardownCodexIntegration() error {
	var errs []string
	if out, err := runCodexQuiet("mcp", "remove", "desktop-quota-guard"); err != nil {
		if strings.TrimSpace(out) != "" {
			errs = append(errs, "remove MCP: "+strings.TrimSpace(out))
		} else {
			errs = append(errs, "remove MCP: "+err.Error())
		}
	}
	if err := removeAgentInstructions(); err != nil {
		errs = append(errs, "remove AGENTS instructions: "+err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	fmt.Println("Codex integration removed.")
	return nil
}

func installedCompanionPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "desktop-companion.exe"), nil
}

func installAgentInstructions() error {
	path, err := agentsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing, _ := os.ReadFile(path)
	block := agentsBlockStart + `
## Desktop Quota Guard
For substantial coding tasks in Codex Desktop:
- Near the beginning, call the desktop_task_register tool from the desktop-quota-guard MCP server with a concise objective and current workspace.
- Before a substantial new phase, and at natural safe boundaries during long work, call quota_check. Do not call it before every tiny read/edit/command.
- If quota_check returns action=pause, do not start new work. Finish only the current atomic operation, call task_checkpoint with completed/pending/test state, then call task_mark_paused and end the turn.
- When a [Desktop Quota Guard] Quota is available again message arrives, continue from the existing thread/checkpoint and call quota_check again before another long phase.
- When the objective is complete, call task_complete.
` + agentsBlockEnd
	updated := replaceManagedBlock(string(existing), agentsBlockStart, agentsBlockEnd, block)
	return os.WriteFile(path, []byte(updated), 0o644)
}

func removeAgentInstructions() error {
	path, err := agentsPath()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	text := string(b)
	if a := strings.Index(text, agentsBlockStart); a >= 0 {
		if b := strings.Index(text[a:], agentsBlockEnd); b >= 0 {
			text = strings.TrimSpace(text[:a] + text[a+b+len(agentsBlockEnd):])
			if text != "" {
				text += "\r\n"
			}
		}
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func agentsPath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("CODEX_HOME")); custom != "" {
		return filepath.Join(custom, "AGENTS.md"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "AGENTS.md"), nil
}

func replaceManagedBlock(existing, start, end, block string) string {
	if a := strings.Index(existing, start); a >= 0 {
		if b := strings.Index(existing[a:], end); b >= 0 {
			before := strings.TrimRight(existing[:a], "\r\n")
			after := strings.TrimLeft(existing[a+b+len(end):], "\r\n")
			if before != "" {
				before += "\r\n\r\n"
			}
			if after != "" {
				after = "\r\n\r\n" + after
			}
			return before + block + after + "\r\n"
		}
	}
	existing = strings.TrimRight(existing, "\r\n")
	if existing != "" {
		existing += "\r\n\r\n"
	}
	return existing + block + "\r\n"
}
