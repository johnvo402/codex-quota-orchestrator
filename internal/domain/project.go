package domain

import "time"

type ProjectQueueMode string

const (
	ProjectQueueAuto   ProjectQueueMode = "AUTO"
	ProjectQueueManual ProjectQueueMode = "MANUAL"
	ProjectQueuePaused ProjectQueueMode = "PAUSED"
)

func (m ProjectQueueMode) Valid() bool {
	switch m {
	case ProjectQueueAuto, ProjectQueueManual, ProjectQueuePaused:
		return true
	default:
		return false
	}
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Path        string    `json:"path,omitempty"`
	Description string    `json:"description,omitempty"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type TaskEvent struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"taskId"`
	FromState TaskState `json:"fromState"`
	ToState   TaskState `json:"toState"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}
