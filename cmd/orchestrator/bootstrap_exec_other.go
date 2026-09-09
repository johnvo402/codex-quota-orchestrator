//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func runCodexQuiet(args ...string) (string, error) {
	resolved, err := exec.LookPath("codex")
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
