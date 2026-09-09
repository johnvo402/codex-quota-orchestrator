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
)

func main() {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ensureDaemon(cfg, log)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := mcpserver.New(cfg, log).Run(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "desktop companion:", err)
		os.Exit(1)
	}
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
