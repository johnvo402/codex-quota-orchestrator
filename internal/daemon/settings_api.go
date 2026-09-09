package daemon

import (
	"errors"
	"net/http"
	"os"
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

func (s *Server) persistedSettings() (config.Config, error) {
	path := s.service.cfg.ConfigPath()
	cfg, err := config.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return s.service.cfg, nil
	}
	return cfg, err
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		persisted, err := s.persistedSettings()
		if err != nil {
			httpErr(w, http.StatusInternalServerError, err)
			return
		}
		jsonOut(w, http.StatusOK, settingsResponse{
			Config:          persisted,
			ConfigPath:      s.service.cfg.ConfigPath(),
			RestartRequired: settingsRequireRestart(s.service.cfg, persisted),
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

	persisted, err := s.persistedSettings()
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	next := persisted
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
	if err := config.Save(s.service.cfg.ConfigPath(), next); err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}

	jsonOut(w, http.StatusOK, settingsResponse{
		Config:          next,
		ConfigPath:      s.service.cfg.ConfigPath(),
		RestartRequired: settingsRequireRestart(s.service.cfg, next),
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
