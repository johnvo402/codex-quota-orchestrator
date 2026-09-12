//go:build windows

package desktop

import (
	"strings"
	"testing"
)

func TestParseLatestTurnSnapshotCurrentSchema(t *testing.T) {
	turnID, status, err := parseLatestTurnSnapshot(`{
		"schemaVersion": 1,
		"thread": {"id": "thread-1", "status": "active"},
		"turns": [{"id": "turn-current", "status": "inProgress"}]
	}`)
	if err != nil {
		t.Fatalf("parse current schema: %v", err)
	}
	if turnID != "turn-current" {
		t.Fatalf("turn ID = %q, want %q", turnID, "turn-current")
	}
	if status != "inProgress" {
		t.Fatalf("status = %q, want %q", status, "inProgress")
	}
}

func TestParseLatestTurnSnapshotLegacyTurnID(t *testing.T) {
	turnID, status, err := parseLatestTurnSnapshot(`{
		"turns": [{"turnId": "turn-legacy", "status": "interrupted"}]
	}`)
	if err != nil {
		t.Fatalf("parse legacy schema: %v", err)
	}
	if turnID != "turn-legacy" {
		t.Fatalf("turn ID = %q, want %q", turnID, "turn-legacy")
	}
	if status != "interrupted" {
		t.Fatalf("status = %q, want %q", status, "interrupted")
	}
}

func TestParseLatestTurnSnapshotPrefersCurrentID(t *testing.T) {
	turnID, _, err := parseLatestTurnSnapshot(`{
		"turns": [{"id": "current", "turnId": "legacy", "status": "inProgress"}]
	}`)
	if err != nil {
		t.Fatalf("parse dual schema: %v", err)
	}
	if turnID != "current" {
		t.Fatalf("turn ID = %q, want current schema ID", turnID)
	}
}

func TestParseLatestTurnSnapshotRejectsMissingTurns(t *testing.T) {
	_, _, err := parseLatestTurnSnapshot(`{"turns": []}`)
	if err == nil || !strings.Contains(err.Error(), "no turns") {
		t.Fatalf("error = %v, want no turns error", err)
	}
}

func TestParseLatestTurnSnapshotRejectsMissingTurnID(t *testing.T) {
	_, _, err := parseLatestTurnSnapshot(`{"turns": [{"status": "inProgress"}]}`)
	if err == nil || !strings.Contains(err.Error(), "has no id") {
		t.Fatalf("error = %v, want missing id error", err)
	}
}
