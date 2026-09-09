package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/codexquota"
	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/store"
)

type Status string

const (
	StatusPass    Status = "PASS"
	StatusWarn    Status = "WARN"
	StatusFail    Status = "FAIL"
	StatusUnknown Status = "UNKNOWN"
)

type Check struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Status  Status         `json:"status"`
	Summary string         `json:"summary"`
	Details map[string]any `json:"details,omitempty"`
}

type Report struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Overall     Status    `json:"overall"`
	Checks      []Check   `json:"checks"`
}

func Run(ctx context.Context, cfg config.Config) Report {
	r := Report{GeneratedAt: time.Now().UTC()}
	r.Checks = append(r.Checks,
		checkConfig(cfg),
		checkDaemon(ctx, cfg),
		checkRuntime(ctx, cfg),
		checkDatabase(ctx, cfg),
		checkCompanion(),
	)

	cliPath, cliCheck := checkCodexCLI(cfg)
	r.Checks = append(r.Checks, cliCheck)
	if cliCheck.Status == StatusPass {
		r.Checks = append(r.Checks, checkMCP(ctx, cfg, cliPath))
		r.Checks = append(r.Checks, checkQuota(ctx, cfg))
	} else {
		r.Checks = append(r.Checks,
			Check{ID: "mcp", Name: "MCP registration", Status: StatusUnknown, Summary: "Codex CLI is unavailable"},
			Check{ID: "quota", Name: "Quota provider", Status: StatusUnknown, Summary: "Codex CLI is unavailable"},
		)
	}
	r.Checks = append(r.Checks, Check{
		ID:      "native_pipe",
		Name:    "Desktop native pipe",
		Status:  StatusUnknown,
		Summary: "Only the Desktop companion can verify the native tools pipe",
		Details: map[string]any{"action": "run desktop_guard_status from a Codex Desktop task"},
	})
	r.Overall = overallStatus(r.Checks)
	return r
}

func overallStatus(checks []Check) Status {
	overall := StatusPass
	for _, c := range checks {
		switch c.Status {
		case StatusFail:
			return StatusFail
		case StatusWarn:
			overall = StatusWarn
		case StatusUnknown:
			if overall == StatusPass {
				overall = StatusWarn
			}
		}
	}
	return overall
}

func checkConfig(cfg config.Config) Check {
	if err := cfg.Validate(); err != nil {
		return Check{ID: "config", Name: "Configuration", Status: StatusFail, Summary: err.Error(), Details: map[string]any{"path": cfg.ConfigPath()}}
	}
	return Check{ID: "config", Name: "Configuration", Status: StatusPass, Summary: "Valid shared configuration", Details: map[string]any{"path": cfg.ConfigPath(), "listenAddr": cfg.ListenAddr}}
}

func checkDaemon(ctx context.Context, cfg config.Config) Check {
	if healthyURL(ctx, cfg.BaseURL()+"/healthz") {
		return Check{ID: "daemon", Name: "Daemon", Status: StatusPass, Summary: "Healthy", Details: map[string]any{"url": cfg.BaseURL()}}
	}
	return Check{ID: "daemon", Name: "Daemon", Status: StatusFail, Summary: "Health endpoint is not reachable", Details: map[string]any{"url": cfg.BaseURL()}}
}

func checkRuntime(ctx context.Context, cfg config.Config) Check {
	v, err := config.LoadRuntime(cfg.DataDir)
	if errors.Is(err, os.ErrNotExist) {
		return Check{ID: "runtime", Name: "Runtime metadata", Status: StatusWarn, Summary: "daemon-runtime.json is missing", Details: map[string]any{"path": config.RuntimePath(cfg.DataDir)}}
	}
	if err != nil {
		return Check{ID: "runtime", Name: "Runtime metadata", Status: StatusFail, Summary: err.Error(), Details: map[string]any{"path": config.RuntimePath(cfg.DataDir)}}
	}
	url := "http://" + v.ListenAddr
	status := StatusWarn
	summary := "Metadata is valid but its daemon is not reachable"
	if healthyURL(ctx, url+"/healthz") {
		status = StatusPass
		summary = "Runtime endpoint is healthy"
	}
	return Check{ID: "runtime", Name: "Runtime metadata", Status: status, Summary: summary, Details: map[string]any{"listenAddr": v.ListenAddr, "pid": v.PID, "startedAt": v.StartedAt}}
}

