//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	appName         = "Codex Desktop Quota Guard"
	uninstallKey    = `Software\Microsoft\Windows\CurrentVersion\Uninstall\CodexQuotaGuard`
	runKey          = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName    = "CodexQuotaGuard"
	legacyTaskName  = "Codex Desktop Quota Guard"
	createNoWindow  = 0x08000000
	mbOK            = 0x00000000
	mbYesNo         = 0x00000004
	mbIconError     = 0x00000010
	mbIconQuestion  = 0x00000020
	mbIconInfo      = 0x00000040
	idYes           = 6
	wmSettingChange = 0x001A
	hwndBroadcast   = 0xFFFF
	smtoAbortIfHung = 0x0002
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW        = user32.NewProc("MessageBoxW")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

func lockedFileError(err error) error {
	return fmt.Errorf("cannot replace installed files: %w\n\nFully quit Codex Desktop, then run CodexQuotaGuardSetup.exe again", err)
}

func copyWithRetry(src, dst string) error {
	var last error
	for i := 0; i < 12; i++ {
		last = copyFile(src, dst)
		if last == nil {
			return nil
		}
		if strings.EqualFold(filepath.Base(dst), "desktop-companion.exe") {
			_, _ = runHidden("taskkill.exe", "/IM", "desktop-companion.exe", "/F")
		}
		time.Sleep(250 * time.Millisecond)
	}
	return last
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".new"
	_ = os.Remove(tmp)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func stopInstalledProcesses() {
	for _, image := range []string{"desktop-companion.exe", "orchestrator-daemon.exe", "orchestrator.exe", "orch.exe"} {
		_, _ = runHidden("taskkill.exe", "/IM", image, "/F")
	}
	time.Sleep(200 * time.Millisecond)
}

func removeLegacyScheduledTask() {
	_, _ = runHidden("schtasks.exe", "/End", "/TN", legacyTaskName)
	_, _ = runHidden("schtasks.exe", "/Delete", "/TN", legacyTaskName, "/F")
}

func addMCP(companion string) error {
	_, _ = runCodex("mcp", "remove", "desktop-quota-guard")
	out, err := runCodex("mcp", "add", "desktop-quota-guard", "--", companion)
	if err != nil {
		return fmt.Errorf("register Codex MCP companion: %w\n%s", err, out)
	}
	return nil
}

func removeMCP() error {
	_, err := runCodex("mcp", "remove", "desktop-quota-guard")
	return err
}

func ensureRelay(orchestrator, dataDir string) error {
	relay := filepath.Join(dataDir, "relay.json")
	if _, err := os.Stat(relay); err == nil {
		return nil
	}
	out, err := runExecutable(orchestrator, "relay-init")
	if err != nil {
		return fmt.Errorf("initialize relay: %w\n%s", err, out)
	}
	return nil
}

func installRunEntry(daemon string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Windows Run key: %w", err)
	}
	defer key.Close()
	return key.SetStringValue(runValueName, fmt.Sprintf("\"%s\" daemon", daemon))
}

func removeRunEntry() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	_ = key.DeleteValue(runValueName)
	return nil
}

func registerUninstaller(p installPaths) error {
	return registerUninstallerVersion(p, normalizedVersion(version))
}

func registerUninstallerVersion(p installPaths, displayVersion string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("register Windows uninstall entry: %w", err)
	}
	defer key.Close()
	values := map[string]string{
		"DisplayName":          appName,
		"DisplayVersion":       displayVersion,
		"Publisher":            "johnvo402",
		"InstallLocation":      p.installRoot,
		"DisplayIcon":          p.orch,
		"UninstallString":      fmt.Sprintf("\"%s\" uninstall", p.uninstaller),
		"QuietUninstallString": fmt.Sprintf("\"%s\" uninstall --silent", p.uninstaller),
		"URLInfoAbout":         "https://github.com/johnvo402/codex-quota-orchestrator",
	}
	for name, value := range values {
		if err := key.SetStringValue(name, value); err != nil {
			return err
		}
	}
	_ = key.SetDWordValue("NoModify", 1)
	_ = key.SetDWordValue("NoRepair", 1)
	return nil
}

func addUserPath(dir string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	current, _, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	parts := splitPath(current)
	for _, part := range parts {
		if samePath(part, dir) {
			return nil
		}
	}
	parts = append(parts, dir)
	return key.SetStringValue("Path", strings.Join(parts, ";"))
}

