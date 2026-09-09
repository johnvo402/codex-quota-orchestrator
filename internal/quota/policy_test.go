package quota

import (
	"testing"

	"codex-desktop-quota-guard/internal/codexquota"
)

func TestFromRateLimitsSeparatesWindows(
	t *testing.T,
) {
	p := Policy{
		SoftThreshold:   10,
		HardThreshold:   5,
		ResumeThreshold: 20,
	}

	r := codexquota.RateLimitsResponse{
		RateLimits: codexquota.RateLimitBucket{
			LimitID: "codex",

			Primary: &codexquota.RateLimitWindow{
				UsedPercent:        11,
				WindowDurationMins: 300,
				ResetsAt:           1788953836,
			},

			Secondary: &codexquota.RateLimitWindow{
				UsedPercent:        47,
				WindowDurationMins: 10080,
				ResetsAt:           1789447924,
			},
		},
	}

	got := FromRateLimits(r, p)

	if !got.FiveHour.Available {
		t.Fatal("5h window should be available")
	}

	if got.FiveHour.RemainingPercent != 89 {
		t.Fatalf(
			"5h remaining = %v, want 89",
			got.FiveHour.RemainingPercent,
		)
	}

	if !got.Weekly.Available {
		t.Fatal("weekly window should be available")
	}

	if got.Weekly.RemainingPercent != 53 {
		t.Fatalf(
			"weekly remaining = %v, want 53",
			got.Weekly.RemainingPercent,
		)
	}

	if got.RemainingPercent != 53 {
		t.Fatalf(
			"effective = %v, want 53",
			got.RemainingPercent,
		)
	}

	if got.SoftPause {
		t.Fatal("quota should still be healthy")
	}

	if !got.CanResume {
		t.Fatal("quota should be resumable")
	}
}

func TestWeeklyCanPauseIndependently(
	t *testing.T,
) {
	p := Policy{
		SoftThreshold:   10,
		HardThreshold:   5,
		ResumeThreshold: 20,
	}

	r := codexquota.RateLimitsResponse{
		RateLimits: codexquota.RateLimitBucket{
			Primary: &codexquota.RateLimitWindow{
				UsedPercent:        10,
				WindowDurationMins: 300,
			},

			Secondary: &codexquota.RateLimitWindow{
				UsedPercent:        94,
				WindowDurationMins: 10080,
			},
		},
	}

	got := FromRateLimits(r, p)

	if !got.SoftPause {
		t.Fatal("weekly quota should trigger pause")
	}

	if got.PauseReason != "weekly" {
		t.Fatalf(
			"reason=%q, want weekly",
			got.PauseReason,
		)
	}
}

func TestFiveHourCanPauseIndependently(
	t *testing.T,
) {
	p := Policy{
		SoftThreshold:   10,
		HardThreshold:   5,
		ResumeThreshold: 20,
	}

	r := codexquota.RateLimitsResponse{
		RateLimits: codexquota.RateLimitBucket{
			Primary: &codexquota.RateLimitWindow{
				UsedPercent:        94,
				WindowDurationMins: 300,
			},

			Secondary: &codexquota.RateLimitWindow{
				UsedPercent:        30,
				WindowDurationMins: 10080,
			},
		},
	}

	got := FromRateLimits(r, p)

	if !got.SoftPause {
		t.Fatal("5h quota should trigger pause")
	}

	if got.PauseReason != "five_hour" {
		t.Fatalf(
			"reason=%q, want five_hour",
			got.PauseReason,
		)
	}
}
