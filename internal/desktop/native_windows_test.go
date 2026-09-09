//go:build windows

package desktop

import "testing"

func TestNativePipeFromCodexCommandLine(
	t *testing.T,
) {
	commandLine := `C:\Users\Admin\AppData\Local\OpenAI\Codex\bin\abc\codex.exe -c features.code_mode_host=true app-server --analytics-default-enabled -c "mcp_servers.codex_app={\"command\"=\"cmd.exe\",\"env\"={\"CODEX_APP_TOOLS_PIPE_PATH\"=\"\\\\.\\pipe\\codex-browser-use-test\",\"CODEX_MCP_NODE_PATH\"=\"C:\\test\\node.exe\"}}"`

	got := nativePipeFromCodexCommandLine(
		commandLine,
	)

	want := `\\.\pipe\codex-browser-use-test`

	if got != want {
		t.Fatalf(
			"pipe = %q, want %q",
			got,
			want,
		)
	}
}
