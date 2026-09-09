//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const bootstrapCreateNoWindow = 0x08000000

func runCodexQuiet(args ...string) (string, error) {
	resolved, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("find Codex CLI: %w", err)
	}

	var cmd *exec.Cmd
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext == ".cmd" || ext == ".bat" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		parts := []string{quoteWindowsCmdArg(resolved)}
		for _, arg := range args {
			parts = append(parts, quoteWindowsCmdArg(arg))
		}
		cmd = exec.Command(comspec)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: bootstrapCreateNoWindow,
			CmdLine:       `/d /s /c "` + strings.Join(parts, " ") + `"`,
		}
	} else {
		cmd = exec.Command(resolved, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: bootstrapCreateNoWindow}
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Stdin = nil
	err = cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func quoteWindowsCmdArg(v string) string {
	return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
}
