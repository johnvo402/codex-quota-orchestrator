package codexquota

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("app-server rpc %d: %s", e.Code, e.Message) }

type wireMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}
type rpcResponse struct {
	Result json.RawMessage
	Error  *rpcError
}

type Client struct {
	command       string
	logger        *slog.Logger
	timeout       time.Duration
	process       processHandle
	stdin         io.WriteCloser
	writeMu       sync.Mutex
	mu            sync.Mutex
	pending       map[int64]chan rpcResponse
	nextID        atomic.Int64
	notifications chan Notification
	done          chan error
	closed        atomic.Bool
	stderrMu      sync.Mutex
	stderr        string
}

type processHandle interface {
	Wait() error
	Kill() error
}

type execProcess struct {
	wait func() error
	kill func() error
}

func (p execProcess) Wait() error { return p.wait() }
func (p execProcess) Kill() error { return p.kill() }

func New(command string, timeout time.Duration, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{command: command, logger: logger, timeout: timeout, pending: map[int64]chan rpcResponse{}, notifications: make(chan Notification, 128), done: make(chan error, 1)}
}

func (c *Client) Notifications() <-chan Notification { return c.notifications }
func (c *Client) Done() <-chan error                 { return c.done }

func (c *Client) Start(ctx context.Context) error {
	if c.process != nil {
		return errors.New("Codex client already started")
	}
	launched, err := launchCodex(c.command)
	if err != nil {
		return err
	}
	c.process, c.stdin = launched.process, launched.stdin
	go c.readLoop(launched.stdout)
	go c.stderrLoop(launched.stderr)
	go func() {
		err := c.process.Wait()
		if c.closed.Load() {
			err = nil
		}
		if err != nil && c.stderrText() != "" {
			err = fmt.Errorf("%w; stderr: %s", err, c.stderrText())
		}
		c.failPending(fmt.Errorf("app-server exited: %w", err))
		select {
		case c.done <- err:
		default:
		}
	}()

	var initResult map[string]any
	if err := c.Call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "codex_desktop_quota_guard", "title": "Codex Desktop Quota Guard", "version": "0.2.0"},
		"capabilities": map[string]any{"experimentalApi": false},
	}, &initResult); err != nil {
		_ = c.Close()
		return fmt.Errorf("initialize app-server: %w", err)
	}
	if err := c.Notify("initialized", map[string]any{}); err != nil {
		_ = c.Close()
		return err
	}
	return nil
}

func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.process != nil {
		_ = c.process.Kill()
	}
	return nil
}

func (c *Client) Call(parent context.Context, method string, params any, out any) error {
	ctx := parent
	if _, ok := parent.Deadline(); !ok && c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, c.timeout)
		defer cancel()
	}
	id := c.nextID.Add(1)
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if err := c.write(msg); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (c *Client) Notify(method string, params any) error {
	msg := map[string]any{"method": method}
	if params != nil {
		msg["params"] = params
	}
	return c.write(msg)
}

func (c *Client) write(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if c.stdin == nil {
		return errors.New("app-server stdin unavailable")
	}
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write app-server: %w", err)
	}
	return nil
}

func (c *Client) readLoop(r io.Reader) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for s.Scan() {
		var msg wireMessage
		if err := json.Unmarshal(s.Bytes(), &msg); err != nil {
			c.logger.Warn("invalid app-server JSON", "error", err)
			continue
		}
		if msg.ID != nil && msg.Method == "" {
			c.mu.Lock()
			ch := c.pending[*msg.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- rpcResponse{Result: msg.Result, Error: msg.Error}
			}
			continue
		}
		if msg.Method != "" && msg.ID == nil {
			select {
			case c.notifications <- Notification{Method: msg.Method, Params: msg.Params}:
			default:
			}
		}
	}
}

func (c *Client) stderrLoop(r io.Reader) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		c.stderrMu.Lock()
		c.stderr += s.Text() + "\n"
		if len(c.stderr) > 16384 {
			c.stderr = c.stderr[len(c.stderr)-16384:]
		}
		c.stderrMu.Unlock()
	}
}
func (c *Client) stderrText() string { c.stderrMu.Lock(); defer c.stderrMu.Unlock(); return c.stderr }
func (c *Client) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.pending {
		select {
		case ch <- rpcResponse{Error: &rpcError{Code: -32000, Message: err.Error()}}:
		default:
		}
	}
}

func (c *Client) ReadAccount(ctx context.Context) (AccountReadResponse, error) {
	var out AccountReadResponse
	err := c.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &out)
	return out, err
}
func (c *Client) ReadRateLimits(ctx context.Context) (RateLimitsResponse, error) {
	var out RateLimitsResponse
	err := c.Call(ctx, "account/rateLimits/read", map[string]any{}, &out)
	return out, err
}
func (c *Client) StartThread(ctx context.Context, cwd string) (ThreadStartResponse, error) {
	var out ThreadStartResponse
	err := c.Call(ctx, "thread/start", map[string]any{"cwd": cwd, "approvalPolicy": "never", "sandbox": "read-only", "serviceName": "Desktop Quota Guard Relay"}, &out)
	return out, err
}
func (c *Client) StartTurn(ctx context.Context, threadID, text string) (TurnStartResponse, error) {
	var out TurnStartResponse
	err := c.Call(ctx, "turn/start", map[string]any{"threadId": threadID, "input": []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}}}, &out)
	return out, err
}

func (c *Client) WaitTurnCompleted(ctx context.Context, turnID string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case n := <-c.notifications:
			if n.Method != "turn/completed" {
				continue
			}
			var p struct {
				Turn Turn `json:"turn"`
			}
			if json.Unmarshal(n.Params, &p) == nil && p.Turn.ID == turnID {
				return nil
			}
		case err := <-c.done:
			if err == nil {
				return errors.New("app-server exited")
			}
			return err
		}
	}
}
