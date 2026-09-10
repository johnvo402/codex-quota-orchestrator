//go:build windows

package desktop

import "testing"

func TestDesktopStopTurnStatusClassification(t *testing.T) {
	tests := []struct {
		status      string
		inProgress  bool
		interrupted bool
	}{
		{status: "inProgress", inProgress: true},
		{status: "INPROGRESS", inProgress: true},
		{status: " interrupted ", interrupted: true},
		{status: "completed"},
		{status: "failed"},
		{status: ""},
	}

	for _, tt := range tests {
		if got := isTurnInProgress(tt.status); got != tt.inProgress {
			t.Fatalf("isTurnInProgress(%q)=%v want %v", tt.status, got, tt.inProgress)
		}
		if got := isTurnInterrupted(tt.status); got != tt.interrupted {
			t.Fatalf("isTurnInterrupted(%q)=%v want %v", tt.status, got, tt.interrupted)
		}
	}
}

func TestDesktopStopRejectsUnknownAutomationMode(t *testing.T) {
	attempted, err := runDesktopStopAutomation(t.Context(), "unknown")
	if err == nil {
		t.Fatal("expected unsupported mode error")
	}
	if attempted {
		t.Fatal("unsupported mode must not report an attempted Stop")
	}
}
