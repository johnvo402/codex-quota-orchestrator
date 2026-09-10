//go:build windows

package codexquota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCommandSkipsCurrentDirectoryAndRelativePATH(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "cwd")
	pathDir := filepath.Join(root, "path")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cwdCodex := filepath.Join(cwd, "codex.cmd")
	pathCodex := filepath.Join(pathDir, "codex.cmd")
	if err := os.WriteFile(cwdCodex, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathCodex, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	t.Setenv("PATHEXT", ".EXE;.CMD;.BAT")
	t.Setenv("PATH", "."+string(os.PathListSeparator)+pathDir)

	got, err := ResolveCommand("codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(pathCodex)) {
		t.Fatalf("ResolveCommand returned %q, want PATH executable %q", got, pathCodex)
	}
}

func TestResolveCommandAcceptsAbsoluteCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex.cmd")
	if err := os.WriteFile(path, []byte("@echo off\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveCommand(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(path)) {
		t.Fatalf("ResolveCommand returned %q, want %q", got, path)
	}
}

func TestResolveCommandRejectsExplicitRelativePath(t *testing.T) {
	if _, err := ResolveCommand(`.\codex.cmd`); err == nil {
		t.Fatal("expected relative Codex command to be rejected")
	}
}
