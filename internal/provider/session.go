package provider

import (
	"strings"
	"sync"
)

// ReasoningEffortLevel is a valid reasoning.effort value for the Responses API.
type ReasoningEffortLevel string

const (
	ReasoningMinimal ReasoningEffortLevel = "minimal"
	ReasoningLow     ReasoningEffortLevel = "low"
	ReasoningMedium  ReasoningEffortLevel = "medium"
	ReasoningHigh    ReasoningEffortLevel = "high"
)

// ValidReasoningLevels lists accepted reasoning effort values.
var ValidReasoningLevels = []ReasoningEffortLevel{
	ReasoningMinimal,
	ReasoningLow,
	ReasoningMedium,
	ReasoningHigh,
}

// sessionState holds the process-wide reasoning override.
//
// TODO(plan 02 — docs/plans/concurrency-cohesion-2026-06/02-provider-streaming-session.md):
// the /reasoning override is still a hidden process global. The clean version
// injects it onto the live generator (construction + a setter the slash command
// drives), but that rewire crosses the slash→app→runner→generator seam and risks
// losing the override on generator rebuild, so it is deferred. The Responses
// chaining id (the actual cross-session-bleed bug) is now per-generator.
type sessionState struct {
	mu                sync.RWMutex
	reasoningOverride string
}

var defaultSession = &sessionState{}

// SetSessionReasoningOverride sets a session-only reasoning effort override.
func SetSessionReasoningOverride(level string) {
	defaultSession.mu.Lock()
	defer defaultSession.mu.Unlock()
	defaultSession.reasoningOverride = strings.TrimSpace(level)
}

// ClearSessionReasoningOverride drops the session reasoning override.
func ClearSessionReasoningOverride() {
	defaultSession.mu.Lock()
	defer defaultSession.mu.Unlock()
	defaultSession.reasoningOverride = ""
}

// SessionReasoningOverride returns the active session override, if any.
func SessionReasoningOverride() string {
	defaultSession.mu.RLock()
	defer defaultSession.mu.RUnlock()
	return defaultSession.reasoningOverride
}

// ResolveReasoningEffort returns session override, then persisted provider value.
func ResolveReasoningEffort(persisted string) string {
	if override := SessionReasoningOverride(); override != "" {
		return override
	}
	return strings.TrimSpace(persisted)
}

// IsValidReasoningLevel reports whether level is an accepted effort value.
func IsValidReasoningLevel(level string) bool {
	level = strings.TrimSpace(level)
	for _, v := range ValidReasoningLevels {
		if string(v) == level {
			return true
		}
	}
	return false
}
