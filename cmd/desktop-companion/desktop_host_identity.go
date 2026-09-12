package main

import (
	"fmt"
	"log/slog"
	"net/http"

	"codex-desktop-quota-guard/internal/processjob"
)

const companionHostPIDHeader = "X-CDQG-Desktop-Host-PID"

var (
	companionDesktopHost, companionDesktopHostErr = processjob.CurrentDesktopHost()
)

func logDesktopHostIdentity(log *slog.Logger) {
	if companionDesktopHostErr != nil {
		log.Warn("cannot resolve Codex Desktop host process", "error", companionDesktopHostErr)
		return
	}
	if companionDesktopHost.PID > 0 {
		log.Info("Codex Desktop host resolved", "pid", companionDesktopHost.PID, "name", companionDesktopHost.Name)
	}
}

func applyDesktopHostHeader(req *http.Request) {
	if companionDesktopHost.PID <= 0 {
		return
	}
	req.Header.Set(companionHostPIDHeader, fmt.Sprintf("%d", companionDesktopHost.PID))
}
