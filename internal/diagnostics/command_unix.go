//go:build !windows

package diagnostics

import "os/exec"

func configureCommand(*exec.Cmd) {}
