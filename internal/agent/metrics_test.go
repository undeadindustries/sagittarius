package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestSessionMetricsRecordTurnUsage(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	key := usageKey{provider: "openai", model: "gpt-4o", mode: "agent", agentType: "main"}
	m.recordTurnUsage(key, provider.Usage{InputTokens: 100, OutputTokens: 40, CachedTokens: 80, CachedReported: true})
	m.recordTurnUsage(key, provider.Usage{InputTokens: 200, OutputTokens: 80, CachedTokens: 180, CachedReported: true})

	snap := m.snapshot()
	if snap.Turns != 0 { // recordTurnUsage does not increment turns; recordTurn does
		t.Errorf("turns = %d, want 0", snap.Turns)
	}
	if snap.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", snap.InputTokens)
	}
	if snap.OutputTokens != 120 {
		t.Errorf("OutputTokens = %d, want 120", snap.OutputTokens)
	}
	if snap.CachedTokens != 260 || !snap.CacheReported {
		t.Errorf("CachedTokens = %d (reported=%v), want 260 (true)", snap.CachedTokens, snap.CacheReported)
	}
	if snap.SessionCostKnown {
		t.Error("costKnown should be false when no cost reported")
	}
	if snap.SessionCostUSD != 0 {
		t.Errorf("costUSD = %f, want 0", snap.SessionCostUSD)
	}
	// Last turn should reflect the second call only.
	if snap.LastInputTokens != 200 {
		t.Errorf("lastInTokens = %d, want 200", snap.LastInputTokens)
	}
	if snap.LastOutputTokens != 80 {
		t.Errorf("lastOutTokens = %d, want 80", snap.LastOutputTokens)
	}
	if snap.LastCachedTokens != 180 || !snap.LastCacheReported {
		t.Errorf("lastCachedTokens = %d (reported=%v), want 180 (true)", snap.LastCachedTokens, snap.LastCacheReported)
	}
	if snap.LastCostKnown {
		t.Error("lastCostKnown should be false")
	}
	if snap.LastCostUSD != 0 {
		t.Errorf("lastCost = %f, want 0", snap.LastCostUSD)
	}
}

func TestSessionMetricsOpenRouterCost(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	key := usageKey{provider: "openrouter", model: "mistral/7b", mode: "plan", agentType: "main"}
	m.recordTurnUsage(key, provider.Usage{InputTokens: 50, OutputTokens: 20, CostUSD: 0.0010, CostKnown: true})
	m.recordTurnUsage(key, provider.Usage{InputTokens: 60, OutputTokens: 25, CostUSD: 0.0012, CostKnown: true})

	snap := m.snapshot()
	if !snap.SessionCostKnown {
		t.Error("costKnown should be true when OpenRouter reports cost")
	}
	const wantCost = 0.0022
	if diff := snap.SessionCostUSD - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cumulative costUSD = %f, want %f", snap.SessionCostUSD, wantCost)
	}
	if snap.LastInputTokens != 60 {
		t.Errorf("lastInTokens = %d, want 60", snap.LastInputTokens)
	}
	if !snap.LastCostKnown {
		t.Error("lastCostKnown should be true")
	}
	if snap.LastCostUSD != 0.0012 {
		t.Errorf("lastCostUSD = %f, want 0.0012", snap.LastCostUSD)
	}
}

func TestSessionMetricsSetContextTokens(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	key := usageKey{provider: "openai", model: "gpt-4o", mode: "agent", agentType: "main"}
	m.recordTurnUsage(key, provider.Usage{InputTokens: 9000, OutputTokens: 40})
	m.setContextTokens(1200)

	snap := m.snapshot()
	if snap.ContextTokens != 1200 {
		t.Errorf("contextTokens = %d, want 1200", snap.ContextTokens)
	}
	if snap.LastInputTokens != 9000 {
		t.Errorf("lastInTokens = %d, want 9000 (unchanged)", snap.LastInputTokens)
	}
}

func TestSessionMetricsAuxDoesNotUpdateLastTurn(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordTurnUsage(
		usageKey{provider: "openai", model: "gpt-4o", mode: "agent", agentType: "main"},
		provider.Usage{InputTokens: 100, OutputTokens: 40},
	)
	// Compression should not overwrite last-turn snapshot.
	m.recordAuxUsage(
		usageKey{provider: "openai", model: "gpt-4o-mini", mode: "agent", agentType: "main"},
		provider.Usage{InputTokens: 50, OutputTokens: 15},
	)

	snap := m.snapshot()
	if snap.InputTokens != 150 {
		t.Errorf("session InputTokens = %d, want 150", snap.InputTokens)
	}
	if snap.OutputTokens != 55 {
		t.Errorf("session OutputTokens = %d, want 55", snap.OutputTokens)
	}
	// Last-turn snapshot must still be from the main turn.
	if snap.LastInputTokens != 100 {
		t.Errorf("lastInTokens = %d, want 100 (main turn, not aux)", snap.LastInputTokens)
	}
	if snap.LastOutputTokens != 40 {
		t.Errorf("lastOutTokens = %d, want 40 (main turn, not aux)", snap.LastOutputTokens)
	}
}

