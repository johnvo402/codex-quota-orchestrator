//go:build !windows

package main

import "os/exec"

type backgroundStartInfo struct {
	JobSupported        bool
	ParentInJob         bool
	Breakaway           bool
	ParentOverride      bool
	OverrideParentPID   int
	ProbeError          error
	BreakawayError      error
	ParentOverrideError error
}

func configureBackgroundCommand(cmd *exec.Cmd, breakaway bool) {}

func startBackgroundCommand(cmd *exec.Cmd) (*exec.Cmd, backgroundStartInfo, error) {
	configureBackgroundCommand(cmd, false)
	return cmd, backgroundStartInfo{}, cmd.Start()
}
