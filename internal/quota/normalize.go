package quota

import (
	"math"
	"time"

	"codex-desktop-quota-guard/internal/codexquota"
)

const (
	FiveHourWindowMinutes = 300
	WeeklyWindowMinutes   = 10080
)

func FromRateLimits(
	r codexquota.RateLimitsResponse,
	p Policy,
) Snapshot {
	bucket := r.RateLimits

	// Ưu tiên bucket Codex nếu backend trả map.
	if v, ok := r.RateLimitsByID["codex"]; ok {
		bucket = v
	}

	snapshot := Snapshot{
		ObservedAt: time.Now().UTC(),
	}

	blocked := false

	if bucket.RateLimitReachedType != nil &&
		*bucket.RateLimitReachedType != "" {
		blocked = true
	}

	windows := []*codexquota.RateLimitWindow{
		bucket.Primary,
		bucket.Secondary,
	}

	for _, raw := range windows {
		if raw == nil {
			continue
		}

		window := normalizeWindow(raw)

		switch raw.WindowDurationMins {
		case FiveHourWindowMinutes:
			snapshot.FiveHour = window

		case WeeklyWindowMinutes:
			snapshot.Weekly = window
		}
	}

	// Effective quota = window thấp nhất.
	effective := 100.0
	seen := false

	var limitingReset *time.Time

	if snapshot.FiveHour.Available {
		effective = snapshot.FiveHour.RemainingPercent
		limitingReset = snapshot.FiveHour.ResetAt
		seen = true
	}

	if snapshot.Weekly.Available {
		if !seen ||
			snapshot.Weekly.RemainingPercent < effective {
			effective = snapshot.Weekly.RemainingPercent
			limitingReset = snapshot.Weekly.ResetAt
		}

		seen = true
	}

	if !seen {
		effective = 0
		blocked = true
	}

	snapshot.RemainingPercent = effective
	snapshot.ResetAt = limitingReset

	fiveHourHard :=
		snapshot.FiveHour.Available &&
			snapshot.FiveHour.RemainingPercent <= p.HardThreshold

	weeklyHard :=
		snapshot.Weekly.Available &&
			snapshot.Weekly.RemainingPercent <= p.HardThreshold

	fiveHourSoft :=
		snapshot.FiveHour.Available &&
			snapshot.FiveHour.RemainingPercent <= p.SoftThreshold

	weeklySoft :=
		snapshot.Weekly.Available &&
			snapshot.Weekly.RemainingPercent <= p.SoftThreshold

	snapshot.HardPause =
		blocked ||
			fiveHourHard ||
			weeklyHard

	snapshot.SoftPause =
		blocked ||
			fiveHourSoft ||
			weeklySoft

	snapshot.PauseReason = pauseReason(
		blocked,
		fiveHourHard || fiveHourSoft,
		weeklyHard || weeklySoft,
	)

	// Resume only when every available window meets its own resume threshold.
	// Soft/hard pause is evaluated separately by the caller, so a threshold
	// below SoftThreshold simply means that the soft threshold is the effective
	// lower bound for that window.
	canResume := seen && !blocked

	if snapshot.FiveHour.Available &&
		snapshot.FiveHour.RemainingPercent < p.fiveHourResumeThreshold() {
		canResume = false
	}

	if snapshot.Weekly.Available &&
		snapshot.Weekly.RemainingPercent < p.weeklyResumeThreshold() {
		canResume = false
	}

	snapshot.CanResume = canResume

	return snapshot
}

func normalizeWindow(
	raw *codexquota.RateLimitWindow,
) Window {
	used := math.Max(
		0,
		math.Min(100, raw.UsedPercent),
	)

	remaining := 100 - used

	var resetAt *time.Time

	if raw.ResetsAt > 0 {
		t := time.Unix(raw.ResetsAt, 0)
		resetAt = &t
	}

	return Window{
		Available:          true,
		UsedPercent:        used,
		RemainingPercent:   remaining,
		WindowDurationMins: raw.WindowDurationMins,
		ResetAt:            resetAt,
	}
}

func pauseReason(
	blocked bool,
	fiveHour bool,
	weekly bool,
) string {
	if blocked {
		return "backend_rate_limit"
	}

	switch {
	case fiveHour && weekly:
		return "five_hour_and_weekly"

	case fiveHour:
		return "five_hour"

	case weekly:
		return "weekly"

	default:
		return ""
	}
}
