package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/quota"
	"codex-desktop-quota-guard/internal/store"
)

type daemonClient struct {
	base string
	http *http.Client
}

func newDaemonClient(base string) *daemonClient {
	return &daemonClient{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *daemonClient) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("daemon %s: %s", resp.Status, string(b))
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (c *daemonClient) register(ctx context.Context, thread, turn, objective, workspace string) (domain.Task, error) {
	var out domain.Task
	err := c.do(ctx, "POST", "/v1/tasks/register", map[string]any{"threadId": thread, "turnId": turn, "objective": objective, "workspace": workspace}, &out)
	return out, err
}

func (c *daemonClient) checkpoint(ctx context.Context, thread, summary, pending, lastTest string) error {
	return c.do(ctx, "POST", "/v1/tasks/checkpoint", map[string]any{"threadId": thread, "summary": summary, "pending": pending, "lastTest": lastTest}, nil)
}

func (c *daemonClient) paused(ctx context.Context, thread, reason string) error {
	return c.do(ctx, "POST", "/v1/tasks/paused", map[string]any{"threadId": thread, "reason": reason}, nil)
}

func (c *daemonClient) complete(ctx context.Context, thread, summary string) error {
	return c.do(ctx, "POST", "/v1/tasks/complete", map[string]any{"threadId": thread, "summary": summary}, nil)
}

func (c *daemonClient) quota(ctx context.Context) (quota.Snapshot, quota.Decision, error) {
	var out struct {
		Quota    quota.Snapshot `json:"quota"`
		Decision quota.Decision `json:"decision"`
	}
	err := c.do(ctx, "GET", "/v1/quota", nil, &out)
	return out.Quota, out.Decision, err
}

func (c *daemonClient) actions(ctx context.Context) ([]store.Action, error) {
	var out []store.Action
	err := c.do(ctx, "GET", "/v1/actions", nil, &out)
	return out, err
}

func (c *daemonClient) claim(ctx context.Context, id int64) error {
	return c.do(ctx, "POST", fmt.Sprintf("/v1/actions/%d/claim", id), map[string]any{}, nil)
}

func (c *daemonClient) uncertain(ctx context.Context, id int64, reason string) error {
	return c.do(ctx, "POST", fmt.Sprintf("/v1/actions/%d/uncertain", id), map[string]any{"reason": reason}, nil)
}

func (c *daemonClient) ack(ctx context.Context, id int64, success bool, msg string) error {
	return c.do(ctx, "POST", fmt.Sprintf("/v1/actions/%d/ack", id), map[string]any{"success": success, "error": msg}, nil)
}
