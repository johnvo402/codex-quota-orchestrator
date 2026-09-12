package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/mcpserver"
	"codex-desktop-quota-guard/internal/observability"
)

const (
	systemControlHeader  = "X-CDQG-Control"
	companionIDHeader    = "X-CDQG-Companion-ID"
	companionStateHeader = "X-CDQG-Companion-State"
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	log, logCloser, logErr := observability.NewLogger(cfg.DataDir, "companion", os.Stderr)
	if logErr != nil {
		fmt.Fprintln(os.Stderr, "companion logging:", logErr)
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	} else {
		defer logCloser.Close()
	}

	log.Info("Desktop companion started")
	// Preserve the proven v0.1.5 startup ordering: make sure the daemon exists
	// before entering the MCP stdio loop, then let the heartbeat own recovery.
	ensureDaemon(cfg, log)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	instanceID := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	go maintainDaemonLifecycle(ctx, cfg, instanceID, log)

	runErr := mcpserver.New(cfg, log).Run(ctx, os.Stdin, os.Stdout)
	cancel()

	disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	if err := signalCompanion(disconnectCtx, cfg, instanceID, "disconnect"); err != nil && daemonHealthy(cfg) {
		log.Warn("failed to release Desktop companion lease", "error", err)
	}
	disconnectCancel()

	if runErr != nil {
		log.Error("Desktop companion failed", "error", runErr)
		fmt.Fprintln(os.Stderr, "desktop companion:", runErr)
		os.Exit(1)
	}
	log.Info("Desktop companion stopped")
}

func maintainDaemonLifecycle(ctx context.Context, cfg config.Config, instanceID string, log *slog.Logger) {
	heartbeat := func() {
		hbCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		err := signalCompanion(hbCtx, cfg, instanceID, "heartbeat")
		cancel()
		if err == nil || ctx.Err() != nil {
			return
		}

		// If the daemon disappeared unexpectedly while Codex is still open,
		// restore it and re-register this companion lease.
		log.Warn("quota daemon heartbeat failed", "error", err)
		ensureDaemon(cfg, log)
		retryCtx, retryCancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		if retryErr := signalCompanion(retryCtx, cfg, instanceID, "heartbeat"); retryErr != nil && ctx.Err() == nil {
			log.Warn("quota daemon heartbeat retry failed", "error", retryErr)
		}
		retryCancel()
	}

	heartbeat()
	interval := cfg.CompanionPollInterval()
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeat()
		}
	}
}

func signalCompanion(ctx context.Context, cfg config.Config, instanceID, state string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL()+"/v1/system/restart", nil)
	if err != nil {
		return err
	}
	req.Header.Set(systemControlHeader, "companion")
	req.Header.Set(companionIDHeader, instanceID)
	req.Header.Set(companionStateHeader, state)

	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("companion lifecycle endpoint returned %s", resp.Status)
	}
	return nil
}

func ensureDaemon(cfg config.Config, log *slog.Logger) {
	if daemonHealthy(cfg) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		log.Warn("cannot resolve companion executable for daemon autostart", "error", err)
		return
	}
	binDir := filepath.Dir(exe)
	daemon := filepath.Join(binDir, "orchestrator-daemon.exe")
	if _, err := os.Stat(daemon); err != nil {
		daemon = filepath.Join(binDir, "orchestrator.exe")
	}
	if _, err := os.Stat(daemon); err != nil {
		log.Warn("orchestrator daemon binary not found next to desktop companion", "path", daemon, "error", err)
		return
	}
	cmd := exec.Command(daemon, "daemon")
	configureBackgroundCommand(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		log.Warn("failed to auto-start quota daemon", "error", err)
		return
	}
	_ = cmd.Process.Release()
	for i := 0; i < 10; i++ {
		if daemonHealthy(cfg) {
			log.Info("quota daemon auto-started with Codex Desktop")
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	log.Warn("quota daemon was started but did not become healthy yet")
}

func daemonHealthy(cfg config.Config) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
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
