//go:build windows

package main

import (
	"fmt"
	"os"

	"codex-desktop-quota-guard/internal/processjob"
)

// Emit this before the daemon logger starts so the companion's stderr capture
// can prove whether CREATE_BREAKAWAY_FROM_JOB actually detached the daemon.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "daemon" {
		return
	}
	state, err := processjob.Current()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon Windows job membership probe failed: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "daemon Windows job membership inJob=%t pid=%d parentPid=%d\n", state.InJob, os.Getpid(), os.Getppid())
}
