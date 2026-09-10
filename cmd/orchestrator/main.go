package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"codex-desktop-quota-guard/internal/codexquota"
	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/daemon"
	"codex-desktop-quota-guard/internal/desktop"
	"codex-desktop-quota-guard/internal/diagnostics"
	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/observability"
	"codex-desktop-quota-guard/internal/quota"
	"codex-desktop-quota-guard/internal/store"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return errors.New("command required")
	}

	cmd := os.Args[1]
	if cmd == "version" {
		fmt.Println(version)
		return nil
	}
	if cmd == "logs" {
		return logsCommand(os.Args[2:])
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	cfgPath := fs.String("config", "", "config JSON")
	jsonOut := fs.Bool("json", false, "JSON output")
	threadID := fs.String("thread", "", "Codex Desktop thread ID")
	resolution := fs.String("resolution", "retry", "recovery resolution: retry, running, or cancel")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	switch cmd {
	case "setup":
		return setupCodexIntegration(cfg)
	case "teardown":
		return teardownCodexIntegration()
	case "config":
		return configCommand(cfg, fs.Args(), *jsonOut)
	case "restart":
		return restartDaemon(cfg)
	case "doctor":
		return doctor(cfg, *jsonOut)
	case "relay-init":
		return relayInit(cfg)
	case "daemon":
		return runDaemon(cfg)
	case "list":
		return listTasks(cfg, *jsonOut)
	case "status":
		return status(cfg, *jsonOut)
	case "recover":
		return recoverTask(cfg, strings.TrimSpace(*threadID), strings.TrimSpace(*resolution), *jsonOut)
	case "ui":
		return openDashboard(cfg)
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func doctor(cfg config.Config, jsonOut bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	report := diagnostics.Run(ctx, cfg)
	if jsonOut {
		if err := printJSON(report); err != nil {
			return err
		}
	} else {
		fmt.Printf("System diagnostics: %s\n\n", report.Overall)
		for _, check := range report.Checks {
			fmt.Printf("%-8s %-24s %s\n", check.Status, check.Name, check.Summary)
		}
	}
	if report.Overall == diagnostics.StatusFail {
		return errors.New("one or more diagnostic checks failed")
	}
	return nil
}

func relayInit(cfg config.Config) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := codexquota.New(cfg.CodexCommand, cfg.RequestTimeout(), log)
	if err := c.Start(ctx); err != nil {
		return err
	}
	defer c.Close()
	home, _ := os.UserHomeDir()
	start, err := c.StartThread(ctx, home)
	if err != nil {
		return fmt.Errorf("create relay thread: %w", err)
	}
	turn, err := c.StartTurn(ctx, start.Thread.ID, "Reply exactly: CQO_RELAY_READY")
	if err != nil {
		return fmt.Errorf("persist relay thread: %w", err)
	}
	if err := c.WaitTurnCompleted(ctx, turn.Turn.ID); err != nil {
		return fmt.Errorf("wait relay turn: %w", err)
	}
	v := desktop.RelayConfig{ExecutorThreadID: start.Thread.ID, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := desktop.SaveRelay(cfg.RelayPath(), v); err != nil {
		return err
	}
	fmt.Println("Relay executor created:", start.Thread.ID)
	fmt.Println("Saved:", cfg.RelayPath())
	return nil
}

func runDaemon(cfg config.Config) error {
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		if daemonHealthy(cfg) {
			fmt.Printf("Daemon already running at %s\n", cfg.BaseURL())
			return nil
		}
		return fmt.Errorf("cannot listen on %s; the port may be used by another process: %w", cfg.ListenAddr, err)
	}
	defer listener.Close()

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	log, logCloser, err := observability.NewLogger(cfg.DataDir, "daemon", os.Stderr)
	if err != nil {
		return err
	}
	defer logCloser.Close()

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.BackfillProjects(context.Background()); err != nil {
		log.Warn("workspace project backfill failed", "error", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	svc := daemon.NewService(cfg, st, log)
	if err := svc.Recover(ctx); err != nil {
		return fmt.Errorf("startup recovery: %w", err)
	}
	go svc.RunMonitor(ctx)

	srv := daemon.NewServer(cfg.ListenAddr, svc, st, log)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(listener) }()
	log.Info("Desktop quota daemon started", "listen", cfg.ListenAddr, "db", cfg.DBPath(), "dashboard", cfg.BaseURL())
	select {
	case <-ctx.Done():
		log.Info("Desktop quota daemon stopping", "reason", "signal")
		sd, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		return srv.Shutdown(sd)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			log.Info("Desktop quota daemon stopped")
			return nil
		}
		log.Error("Desktop quota daemon server failed", "error", err)
		return err
	}
}

func daemonHealthy(cfg config.Config) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL()+"/healthz", nil)
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

func openDashboard(cfg config.Config) error {
	if !daemonHealthy(cfg) {
		return fmt.Errorf("daemon is not running at %s; start it first with `orchestrator daemon`", cfg.BaseURL())
	}
	url := cfg.BaseURL() + "/"
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open dashboard: %w", err)
	}
	fmt.Println("Dashboard:", url)
	return nil
}

func listTasks(cfg config.Config, jsonOut bool) error {
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()
	items, err := st.ListTasks(context.Background())
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(items)
	}
	if len(items) == 0 {
		fmt.Println("No managed Desktop tasks yet.")
		return nil
	}
	for _, t := range items {
		q := "-"
		if t.LastQuotaRemaining != nil {
			q = fmt.Sprintf("%.0f%%", *t.LastQuotaRemaining)
		}
		fmt.Printf("%-18s %-6s %s  %s\n", t.State, q, t.ThreadID, trim(t.Objective, 70))
	}
	return nil
}

