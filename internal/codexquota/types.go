package codexquota

import "encoding/json"

type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Thread struct {
	ID string `json:"id"`
}
type Turn struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Error  json.RawMessage `json:"error"`
}
type ThreadStartResponse struct {
	Thread Thread `json:"thread"`
}
type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type AccountReadResponse struct {
	Account *struct {
		Type     string `json:"type"`
		Email    string `json:"email"`
		PlanType string `json:"planType"`
	} `json:"account"`
	RequiresOpenAIAuth bool `json:"requiresOpenAIAuth"`
}

type RateLimitWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int     `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

type RateLimitBucket struct {
	LimitID              string           `json:"limitId"`
	LimitName            *string          `json:"limitName"`
	PlanType             string           `json:"planType"`
	Primary              *RateLimitWindow `json:"primary"`
	Secondary            *RateLimitWindow `json:"secondary"`
	RateLimitReachedType *string          `json:"rateLimitReachedType"`
}

type RateLimitsResponse struct {
	RateLimits        RateLimitBucket            `json:"rateLimits"`
	RateLimitsByID    map[string]RateLimitBucket `json:"rateLimitsByLimitId"`
	RateLimitResetRaw json.RawMessage            `json:"rateLimitResetCredits"`
}