func removeUserPath(dir string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	current, _, err := key.GetStringValue("Path")
	if err != nil {
		return nil
	}
	parts := splitPath(current)
	out := parts[:0]
	for _, part := range parts {
		if !samePath(part, dir) {
			out = append(out, part)
		}
	}
	return key.SetStringValue("Path", strings.Join(out, ";"))
}

func splitPath(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func installAgentInstructions() error {
	path, err := agentsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing, _ := os.ReadFile(path)
	start := "<!-- desktop-quota-guard:start -->"
	end := "<!-- desktop-quota-guard:end -->"
	block := start + `
## Desktop Quota Guard
For substantial coding tasks in Codex Desktop:
- Near the beginning, call the desktop_task_register tool from the desktop-quota-guard MCP server with a concise objective and current workspace.
- Before a substantial new phase, and at natural safe boundaries during long work, call quota_check. Do not call it before every tiny read/edit/command.
- If quota_check returns action=pause, do not start new work. Finish only the current atomic operation, call task_checkpoint with completed/pending/test state, then call task_mark_paused and end the turn.
- When a [Desktop Quota Guard] Quota is available again message arrives, continue from the existing thread/checkpoint and call quota_check again before another long phase.
- When the objective is complete, call task_complete.
` + end
	updated := replaceManagedBlock(string(existing), start, end, block)
	return os.WriteFile(path, []byte(updated), 0o644)
}

func removeAgentInstructions() error {
	path, err := agentsPath()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	start := "<!-- desktop-quota-guard:start -->"
	end := "<!-- desktop-quota-guard:end -->"
	text := string(b)
	if a := strings.Index(text, start); a >= 0 {
		if b := strings.Index(text[a:], end); b >= 0 {
			text = strings.TrimSpace(text[:a] + text[a+b+len(end):])
			if text != "" {
				text += "\r\n"
			}
		}
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func agentsPath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("CODEX_HOME")); custom != "" {
		return filepath.Join(custom, "AGENTS.md"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "AGENTS.md"), nil
}

func replaceManagedBlock(existing, start, end, block string) string {
	if a := strings.Index(existing, start); a >= 0 {
		if b := strings.Index(existing[a:], end); b >= 0 {
			before := strings.TrimRight(existing[:a], "\r\n")
			after := strings.TrimLeft(existing[a+b+len(end):], "\r\n")
			if before != "" {
				before += "\r\n\r\n"
			}
			if after != "" {
				after = "\r\n\r\n" + after
			}
			return before + block + after + "\r\n"
		}
	}
	existing = strings.TrimRight(existing, "\r\n")
	if existing != "" {
		existing += "\r\n\r\n"
	}
	return existing + block + "\r\n"
}

func startBackgroundDaemon(path string) error {
	cmd := exec.Command(path, "daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func runCodex(args ...string) (string, error) {
	return runHidden("codex", args...)
}

func runExecutable(path string, args ...string) (string, error) {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return capture(cmd)
}

func runHidden(name string, args ...string) (string, error) {
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	var cmd *exec.Cmd
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext == ".cmd" || ext == ".bat" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		pieces := []string{quoteCmdArg(resolved)}
		for _, arg := range args {
			pieces = append(pieces, quoteCmdArg(arg))
		}
		cmd = exec.Command(comspec)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: createNoWindow,
			CmdLine:       `/d /s /c "` + strings.Join(pieces, " ") + `"`,
		}
	} else {
		cmd = exec.Command(resolved, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	}
	return capture(cmd)
}

func capture(cmd *exec.Cmd) (string, error) {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.Stdin = nil
	err := cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

func quoteCmdArg(v string) string {
	return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
}

func scheduleDirectoryRemoval(dir string) error {
	comspec := os.Getenv("COMSPEC")
	if comspec == "" {
		comspec = `C:\Windows\System32\cmd.exe`
	}
	command := fmt.Sprintf(`timeout /t 2 /nobreak >nul & rmdir /s /q "%s"`, dir)
	cmd := exec.Command(comspec, "/d", "/s", "/c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func broadcastEnvironmentChange() {
	text, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	procSendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(text)), smtoAbortIfHung, 2000, uintptr(unsafe.Pointer(&result)))
}

func messageBox(text, title string, flags uintptr) int {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(title)
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags)
	return int(r)
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(strings.TrimSpace(a)), filepath.Clean(strings.TrimSpace(b)))
}

func sameFilePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return samePath(aa, bb)
}

func sameOrChildPath(path, root string) bool {
	path, _ = filepath.Abs(path)
	root, _ = filepath.Abs(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, `..\`))
}
