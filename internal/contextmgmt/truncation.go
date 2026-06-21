package contextmgmt

import "math"

// preserveLeadingEntries is the number of leading history entries always kept.
// These typically encode the initial system/environment context and the first
// user instruction that defines the task; dropping them would destroy the
// agent's understanding of its goal.
const preserveLeadingEntries = 2

// conservativeMaxTokens is returned when token estimation cannot be performed,
// so callers never under-truncate.
const conservativeMaxTokens = math.MaxInt32

// TruncationResult reports the outcome of a hard-truncation pass.
type TruncationResult struct {
	// NewHistory is the history with oldest entries dropped.
	NewHistory []Message
	// DroppedCount is the number of entries removed from the original history.
	DroppedCount int
	// NewTokenCount is the estimated token count of NewHistory.
	NewTokenCount int
}

// TruncateHistoryToFit drops oldest history entries until the estimated token
// count fits within targetTokens. It always preserves the leading entries and
// never splits a functionCall from its matching functionResponse, nor leaves a
// dangling functionResponse at the head of the kept window.
//
// It is a pure function: the input slice is not mutated.
func TruncateHistoryToFit(history []Message, targetTokens int, estimate EstimateFn) TruncationResult {
	if len(history) <= preserveLeadingEntries {
		return TruncationResult{
			NewHistory:    cloneHistory(history),
			DroppedCount:  0,
			NewTokenCount: estimateHistoryTokens(history, estimate),
		}
	}
	if targetTokens <= 0 {
		return TruncationResult{
			NewHistory:    cloneHistory(history),
			DroppedCount:  0,
			NewTokenCount: estimateHistoryTokens(history, estimate),
		}
	}

	leading := history[:preserveLeadingEntries]
	tail := history[preserveLeadingEntries:]
	droppedCount := 0
	currentTokens := estimateHistoryTokens(concatHistory(leading, tail), estimate)

	for currentTokens > targetTokens && len(tail) > 0 {
		dropCount := 1
		head := tail[0]
		if head.Role == RoleModel && containsFunctionCall(head) &&
			len(tail) > 1 && containsFunctionResponse(tail[1]) {
			dropCount = 2
		}
		tail = tail[dropCount:]
		droppedCount += dropCount

		for len(tail) > 0 && containsFunctionResponse(tail[0]) {
			tail = tail[1:]
			droppedCount++
		}

		currentTokens = estimateHistoryTokens(concatHistory(leading, tail), estimate)
	}

	return TruncationResult{
		NewHistory:    concatHistory(leading, tail),
		DroppedCount:  droppedCount,
		NewTokenCount: currentTokens,
	}
}

func estimateHistoryTokens(history []Message, estimate EstimateFn) int {
	if estimate == nil {
		return conservativeMaxTokens
	}
	parts := make([]Part, 0, len(history))
	for i := range history {
		parts = append(parts, history[i].Parts...)
	}
	return estimate(parts)
}

func containsFunctionCall(content Message) bool {
	for i := range content.Parts {
		if content.Parts[i].FunctionCall != nil {
			return true
		}
	}
	return false
}

func containsFunctionResponse(content Message) bool {
	for i := range content.Parts {
		if content.Parts[i].FunctionResponse != nil {
			return true
		}
	}
	return false
}
