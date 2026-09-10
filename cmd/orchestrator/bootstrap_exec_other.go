//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"codex-desktop-quota-guard/internal/codexquota"
)

func runCodexQuiet(args ...string) (string, error) {
	resolved, err := codexquota.ResolveCommand("codex")
	if err != nil {
		return "", fmt.Errorf("find Codex CLI: %w", err)
	}
	cmd := exec.Command(resolved, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Stdin = nil
	err = cmd.Run()
	return strings.TrimSpace(out.String()), err
}
