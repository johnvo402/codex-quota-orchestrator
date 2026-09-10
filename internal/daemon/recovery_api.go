package daemon

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"codex-desktop-quota-guard/internal/domain"
	"codex-desktop-quota-guard/internal/store"
)

type recoveryItem struct {
	Source      string            `json:"source"`
	ID          string            `json:"id"`
	Objective   string            `json:"objective"`
	State       string            `json:"state"`
	ProjectID   string            `json:"projectId,omitempty"`
	ProjectName string            `json:"projectName,omitempty"`
	ThreadID    string            `json:"threadId,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Details     string            `json:"details,omitempty"`
	Action      *store.ActionInfo `json:"action,omitempty"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

func (s *Server) dashboardRecovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	archived := false
	managed, err := s.store.ListDashboardTasks(r.Context(), store.TaskFilter{
		State:    string(domain.StateNeedsReview),
		Archived: &archived,
	})
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]recoveryItem, 0, len(managed))
	for _, task := range managed {
		item := recoveryItem{
			Source:      "managed_task",
			ID:          task.ID,
			Objective:   task.Objective,
			State:       string(task.State),
			ProjectID:   task.ProjectID,
			ProjectName: task.ProjectName,
			ThreadID:    task.ThreadID,
			Reason:      task.PauseReason,
			UpdatedAt:   task.UpdatedAt,
		}
		if action, err := s.store.LatestRecoveryActionInfo(r.Context(), task.ThreadID); err == nil {
			item.Action = &action
		} else if !errors.Is(err, sql.ErrNoRows) {
			httpErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, item)
	}

	queued, err := s.store.ListProjectTasksNeedingReview(r.Context())
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	projectNames := map[string]string{}
	for _, task := range queued {
		name, ok := projectNames[task.ProjectID]
		if !ok {
			project, err := s.store.GetProject(r.Context(), task.ProjectID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					name = "Deleted project"
				} else {
					httpErr(w, http.StatusInternalServerError, err)
					return
				}
			} else {
				name = project.Name
			}
			projectNames[task.ProjectID] = name
		}
		item := recoveryItem{
			Source:      "project_task",
			ID:          task.ID,
			Objective:   task.Objective,
			State:       string(task.State),
			ProjectID:   task.ProjectID,
			ProjectName: name,
			ThreadID:    task.TargetThreadID,
			Details:     task.Details,
			UpdatedAt:   task.UpdatedAt,
		}
		if task.ActionID != nil {
			if action, err := s.store.GetActionInfo(r.Context(), *task.ActionID); err == nil {
				item.Action = &action
			} else if !errors.Is(err, sql.ErrNoRows) {
				httpErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		if item.Action != nil && item.Action.Status == "uncertain" {
			item.Reason = "Desktop dispatch outcome is uncertain"
		} else {
			item.Reason = "Queued project task requires manual review"
		}
		out = append(out, item)
	}

	jsonOut(w, http.StatusOK, out)
}
