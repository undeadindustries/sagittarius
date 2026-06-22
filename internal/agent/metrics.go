package agent

import (
	"sort"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// modelStats holds heuristic token tallies for one (model, kind) pair.
type modelStats struct {
	requests  int
	inTokens  int
	outTokens int
}

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
	// perModel tracks usage keyed by "model\x00kind". Allocated on first write.
	perModel map[string]*modelStats
}

func newSessionMetrics() *sessionMetrics {
	return &sessionMetrics{start: time.Now()}
}

func (s *sessionMetrics) recordTurn() {
	s.mu.Lock()
	s.turns++
	s.mu.Unlock()
}

// recordUsage is the single entry point for token accounting. It updates the
// flat session totals and the per-(model, kind) breakdown atomically.
// kind is "main" for user-turn generation or "compression" for the summarizer.
// Pass inTok=0 or outTok=0 when only one direction is known for a call.
func (s *sessionMetrics) recordUsage(model, kind string, inTok, outTok int) {
	s.mu.Lock()
	s.inputTokens += inTok
	s.outputTokens += outTok
	if inTok > 0 {
		// Keep contextTokens as the most recent request size (same semantics as
		// the old recordRequest).
		s.contextTokens = inTok
	}
	if s.perModel == nil {
		s.perModel = make(map[string]*modelStats)
	}
	key := model + "\x00" + kind
	ms := s.perModel[key]
	if ms == nil {
		ms = &modelStats{}
		s.perModel[key] = ms
	}
	ms.requests++
	ms.inTokens += inTok
	ms.outTokens += outTok
	s.mu.Unlock()
}

// recordRequest tracks the tokens sent on one generate request and updates the
// live context-window estimate (the most recent request size).
// Deprecated: prefer recordUsage(model, kind, n, 0).
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

// usageSnapshot returns a stable-sorted slice of per-(model, kind) usage stats.
func (s *sessionMetrics) usageSnapshot() []ui.ModelUsageStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.perModel) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.perModel))
	for k := range s.perModel {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ui.ModelUsageStat, 0, len(keys))
	for _, k := range keys {
		ms := s.perModel[k]
		// key is "model\x00kind"
		sep := 0
		for i := range k {
			if k[i] == 0 {
				sep = i
				break
			}
		}
		model, kind := k[:sep], k[sep+1:]
		out = append(out, ui.ModelUsageStat{
			Model:     model,
			Kind:      kind,
			Requests:  ms.requests,
			InTokens:  ms.inTokens,
			OutTokens: ms.outTokens,
		})
	}
	return out
}

func estimateMessageTokens(messages []provider.Message) int {
	total := 0
	for i := range messages {
		total += contextmgmt.EstimateTokens(messages[i].Parts)
	}
	return total
}
