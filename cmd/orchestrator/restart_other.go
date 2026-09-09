//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func launchDaemonReplacement(configPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"daemon"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start replacement daemon: %w", err)
	}
	return cmd.Process.Release()
}
