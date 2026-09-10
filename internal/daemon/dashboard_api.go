package daemon

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/store"
)

type projectReq struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Archived    *bool  `json:"archived"`
}

type taskMetaReq struct {
	Objective string `json:"objective"`
	ProjectID string `json:"projectId"`
	Notes     string `json:"notes"`
	Archived  *bool  `json:"archived"`
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		includeArchived := r.URL.Query().Get("archived") == "all"
		items, err := s.store.ListProjects(r.Context(), includeArchived)
		if err != nil {
			httpErr(w, 500, err)
			return
		}
		jsonOut(w, 200, items)
	case http.MethodPost:
		v, err := decode[projectReq](r)
		if err != nil {
			httpErr(w, 400, err)
			return
		}
		p, err := s.store.CreateProject(r.Context(), v.Name, v.Path, v.Description)
		if err != nil {
			httpErr(w, 400, err)
			return
		}
		jsonOut(w, http.StatusCreated, p)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) projectRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/projects/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	id := parts[0]
	if len(parts) == 2 && parts[1] == "tasks" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		archived := false
		items, err := s.store.ListDashboardTasks(r.Context(), store.TaskFilter{ProjectID: id, Archived: &archived})
		if err != nil {
			httpErr(w, 500, err)
			return
		}
		jsonOut(w, 200, items)
		return
	}
	if len(parts) == 2 && parts[1] == "queue-mode" {
		s.projectQueueMode(w, r, id)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		p, err := s.store.GetProject(r.Context(), id)
		if err != nil {
			httpErr(w, statusForStoreErr(err), err)
			return
		}
		jsonOut(w, 200, p)
	case http.MethodPatch:
		v, err := decode[projectReq](r)
		if err != nil {
			httpErr(w, 400, err)
			return
		}
		p, err := s.store.UpdateProject(r.Context(), id, v.Name, v.Path, v.Description, v.Archived)
		if err != nil {
			httpErr(w, statusForStoreErr(err), err)
			return
		}
		jsonOut(w, 200, p)
	case http.MethodDelete:
		if err := s.store.DeleteProject(r.Context(), id); err != nil {
			httpErr(w, statusForStoreErr(err), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) dashboardTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var archived *bool
	switch r.URL.Query().Get("archived") {
	case "all":
		archived = nil
	case "true":
		v := true
		archived = &v
	default:
		v := false
		archived = &v
	}
	items, err := s.store.ListDashboardTasks(r.Context(), store.TaskFilter{
		ProjectID: r.URL.Query().Get("projectId"),
		State:     r.URL.Query().Get("state"),
		Query:     r.URL.Query().Get("q"),
		Archived:  archived,
	})
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	jsonOut(w, 200, items)
}

func (s *Server) dashboardTaskRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/dashboard/tasks/"), "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	if path == "recovery" {
		s.dashboardRecovery(w, r)
		return
	}
	parts := strings.Split(path, "/")
	id := parts[0]
	if len(parts) == 2 {
		s.taskDashboardAction(w, r, id, parts[1])
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		t, err := s.store.GetDashboardTask(r.Context(), id)
		if err != nil {
			httpErr(w, statusForStoreErr(err), err)
			return
		}
		jsonOut(w, 200, t)
	case http.MethodPatch:
		v, err := decode[taskMetaReq](r)
		if err != nil {
			httpErr(w, 400, err)
			return
		}
		t, err := s.store.SetTaskDashboardMetadata(r.Context(), id, v.Objective, v.ProjectID, v.Notes, v.Archived)
		if err != nil {
			httpErr(w, statusForStoreErr(err), err)
			return
		}
		jsonOut(w, 200, t)
	case http.MethodDelete:
		if err := s.store.DeleteTask(r.Context(), id); err != nil {
			status := http.StatusConflict
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			httpErr(w, status, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) taskDashboardAction(w http.ResponseWriter, r *http.Request, id, action string) {
	if r.Method != http.MethodPost && !(action == "events" && r.Method == http.MethodGet) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	t, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		httpErr(w, statusForStoreErr(err), err)
		return
	}

	switch action {
	case "events":
		items, err := s.store.ListTaskEvents(r.Context(), id)
		if err != nil {
			httpErr(w, 500, err)
			return
		}
		jsonOut(w, 200, items)
	case "cancel":
		updated, err := s.store.SafeCancelManagedTask(r.Context(), t.ThreadID)
		if err != nil {
			httpErr(w, http.StatusConflict, err)
			return
		}
		jsonOut(w, 200, updated)
	case "pause":
		updated, err := s.store.Transition(r.Context(), t.ThreadID, domain.StatePauseRequested, "pause requested from dashboard")
		if err != nil {
			httpErr(w, http.StatusConflict, err)
			return
		}
		message := "[Desktop Quota Guard] Pause requested from the local dashboard. Reach the next safe boundary, checkpoint, call task_mark_paused, then finish this turn."
		_, _ = s.store.EnqueueAction(r.Context(), "pause_notice", t.ThreadID, message)
		jsonOut(w, 200, updated)
	case "retry":
		if t.State != domain.StateNeedsReview {
			httpErr(w, http.StatusConflict, errors.New("retry is only available for NEEDS_REVIEW tasks"))
			return
		}
		updated, err := s.store.Transition(r.Context(), t.ThreadID, domain.StatePausedQuota, "manual dashboard recovery: retry resume")
		if err != nil {
			httpErr(w, http.StatusConflict, err)
			return
		}
		jsonOut(w, 200, updated)
	case "running":
		if t.State != domain.StateNeedsReview {
			httpErr(w, http.StatusConflict, errors.New("running recovery is only available for NEEDS_REVIEW tasks"))
			return
		}
		updated, err := s.store.Transition(r.Context(), t.ThreadID, domain.StateRunning, "manual dashboard recovery: confirmed already running")
		if err != nil {
			httpErr(w, http.StatusConflict, err)
			return
		}
		jsonOut(w, 200, updated)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) dashboardSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	projects, err := s.store.ListProjects(r.Context(), false)
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	archived := false
	tasks, err := s.store.ListDashboardTasks(r.Context(), store.TaskFilter{Archived: &archived})
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	counts := map[string]int{}
	for _, t := range tasks {
		counts[string(t.State)]++
	}
	queueReview, err := s.store.ListProjectTasksNeedingReview(r.Context())
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	q, d, qErr := s.service.CurrentDecision(r.Context())
	out := map[string]any{
		"projects":    len(projects),
		"tasks":       len(tasks),
		"states":      counts,
		"reviewCount": counts[string(domain.StateNeedsReview)] + len(queueReview),
	}
	if qErr == nil {
		out["quota"] = q
		out["decision"] = d
	}
	jsonOut(w, 200, out)
}

func statusForStoreErr(err error) int {
	if errors.Is(err, sql.ErrNoRows) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}
