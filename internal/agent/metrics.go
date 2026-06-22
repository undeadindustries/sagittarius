package agent

import (
	"sort"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// modelStats holds token tallies and optional cost for one (provider, model, mode) triple.
type modelStats struct {
	requests  int
	inTokens  int
	outTokens int
	costUSD   float64
	costKnown bool
}

// sessionMetrics accumulates per-process session telemetry: turn/tool counts,
// provider-reported (or heuristic fallback) token estimates, and optional cost.
// All access is mutex-guarded because RunTurn streams on a goroutine while the
// TUI reads snapshots from the render loop.
type sessionMetrics struct {
	mu           sync.Mutex
	start        time.Time
	turns        int
	toolCalls    int
	toolFailures int

	// Session-level totals.
	inputTokens  int
	outputTokens int
	costUSD      float64
	costKnown    bool

	// Last completed turn (main turn only, not compression).
	lastInTokens  int
	lastOutTokens int
	lastCostUSD   float64
	lastCostKnown bool

	// contextTokens is the most-recent input size for the context-% footer gauge.
	contextTokens int

	// perKey tracks usage keyed by "provider\x00model\x00mode". Allocated on first write.
	perKey map[string]*modelStats
}

func newSessionMetrics() *sessionMetrics {
	return &sessionMetrics{start: time.Now()}
}

func (s *sessionMetrics) recordTurn() {
	s.mu.Lock()
	s.turns++
	s.mu.Unlock()
}

// recordTurnUsage is the entry point for main-turn token accounting.
// It updates the session totals, the last-turn snapshot (shown next to the
// model in the footer), and the per-(provider,model,mode) breakdown.
func (s *sessionMetrics) recordTurnUsage(prov, model, mode string, inTok, outTok int, costUSD float64, costKnown bool) {
	s.mu.Lock()
	s.inputTokens += inTok
	s.outputTokens += outTok
	if inTok > 0 {
		s.contextTokens = inTok
	}
	s.costUSD += costUSD
	if costKnown {
		s.costKnown = true
	}

	s.lastInTokens = inTok
	s.lastOutTokens = outTok
	s.lastCostUSD = costUSD
	s.lastCostKnown = costKnown

	s.updatePerKey(prov, model, mode, inTok, outTok, costUSD, costKnown)
	s.mu.Unlock()
}

// recordAuxUsage is the entry point for compression / summarizer token accounting.
// It updates session totals and per-key breakdown but does NOT update the
// last-turn snapshot (compression should not overwrite the most-recent user turn).
func (s *sessionMetrics) recordAuxUsage(prov, model, mode string, inTok, outTok int, costUSD float64, costKnown bool) {
	s.mu.Lock()
	s.inputTokens += inTok
	s.outputTokens += outTok
	s.costUSD += costUSD
	if costKnown {
		s.costKnown = true
	}
	s.updatePerKey(prov, model, mode, inTok, outTok, costUSD, costKnown)
	s.mu.Unlock()
}

func (s *sessionMetrics) updatePerKey(prov, model, mode string, inTok, outTok int, costUSD float64, costKnown bool) {
	if s.perKey == nil {
		s.perKey = make(map[string]*modelStats)
	}
	key := prov + "\x00" + model + "\x00" + mode
	ms := s.perKey[key]
	if ms == nil {
		ms = &modelStats{}
		s.perKey[key] = ms
	}
	ms.requests++
	ms.inTokens += inTok
	ms.outTokens += outTok
	ms.costUSD += costUSD
	if costKnown {
		ms.costKnown = true
	}
}

func (s *sessionMetrics) recordTools(calls, failures int) {
	s.mu.Lock()
	s.toolCalls += calls
	s.toolFailures += failures
	s.mu.Unlock()
}

// snapshot returns a consistent copy of the session-level counters.
func (s *sessionMetrics) snapshot() (turns, toolCalls, toolFailures, inTok, outTok, ctxTok int, costUSD float64, costKnown bool, dur time.Duration, lastIn, lastOut int, lastCost float64, lastCostKnown bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns, s.toolCalls, s.toolFailures,
		s.inputTokens, s.outputTokens, s.contextTokens,
		s.costUSD, s.costKnown,
		time.Since(s.start),
		s.lastInTokens, s.lastOutTokens, s.lastCostUSD, s.lastCostKnown
}

// usageSnapshot returns a stable-sorted slice of per-(provider,model,mode) usage stats.
func (s *sessionMetrics) usageSnapshot() []ui.ModelUsageStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.perKey) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.perKey))
	for k := range s.perKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ui.ModelUsageStat, 0, len(keys))
	for _, k := range keys {
		ms := s.perKey[k]
		// key is "provider\x00model\x00mode"
		parts := splitKey(k)
		out = append(out, ui.ModelUsageStat{
			Provider:  parts[0],
			Model:     parts[1],
			Mode:      parts[2],
			Requests:  ms.requests,
			InTokens:  ms.inTokens,
			OutTokens: ms.outTokens,
			CostUSD:   ms.costUSD,
			CostKnown: ms.costKnown,
		})
	}
	return out
}

func splitKey(k string) [3]string {
	var out [3]string
	idx := 0
	start := 0
	for i := 0; i < len(k) && idx < 2; i++ {
		if k[i] == 0 {
			out[idx] = k[start:i]
			idx++
			start = i + 1
		}
	}
	out[idx] = k[start:]
	return out
}

func estimateMessageTokens(messages []provider.Message) int {
	total := 0
	for i := range messages {
		total += contextmgmt.EstimateTokens(messages[i].Parts)
	}
	return total
}
