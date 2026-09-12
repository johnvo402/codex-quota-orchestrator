package processjob

import "testing"

func TestFindTopmostCodexAncestor(t *testing.T) {
	processes := map[int]processInfo{
		40: {PID: 40, ParentPID: 30, Name: "desktop-companion.exe"},
		30: {PID: 30, ParentPID: 20, Name: "codex-helper.exe"},
		20: {PID: 20, ParentPID: 10, Name: "Codex.exe"},
		10: {PID: 10, ParentPID: 1, Name: "explorer.exe"},
	}

	host := findTopmostCodexAncestor(processes, 40)
	if !host.Supported || host.PID != 20 || host.Name != "Codex.exe" {
		t.Fatalf("unexpected host: %+v", host)
	}
}

func TestFindTopmostCodexAncestorMissing(t *testing.T) {
	processes := map[int]processInfo{
		40: {PID: 40, ParentPID: 10, Name: "desktop-companion.exe"},
		10: {PID: 10, ParentPID: 1, Name: "explorer.exe"},
	}

	host := findTopmostCodexAncestor(processes, 40)
	if !host.Supported || host.PID != 0 {
		t.Fatalf("expected supported lookup with no Codex ancestor, got %+v", host)
	}
}
