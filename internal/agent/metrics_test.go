package agent

import (
	"testing"
)

func TestSessionMetricsRecordTurnUsage(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordTurnUsage("openai", "gpt-4o", "agent", 100, 40, 0, false)
	m.recordTurnUsage("openai", "gpt-4o", "agent", 200, 80, 0, false)

	turns, _, _, inTok, outTok, _, costUSD, costKnown, _, lastIn, lastOut, lastCost, lastCostKnown := m.snapshot()
	if turns != 0 { // recordTurnUsage does not increment turns; recordTurn does
		t.Errorf("turns = %d, want 0", turns)
	}
	if inTok != 300 {
		t.Errorf("InputTokens = %d, want 300", inTok)
	}
	if outTok != 120 {
		t.Errorf("OutputTokens = %d, want 120", outTok)
	}
	if costKnown {
		t.Error("costKnown should be false when no cost reported")
	}
	if costUSD != 0 {
		t.Errorf("costUSD = %f, want 0", costUSD)
	}
	// Last turn should reflect the second call only.
	if lastIn != 200 {
		t.Errorf("lastInTokens = %d, want 200", lastIn)
	}
	if lastOut != 80 {
		t.Errorf("lastOutTokens = %d, want 80", lastOut)
	}
	if lastCostKnown {
		t.Error("lastCostKnown should be false")
	}
	if lastCost != 0 {
		t.Errorf("lastCost = %f, want 0", lastCost)
	}
}

func TestSessionMetricsOpenRouterCost(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordTurnUsage("openrouter", "mistral/7b", "plan", 50, 20, 0.0010, true)
	m.recordTurnUsage("openrouter", "mistral/7b", "plan", 60, 25, 0.0012, true)

	_, _, _, _, _, _, costUSD, costKnown, _, lastIn, _, lastCost, lastCostKnown := m.snapshot()
	if !costKnown {
		t.Error("costKnown should be true when OpenRouter reports cost")
	}
	const wantCost = 0.0022
	if diff := costUSD - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cumulative costUSD = %f, want %f", costUSD, wantCost)
	}
	if lastIn != 60 {
		t.Errorf("lastInTokens = %d, want 60", lastIn)
	}
	if !lastCostKnown {
		t.Error("lastCostKnown should be true")
	}
	if lastCost != 0.0012 {
		t.Errorf("lastCostUSD = %f, want 0.0012", lastCost)
	}
}

func TestSessionMetricsSetContextTokens(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordTurnUsage("openai", "gpt-4o", "agent", 9000, 40, 0, false)
	m.setContextTokens(1200)

	_, _, _, _, _, ctxTok, _, _, _, lastIn, _, _, _ := m.snapshot()
	if ctxTok != 1200 {
		t.Errorf("contextTokens = %d, want 1200", ctxTok)
	}
	if lastIn != 9000 {
		t.Errorf("lastInTokens = %d, want 9000 (unchanged)", lastIn)
	}
}

func TestSessionMetricsAuxDoesNotUpdateLastTurn(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordTurnUsage("openai", "gpt-4o", "agent", 100, 40, 0, false)
	// Compression should not overwrite last-turn snapshot.
	m.recordAuxUsage("openai", "gpt-4o-mini", "agent", 50, 15, 0, false)

	_, _, _, inTok, outTok, _, _, _, _, lastIn, lastOut, _, _ := m.snapshot()
	if inTok != 150 {
		t.Errorf("session InputTokens = %d, want 150", inTok)
	}
	if outTok != 55 {
		t.Errorf("session OutputTokens = %d, want 55", outTok)
	}
	// Last-turn snapshot must still be from the main turn.
	if lastIn != 100 {
		t.Errorf("lastInTokens = %d, want 100 (main turn, not aux)", lastIn)
	}
	if lastOut != 40 {
		t.Errorf("lastOutTokens = %d, want 40 (main turn, not aux)", lastOut)
	}
}

func TestSessionMetricsPerKeyBreakdown(t *testing.T) {
	t.Parallel()

	m := newSessionMetrics()
	m.recordTurnUsage("openai", "gpt-4o", "agent", 100, 40, 0, false)
	m.recordTurnUsage("openrouter", "claude-3.5", "plan", 60, 20, 0.0015, true)
	m.recordAuxUsage("openai", "gpt-4o-mini", "agent", 30, 10, 0, false)

	stats := m.usageSnapshot()
	if len(stats) != 3 {
		t.Fatalf("expected 3 per-key entries, got %d", len(stats))
	}
	// Entries are sorted by key (provider\x00model\x00mode).
	// Sort order: "openai\x00gpt-4o\x00agent" < "openai\x00gpt-4o-mini\x00agent" < "openrouter\x00claude-3.5\x00plan"
	// because "\x00" (0) < "-" (45), so "gpt-4o\x00agent" < "gpt-4o-mini\x00agent".
	if stats[0].Model != "gpt-4o" || stats[0].Provider != "openai" {
		t.Errorf("entry[0] = (%s, %s), want (openai, gpt-4o)", stats[0].Provider, stats[0].Model)
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
