package contextmgmt

// ReasoningPruneConfig configures batched reasoning retention pruning.
// It shares the protection and prunable token thresholds with tool-output
// masking so no additional settings knob is required.
type ReasoningPruneConfig struct {
	// ProtectionThresholdTokens is the recent reasoning token budget kept raw
	// (prior to the latest user message).
	ProtectionThresholdTokens int
	// MinPrunableThresholdTokens is the minimum unprotected reasoning token
	// buffer required before a batch prune fires.
	MinPrunableThresholdTokens int
}

// ReasoningPruneResult reports the outcome of a batched reasoning prune pass.
type ReasoningPruneResult struct {
	// NewHistory has unprotected older Part.Reasoning fields cleared when a
	// batch fired, or the original slice when below threshold.
	NewHistory []Message
	// PrunedCount is the number of assistant messages whose reasoning was cleared.
	PrunedCount int
	// TokensSaved is the estimated reasoning tokens reclaimed.
	TokensSaved int
}

// PruneReasoning scans history newest-to-oldest, unconditionally protecting
// every assistant turn after the latest genuine user message (active tool loop)
// plus newer reasoning turns up to cfg.ProtectionThresholdTokens. Once the
// unprotected reasoning buffer reaches cfg.MinPrunableThresholdTokens, it
// clears Part.Reasoning on all unprotected turns in a single batch (one prefix
// break, many turns reclaimed). The input history is not mutated.
func PruneReasoning(history []Message, cfg ReasoningPruneConfig, estimate EstimateFn) ReasoningPruneResult {
	if len(history) == 0 {
		return ReasoningPruneResult{NewHistory: history}
	}
	if estimate == nil {
		estimate = EstimateTokens
	}

	lastUserIdx := lastGenuineUserIndex(history)
	if lastUserIdx <= 0 {
		// Either no user message exists or only the first message is a user
		// message (every assistant turn is inside the active user turn).
		return ReasoningPruneResult{NewHistory: history}
	}

	type prunableMsg struct {
		index  int
		tokens int
	}

	var prunable []prunableMsg
	cumulative := 0
	boundaryReached := false
	totalPrunable := 0

	for i := lastUserIdx - 1; i >= 0; i-- {
		tokens := messageReasoningTokens(history[i], estimate)
		if tokens <= 0 {
			continue
		}
		if !boundaryReached {
			cumulative += tokens
			if cumulative > cfg.ProtectionThresholdTokens {
				boundaryReached = true
				totalPrunable += tokens
				prunable = append(prunable, prunableMsg{index: i, tokens: tokens})
			}
			continue
		}
		totalPrunable += tokens
		prunable = append(prunable, prunableMsg{index: i, tokens: tokens})
	}

	if len(prunable) == 0 || totalPrunable < cfg.MinPrunableThresholdTokens {
		return ReasoningPruneResult{NewHistory: history}
	}

	newHistory := cloneHistory(history)
	saved := 0
	for _, item := range prunable {
		newHistory[item.index] = stripMessageReasoning(newHistory[item.index])
		saved += item.tokens
	}
	return ReasoningPruneResult{
		NewHistory:  newHistory,
		PrunedCount: len(prunable),
		TokensSaved: saved,
	}
}

// StripAllReasoning clears Part.Reasoning on every assistant message and
// returns the number of messages changed. It exists so history never carries
// reasoning the wire will not replay: token estimates, masking, truncation, and
// the context gauge would otherwise budget for text the model never receives.
// The input history is not mutated; it is returned as-is when nothing changed.
func StripAllReasoning(history []Message) ([]Message, int) {
	var out []Message
	stripped := 0
	for i := range history {
		if !messageHasReasoning(history[i]) {
			continue
		}
		if out == nil {
			out = cloneHistory(history)
		}
		out[i] = stripMessageReasoning(out[i])
		stripped++
	}
	if out == nil {
		return history, 0
	}
	return out, stripped
}

func messageHasReasoning(msg Message) bool {
	if msg.Role != RoleModel {
		return false
	}
	for i := range msg.Parts {
		if msg.Parts[i].Reasoning != "" {
			return true
		}
	}
	return false
}

// lastGenuineUserIndex returns the index of the latest RoleUser message that
// is not a tool-response carrier, or -1 if none exists.
func lastGenuineUserIndex(history []Message) int {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser && !hasFunctionResponse(history[i]) {
			return i
		}
	}
	return -1
}

func messageReasoningTokens(msg Message, estimate EstimateFn) int {
	if msg.Role != RoleModel {
		return 0
	}
	total := 0
	for _, p := range msg.Parts {
		if p.Reasoning != "" {
			total += estimate([]Part{{Reasoning: p.Reasoning}})
		}
	}
	return total
}

func stripMessageReasoning(msg Message) Message {
	if len(msg.Parts) == 0 {
		return msg
	}
	out := make([]Part, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		if p.Reasoning == "" {
			out = append(out, p)
			continue
		}
		p.Reasoning = ""
		if p.Text == "" && p.FunctionCall == nil && p.FunctionResponse == nil && len(p.ThoughtSignature) == 0 {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		out = []Part{{}}
	}
	return Message{Role: msg.Role, Parts: out}
}
