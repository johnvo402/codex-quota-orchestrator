//go:build !windows

package main

import "os/exec"

type backgroundStartInfo struct {
	JobSupported   bool
	ParentInJob    bool
	Breakaway      bool
	Fallback       bool
	BreakawayError error
}

func configureBackgroundCommand(cmd *exec.Cmd, breakaway bool) {}

func startBackgroundCommand(cmd *exec.Cmd) (*exec.Cmd, backgroundStartInfo, error) {
	configureBackgroundCommand(cmd, false)
	return cmd, backgroundStartInfo{}, cmd.Start()
}