func status(cfg config.Config, jsonOut bool) error {
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	items, err := st.ListTasks(ctx)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, t := range items {
		counts[string(t.State)]++
	}

	q, qErr := st.LatestQuota(ctx)
	relay, relayErr := desktop.LoadRelay(cfg.RelayPath())
	out := map[string]any{
		"version":         version,
		"daemonRunning":   daemonHealthy(cfg),
		"listenAddr":      cfg.ListenAddr,
		"dashboardURL":    cfg.BaseURL() + "/",
		"dataDir":         cfg.DataDir,
		"dbPath":          cfg.DBPath(),
		"managedTasks":    len(items),
		"taskStateCounts": counts,
		"relayConfigured": relayErr == nil,
		"relay":           relay,
	}
	if qErr == nil {
		out["quota"] = q
	} else if !errors.Is(qErr, sql.ErrNoRows) {
		out["quotaError"] = qErr.Error()
	}
	if jsonOut {
		return printJSON(out)
	}

	fmt.Printf("Version:       %s\n", version)
	fmt.Printf("Daemon:        %s (%s)\n", yesNo(daemonHealthy(cfg), "running", "stopped"), cfg.ListenAddr)
	fmt.Printf("Dashboard:     %s/\n", cfg.BaseURL())
	fmt.Printf("Data:          %s\n", cfg.DataDir)
	fmt.Printf("Config:        %s\n", cfg.ConfigPath())
	if qErr == nil {
		fmt.Printf("Quota 5h:      %s\n", windowRemaining(q.FiveHour))
		fmt.Printf("Quota weekly:  %s\n", windowRemaining(q.Weekly))
		fmt.Printf("Quota policy:  hard=%.0f%% soft=%.0f%% resume=%.0f%%\n", cfg.HardThresholdPercent, cfg.SoftThresholdPercent, cfg.ResumeThresholdPercent)
	} else {
		fmt.Println("Quota:         not sampled yet")
	}
	fmt.Printf("Auto dispatch: %t\n", cfg.AutoDispatch)
	fmt.Printf("Relay:         %s\n", yesNo(relayErr == nil, "configured", "not configured"))
	fmt.Printf("Managed tasks: %d\n", len(items))

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-16s %d\n", k, counts[k])
	}
	if counts[string(domain.StateNeedsReview)] > 0 {
		fmt.Println("Needs review: use the dashboard or run `orchestrator recover --thread <id> --resolution <retry|running|cancel>`.")
	}
	return nil
}

func recoverTask(cfg config.Config, threadID, resolution string, jsonOut bool) error {
	if threadID == "" {
		return errors.New("--thread is required")
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	ctx := context.Background()
	t, err := st.GetByThread(ctx, threadID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if t.State != domain.StateNeedsReview {
		return fmt.Errorf("task %s is %s, not NEEDS_REVIEW", threadID, t.State)
	}

	var target domain.TaskState
	var reason string
	switch strings.ToLower(resolution) {
	case "retry":
		target = domain.StatePausedQuota
		reason = "manual recovery: retry resume delivery"
	case "running":
		target = domain.StateRunning
		reason = "manual recovery: confirmed Desktop thread is already running"
	case "cancel":
		target = domain.StateCancelled
		reason = "manual recovery: cancelled by user"
	default:
		return fmt.Errorf("unknown resolution %q; use retry, running, or cancel", resolution)
	}

	updated, err := st.Transition(ctx, threadID, target, reason)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(updated)
	}
	fmt.Printf("Recovered %s: %s -> %s\n", threadID, t.State, updated.State)
	if strings.EqualFold(resolution, "retry") {
		fmt.Println("The task is PAUSED_QUOTA again. When quota is resumable, the daemon will queue one new continuation delivery.")
		fmt.Println("Use retry only after checking that the previous uncertain continuation did not already start the task.")
	}
	return nil
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		fmt.Println(string(b))
	}
	return err
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func usage() {
	fmt.Println("orchestrator <setup|teardown|config|restart|logs|doctor|relay-init|daemon|ui|list|status|recover|version> [options]")
	fmt.Println("  setup                                   Configure Codex MCP, AGENTS.md, and relay")
	fmt.Println("  teardown                                Remove Codex MCP and managed AGENTS.md block")
	fmt.Println("  config [show|path|validate]              Inspect shared configuration")
	fmt.Println("  restart                                 Gracefully restart the quota daemon")
	fmt.Println("  logs [--follow] [--tail N] [--level L]  View daemon and companion logs")
	fmt.Println("  doctor                                  Run system diagnostics")
	fmt.Println("  ui                                      Open local dashboard")
	fmt.Println("  recover --thread <threadId> --resolution <retry|running|cancel>")
}

func printQuotaWindow(name string, w quota.Window) {
	if !w.Available {
		fmt.Printf("%-10s unavailable\n", name+":")
		return
	}
	fmt.Printf("%-10s %.0f%% remaining (%.0f%% used)\n", name+":", w.RemainingPercent, w.UsedPercent)
	if w.ResetAt != nil {
		fmt.Printf("%-10s %s\n", name+" reset:", w.ResetAt.Local().Format(time.RFC3339))
	}
}

func windowRemaining(w quota.Window) string {
	if !w.Available {
		return "unavailable"
	}
	return fmt.Sprintf("%.0f%% remaining", w.RemainingPercent)
}

func yesNo(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}