func checkDatabase(ctx context.Context, cfg config.Config) Check {
	if _, err := os.Stat(cfg.DBPath()); errors.Is(err, os.ErrNotExist) {
		return Check{ID: "database", Name: "SQLite", Status: StatusWarn, Summary: "State database has not been initialized", Details: map[string]any{"path": cfg.DBPath()}}
	} else if err != nil {
		return Check{ID: "database", Name: "SQLite", Status: StatusFail, Summary: err.Error(), Details: map[string]any{"path": cfg.DBPath()}}
	}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return Check{ID: "database", Name: "SQLite", Status: StatusFail, Summary: err.Error(), Details: map[string]any{"path": cfg.DBPath()}}
	}
	defer st.Close()

	tasks, err := st.ListTasks(ctx)
	if err != nil {
		return Check{ID: "database", Name: "SQLite", Status: StatusFail, Summary: "Cannot read managed tasks: " + err.Error()}
	}
	projects, err := st.ListProjects(ctx, true)
	if err != nil {
		return Check{ID: "database", Name: "SQLite", Status: StatusFail, Summary: "Cannot read projects: " + err.Error()}
	}
	if err := checkQueueIntegrity(ctx, st, projects); err != nil {
		return Check{ID: "database", Name: "SQLite / queue integrity", Status: StatusFail, Summary: err.Error(), Details: map[string]any{"tasks": len(tasks), "projects": len(projects)}}
	}
	return Check{ID: "database", Name: "SQLite / queue integrity", Status: StatusPass, Summary: "Readable; queued positions are normalized", Details: map[string]any{"path": cfg.DBPath(), "tasks": len(tasks), "projects": len(projects)}}
}

func checkQueueIntegrity(ctx context.Context, st *store.Store, projects []domain.Project) error {
	for _, p := range projects {
		items, err := st.ListProjectTasks(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("project %s queue: %w", p.ID, err)
		}
		queued := make([]domain.ProjectTask, 0)
		for _, item := range items {
			if item.State == domain.ProjectTaskQueued {
				queued = append(queued, item)
			}
		}
		sort.SliceStable(queued, func(i, j int) bool { return queued[i].Position < queued[j].Position })
		for i, item := range queued {
			if item.Position != int64(i+1) {
				return fmt.Errorf("project %s queue position gap/duplicate: task %s has %d, expected %d", p.ID, item.ID, item.Position, i+1)
			}
		}
	}
	return nil
}

func checkCompanion() Check {
	exe, err := os.Executable()
	if err != nil {
		return Check{ID: "companion", Name: "Desktop companion", Status: StatusWarn, Summary: "Cannot resolve current executable: " + err.Error()}
	}
	path := filepath.Join(filepath.Dir(exe), "desktop-companion.exe")
	if _, err := os.Stat(path); err == nil {
		return Check{ID: "companion", Name: "Desktop companion", Status: StatusPass, Summary: "Installed beside orchestrator", Details: map[string]any{"path": path}}
	}
	return Check{ID: "companion", Name: "Desktop companion", Status: StatusWarn, Summary: "desktop-companion.exe was not found beside this executable", Details: map[string]any{"path": path}}
}

func checkCodexCLI(cfg config.Config) (string, Check) {
	path, err := exec.LookPath(cfg.CodexCommand)
	if err != nil {
		return "", Check{ID: "codex_cli", Name: "Codex CLI", Status: StatusFail, Summary: err.Error(), Details: map[string]any{"command": cfg.CodexCommand}}
	}
	return path, Check{ID: "codex_cli", Name: "Codex CLI", Status: StatusPass, Summary: "Found", Details: map[string]any{"path": path}}
}

func checkMCP(ctx context.Context, cfg config.Config, cliPath string) Check {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, cliPath, "mcp", "list")
	configureCommand(cmd)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return Check{ID: "mcp", Name: "MCP registration", Status: StatusFail, Summary: "`codex mcp list` failed", Details: map[string]any{"error": err.Error(), "output": text}}
	}
	if !strings.Contains(text, "desktop-quota-guard") {
		return Check{ID: "mcp", Name: "MCP registration", Status: StatusFail, Summary: "desktop-quota-guard is not registered", Details: map[string]any{"action": "run orch setup"}}
	}
	return Check{ID: "mcp", Name: "MCP registration", Status: StatusPass, Summary: "desktop-quota-guard is registered"}
}

func checkQuota(ctx context.Context, cfg config.Config) Check {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c := codexquota.New(cfg.CodexCommand, cfg.RequestTimeout(), log)
	if err := c.Start(qctx); err != nil {
		return Check{ID: "quota", Name: "Quota provider", Status: StatusFail, Summary: "Cannot start Codex app-server: " + err.Error()}
	}
	defer c.Close()
	acct, err := c.ReadAccount(qctx)
	if err != nil {
		return Check{ID: "quota", Name: "Quota provider", Status: StatusFail, Summary: "Cannot read Codex account: " + err.Error()}
	}
	if acct.Account == nil || acct.Account.Type != "chatgpt" {
		return Check{ID: "quota", Name: "Quota provider", Status: StatusFail, Summary: "Codex is not signed in with ChatGPT"}
	}
	rates, err := c.ReadRateLimits(qctx)
	if err != nil {
		return Check{ID: "quota", Name: "Quota provider", Status: StatusFail, Summary: "Cannot read rate limits: " + err.Error()}
	}
	return Check{
		ID:      "quota",
		Name:    "Quota provider",
		Status:  StatusPass,
		Summary: "Codex account and rate limits are readable",
		Details: map[string]any{"email": acct.Account.Email, "plan": acct.Account.PlanType, "rateLimits": rates.RateLimits},
	}
}

func healthyURL(ctx context.Context, url string) bool {
	hctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode/100 == 2
}
