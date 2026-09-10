//go:build !windows

package codexquota

import (
	"fmt"
	"os/exec"
)

func ResolveCommand(command string) (string, error) {
	resolved, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("find Codex executable %q: %w", command, err)
	}
	return resolved, nil
}

func launchCodex(command string) (launchedProcess, error) {
	resolved, err := ResolveCommand(command)
	if err != nil {
		return launchedProcess{}, err
	}
	cmd := exec.Command(resolved, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return launchedProcess{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return launchedProcess{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return launchedProcess{}, err
	}
	if err := cmd.Start(); err != nil {
		return launchedProcess{}, err
	}
	return launchedProcess{process: execProcess{wait: cmd.Wait, kill: func() error { return cmd.Process.Kill() }}, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}
