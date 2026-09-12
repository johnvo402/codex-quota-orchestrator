//go:build windows

package processjob

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	th32csSnapProcess              = 0x00000002
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
	errorInvalidParameter          = syscall.Errno(87)
)

var procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")

// CurrentDesktopHost walks the current process ancestry and returns the
// top-most Codex process. MCP companions may be short-lived, but this Desktop
// host remains alive for the lifetime of the actual Codex Desktop app.
func CurrentDesktopHost() (DesktopHost, error) {
	processes, err := snapshotProcesses()
	if err != nil {
		return DesktopHost{Supported: true}, err
	}
	host := findTopmostCodexAncestor(processes, os.Getpid())
	if host.PID == 0 {
		return host, fmt.Errorf("no Codex Desktop ancestor found for pid %d", os.Getpid())
	}
	return host, nil
}

func snapshotProcesses() (map[int]processInfo, error) {
	snapshot, err := syscall.CreateToolhelp32Snapshot(th32csSnapProcess, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snapshot)

	entry := syscall.ProcessEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := syscall.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}

	processes := make(map[int]processInfo)
	for {
		pid := int(entry.ProcessID)
		processes[pid] = processInfo{
			PID:       pid,
			ParentPID: int(entry.ParentProcessID),
			Name:      syscall.UTF16ToString(entry.ExeFile[:]),
		}
		if err := syscall.Process32Next(snapshot, &entry); err != nil {
			if err == syscall.ERROR_NO_MORE_FILES {
				break
			}
			return nil, err
		}
	}
	return processes, nil
}

// ProcessAlive reports whether pid still refers to a running process.
func ProcessAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		if err == errorInvalidParameter {
			return false, nil
		}
		return false, err
	}
	defer syscall.CloseHandle(h)

	var exitCode uint32
	ok, _, callErr := procGetExitCodeProcess.Call(uintptr(h), uintptr(unsafe.Pointer(&exitCode)))
	if ok == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return false, callErr
		}
		return false, fmt.Errorf("GetExitCodeProcess failed")
	}
	return exitCode == stillActive, nil
}
