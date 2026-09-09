package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"codex-desktop-quota-guard/internal/codexquota"
	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/daemon"
	"codex-desktop-quota-guard/internal/desktop"
	"codex-desktop-quota-guard/internal/quota"
	"codex-desktop-quota-guard/internal/store"
)

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
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	cfgPath := fs.String("config", "", "config JSON")
	jsonOut := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	switch cmd {
	case "doctor":
		return doctor(cfg, *jsonOut)
	case "relay-init":
		return relayInit(cfg)
	case "daemon":
		return runDaemon(cfg)
	case "list":
		return listTasks(cfg, *jsonOut)
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func doctor(cfg config.Config, jsonOut bool) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := codexquota.New(cfg.CodexCommand, cfg.RequestTimeout(), log)
	if err := c.Start(ctx); err != nil {
		return err
	}
	defer c.Close()
	acct, err := c.ReadAccount(ctx)
	if err != nil {
		return err
	}
	rates, err := c.ReadRateLimits(ctx)
	if err != nil {
		return err
	}
	policy := quota.Policy{SoftThreshold: cfg.SoftThresholdPercent, HardThreshold: cfg.HardThresholdPercent, ResumeThreshold: cfg.ResumeThresholdPercent}
	snap := quota.FromRateLimits(rates, policy)
	relay, relayErr := desktop.LoadRelay(cfg.RelayPath())
	out := map[string]any{
		"account":         acct.Account,
		"quota":           snap,
		"rawRateLimits":   rates,
		"relay":           relay,
		"relayConfigured": relayErr == nil,
		"desktopNativeCheck": map[string]any{
			"status": "not_checked",
			"reason": "doctor runs outside Codex Desktop; use desktop_guard_status from a Desktop task",
		},
	}
	if jsonOut {
		return printJSON(out)
	}
	if acct.Account == nil {
		fmt.Println("Account: signed out")
	} else {
		fmt.Printf("Account: %s (%s)\n", acct.Account.Email, acct.Account.PlanType)
	}
	printQuotaWindow("5h", snap.FiveHour)
	printQuotaWindow("Weekly", snap.Weekly)
	fmt.Printf("Effective: %.0f%% remaining\n", snap.RemainingPercent)
	if snap.PauseReason != "" {
		fmt.Println("Pause reason:", snap.PauseReason)
	}
	if relayErr != nil {
		fmt.Println("Relay: not initialized (run: orchestrator relay-init)")
	} else {
		fmt.Println("Relay thread:", relay.ExecutorThreadID)
	}
	fmt.Println("Desktop native delivery: not tested here")
	fmt.Println("Use desktop_guard_status inside Codex Desktop.")
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
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	svc := daemon.NewService(cfg, st, log)
	if err := svc.Recover(ctx); err != nil {
		return fmt.Errorf("startup recovery: %w", err)
	}
	go svc.RunMonitor(ctx)

	srv := daemon.NewServer(cfg.ListenAddr, svc, st, log)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("Desktop quota daemon started", "listen", cfg.ListenAddr, "db", cfg.DBPath())
	select {
	case <-ctx.Done():
		sd, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		return srv.Shutdown(sd)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
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
	for _, t := range items {
		q := "-"
		if t.LastQuotaRemaining != nil {
			q = fmt.Sprintf("%.0f%%", *t.LastQuotaRemaining)
		}
		fmt.Printf("%-18s %-6s %s  %s\n", t.State, q, t.ThreadID, trim(t.Objective, 70))
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

func usage() { fmt.Println("orchestrator <doctor|relay-init|daemon|list> [--config path] [--json]") }

var _ = filepath.Separator

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
