//go:build windows

package main

import "testing"

func TestCompanionPipeFromCommandLineCanonical(t *testing.T) {
	commandLine := `C:\\Codex\\codex.exe app-server -c "mcp_servers.codex_app={env={CODEX_APP_TOOLS_PIPE_PATH='\\.\pipe\codex-app-tools-123'}}"`
	if got := companionPipeFromCommandLine(commandLine); got != `\\.\pipe\codex-app-tools-123` {
		t.Fatalf("pipe=%q", got)
	}
}

func TestCompanionPipeFromCommandLineEscaped(t *testing.T) {
	commandLine := `C:\\Codex\\codex.exe app-server -c "mcp_servers.codex_app={env={CODEX_APP_TOOLS_PIPE_PATH=\"\\\\.\\pipe\\codex-app-tools-456\"}}"`
	if got := companionPipeFromCommandLine(commandLine); got != `\\.\pipe\codex-app-tools-456` {
		t.Fatalf("pipe=%q", got)
	}
}

func TestNormalizeCompanionPipeRejectsRemotePipe(t *testing.T) {
	if got := normalizeCompanionPipe(`\\server\pipe\remote`); got != "" {
		t.Fatalf("remote pipe accepted: %q", got)
	}
}
