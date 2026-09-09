//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const restartCreateNoWindow = 0x08000000

func launchDaemonReplacement(configPath string) error {
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
	if filepath.Base(target) == "orch.exe" || filepath.Base(target) == "orchestrator.exe" {
		if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "orchestrator-daemon.exe")); err == nil {
			target = filepath.Join(filepath.Dir(exe), "orchestrator-daemon.exe")
		}
	}

	args := []string{"daemon"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	cmd := exec.Command(target, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: restartCreateNoWindow}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start replacement daemon: %w", err)
	}
	return cmd.Process.Release()
}
