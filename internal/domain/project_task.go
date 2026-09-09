package domain

import "time"

type ProjectTaskState string

const (
	ProjectTaskQueued      ProjectTaskState = "QUEUED"
	ProjectTaskDispatching ProjectTaskState = "DISPATCHING"
	ProjectTaskDispatched  ProjectTaskState = "DISPATCHED"
	ProjectTaskRunning     ProjectTaskState = "RUNNING"
	ProjectTaskCompleted   ProjectTaskState = "COMPLETED"
	ProjectTaskCancelled   ProjectTaskState = "CANCELLED"
	ProjectTaskNeedsReview ProjectTaskState = "NEEDS_REVIEW"
)

type ProjectTask struct {
	ID             string           `json:"id"`
	ProjectID      string           `json:"projectId"`
	Objective      string           `json:"objective"`
	Details        string           `json:"details,omitempty"`
	Position       int64            `json:"position"`
	State          ProjectTaskState `json:"state"`
	TargetThreadID string           `json:"targetThreadId,omitempty"`
	ActionID       *int64           `json:"actionId,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
	StartedAt      *time.Time       `json:"startedAt,omitempty"`
	CompletedAt    *time.Time       `json:"completedAt,omitempty"`
}
