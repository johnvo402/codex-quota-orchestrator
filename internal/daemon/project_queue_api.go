package daemon

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

type projectTaskReq struct {
	ProjectID string `json:"projectId"`
	Objective string `json:"objective"`
	Details   string `json:"details"`
	Position  *int64 `json:"position"`
}

type projectTaskReorderReq struct {
	ProjectID string   `json:"projectId"`
	TaskIDs   []string `json:"taskIds"`
}

func (s *Server) projectTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
		if projectID == "" {
			httpErr(w, http.StatusBadRequest, errors.New("projectId required"))
			return
		}
		items, err := s.store.ListProjectTasks(r.Context(), projectID)
		if err != nil {
			httpErr(w, http.StatusInternalServerError, err)
			return
		}
		jsonOut(w, http.StatusOK, items)
	case http.MethodPost:
		v, err := decode[projectTaskReq](r)
		if err != nil || strings.TrimSpace(v.ProjectID) == "" {
			httpErr(w, http.StatusBadRequest, errors.New("projectId required"))
			return
		}
		item, err := s.store.CreateProjectTask(r.Context(), v.ProjectID, v.Objective, v.Details)
		if err != nil {
			httpErr(w, http.StatusBadRequest, err)
			return
		}
		s.service.ReconcileProjectQueues(r.Context())
		item, _ = s.store.GetProjectTask(r.Context(), item.ID)
		jsonOut(w, http.StatusCreated, item)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) projectTaskRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/project-tasks/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	if path == "reorder" {
		s.reorderProjectTasks(w, r)
		return
	}

	parts := strings.Split(path, "/")
	id := parts[0]
	if len(parts) == 2 {
		switch parts[1] {
		case "cancel":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			item, err := s.store.CancelProjectTask(r.Context(), id)
			if err != nil {
				httpErr(w, http.StatusConflict, err)
				return
			}
			jsonOut(w, http.StatusOK, item)
			return
		case "start":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			item, err := s.service.StartProjectQueueItem(r.Context(), id)
			if err != nil {
				httpErr(w, queueStatus(err), err)
				return
			}
			jsonOut(w, http.StatusOK, item)
			return
		default:
			http.NotFound(w, r)
			return
		}
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.store.GetProjectTask(r.Context(), id)
		if err != nil {
			httpErr(w, queueStatus(err), err)
			return
		}
		jsonOut(w, http.StatusOK, item)
	case http.MethodPatch:
		v, err := decode[projectTaskReq](r)
		if err != nil {
			httpErr(w, http.StatusBadRequest, err)
			return
		}
		item, err := s.store.UpdateProjectTask(r.Context(), id, v.Objective, v.Details, v.Position)
		if err != nil {
			httpErr(w, http.StatusConflict, err)
			return
		}
		jsonOut(w, http.StatusOK, item)
	case http.MethodDelete:
		if err := s.store.DeleteProjectTask(r.Context(), id); err != nil {
			httpErr(w, queueStatus(err), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) reorderProjectTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	v, err := decode[projectTaskReorderReq](r)
	if err != nil {
		httpErr(w, http.StatusBadRequest, err)
		return
	}
	v.ProjectID = strings.TrimSpace(v.ProjectID)
	if v.ProjectID == "" {
		httpErr(w, http.StatusBadRequest, errors.New("projectId required"))
		return
	}
	if err := s.store.ReorderQueuedProjectTasks(r.Context(), v.ProjectID, v.TaskIDs); err != nil {
		httpErr(w, queueStatus(err), err)
		return
	}
	items, err := s.store.ListProjectTasks(r.Context(), v.ProjectID)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	jsonOut(w, http.StatusOK, items)
}

func queueStatus(err error) int {
	if errors.Is(err, sql.ErrNoRows) {
		return http.StatusNotFound
	}
	return http.StatusConflict
}
