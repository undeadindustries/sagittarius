package ui

import "time"

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

	// InputTokens / OutputTokens are cumulative estimates across the session
	// (heuristic, not provider-reported usage).
	InputTokens  int
	OutputTokens int

	// ContextTokens is the estimated size of the current context window and
	// ContextLimit its capacity (0 when no limit is known, e.g. off the
	// openai-chat path). ContextPercent derives the footer usage figure.
	ContextTokens int
	ContextLimit  int

	// Duration is the wall-clock session length.
	Duration time.Duration
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
