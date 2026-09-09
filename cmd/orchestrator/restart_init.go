package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"codex-desktop-quota-guard/internal/config"
)

func init() {
	waitForRestartPredecessor()
	if len(os.Args) < 2 || os.Args[1] != "restart" {
		return
	}

	fs := flag.NewFlagSet("restart", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "config JSON")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	cfg, err := config.Load(*cfgPath)
	if err == nil {
		err = restartDaemon(cfg)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func waitForRestartPredecessor() {
	url := strings.TrimSpace(os.Getenv("CDQG_RESTART_WAIT_URL"))
	if url == "" {
		return
	}
	_ = os.Unsetenv("CDQG_RESTART_WAIT_URL")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !healthyURL(url) {
			// Allow the predecessor's deferred DB/file cleanup to finish after
			// its listener has disappeared.
			time.Sleep(150 * time.Millisecond)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
