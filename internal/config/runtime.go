package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DaemonRuntime is best-effort discovery metadata for CLI commands that need
// to contact the currently running daemon after persisted settings changed.
// It is not configuration and is safe to treat as stale unless /healthz
// confirms the endpoint is live.
type DaemonRuntime struct {
	ListenAddr string    `json:"listenAddr"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"startedAt"`
}

func RuntimePath(dataDir string) string {
	return filepath.Join(dataDir, "daemon-runtime.json")
}

func SaveRuntime(dataDir string, v DaemonRuntime) error {
	if dataDir == "" {
		return errors.New("dataDir required")
	}
	if !runtimeListenAddrAllowed(v.ListenAddr) {
		return errors.New("runtime listenAddr must be loopback")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dataDir, ".daemon-runtime-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, RuntimePath(dataDir))
}

func LoadRuntime(dataDir string) (DaemonRuntime, error) {
	b, err := os.ReadFile(RuntimePath(dataDir))
	if err != nil {
		return DaemonRuntime{}, err
	}
	var v DaemonRuntime
	if err := json.Unmarshal(b, &v); err != nil {
		return DaemonRuntime{}, err
	}
	if !runtimeListenAddrAllowed(v.ListenAddr) {
		return DaemonRuntime{}, errors.New("runtime listenAddr must be loopback")
	}
	return v, nil
}

func RemoveRuntimeIfPID(dataDir string, pid int) error {
	v, err := LoadRuntime(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if v.PID != pid {
		return nil
	}
	if err := os.Remove(RuntimePath(dataDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func runtimeListenAddrAllowed(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
