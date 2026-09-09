//go:build windows

package codexquota

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

func launchCodex(command string) (launchedProcess, error) {
	resolved, err := exec.LookPath(command)
	if err != nil {
		return launchedProcess{}, fmt.Errorf("find Codex executable %q: %w", command, err)
	}

	var cmd *exec.Cmd
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext == ".cmd" || ext == ".bat" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		cmd = exec.Command(comspec)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: createNoWindow,
			CmdLine:       fmt.Sprintf(`/d /s /c ""%s" app-server --stdio"`, resolved),
		}
	} else {
		cmd = exec.Command(resolved, "app-server", "--stdio")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: createNoWindow,
		}
	}

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
	return launchedProcess{
		process: execProcess{wait: cmd.Wait, kill: func() error { return cmd.Process.Kill() }},
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
	}, nil
}
