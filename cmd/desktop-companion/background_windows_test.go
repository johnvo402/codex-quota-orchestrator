//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestConfigureParentedCommandUsesExplorerParentWithoutBreakaway(t *testing.T) {
	cmd := exec.Command(`C:\test\orchestrator-daemon.exe`, "daemon")
	const parent syscall.Handle = 1234
	configureParentedCommand(cmd, parent)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr was not configured")
	}
	if got := cmd.SysProcAttr.ParentProcess; got != parent {
		t.Fatalf("ParentProcess=%d, want %d", got, parent)
	}
	if got := cmd.SysProcAttr.CreationFlags; got&createNoWindow == 0 {
		t.Fatalf("CreationFlags=%#x, want CREATE_NO_WINDOW", got)
	}
	if got := cmd.SysProcAttr.CreationFlags; got&createBreakawayFromJob != 0 {
		t.Fatalf("CreationFlags=%#x unexpectedly contains CREATE_BREAKAWAY_FROM_JOB", got)
	}
}

func TestCloneCommandForRetryPreservesLaunchInputs(t *testing.T) {
	cmd := exec.Command(`C:\Program Files\CodexQuotaGuard\orchestrator-daemon.exe`, "daemon")
	cmd.Dir = `C:\work`
	cmd.Env = []string{"A=B"}

	retry := cloneCommandForRetry(cmd)
	if retry.Path != cmd.Path {
		t.Fatalf("Path=%q, want %q", retry.Path, cmd.Path)
	}
	if len(retry.Args) != len(cmd.Args) || retry.Args[1] != "daemon" {
		t.Fatalf("Args=%v, want %v", retry.Args, cmd.Args)
	}
	if retry.Dir != cmd.Dir {
		t.Fatalf("Dir=%q, want %q", retry.Dir, cmd.Dir)
	}
	if len(retry.Env) != 1 || retry.Env[0] != "A=B" {
		t.Fatalf("Env=%v", retry.Env)
	}
}
