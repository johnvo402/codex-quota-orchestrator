package main

import (
	"os"
	"strings"
	"time"
)

func init() {
	waitForRestartPredecessor()
}

func waitForRestartPredecessor() {
	url := strings.TrimSpace(os.Getenv("CDQG_RESTART_WAIT_URL"))
	if url == "" {
		return
	}
	_ = os.Unsetenv("CDQG_RESTART_WAIT_URL")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !healthyURL(url) {
			// Allow the predecessor's deferred DB/file cleanup to finish after
			// its listener has disappeared.
			time.Sleep(150 * time.Millisecond)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
