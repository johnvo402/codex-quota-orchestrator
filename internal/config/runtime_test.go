package config

import (
	"os"
	"testing"
	"time"
)

func TestRuntimeRoundTripAndPIDCleanup(t *testing.T) {
	dir := t.TempDir()
	want := DaemonRuntime{
		ListenAddr: "127.0.0.1:48701",
		PID:        1234,
		StartedAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := SaveRuntime(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRuntime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenAddr != want.ListenAddr || got.PID != want.PID {
		t.Fatalf("unexpected runtime metadata: %#v", got)
	}

	if err := RemoveRuntimeIfPID(dir, 9999); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(RuntimePath(dir)); err != nil {
		t.Fatalf("runtime file should remain for different PID: %v", err)
	}

	if err := RemoveRuntimeIfPID(dir, want.PID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(RuntimePath(dir)); !os.IsNotExist(err) {
		t.Fatalf("runtime file should be removed for matching PID, err=%v", err)
	}
}

func TestRuntimeRejectsNonLoopbackEndpoint(t *testing.T) {
	dir := t.TempDir()
	if err := SaveRuntime(dir, DaemonRuntime{ListenAddr: "192.168.1.10:47631", PID: 1}); err == nil {
		t.Fatal("expected non-loopback runtime endpoint to be rejected")
	}
}

func TestRuntimeAcceptsIPv6Loopback(t *testing.T) {
	dir := t.TempDir()
	if err := SaveRuntime(dir, DaemonRuntime{ListenAddr: "[::1]:47631", PID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuntime(dir); err != nil {
		t.Fatal(err)
	}
}
