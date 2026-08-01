package provider

import "strings"

// ReasoningEffortLevel is a valid reasoning.effort value for the Responses API.
type ReasoningEffortLevel string

const (
	ReasoningNone    ReasoningEffortLevel = "none"
	ReasoningMinimal ReasoningEffortLevel = "minimal"
	ReasoningLow     ReasoningEffortLevel = "low"
	ReasoningMedium  ReasoningEffortLevel = "medium"
	ReasoningHigh    ReasoningEffortLevel = "high"
	ReasoningXHigh   ReasoningEffortLevel = "xhigh"
)

// ValidReasoningLevels lists accepted reasoning effort values across every
// known family (see config.ModelReasoningRule for which subset a given model
// actually accepts — this is the generic settings-validation superset, used
// where the specific model isn't known, e.g. persisting a raw settings value).
var ValidReasoningLevels = []ReasoningEffortLevel{
	ReasoningNone,
	ReasoningMinimal,
	ReasoningLow,
	ReasoningMedium,
	ReasoningHigh,
	ReasoningXHigh,
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
