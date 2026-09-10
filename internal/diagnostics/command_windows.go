//go:build windows

package diagnostics

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	resolved := cmd.Path
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext == ".cmd" || ext == ".bat" {
		parts := []string{quoteWindowsCmdArg(resolved)}
		for _, arg := range cmd.Args[1:] {
			parts = append(parts, quoteWindowsCmdArg(arg))
		}
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		cmd.Path = comspec
		cmd.Args = []string{comspec}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: createNoWindow,
			CmdLine:       `/d /s /c "` + strings.Join(parts, " ") + `"`,
		}
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func quoteWindowsCmdArg(v string) string {
	return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
}
