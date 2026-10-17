package agent

import (
	"sort"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// usageKey identifies one (provider, model, mode, agentType) bucket in sessionMetrics.
type usageKey struct {
	provider  string
	model     string
	mode      string
	agentType string
}

// modelStats holds token tallies and optional cost for one (provider, model, mode) triple.
type modelStats struct {
	requests      int
	inTokens      int
	outTokens     int
	cachedTokens  int
	cacheReported bool
	costUSD       float64
	costKnown     bool
}

// sessionMetrics accumulates per-process session telemetry: turn/tool counts,
// provider-reported (or heuristic fallback) token estimates, prompt-cache hit
// counts, prefix breaks, and optional cost.
// All access is mutex-guarded because RunTurn streams on a goroutine while the
// TUI reads snapshots from the render loop.
type sessionMetrics struct {
	mu           sync.Mutex
	start        time.Time
	turns        int
	toolCalls    int
	toolFailures int

	// Session-level totals.
	inputTokens   int
	outputTokens  int
	cachedTokens  int
	cacheReported bool
	costUSD       float64
	costKnown     bool

	// Last completed turn (main turn only, not compression).
	lastInTokens      int
	lastOutTokens     int
	lastCachedTokens  int
	lastCacheReported bool
	lastCostUSD       float64
	lastCostKnown     bool

	// contextTokens drives the footer context-% gauge: last main-turn prompt_tokens
	// when the provider reports usage, otherwise a local history estimate after
	// compression or prepareContext defenses.
	contextTokens int

	// turnGenTokens and turnDecodeDur are the generated tokens and decode time
	// for the active turn. recordTurn clears both so a new turn never shows
	// the previous turn's speed.
	turnGenTokens int
	turnDecodeDur time.Duration

	// perKey tracks usage keyed by "provider\x00model\x00mode\x00agentType". Allocated on first write.
	perKey map[string]*modelStats

	// cacheBreaks records chronological prompt-prefix breaks for /stats cache.
	cacheBreaks      []ui.CacheBreakStat
	pendingBreakSlot int // index into cacheBreaks awaiting the round's usage, or -1
}

func newSessionMetrics() *sessionMetrics {
	return &sessionMetrics{
		start:            time.Now(),
		pendingBreakSlot: -1,
	}
}

func (s *sessionMetrics) recordTurn() {
	s.mu.Lock()
	s.turns++
	s.turnGenTokens = 0
	s.turnDecodeDur = 0
	s.mu.Unlock()
}

// minSpeedDecode and minSpeedTokens are the smallest turn that may show a
// decode rate. Below either floor the figure is noise: a one-chunk reply has
// a zero duration, and a few tokens over a few hundred milliseconds swing
// wildly.
const (
	minSpeedDecode = 500 * time.Millisecond
	minSpeedTokens = 16
)

// recordDecode adds one round's generated tokens and decode duration to the
// active turn. A non-positive token count or duration (a single buffered
// chunk) adds nothing. The footer hides the rate until the turn clears both
// minimums.
func (s *sessionMetrics) recordDecode(tokens int, d time.Duration) {
	if tokens <= 0 || d <= 0 {
		return
	}
	s.mu.Lock()
	s.turnGenTokens += tokens
	s.turnDecodeDur += d
	s.mu.Unlock()
}

// recordTurnUsage is the entry point for main-turn token accounting.
// It updates the session totals, the last-turn snapshot (shown next to the
// model in the footer), any pending prefix-break entry for this round, and the
// per-(provider,model,mode) breakdown.
func (s *sessionMetrics) recordTurnUsage(key usageKey, u provider.Usage) {
	s.mu.Lock()
	s.inputTokens += u.InputTokens
	s.outputTokens += u.OutputTokens
	s.cachedTokens += u.CachedTokens
	if u.CachedReported {
		s.cacheReported = true
	}
	if u.InputTokens > 0 {
		s.contextTokens = u.InputTokens
	}
	s.costUSD += u.CostUSD
	if u.CostKnown {
		s.costKnown = true
	}

	s.lastInTokens = u.InputTokens
	s.lastOutTokens = u.OutputTokens
	s.lastCachedTokens = u.CachedTokens
	s.lastCacheReported = u.CachedReported
	s.lastCostUSD = u.CostUSD
	s.lastCostKnown = u.CostKnown

	if s.pendingBreakSlot >= 0 && s.pendingBreakSlot < len(s.cacheBreaks) {
		s.cacheBreaks[s.pendingBreakSlot].NextInputTokens = u.InputTokens
		s.cacheBreaks[s.pendingBreakSlot].NextCachedTokens = u.CachedTokens
		s.cacheBreaks[s.pendingBreakSlot].NextCacheReported = u.CachedReported
		s.pendingBreakSlot = -1
	}

	s.updatePerKey(key, u)
	s.mu.Unlock()
}

// recordCacheBreak appends one prefix-break entry and marks it to receive the
// token counts reported when the current round finishes.
func (s *sessionMetrics) recordCacheBreak(round int, reason string, pos int) {
	s.mu.Lock()
	s.cacheBreaks = append(s.cacheBreaks, ui.CacheBreakStat{
		Round:    round,
		Reason:   reason,
		Position: pos,
	})
	s.pendingBreakSlot = len(s.cacheBreaks) - 1
	s.mu.Unlock()
}

// resetCacheBreaks clears recorded prefix breaks (called on /clear or session switch).
func (s *sessionMetrics) resetCacheBreaks() {
	s.mu.Lock()
	s.cacheBreaks = nil
	s.pendingBreakSlot = -1
	s.mu.Unlock()
}

// setContextTokens updates the footer context-% gauge without touching session
// totals or the last-turn snapshot (used after /compress and prepareContext).
func (s *sessionMetrics) setContextTokens(tokens int) {
	if tokens <= 0 {
		return
	}
	s.mu.Lock()
	s.contextTokens = tokens
	s.mu.Unlock()
}

// recordAuxUsage is the entry point for compression / summarizer token accounting.
// It updates session totals and per-key breakdown but does NOT update the
// last-turn snapshot (compression should not overwrite the most-recent user turn).
func (s *sessionMetrics) recordAuxUsage(key usageKey, u provider.Usage) {
	s.mu.Lock()
	s.inputTokens += u.InputTokens
	s.outputTokens += u.OutputTokens
	s.cachedTokens += u.CachedTokens
	if u.CachedReported {
		s.cacheReported = true
	}
	s.costUSD += u.CostUSD
	if u.CostKnown {
		s.costKnown = true
	}
	s.updatePerKey(key, u)
	s.mu.Unlock()
}

func (s *sessionMetrics) updatePerKey(key usageKey, u provider.Usage) {
	if s.perKey == nil {
		s.perKey = make(map[string]*modelStats)
	}
	k := key.provider + "\x00" + key.model + "\x00" + key.mode + "\x00" + key.agentType
	ms := s.perKey[k]
	if ms == nil {
		ms = &modelStats{}
		s.perKey[k] = ms
	}
	ms.requests++
	ms.inTokens += u.InputTokens
	ms.outTokens += u.OutputTokens
	ms.cachedTokens += u.CachedTokens
	if u.CachedReported {
		ms.cacheReported = true
	}
	ms.costUSD += u.CostUSD
	if u.CostKnown {
		ms.costKnown = true
	}
}

func (s *sessionMetrics) recordTools(calls, failures int) {
	s.mu.Lock()
	s.toolCalls += calls
	s.toolFailures += failures
	s.mu.Unlock()
}

// snapshot returns a consistent copy of the session-level counters and breakdowns.
func (s *sessionMetrics) snapshot() ui.SessionStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	var breaks []ui.CacheBreakStat
	if len(s.cacheBreaks) > 0 {
		breaks = make([]ui.CacheBreakStat, len(s.cacheBreaks))
		copy(breaks, s.cacheBreaks)
	}
	return ui.SessionStats{
		Turns:             s.turns,
		ToolCalls:         s.toolCalls,
		ToolFailures:      s.toolFailures,
		InputTokens:       s.inputTokens,
		OutputTokens:      s.outputTokens,
		CachedTokens:      s.cachedTokens,
		CacheReported:     s.cacheReported,
		SessionCostUSD:    s.costUSD,
		SessionCostKnown:  s.costKnown,
		LastInputTokens:   s.lastInTokens,
		LastOutputTokens:  s.lastOutTokens,
		LastCachedTokens:  s.lastCachedTokens,
		LastCacheReported: s.lastCacheReported,
		LastCostUSD:       s.lastCostUSD,
		LastCostKnown:     s.lastCostKnown,
		ContextTokens:     s.contextTokens,
		TokensPerSec:      s.tokensPerSecLocked(),
		Duration:          time.Since(s.start),
		ModelUsage:        s.usageSnapshotLocked(),
		CacheBreaks:       breaks,
	}
}

