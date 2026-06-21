package contextmgmt

import "math"

// defaultProactiveCompressAt is the clamp fallback for a non-finite trigger.
const defaultProactiveCompressAt = 0.8

// PreTurnBudgetInput holds the inputs for a single pre-turn budget assessment.
type PreTurnBudgetInput struct {
	// CurrentHistoryTokens is the estimated token count of existing history.
	CurrentHistoryTokens int
	// EstimatedRequestTokens is the estimated token count of this turn's input.
	EstimatedRequestTokens int
	// ContextLimit is the total available context window in tokens.
	ContextLimit int
	// ReservedResponseTokens is budget reserved for the model's reply.
	ReservedResponseTokens int
	// ProactiveCompressAt is the fraction of ContextLimit at/above which a
	// proactive compression is triggered. Values outside [0,1] are clamped.
	ProactiveCompressAt float64
}

// PreTurnBudgetAssessment is the result of AssessTurnBudget.
type PreTurnBudgetAssessment struct {
	// ShouldCompressFirst reports whether the caller should compress before the turn.
	ShouldCompressFirst bool
	// ProjectedFraction is the projected fraction of the context window used.
	ProjectedFraction float64
	// ProjectedTokens is the absolute projected token count.
	ProjectedTokens int
}

// AssessTurnBudget computes whether to pre-emptively compress before this turn.
//
// A non-positive ContextLimit yields ShouldCompressFirst=false (no meaningful
// decision is possible; reactive recovery handles real overflow). Negative
// inputs are clamped to zero and ProactiveCompressAt is clamped to [0,1].
func AssessTurnBudget(input PreTurnBudgetInput) PreTurnBudgetAssessment {
	if input.ContextLimit <= 0 {
		return PreTurnBudgetAssessment{}
	}

	history := maxInt(0, input.CurrentHistoryTokens)
	request := maxInt(0, input.EstimatedRequestTokens)
	reserved := maxInt(0, input.ReservedResponseTokens)
	projected := history + request + reserved
	fraction := float64(projected) / float64(input.ContextLimit)

	return PreTurnBudgetAssessment{
		ShouldCompressFirst: fraction >= clampUnit(input.ProactiveCompressAt),
		ProjectedFraction:   fraction,
		ProjectedTokens:     projected,
	}
}

func clampUnit(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return defaultProactiveCompressAt
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
