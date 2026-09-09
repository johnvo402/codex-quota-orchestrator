//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
)

func launchRestartChild(configPath, waitURL string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"daemon"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "CDQG_RESTART_WAIT_URL="+waitURL)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start replacement daemon: %w", err)
	}
	return cmd.Process.Release()
}
