package processjob

import "strings"

// DesktopHost identifies the long-lived Codex Desktop process that owns an MCP
// companion. Supported is false on platforms where process ancestry discovery
// is unavailable.
type DesktopHost struct {
	Supported bool
	PID       int
	Name      string
}

type processInfo struct {
	PID       int
	ParentPID int
	Name      string
}

func findTopmostCodexAncestor(processes map[int]processInfo, startPID int) DesktopHost {
	if startPID <= 0 {
		return DesktopHost{Supported: true}
	}

	seen := make(map[int]struct{})
	pid := startPID
	var host DesktopHost
	for depth := 0; depth < 64 && pid > 0; depth++ {
		if _, duplicate := seen[pid]; duplicate {
			break
		}
		seen[pid] = struct{}{}

		p, ok := processes[pid]
		if !ok {
			break
		}
		if isCodexProcessName(p.Name) {
			host = DesktopHost{Supported: true, PID: p.PID, Name: p.Name}
		}
		if p.ParentPID <= 0 || p.ParentPID == pid {
			break
		}
		pid = p.ParentPID
	}
	if host.PID == 0 {
		host.Supported = true
	}
	return host
}

func isCodexProcessName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".exe")
	return name == "codex" || strings.HasPrefix(name, "codex-")
}
