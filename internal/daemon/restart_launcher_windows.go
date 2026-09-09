//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const daemonRestartCreateNoWindow = 0x08000000

func launchRestartChild(configPath, waitURL string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	target := filepath.Join(filepath.Dir(exe), "orchestrator-daemon.exe")
	if _, err := os.Stat(target); err != nil {
		target = exe
	}

	args := []string{"daemon"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	cmd := exec.Command(target, args...)
	cmd.Env = append(os.Environ(), "CDQG_RESTART_WAIT_URL="+waitURL)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: daemonRestartCreateNoWindow}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start replacement daemon: %w", err)
	}
	return cmd.Process.Release()
}
