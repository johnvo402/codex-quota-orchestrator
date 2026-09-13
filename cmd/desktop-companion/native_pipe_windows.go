//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

type companionParentProcess struct {
	ProcessID       int    `json:"ProcessId"`
	ParentProcessID int    `json:"ParentProcessId"`
	Name            string `json:"Name"`
	CommandLine     string `json:"CommandLine"`
}

var companionPipeRE = regexp.MustCompile(
	`(?i)(?:\\\\\\\\\.\\\\pipe\\\\|\\\\\.\\pipe\\)[^'"\s,}]+`,
)

func resolveCompanionNativePipe() string {
	for _, name := range []string{"CODEX_APP_TOOLS_PIPE_PATH", "CDQG_DESKTOP_NATIVE_PIPE"} {
		if pipe := normalizeCompanionPipe(os.Getenv(name)); pipe != "" {
			return pipe
		}
	}

	candidates := map[string]struct{}{}
	for _, process := range companionParentProcesses(os.Getppid(), 12) {
		name := strings.ToLower(filepath.Base(process.Name))
		if name != "codex.exe" && name != "codex" {
			continue
		}
		if pipe := companionPipeFromCommandLine(process.CommandLine); pipe != "" {
			candidates[pipe] = struct{}{}
		}
	}
	if len(candidates) != 1 {
		return ""
	}
	for pipe := range candidates {
		return pipe
	}
	return ""
}

func companionPipeFromCommandLine(commandLine string) string {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" || strings.ContainsAny(commandLine, "\r\n\x00") {
		return ""
	}
	matches := companionPipeRE.FindAllString(commandLine, -1)
	candidates := map[string]struct{}{}
	for _, match := range matches {
		if pipe := normalizeCompanionPipe(match); pipe != "" {
			candidates[pipe] = struct{}{}
		}
	}
	if len(candidates) != 1 {
		return ""
	}
	for pipe := range candidates {
		return pipe
	}
	return ""
}

func normalizeCompanionPipe(value string) string {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	value = strings.ReplaceAll(value, "/", `\`)
	if strings.HasPrefix(value, `\\\\.\\pipe\\`) {
		value = strings.ReplaceAll(value, `\\`, `\`)
	}
	const prefix = `\\.\pipe\`
	if len(value) <= len(prefix) || !strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)) {
		return ""
	}
	value = strings.TrimRight(value, `\`)
	if len(value) <= len(prefix) {
		return ""
	}
	return value
}

func companionParentProcesses(startPID, maxDepth int) []companionParentProcess {
	if startPID <= 0 || maxDepth <= 0 {
		return nil
	}
	script := fmt.Sprintf(
		`$id=%d; for($i=0; $i -lt %d -and $id -gt 0; $i++){ `+
			`$p=Get-CimInstance Win32_Process -Filter ("ProcessId="+$id) -ErrorAction SilentlyContinue; `+
			`if($null -eq $p){break}; `+
			`[pscustomobject]@{ProcessId=[int]$p.ProcessId;ParentProcessId=[int]$p.ParentProcessId;Name=[string]$p.Name;CommandLine=[string]$p.CommandLine} | ConvertTo-Json -Compress; `+
			`$id=[int]$p.ParentProcessId }`,
		startPID,
		maxDepth,
	)

	shell := "powershell.exe"
	if path, err := exec.LookPath("pwsh.exe"); err == nil {
		shell = path
	}
	cmd := exec.Command(shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var result []companionParentProcess
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var process companionParentProcess
		if json.Unmarshal([]byte(line), &process) == nil {
			result = append(result, process)
		}
	}
	return result
}
