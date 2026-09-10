package diagnostics

import "testing"

func TestOverallStatusIgnoresUnknown(t *testing.T) {
	got := overallStatus([]Check{
		{Status: StatusPass},
		{Status: StatusUnknown},
	})
	if got != StatusPass {
		t.Fatalf("got %s, want PASS", got)
	}
}

func TestOverallStatusWarnAndFail(t *testing.T) {
	if got := overallStatus([]Check{{Status: StatusPass}, {Status: StatusWarn}}); got != StatusWarn {
		t.Fatalf("got %s, want WARN", got)
	}
	if got := overallStatus([]Check{{Status: StatusWarn}, {Status: StatusFail}}); got != StatusFail {
		t.Fatalf("got %s, want FAIL", got)
	}
}
