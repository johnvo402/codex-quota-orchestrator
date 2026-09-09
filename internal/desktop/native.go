package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type NativeSender interface {
	Available() bool
	Description() string
	SendMessage(context.Context, string, string) error
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

func validateNativeResponse(b []byte) error {
	var r struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			Success bool `json:"success"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("decode native response: %w", err)
	}
	if r.Error != nil {
		return fmt.Errorf("native tools rpc %d: %s", r.Error.Code, r.Error.Message)
	}
	if !r.Result.Success {
		return errors.New("Desktop native tool returned success=false")
	}
	return nil
}