func TestSessionMetricsPerKeyBreakdown(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordTurnUsage(
		usageKey{provider: "openai", model: "gpt-4o", mode: "agent", agentType: "main"},
		provider.Usage{InputTokens: 100, OutputTokens: 40, CachedTokens: 90, CachedReported: true},
	)
	m.recordTurnUsage(
		usageKey{provider: "openrouter", model: "claude-3.5", mode: "plan", agentType: "main"},
		provider.Usage{InputTokens: 60, OutputTokens: 20, CostUSD: 0.0015, CostKnown: true},
	)
	m.recordAuxUsage(
		usageKey{provider: "openai", model: "gpt-4o-mini", mode: "agent", agentType: "main"},
		provider.Usage{InputTokens: 30, OutputTokens: 10},
	)

	stats := m.usageSnapshot()
	if len(stats) != 3 {
		t.Fatalf("expected 3 per-key entries, got %d", len(stats))
	}
	// Entries are sorted by key (provider\x00model\x00mode\x00agentType).
	if stats[0].Model != "gpt-4o" || stats[0].Provider != "openai" || !stats[0].CacheReported || stats[0].CachedTokens != 90 {
		t.Errorf("entry[0] = %+v, want (openai, gpt-4o, cached=90, reported=true)", stats[0])
	}
	if stats[1].Model != "gpt-4o-mini" || stats[1].Mode != "agent" {
		t.Errorf("entry[1] = (%s, %s), want (gpt-4o-mini, agent)", stats[1].Model, stats[1].Mode)
	}
	if stats[2].Model != "claude-3.5" || !stats[2].CostKnown {
		t.Errorf("entry[2] = %+v, want claude-3.5 with CostKnown", stats[2])
	}
	const wantCost2 = 0.0015
	if diff := stats[2].CostUSD - wantCost2; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("entry[2].CostUSD = %f, want %f", stats[2].CostUSD, wantCost2)
	}
}

func TestSessionMetricsDecodeAggregatesRounds(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordTurn()
	m.recordDecode(40, time.Second)
	m.recordDecode(40, time.Second)

	snap := m.snapshot()
	if snap.TokensPerSec != 40 {
		t.Errorf("TokensPerSec = %v, want 40 (80 tokens / 2s)", snap.TokensPerSec)
	}
}

func TestSessionMetricsDecodeMinimumsHideShortReply(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		tokens int
		d      time.Duration
	}{
		{name: "too few tokens", tokens: minSpeedTokens - 1, d: time.Second},
		{name: "too short", tokens: 100, d: minSpeedDecode - time.Millisecond},
		{name: "single chunk", tokens: 100, d: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newSessionMetrics()
			m.recordDecode(tc.tokens, tc.d)
			if got := m.snapshot().TokensPerSec; got != 0 {
				t.Errorf("TokensPerSec = %v, want 0", got)
			}
		})
	}
}

func TestSessionMetricsDecodeResetsOnNewTurn(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordDecode(32, time.Second)
	if got := m.snapshot().TokensPerSec; got != 32 {
		t.Fatalf("TokensPerSec = %v, want 32", got)
	}
	m.recordTurn()
	snap := m.snapshot()
	if snap.Turns != 1 {
		t.Errorf("turns = %d, want 1", snap.Turns)
	}
	if snap.TokensPerSec != 0 {
		t.Errorf("TokensPerSec after recordTurn = %v, want 0", snap.TokensPerSec)
	}
}

func TestSessionMetricsDecodeRecordsEstimate(t *testing.T) {
	t.Parallel()

	// 80 ASCII characters is 20 estimated tokens, past the 16-token floor.
	text := strings.Repeat("abcd", 20)
	n := contextmgmt.EstimateTokens([]provider.Part{{Text: text}})
	if n < minSpeedTokens {
		t.Fatalf("estimate %d is below the floor; the fixture is too short", n)
	}
	m := newSessionMetrics()
	m.recordDecode(n, time.Second)
	if got := m.snapshot().TokensPerSec; got != float64(n) {
		t.Errorf("TokensPerSec = %v, want %v", got, n)
	}
}
