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

type sessionState struct {
	mu                sync.RWMutex
	reasoningOverride string
	lastResponseID    string
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

// SetLastResponseID stores the trailing response id for chaining.
func SetLastResponseID(id string) {
	defaultSession.mu.Lock()
	defer defaultSession.mu.Unlock()
	defaultSession.lastResponseID = strings.TrimSpace(id)
}

// LastResponseID returns the stored response id for chaining.
func LastResponseID() string {
	defaultSession.mu.RLock()
	defer defaultSession.mu.RUnlock()
	return defaultSession.lastResponseID
}

// ClearLastResponseID clears the stored response id after errors.
func ClearLastResponseID() {
	defaultSession.mu.Lock()
	defer defaultSession.mu.Unlock()
	defaultSession.lastResponseID = ""
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
