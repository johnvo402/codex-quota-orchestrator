package main

import (
	"sync"
	"time"
)

const companionNativePipeRetryInterval = 5 * time.Second

var resolveCompanionNativePipeFn = resolveCompanionNativePipe

var companionNativePipeCache struct {
	sync.Mutex
	pipe        string
	nextAttempt time.Time
	resolving   bool
}

func companionNativePipePath() string {
	now := time.Now()
	companionNativePipeCache.Lock()
	if companionNativePipeCache.pipe != "" {
		pipe := companionNativePipeCache.pipe
		companionNativePipeCache.Unlock()
		return pipe
	}
	if companionNativePipeCache.resolving || now.Before(companionNativePipeCache.nextAttempt) {
		companionNativePipeCache.Unlock()
		return ""
	}
	companionNativePipeCache.resolving = true
	companionNativePipeCache.nextAttempt = now.Add(companionNativePipeRetryInterval)
	companionNativePipeCache.Unlock()

	// Cache only a successful resolution. v0.2.6 used sync.Once, so one early
	// miss during Desktop/MCP startup permanently prevented the daemon from ever
	// receiving a pipe and left project work stuck in DISPATCHING.
	pipe := resolveCompanionNativePipeFn()

	companionNativePipeCache.Lock()
	companionNativePipeCache.resolving = false
	if pipe != "" {
		companionNativePipeCache.pipe = pipe
	}
	companionNativePipeCache.Unlock()
	return pipe
}
