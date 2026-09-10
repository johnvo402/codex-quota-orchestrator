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

// ResolveCommand resolves a Codex CLI command without allowing Windows' current
// working directory to shadow PATH. Go intentionally rejects such LookPath
// results with exec.ErrDot; Desktop/GUI processes can hit that case even when
// the real Codex CLI is correctly installed on PATH.
func ResolveCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("Codex command is empty")
	}

	if filepath.IsAbs(command) {
		if resolved, ok := resolveWindowsCommandInDir(filepath.Dir(command), filepath.Base(command)); ok {
			return resolved, nil
		}
		return "", fmt.Errorf("find Codex executable %q: file does not exist", command)
	}

	// Never resolve an explicit relative path. In particular, do not turn
	// .\\codex.cmd into an executable candidate from the Desktop working dir.
	if filepath.VolumeName(command) != "" || strings.ContainsAny(command, `\\/`) {
		return "", fmt.Errorf("find Codex executable %q: relative paths are not allowed", command)
	}

	for _, rawDir := range filepath.SplitList(os.Getenv("PATH")) {
		dir := strings.Trim(strings.TrimSpace(rawDir), `"`)
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		if resolved, ok := resolveWindowsCommandInDir(dir, command); ok {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("find Codex executable %q: not found in absolute PATH entries", command)
}

func resolveWindowsCommandInDir(dir, command string) (string, bool) {
	for _, name := range windowsCommandCandidates(command) {
		candidate := filepath.Clean(filepath.Join(dir, name))
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		return filepath.Clean(absolute), true
	}
	return "", false
}

func windowsCommandCandidates(command string) []string {
	if filepath.Ext(command) != "" {
		return []string{command}
	}

	pathext := strings.TrimSpace(os.Getenv("PATHEXT"))
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}

	parts := strings.Split(pathext, ";")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, ext := range parts {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		key := strings.ToLower(ext)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, command+ext)
	}
	return out
}

func launchCodex(command string) (launchedProcess, error) {
	resolved, err := ResolveCommand(command)
	if err != nil {
		return launchedProcess{}, err
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
