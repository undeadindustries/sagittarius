package contextmgmt

import (
	"context"
	"strings"
	"testing"
)

// maskableHistory returns a history whose first entry is a bulky, non-exempt
// tool output (above the protection-window floor) and whose latest turn is a
// protected user message.
func maskableHistory() []Message {
	return []Message{
		outMsg("read_file", strings.Repeat("data ", 4_000)), // ~20000 chars ≈ 5000 tokens
		textEntry("user", "what did that file say?"),
	}
}

func enabledMaskingManager(t *testing.T, enabled bool) *Manager {
	t.Helper()
	return NewManager(ManagerConfig{
		Enabled:                  enabled,
		ContextLimit:             20_000,
		MaskingEnabled:           true,
		MaskingProtectLatestTurn: true,
		OutputDir:                t.TempDir(),
	})
}

func TestManagerMaskingAppliedOnOpenAIChat(t *testing.T) {
	t.Parallel()
	history := maskableHistory()
	before := EstimateTokens(flattenParts(history))

	m := enabledMaskingManager(t, true)
	got, err := m.PrepareTurn(context.Background(), cloneHistory(history), 0)
	if err != nil {
		t.Fatalf("PrepareTurn: %v", err)
	}

	after := EstimateTokens(flattenParts(got))
	if after >= before {
		t.Fatalf("expected masking to reduce tokens: before=%d after=%d", before, after)
	}
	if !historyContains(got, "Output too large. Full output available at:") {
		t.Fatalf("expected a masked marker in history, got %+v", got)
	}
}

func TestManagerMaskingNotAppliedWhenDisabled(t *testing.T) {
	t.Parallel()
	// A disabled manager models the gemini-native and openai-responses paths,
	// which the agent constructs as nil/disabled (AD-014/AD-015): a pure
	// pass-through with no masking or compression.
	history := maskableHistory()
	before := EstimateTokens(flattenParts(history))

	m := enabledMaskingManager(t, false)
	got, err := m.PrepareTurn(context.Background(), cloneHistory(history), 0)
	if err != nil {
		t.Fatalf("PrepareTurn: %v", err)
	}

	after := EstimateTokens(flattenParts(got))
	if after != before {
		t.Fatalf("disabled manager must not change tokens: before=%d after=%d", before, after)
	}
	if len(got) != len(history) {
		t.Fatalf("disabled manager must not change history length: got %d want %d", len(got), len(history))
	}
}

func TestManagerNilIsPassThrough(t *testing.T) {
	t.Parallel()
	var m *Manager
	history := maskableHistory()
	got, err := m.PrepareTurn(context.Background(), history, 0)
	if err != nil {
		t.Fatalf("nil manager PrepareTurn: %v", err)
	}
	if len(got) != len(history) {
		t.Fatalf("nil manager must pass history through unchanged")
	}
}
