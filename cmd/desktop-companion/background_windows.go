//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"codex-desktop-quota-guard/internal/processjob"
)

const (
	createNoWindow         = 0x08000000
	createBreakawayFromJob = 0x01000000

	processCreateProcess    = 0x0080
	processDupHandle        = 0x0040
	processQueryInformation = 0x0400
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procGetShellWindow           = user32.NewProc("GetShellWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
)

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

func configureParentedCommand(cmd *exec.Cmd, parent syscall.Handle) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
		ParentProcess: parent,
	}
}

func startBackgroundCommand(cmd *exec.Cmd) (*exec.Cmd, backgroundStartInfo, error) {
	state, probeErr := processjob.Current()
	info := backgroundStartInfo{
		JobSupported: state.Supported,
		ParentInJob:  state.InJob,
		ProbeError:   probeErr,
	}
	if probeErr != nil {
		configureBackgroundCommand(cmd, false)
		return cmd, info, cmd.Start()
	}

	if !state.InJob {
		configureBackgroundCommand(cmd, false)
		return cmd, info, cmd.Start()
	}

	// Prefer the native breakaway mechanism when the parent Job Object allows
	// it. Codex Desktop currently denies this on some builds, so a successful
	// direct breakaway remains the cheapest path rather than the only path.
	configureBackgroundCommand(cmd, true)
	if err := cmd.Start(); err == nil {
		info.Breakaway = true
		return cmd, info, nil
	} else {
		info.BreakawayError = err
	}

	// A process created with PROC_THREAD_ATTRIBUTE_PARENT_PROCESS inherits the
	// specified parent's job object and token instead of the creating process's.
	// Use the current interactive shell (Explorer) as that parent so the daemon
	// is not tied to Codex Desktop's kill-on-close Job Object. Unlike the old
	// fallback, never knowingly launch another daemon inside the Codex job.
	parented, parentPID, err := startWithShellParent(cmd)
	if err != nil {
		info.ParentOverrideError = err
		return parented, info, fmt.Errorf("start daemon with CREATE_BREAKAWAY_FROM_JOB: %v; Explorer parent override: %w", info.BreakawayError, err)
	}
	info.ParentOverride = true
	info.OverrideParentPID = parentPID
	return parented, info, nil
}

func startWithShellParent(cmd *exec.Cmd) (*exec.Cmd, int, error) {
	parent, parentPID, err := shellParentProcess()
	if err != nil {
		return cmd, 0, err
	}
	defer syscall.CloseHandle(parent)

	parented := cloneCommandForRetry(cmd)
	configureParentedCommand(parented, parent)
	if err := parented.Start(); err != nil {
		return parented, parentPID, fmt.Errorf("start with Explorer parent pid=%d: %w", parentPID, err)
	}
	return parented, parentPID, nil
}

func shellParentProcess() (syscall.Handle, int, error) {
	hwnd, _, _ := procGetShellWindow.Call()
	if hwnd == 0 {
		return 0, 0, fmt.Errorf("GetShellWindow returned no interactive shell")
	}

	var pid uint32
	_, _, callErr := procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return 0, 0, fmt.Errorf("GetWindowThreadProcessId: %w", callErr)
		}
		return 0, 0, fmt.Errorf("GetWindowThreadProcessId returned pid 0")
	}

	parent, err := syscall.OpenProcess(
		processCreateProcess|processDupHandle|processQueryInformation,
		false,
		pid,
	)
	if err != nil {
		return 0, int(pid), fmt.Errorf("open Explorer process pid=%d: %w", pid, err)
	}

	return parent, int(pid), nil
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
