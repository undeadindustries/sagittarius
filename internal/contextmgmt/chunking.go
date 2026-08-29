package contextmgmt

// Summarizer request budgeting. The compressor used to send ~80% of history in
// one call, which exceeds the model window once a session is large. These
// constants leave headroom for the compression prompt and the snapshot output,
// and bound how many sequential refine passes a single compression may spend.
const (
	summarizerInputFraction = 0.5
	maxSummarizerPasses     = 6
)

// splitIntoBudgetedChunks cuts history into slices each within budgetTokens.
// Cuts land only on safe user-turn boundaries (RoleUser and no function
// response) so a model functionCall is never separated from its responses.
// A single message over budget becomes its own chunk. Pure: the input slice
// is not mutated. A non-positive budget returns the whole history as one chunk.
func splitIntoBudgetedChunks(history []Message, budgetTokens int, estimate EstimateFn) [][]Message {
	if len(history) == 0 {
		return nil
	}
	if estimate == nil {
		estimate = EstimateTokens
	}
	if budgetTokens <= 0 {
		return [][]Message{cloneHistory(history)}
	}

	var chunks [][]Message
	start := 0
	acc := 0
	lastBoundary := 0

	flush := func(end int) {
		if end <= start {
			return
		}
		chunks = append(chunks, cloneHistory(history[start:end]))
		start = end
		acc = 0
		lastBoundary = start
	}

	for i := range history {
		tokens := estimate(history[i].Parts)
		if acc > 0 && acc+tokens > budgetTokens {
			switch {
			case lastBoundary > start:
				flush(lastBoundary)
			case !splitsToolPair(history, i):
				flush(i)
			default:
				flush(i - 1)
				acc = estimate(history[i-1].Parts) // flush zeroed acc; the call is now chunk-leading
			}
		}
		if acc == 0 && tokens > budgetTokens {
			chunks = append(chunks, cloneHistory(history[i:i+1]))
			start = i + 1
			acc = 0
			lastBoundary = start
			continue
		}
		acc += tokens
		if i+1 < len(history) && isChunkBoundary(history[i+1]) {
			lastBoundary = i + 1
		}
	}
	if start < len(history) {
		chunks = append(chunks, cloneHistory(history[start:]))
	}
	return chunks
}

func isChunkBoundary(m Message) bool {
	return m.Role == RoleUser && !hasFunctionResponse(m)
}

func splitsToolPair(history []Message, i int) bool {
	if i <= 0 || i >= len(history) {
		return false
	}
	return hasFunctionCall(history[i-1]) && hasFunctionResponse(history[i])
}

func summarizerBudget(limit int) int {
	if limit <= 0 {
		return 0
	}
	budget := int(float64(limit)*summarizerInputFraction) - summarizerRequestOverhead()
	if budget < 1 {
		return 1
	}
	return budget
}

// summarizerRequestOverhead is the token cost of the longest user-turn
// instruction a rolling pass appends (anchored snapshot + scratchpad cue).
// Subtracted from the chunk budget so pass 2+ stays inside EffectiveLimit
// after the prior snapshot is prepended.
func summarizerRequestOverhead() int {
	n := EstimateTokens([]Part{{Text: anchoredSnapshotInstruction + summarizerScratchpadCue}})
	if n < 1 {
		return 1
	}
	return n
}
