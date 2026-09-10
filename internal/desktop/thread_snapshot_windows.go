//go:build windows

package desktop

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// LatestTurn reads the latest turn through the same Codex Desktop native tools
// pipe used for delivery. Stop uses this immediately before UI Automation so a
// queued request for an older turn cannot blindly click Stop on newer work.
func (s *windowsNativeSender) LatestTurn(ctx context.Context, threadID string) (string, string, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", "", errors.New("target thread ID is empty")
	}
	if !s.Available() {
		return "", "", fmt.Errorf("Desktop native delivery unavailable: %s", s.Description())
	}

	f, err := os.OpenFile(s.pipe, os.O_RDWR, 0)
	if err != nil {
		return "", "", fmt.Errorf("open Desktop native pipe %q: %w", s.pipe, err)
	}
	defer f.Close()
	_ = f.SetDeadline(time.Now().Add(nativeTimeout))

	callID := uuidLike()
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"arguments": map[string]any{
				"threadId":              threadID,
				"turnLimit":             1,
				"includeOutputs":        false,
				"maxOutputCharsPerItem": 0,
			},
			"callId":    fmt.Sprintf("cdqg-%s", callID),
			"namespace": "codex_app",
			"threadId":  s.executor,
			"tool":      "read_thread",
			"turnId":    fmt.Sprintf("cdqg-turn-%s", callID),
		},
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return "", "", fmt.Errorf("encode native read_thread request: %w", err)
	}
	if len(payload) == 0 || len(payload) > nativeFrameMaxSize {
		return "", "", fmt.Errorf("native read_thread request has invalid size: %d bytes", len(payload))
	}

	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := f.Write(header[:]); err != nil {
		return "", "", fmt.Errorf("write native read_thread header: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		return "", "", fmt.Errorf("write native read_thread body: %w", err)
	}
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return "", "", fmt.Errorf("read native read_thread response header: %w", err)
	}
	responseLength := binary.LittleEndian.Uint32(header[:])
	if responseLength == 0 || responseLength > 1024*1024 {
		return "", "", fmt.Errorf("native read_thread response has invalid size: %d bytes", responseLength)
	}
	response := make([]byte, responseLength)
	if _, err := io.ReadFull(f, response); err != nil {
		return "", "", fmt.Errorf("read native read_thread response body: %w", err)
	}
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	default:
	}

	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return "", "", fmt.Errorf("decode native read_thread response: %w", err)
	}
	if envelope.Error != nil {
		return "", "", fmt.Errorf("native read_thread rpc %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if len(envelope.Result) == 0 {
		return "", "", errors.New("native read_thread response has no result")
	}

	var result struct {
		Success      *bool `json:"success"`
		IsError      bool  `json:"isError"`
		ContentItems []struct {
			Text string `json:"text"`
		} `json:"contentItems"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return "", "", fmt.Errorf("decode native read_thread tool result: %w", err)
	}
	if result.IsError || (result.Success != nil && !*result.Success) {
		return "", "", errors.New("Codex Desktop read_thread returned failure")
	}

	var text string
	for _, item := range result.ContentItems {
		if strings.TrimSpace(item.Text) != "" {
			text = item.Text
			break
		}
	}
	if text == "" {
		for _, item := range result.Content {
			if strings.TrimSpace(item.Text) != "" {
				text = item.Text
				break
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		return "", "", errors.New("Codex Desktop read_thread returned no snapshot text")
	}

	var snapshot struct {
		Turns []struct {
			TurnID string `json:"turnId"`
			Status string `json:"status"`
		} `json:"turns"`
	}
	if err := json.Unmarshal([]byte(text), &snapshot); err != nil {
		return "", "", fmt.Errorf("decode Codex Desktop thread snapshot: %w", err)
	}
	if len(snapshot.Turns) == 0 || strings.TrimSpace(snapshot.Turns[len(snapshot.Turns)-1].TurnID) == "" {
		return "", "", errors.New("Codex Desktop thread snapshot has no latest turn")
	}
	latest := snapshot.Turns[len(snapshot.Turns)-1]
	return strings.TrimSpace(latest.TurnID), strings.TrimSpace(latest.Status), nil
}
