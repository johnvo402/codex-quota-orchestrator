package daemon

import (
	"context"
	"errors"
	"net/http"
	"time"
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

	if err := launchRestartChildFn(s.service.cfg.ConfigPath(), s.service.cfg.BaseURL()+"/healthz"); err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}

	jsonOut(w, http.StatusAccepted, map[string]any{
		"ok":         true,
		"restarting": true,
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
