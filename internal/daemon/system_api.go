package daemon

import (
	"errors"
	"net/http"
)

const restartControlHeader = "X-CDQG-Control"

func (s *Server) systemRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get(restartControlHeader) != "restart" {
		httpErr(w, http.StatusForbidden, errors.New("restart control header required"))
		return
	}

	jsonOut(w, http.StatusAccepted, map[string]any{
		"ok":         true,
		"restarting": true,
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	select {
	case s.restart <- struct{}{}:
	default:
		// A restart is already queued.
	}
}
