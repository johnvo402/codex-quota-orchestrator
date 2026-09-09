package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"codex-desktop-quota-guard/internal/config"
)

const restartControlHeader = "X-CDQG-Control"

var launchRestartChildFn = launchRestartChild

func (s *Server) systemRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get(restartControlHeader) != "restart" {
		httpErr(w, http.StatusForbidden, errors.New("restart control header required"))
		return
	}

	configPath := s.service.cfg.ConfigPath()
	if _, err := os.Stat(configPath); err != nil {
		configPath = ""
	}
	targetCfg, err := config.Load(configPath)
	if err != nil {
		httpErr(w, http.StatusConflict, fmt.Errorf("saved config is not restartable: %w", err))
		return
	}
	if targetCfg.ListenAddr != s.service.cfg.ListenAddr {
		listener, err := net.Listen("tcp", targetCfg.ListenAddr)
		if err != nil {
			httpErr(w, http.StatusConflict, fmt.Errorf("new listen address %s is unavailable: %w", targetCfg.ListenAddr, err))
			return
		}
		_ = listener.Close()
	}

	if err := launchRestartChildFn(configPath, s.service.cfg.BaseURL()+"/healthz"); err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}

	jsonOut(w, http.StatusAccepted, map[string]any{
		"ok":         true,
		"restarting": true,
		"targetURL":  targetCfg.BaseURL(),
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		// Give the accepted response a moment to leave the socket before
		// gracefully closing listeners. The replacement child is already
		// waiting for this server's health endpoint to disappear.
		time.Sleep(75 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			s.log.Warn("daemon restart shutdown failed", "error", err)
		}
	}()
}
