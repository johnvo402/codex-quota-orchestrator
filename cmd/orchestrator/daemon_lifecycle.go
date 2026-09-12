package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"codex-desktop-quota-guard/internal/config"
	"codex-desktop-quota-guard/internal/observability"
)

// initDaemonLifecycleDiagnostics deliberately does not alter daemon ownership or
// restart behavior. It only leaves durable evidence about whether the previous
// daemon had an opportunity to exit cleanly.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "daemon" {
		return
	}
	cfg, err := config.Load(daemonConfigPathFromArgs(os.Args[2:]))
	if err != nil {
		return
	}
	current, previous, err := observability.BeginProcessLifecycle(cfg.DataDir, "daemon")
	if err != nil {
		fmt.Fprintln(os.Stderr, "daemon lifecycle diagnostics:", err)
		return
	}
	fmt.Fprintf(os.Stderr, "daemon lifecycle started runId=%s pid=%d parentPid=%d marker=%s\n", current.RunID, current.PID, current.ParentPID, observability.LifecycleMarkerPath(cfg.DataDir, "daemon"))
	if previous != nil && !previous.CleanExit {
		fmt.Fprintf(os.Stderr, "previous daemon did not record a clean exit runId=%s pid=%d parentPid=%d startedAt=%s\n", previous.RunID, previous.PID, previous.ParentPID, previous.StartedAt)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		_ = observability.MarkProcessClean(cfg.DataDir, "daemon", "signal:"+sig.String())
		signal.Stop(sigCh)
	}()
}

func daemonConfigPathFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--config="))
		}
		if arg == "--config" && i+1 < len(args) {
			return strings.TrimSpace(args[i+1])
		}
	}
	return ""
}
