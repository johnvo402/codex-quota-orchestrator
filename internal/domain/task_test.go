package domain

import "testing"

func TestTransitions(t *testing.T) {
	valid := [][2]TaskState{{StateRunning, StatePauseRequested}, {StatePauseRequested, StatePausedQuota}, {StatePausedQuota, StateResumeQueued}, {StateResumeQueued, StateRunning}}
	for _, p := range valid {
		if err := ValidateTransition(p[0], p[1]); err != nil {
			t.Fatalf("expected %s -> %s valid: %v", p[0], p[1], err)
		}
	}
	if err := ValidateTransition(StateCompleted, StateRunning); err == nil {
		t.Fatal("completed must not transition back to running")
	}
}
