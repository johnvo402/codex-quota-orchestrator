//go:build windows

package processjob

import (
	"os"
	"testing"
)

func TestProcessAliveCurrentProcess(t *testing.T) {
	alive, err := ProcessAlive(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !alive {
		t.Fatal("current test process must be reported alive")
	}
}
