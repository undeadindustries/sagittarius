package agent

import (
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// sessionMetrics accumulates per-process session telemetry: turn/tool counts and
// heuristic token estimates. Token figures use the same stdlib estimator as the
// context manager (contextmgmt.EstimateTokens) — they are approximations, not
// provider-reported usage. All access is mutex-guarded because RunTurn streams
// on a goroutine while the TUI reads snapshots from the render loop.
type sessionMetrics struct {
	mu            sync.Mutex
	start         time.Time
	turns         int
	toolCalls     int
	toolFailures  int
	inputTokens   int
	outputTokens  int
	contextTokens int
}

func newSessionMetrics() *sessionMetrics {
	return &sessionMetrics{start: time.Now()}
}

func (s *sessionMetrics) recordTurn() {
	s.mu.Lock()
	s.turns++
	s.mu.Unlock()
}

// recordRequest tracks the tokens sent on one generate request and updates the
// live context-window estimate (the most recent request size).
func (s *sessionMetrics) recordRequest(messages []provider.Message) {
	n := estimateMessageTokens(messages)
	s.mu.Lock()
	s.inputTokens += n
	s.contextTokens = n
	s.mu.Unlock()
}

func (s *sessionMetrics) recordOutput(text string) {
	if text == "" {
		return
	}
	n := contextmgmt.EstimateTokens([]provider.Part{{Text: text}})
	s.mu.Lock()
	s.outputTokens += n
	s.mu.Unlock()
}

func (s *sessionMetrics) recordTools(calls, failures int) {
	s.mu.Lock()
	s.toolCalls += calls
	s.toolFailures += failures
	s.mu.Unlock()
}

// snapshot returns a consistent copy of the counters plus the elapsed duration.
func (s *sessionMetrics) snapshot() (turns, toolCalls, toolFailures, inTok, outTok, ctxTok int, dur time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns, s.toolCalls, s.toolFailures, s.inputTokens, s.outputTokens, s.contextTokens, time.Since(s.start)
}

func estimateMessageTokens(messages []provider.Message) int {
	total := 0
	for i := range messages {
		total += contextmgmt.EstimateTokens(messages[i].Parts)
	}
	return total
}
