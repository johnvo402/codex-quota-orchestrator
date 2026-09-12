//go:build windows

package processjob

import "testing"

func TestCurrentReportsWindowsJobState(t *testing.T) {
	state, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if !state.Supported {
		t.Fatal("Windows Job Object probing must be supported on Windows")
	}
}
