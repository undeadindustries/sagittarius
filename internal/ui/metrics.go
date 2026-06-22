package ui

import "time"

// ModelUsageStat holds token counts (and optional cost) for one
// (provider, model, mode) triple observed during the session.
type ModelUsageStat struct {
	Provider  string
	Model     string
	Mode      string // "agent", "plan", "ask", "debug"
	Requests  int
	InTokens  int
	OutTokens int
	CostUSD   float64
	CostKnown bool
}

// SessionStats is a UI-facing snapshot of session telemetry. It carries no
// provider types so the agent/UI seam (AD-004) stays clean: the agent layer
// fills it, the bubbletea footer and exit screen render it.
type SessionStats struct {
	SessionID string
	Provider  string
	Model     string

	Turns        int
	ToolCalls    int
	ToolFailures int

	// InputTokens / OutputTokens are cumulative session totals.
	InputTokens  int
	OutputTokens int

	// SessionCostUSD / SessionCostKnown are the cumulative session cost.
	// SessionCostKnown is true only when at least one request reported a cost
	// (currently only OpenRouter).
	SessionCostUSD   float64
	SessionCostKnown bool

	// LastInputTokens / LastOutputTokens are the token counts for the most
	// recently completed main turn (not compression). Shown in the footer
	// next to the model label.
	LastInputTokens  int
	LastOutputTokens int

	// LastCostUSD / LastCostKnown are the cost for the last main turn.
	LastCostUSD   float64
	LastCostKnown bool

	// ContextTokens is the estimated size of the current context window and
	// ContextLimit its capacity (0 when no limit is known, e.g. off the
	// openai-chat path). ContextPercent derives the footer usage figure.
	ContextTokens int
	ContextLimit  int

	// Duration is the wall-clock session length.
	Duration time.Duration

	// ModelUsage breaks down token counts by provider+model+mode.
	// Empty when no generate calls have been recorded.
	ModelUsage []ModelUsageStat
}

// ContextPercent returns the share of the context window in use (0–100), or -1
// when no limit is known so callers can omit the figure.
func (s SessionStats) ContextPercent() int {
	if s.ContextLimit <= 0 {
		return -1
	}
	pct := s.ContextTokens * 100 / s.ContextLimit
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}

// MetricsProvider is an optional capability the TUI uses to read live session
// telemetry for the footer and exit summary. The agent App implements it.
type MetricsProvider interface {
	SessionMetrics() SessionStats
}
