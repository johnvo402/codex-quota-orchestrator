package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type RelayConfig struct {
	ExecutorThreadID string `json:"executorThreadId"`
	CreatedAt        string `json:"createdAt"`
}

func LoadRelay(path string) (RelayConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RelayConfig{}, err
	}
	var v RelayConfig
	if err := json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	if v.ExecutorThreadID == "" {
		return v, errors.New("relay executorThreadId missing")
	}
	return v, nil
}
func SaveRelay(path string, v RelayConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type NativeDiagnostics struct {
	Available          bool   `json:"available"`
	Source             string `json:"source"`
	Pipe               string `json:"pipe,omitempty"`
	ExecutorConfigured bool   `json:"executorConfigured"`
	EnvironmentHasPipe bool   `json:"environmentHasPipe"`
	ParentPID          int    `json:"parentPid,omitempty"`
}

type NativeSender interface {
	Available() bool
	Description() string
	Diagnostics() NativeDiagnostics
	Probe(context.Context) error
	SendMessage(context.Context, string, string) error
	LatestTurn(context.Context, string) (turnID string, status string, err error)
}

func NewNativeSender(executorThreadID string) NativeSender {
	return newPlatformNativeSender(executorThreadID)
}

func nativeRequest(executor, target, message string, id string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"arguments": map[string]any{"threadId": target, "prompt": message},
			"callId":    id, "namespace": "codex_app", "threadId": executor,
			"tool": "send_message_to_thread", "turnId": id,
		},
	}
}
