//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"

	"codex-desktop-quota-guard/internal/processjob"
)

const (
	createNoWindow         = 0x08000000
	createBreakawayFromJob = 0x01000000
)

type backgroundStartInfo struct {
	JobSupported   bool
	ParentInJob    bool
	Breakaway      bool
	Fallback       bool
	BreakawayError error
}

func configureBackgroundCommand(cmd *exec.Cmd, breakaway bool) {
	flags := uint32(createNoWindow)
	if breakaway {
		flags |= createBreakawayFromJob
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: flags,
	}
}

func startBackgroundCommand(cmd *exec.Cmd) (*exec.Cmd, backgroundStartInfo, error) {
	state, probeErr := processjob.Current()
	info := backgroundStartInfo{
		JobSupported: state.Supported,
		ParentInJob:  state.InJob,
	}
	if probeErr != nil {
		configureBackgroundCommand(cmd, false)
		return cmd, info, cmd.Start()
	}

	if !state.InJob {
		configureBackgroundCommand(cmd, false)
		return cmd, info, cmd.Start()
	}

	configureBackgroundCommand(cmd, true)
	if err := cmd.Start(); err == nil {
		info.Breakaway = true
		return cmd, info, nil
	} else {
		info.BreakawayError = err
	}

	fallback := cloneCommandForRetry(cmd)
	configureBackgroundCommand(fallback, false)
	if err := fallback.Start(); err != nil {
		return fallback, info, fmt.Errorf("start daemon with CREATE_BREAKAWAY_FROM_JOB: %v; fallback start: %w", info.BreakawayError, err)
	}
	info.Fallback = true
	return fallback, info, nil
}

func cloneCommandForRetry(cmd *exec.Cmd) *exec.Cmd {
	retry := exec.Command(cmd.Path, cmd.Args[1:]...)
	retry.Stdin = cmd.Stdin
	retry.Stdout = cmd.Stdout
	retry.Stderr = cmd.Stderr
	retry.Dir = cmd.Dir
	retry.Env = cmd.Env
	return retry
}
