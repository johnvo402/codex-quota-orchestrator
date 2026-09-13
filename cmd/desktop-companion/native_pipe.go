package main

import (
	"sync"

	"codex-desktop-quota-guard/internal/desktop"
)

var (
	companionNativePipeOnce sync.Once
	companionNativePipe     string
)

func companionNativePipePath() string {
	companionNativePipeOnce.Do(func() {
		companionNativePipe = desktop.NewNativeSender("").Diagnostics().Pipe
	})
	return companionNativePipe
}
