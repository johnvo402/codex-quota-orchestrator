package daemon

import (
	"errors"
	"net/http"
	"strings"

	"codex-desktop-quota-guard/internal/config"
)

type settingsUpdate struct {
	ListenAddr             string  `json:"listenAddr"`
	PollIntervalSeconds    int     `json:"pollIntervalSeconds"`
	CompanionPollSeconds   int     `json:"companionPollSeconds"`
	SoftThresholdPercent   float64 `json:"softThresholdPercent"`
	HardThresholdPercent   float64 `json:"hardThresholdPercent"`
	ResumeThresholdPercent float64 `json:"resumeThresholdPercent"`
	RequestTimeoutSeconds  int     `json:"requestTimeoutSeconds"`
	AutoDispatch           *bool   `json:"autoDispatch"`
}

type settingsResponse struct {
	Config          config.Config `json:"config"`
	ConfigPath      string        `json:"configPath"`
	RestartRequired bool          `json:"restartRequired"`
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOut(w, http.StatusOK, settingsResponse{
			Config:     s.service.cfg,
			ConfigPath: s.service.cfg.ConfigPath(),
		})
	case http.MethodPut:
		s.updateSettings(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	v, err := decode[settingsUpdate](r)
	if err != nil {
		httpErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(v.ListenAddr) == "" {
		httpErr(w, http.StatusBadRequest, errors.New("listenAddr required"))
		return
	}
	if v.AutoDispatch == nil {
		httpErr(w, http.StatusBadRequest, errors.New("autoDispatch required"))
		return
	}

	current := s.service.cfg
	next := current
	next.ListenAddr = strings.TrimSpace(v.ListenAddr)
	next.PollIntervalSeconds = v.PollIntervalSeconds
	next.CompanionPollSeconds = v.CompanionPollSeconds
	next.SoftThresholdPercent = v.SoftThresholdPercent
	next.HardThresholdPercent = v.HardThresholdPercent
	next.ResumeThresholdPercent = v.ResumeThresholdPercent
	next.RequestTimeoutSeconds = v.RequestTimeoutSeconds
	next.AutoDispatch = *v.AutoDispatch

	if err := next.Validate(); err != nil {
		httpErr(w, http.StatusBadRequest, err)
		return
	}
	if err := config.Save(current.ConfigPath(), next); err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}

	restartRequired := settingsRequireRestart(current, next)
	jsonOut(w, http.StatusOK, settingsResponse{
		Config:          next,
		ConfigPath:      current.ConfigPath(),
		RestartRequired: restartRequired,
	})
}

func settingsRequireRestart(a, b config.Config) bool {
	return a.ListenAddr != b.ListenAddr ||
		a.PollIntervalSeconds != b.PollIntervalSeconds ||
		a.CompanionPollSeconds != b.CompanionPollSeconds ||
		a.SoftThresholdPercent != b.SoftThresholdPercent ||
		a.HardThresholdPercent != b.HardThresholdPercent ||
		a.ResumeThresholdPercent != b.ResumeThresholdPercent ||
		a.RequestTimeoutSeconds != b.RequestTimeoutSeconds ||
		a.AutoDispatch != b.AutoDispatch
}
