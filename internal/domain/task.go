package domain

import (
	"errors"
	"time"
)

type TaskState string

const (
	StateRunning        TaskState = "RUNNING"
	StatePauseRequested TaskState = "PAUSE_REQUESTED"
	StatePausedQuota    TaskState = "PAUSED_QUOTA"
	StateResumeQueued   TaskState = "RESUME_QUEUED"

	StateNeedsReview TaskState = "NEEDS_REVIEW"

	StateCompleted TaskState = "COMPLETED"
	StateFailed    TaskState = "FAILED"
	StateCancelled TaskState = "CANCELLED"
)

type Task struct {
	ID                 string     `json:"id"`
	ThreadID           string     `json:"threadId"`
	TurnID             string     `json:"turnId,omitempty"`
	Objective          string     `json:"objective"`
	Workspace          string     `json:"workspace,omitempty"`
	State              TaskState  `json:"state"`
	PauseReason        string     `json:"pauseReason,omitempty"`
	Checkpoint         string     `json:"checkpoint,omitempty"`
	Pending            string     `json:"pending,omitempty"`
	LastTest           string     `json:"lastTest,omitempty"`
	LastQuotaRemaining *float64   `json:"lastQuotaRemaining,omitempty"`
	LastQuotaResetAt   *time.Time `json:"lastQuotaResetAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func CanTransition(from, to TaskState) bool {
	allowed := map[TaskState]map[TaskState]bool{
		StateRunning: {
			StatePauseRequested: true,
			StateNeedsReview:    true,
			StateCompleted:      true,
			StateCancelled:      true,
		},
		StatePauseRequested: {
			StatePausedQuota: true,
			StateRunning:     true,
			StateNeedsReview: true,
			StateCompleted:   true,
			StateCancelled:   true,
		},
		StatePausedQuota: {
			StateResumeQueued: true,
			StateNeedsReview:  true,
			StateCompleted:    true,
			StateCancelled:    true,
		},
		StateResumeQueued: {
			StateRunning:     true,
			StatePausedQuota: true,
			StateNeedsReview: true,
			StateCompleted:   true,
			StateCancelled:   true,
		},
		StateNeedsReview: {
			StatePausedQuota: true,
			StateRunning:     true,
			StateCompleted:   true,
			StateCancelled:   true,
		},
	}
	if from == to {
		return true
	}
	return allowed[from][to]
}

func ValidateTransition(from, to TaskState) error {
	if !CanTransition(from, to) {
		return errors.New("invalid task transition: " + string(from) + " -> " + string(to))
	}
	return nil
}
