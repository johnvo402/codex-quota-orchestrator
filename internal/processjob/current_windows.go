//go:build windows

package processjob

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentProcess = kernel32.NewProc("GetCurrentProcess")
	procIsProcessInJob    = kernel32.NewProc("IsProcessInJob")
)

// Current reports whether the calling process belongs to any Windows Job
// Object. Passing a NULL job handle to IsProcessInJob asks Windows to check for
// membership in any job.
func Current() (State, error) {
	process, _, _ := procGetCurrentProcess.Call()
	var inJob int32
	ok, _, callErr := procIsProcessInJob.Call(
		process,
		0,
		uintptr(unsafe.Pointer(&inJob)),
	)
	if ok == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return State{Supported: true}, callErr
		}
		return State{Supported: true}, fmt.Errorf("IsProcessInJob failed")
	}
	return State{Supported: true, InJob: inJob != 0}, nil
}
