package main

import (
	"testing"
	"time"
)

func resetCompanionNativePipeCacheForTest() {
	companionNativePipeCache.Lock()
	companionNativePipeCache.pipe = ""
	companionNativePipeCache.nextAttempt = time.Time{}
	companionNativePipeCache.resolving = false
	companionNativePipeCache.Unlock()
}

func TestCompanionNativePipeRetriesAfterMissAndCachesSuccess(t *testing.T) {
	resetCompanionNativePipeCacheForTest()
	oldResolver := resolveCompanionNativePipeFn
	defer func() {
		resolveCompanionNativePipeFn = oldResolver
		resetCompanionNativePipeCacheForTest()
	}()

	calls := 0
	resolveCompanionNativePipeFn = func() string {
		calls++
		if calls == 1 {
			return ""
		}
		return `\\.\pipe\codex-test`
	}

	if got := companionNativePipePath(); got != "" {
		t.Fatalf("first resolution=%q want empty miss", got)
	}
	if calls != 1 {
		t.Fatalf("resolver calls=%d want=1", calls)
	}

	// The retry interval prevents hot-looping when Desktop is still starting.
	if got := companionNativePipePath(); got != "" {
		t.Fatalf("resolution during backoff=%q want empty", got)
	}
	if calls != 1 {
		t.Fatalf("resolver calls during backoff=%d want=1", calls)
	}

	companionNativePipeCache.Lock()
	companionNativePipeCache.nextAttempt = time.Time{}
	companionNativePipeCache.Unlock()

	if got := companionNativePipePath(); got != `\\.\pipe\codex-test` {
		t.Fatalf("retry resolution=%q", got)
	}
	if calls != 2 {
		t.Fatalf("resolver calls after retry=%d want=2", calls)
	}

	if got := companionNativePipePath(); got != `\\.\pipe\codex-test` {
		t.Fatalf("cached resolution=%q", got)
	}
	if calls != 2 {
		t.Fatalf("cached resolver calls=%d want=2", calls)
	}
}
