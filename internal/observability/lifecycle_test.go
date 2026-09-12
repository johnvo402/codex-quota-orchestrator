package observability

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestProcessLifecycleMarksCleanExit(t *testing.T) {
	dataDir := t.TempDir()
	current, previous, err := BeginProcessLifecycle(dataDir, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	if previous != nil {
		t.Fatalf("expected no previous marker, got %+v", previous)
	}
	if current.CleanExit {
		t.Fatal("new lifecycle marker must start unclean")
	}
	if current.PID != os.Getpid() {
		t.Fatalf("pid=%d want %d", current.PID, os.Getpid())
	}

	if err := MarkProcessClean(dataDir, "daemon", "test shutdown"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(LifecycleMarkerPath(dataDir, "daemon"))
	if err != nil {
		t.Fatal(err)
	}
	var marker ProcessLifecycleMarker
	if err := json.Unmarshal(b, &marker); err != nil {
		t.Fatal(err)
	}
	if !marker.CleanExit || marker.ExitReason != "test shutdown" || marker.ExitedAt == "" {
		t.Fatalf("unexpected marker after clean exit: %+v", marker)
	}
}

func TestProcessLifecycleDetectsPreviousUncleanExit(t *testing.T) {
	dataDir := t.TempDir()
	first, _, err := BeginProcessLifecycle(dataDir, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	second, previous, err := BeginProcessLifecycle(dataDir, "daemon")
	if err != nil {
		t.Fatal(err)
	}
	if previous == nil || previous.RunID != first.RunID || previous.CleanExit {
		t.Fatalf("previous=%+v first=%+v", previous, first)
	}
	if second.RunID == "" {
		t.Fatal("second run id is empty")
	}
	b, err := os.ReadFile(LifecycleEventPath(dataDir, "daemon"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"event":"previous_unclean_exit"`) {
		t.Fatalf("lifecycle log does not record unclean exit: %s", b)
	}
}

func TestMarkProcessCleanRejectsForeignPID(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(LogDir(dataDir), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := ProcessLifecycleMarker{
		RunID:     "foreign",
		Component: "daemon",
		PID:       os.Getpid() + 100000,
		ParentPID: os.Getppid(),
		StartedAt: "2026-09-12T00:00:00Z",
	}
	if err := writeLifecycleMarker(LifecycleMarkerPath(dataDir, "daemon"), marker); err != nil {
		t.Fatal(err)
	}
	if err := MarkProcessClean(dataDir, "daemon", "wrong process"); err == nil {
		t.Fatal("expected foreign pid marker to be rejected")
	}
}
