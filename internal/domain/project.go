package domain

import "time"

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