// tokensPerSecLocked is the active turn's decode rate, or 0 when the turn has
// not yet cleared the minimum token count and decode duration. Caller holds s.mu.
func (s *sessionMetrics) tokensPerSecLocked() float64 {
	if s.turnGenTokens < minSpeedTokens || s.turnDecodeDur < minSpeedDecode {
		return 0
	}
	return float64(s.turnGenTokens) / s.turnDecodeDur.Seconds()
}

// usageSnapshot returns a stable-sorted slice of per-(provider,model,mode) usage stats.
func (s *sessionMetrics) usageSnapshot() []ui.ModelUsageStat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usageSnapshotLocked()
}

func (s *sessionMetrics) usageSnapshotLocked() []ui.ModelUsageStat {
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
		parts := splitKey(k)
		out = append(out, ui.ModelUsageStat{
			Provider:      parts[0],
			Model:         parts[1],
			Mode:          parts[2],
			Agent:         parts[3],
			Requests:      ms.requests,
			InTokens:      ms.inTokens,
			OutTokens:     ms.outTokens,
			CachedTokens:  ms.cachedTokens,
			CacheReported: ms.cacheReported,
			CostUSD:       ms.costUSD,
			CostKnown:     ms.costKnown,
		})
	}
	return out
}

func splitKey(k string) [4]string {
	var out [4]string
	idx := 0
	start := 0
	for i := 0; i < len(k) && idx < 3; i++ {
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
