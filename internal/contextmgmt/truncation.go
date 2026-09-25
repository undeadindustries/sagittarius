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
// count fits within targetTokens.
//
// Only whole turns between the leading entries and the current turn are
// eligible. The current turn runs from the last user text message to the end,
// and dropping it would send the provider a history that no longer contains
// the question being answered. After compression the leading entries end on
// the canned model ack, so losing the current turn also leaves the request
// ending on an assistant message, which Anthropic rejects outright and every
// other provider answers as if the ack were the prompt. A turn starts at a user
// text message, so dropping whole turns never separates a functionCall from its
// functionResponse.
//
// When the leading entries plus the current turn still exceed targetTokens,
// NewTokenCount is left above the target: shrinking an oversized message is
// CapOversizedMessages' job, not this function's.
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

	leadEnd := preserveLeadingEntries
	for leadEnd < len(history) && containsFunctionResponse(history[leadEnd]) {
		leadEnd++
	}
	suffixStart := max(leadEnd, lastUserTextIndex(history))

	leading := history[:leadEnd]
	middle := history[leadEnd:suffixStart]
	suffix := history[suffixStart:]
	kept := func() []Message {
		out := make([]Message, 0, len(leading)+len(middle)+len(suffix))
		out = append(out, leading...)
		out = append(out, middle...)
		return append(out, suffix...)
	}

	droppedCount := 0
	currentTokens := estimateHistoryTokens(kept(), estimate)
	for currentTokens > targetTokens && len(middle) > 0 {
		n := leadingTurnLength(middle)
		middle = middle[n:]
		droppedCount += n
		currentTokens = estimateHistoryTokens(kept(), estimate)
	}

	return TruncationResult{
		NewHistory:    kept(),
		DroppedCount:  droppedCount,
		NewTokenCount: currentTokens,
	}
}

// isUserTextMessage reports whether m opens a turn. Tool results also travel in
// the user role, but they belong to the turn of the call that produced them.
func isUserTextMessage(m Message) bool {
	return m.Role == RoleUser && !containsFunctionResponse(m)
}

// lastUserTextIndex returns the index of the last turn-opening message, or -1.
func lastUserTextIndex(history []Message) int {
	for i := len(history) - 1; i >= 0; i-- {
		if isUserTextMessage(history[i]) {
			return i
		}
	}
	return -1
}

// leadingTurnLength returns how many entries at the head of msgs belong to one
// turn: the first entry plus everything before the next turn-opening message.
func leadingTurnLength(msgs []Message) int {
	n := 1
	for n < len(msgs) && !isUserTextMessage(msgs[n]) {
		n++
	}
	return n
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
