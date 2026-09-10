package quota

import "time"

type Policy struct {
	SoftThreshold             float64
	HardThreshold             float64
	FiveHourResumeThreshold   float64
	WeeklyResumeThreshold     float64

	// ResumeThreshold is kept as a compatibility fallback for older internal
	// callers/tests. New code should set the per-window thresholds above.
	ResumeThreshold float64
}

type Window struct {
	Available          bool       `json:"available"`
	UsedPercent        float64    `json:"usedPercent"`
	RemainingPercent   float64    `json:"remainingPercent"`
	WindowDurationMins int        `json:"windowDurationMins"`
	ResetAt            *time.Time `json:"resetAt,omitempty"`
}

type Snapshot struct {
	// Hai quota window thực tế.
	FiveHour Window `json:"fiveHour"`
	Weekly   Window `json:"weekly"`

	// Backward-compatible:
	// quota thấp nhất trong các window đang available.
	RemainingPercent float64    `json:"remainingPercent"`
	ResetAt          *time.Time `json:"resetAt,omitempty"`

	// Window nào gây ra pause.
	PauseReason string `json:"pauseReason,omitempty"`

	HardPause bool `json:"hardPause"`
	SoftPause bool `json:"softPause"`
	CanResume bool `json:"canResume"`

	ObservedAt time.Time `json:"observedAt"`
}

type Decision struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func (p Policy) Decide(s Snapshot) Decision {
	if s.HardPause || s.RemainingPercent <= p.HardThreshold {
		reason := s.PauseReason
		if reason == "" {
			reason = "hard quota threshold reached"
		}

		return Decision{
			Action: "pause",
			Reason: reason,
		}
	}

	if s.SoftPause || s.RemainingPercent <= p.SoftThreshold {
		reason := s.PauseReason
		if reason == "" {
			reason = "soft quota threshold reached"
		}

		return Decision{
			Action: "pause",
			Reason: reason,
		}
	}

	return Decision{
		Action: "continue",
		Reason: "quota healthy",
	}
}

func (p Policy) CanResume(s Snapshot) bool {
	return s.CanResume
}

func (p Policy) fiveHourResumeThreshold() float64 {
	if p.FiveHourResumeThreshold != 0 || p.ResumeThreshold == 0 {
		return p.FiveHourResumeThreshold
	}
	return p.ResumeThreshold
}

func (p Policy) weeklyResumeThreshold() float64 {
	if p.WeeklyResumeThreshold != 0 || p.ResumeThreshold == 0 {
		return p.WeeklyResumeThreshold
	}
	return p.ResumeThreshold
}
