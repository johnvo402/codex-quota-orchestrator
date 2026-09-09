//go:build windows

package desktop

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type windowsNativeSender struct {
	pipe     string
	executor string
}

func newPlatformNativeSender(executor string) NativeSender {
	pipe := strings.TrimSpace(os.Getenv("CODEX_APP_TOOLS_PIPE_PATH"))
	if pipe == "" {
		pipe = strings.TrimSpace(os.Getenv("CDQG_DESKTOP_NATIVE_PIPE"))
	}
	if pipe == "" {
		pipe = discoverParentPipe()
	}
	return &windowsNativeSender{pipe: pipe, executor: executor}
}
func (s *windowsNativeSender) Available() bool { return s.pipe != "" && s.executor != "" }
func (s *windowsNativeSender) Description() string {
	if s.pipe == "" {
		return "native pipe unavailable"
	}
	if s.executor == "" {
		return "relay executor unavailable"
	}
	return s.pipe
}
func (s *windowsNativeSender) SendMessage(ctx context.Context, target, message string) error {
	if !s.Available() {
		return fmt.Errorf("Desktop native delivery unavailable: %s", s.Description())
	}
	f, err := os.OpenFile(s.pipe, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open Desktop native pipe: %w", err)
	}
	defer f.Close()
	_ = f.SetDeadline(time.Now().Add(20 * time.Second))
	id := uuidLike()
	req := nativeRequest(s.executor, target, message, id)
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if len(payload) > 128*1024 {
		return fmt.Errorf("native payload too large: %d", len(payload))
	}
	var head [4]byte
	binary.LittleEndian.PutUint32(head[:], uint32(len(payload)))
	if _, err := f.Write(head[:]); err != nil {
		return err
	}
	if _, err := f.Write(payload); err != nil {
		return err
	}
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf("read native response header: %w", err)
	}
	n := binary.LittleEndian.Uint32(head[:])
	if n == 0 || n > 1024*1024 {
		return fmt.Errorf("invalid native response length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(f, buf); err != nil {
		return fmt.Errorf("read native response: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return validateNativeResponse(buf)
}
func uuidLike() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	hexv := hex.EncodeToString(b)
	return hexv[0:8] + "-" + hexv[8:12] + "-" + hexv[12:16] + "-" + hexv[16:20] + "-" + hexv[20:32]
}

func discoverParentPipe() string {
	ppid := os.Getppid()
	script := fmt.Sprintf(`$id=%d; for($i=0;$i -lt 6 -and $id -gt 0;$i++){ $p=Get-CimInstance Win32_Process -Filter ("ProcessId="+$id) -ErrorAction SilentlyContinue; if($null -eq $p){break}; [Console]::Out.WriteLine($p.CommandLine); $id=$p.ParentProcessId }`, ppid)
	shell := "powershell.exe"
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		shell = p
	}
	out, err := exec.Command(shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return ""
	}
	text := string(out)
	// Accept either a literal \\.\pipe\... value or the JSON-escaped form found in command lines.
	re := regexp.MustCompile(`(?i)CODEX_APP_TOOLS_PIPE_PATH[^\\\r\n]*(\\\\\\\\\.\\\\pipe\\\\[^\"'\s,}\]]+|\\\\\.\\pipe\\[^\"'\s,}\]]+)`)
	m := re.FindStringSubmatch(text)
	if len(m) < 2 {
		return ""
	}
	v := m[1]
	v = strings.ReplaceAll(v, `\\\\`, `\\`)
	v = strings.ReplaceAll(v, `\\`, `\`)
	if strings.HasPrefix(v, `\\.\pipe\`) {
		return v
	}
	return ""
}
