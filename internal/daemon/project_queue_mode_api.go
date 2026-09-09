package daemon

import (
	"net/http"
	"strings"

	"codex-desktop-quota-guard/internal/domain"
)

type projectQueueModeReq struct {
	Mode string `json:"mode"`
}

func (s *Server) projectQueueMode(w http.ResponseWriter, r *http.Request, projectID string) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.store.GetProjectQueueMode(r.Context(), projectID)
		if err != nil {
			httpErr(w, statusForStoreErr(err), err)
			return
		}
		jsonOut(w, http.StatusOK, settings)
	case http.MethodPatch:
		v, err := decode[projectQueueModeReq](r)
		if err != nil {
			httpErr(w, http.StatusBadRequest, err)
			return
		}
		mode := domain.ProjectQueueMode(strings.ToUpper(strings.TrimSpace(v.Mode)))
		settings, err := s.store.SetProjectQueueMode(r.Context(), projectID, mode)
		if err != nil {
			httpErr(w, http.StatusBadRequest, err)
			return
		}
		if mode == domain.ProjectQueueAuto {
			s.service.ReconcileProjectQueues(r.Context())
		}
		jsonOut(w, http.StatusOK, settings)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
