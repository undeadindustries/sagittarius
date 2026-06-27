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

func TestManagerLatchesFailedCompression(t *testing.T) {
	t.Parallel()
	// A summarizer that always inflates the token count. The first non-forced
	// compression must latch hasFailedCompression so subsequent non-forced turns
	// skip re-summarization entirely (truncation only), matching the fork.
	q := &queuedSummarizer{responses: []string{"x", strings.Repeat("a", 8_000), "x", strings.Repeat("a", 8_000)}}
	history := []Message{
		msg("user", strings.Repeat("alpha ", 8)),
		msg("model", strings.Repeat("beta ", 8)),
		msg("user", strings.Repeat("gamma ", 8)),
		msg("model", strings.Repeat("delta ", 8)),
	}

	m := NewManager(ManagerConfig{
		Enabled:              true,
		ContextLimit:         50, // threshold 0.4 -> fires at 20 tokens; history is larger
		CompressionThreshold: 0.4,
		PreserveFraction:     0.3,
		Summarize:            q.fn,
	})

	if _, err := m.PrepareTurn(context.Background(), cloneHistory(history), 0); err != nil {
		t.Fatalf("first PrepareTurn: %v", err)
	}
	if !m.hasFailedCompression {
		t.Fatal("expected hasFailedCompression to latch after an inflated summary")
	}
	if len(q.calls) != 2 {
		t.Fatalf("summarizer calls after first turn = %d, want 2 (initial + verify)", len(q.calls))
	}

	if _, err := m.PrepareTurn(context.Background(), cloneHistory(history), 1); err != nil {
		t.Fatalf("second PrepareTurn: %v", err)
	}
	if len(q.calls) != 2 {
		t.Fatalf("summarizer calls after second turn = %d, want 2 (no re-summarization)", len(q.calls))
	}
}

// ejectionHistory places a bulky write_file call in an ejectable position
// (after the leading entries, before the protected latest turn).
func ejectionHistory() []Message {
	return []Message{
		textEntry("user", "lead 1"),
		textEntry("model", "lead 2"),
		writeFileCall("/big.ts", bigContent()),
		textEntry("model", "wrote it"),
		textEntry("user", "now make another file"),
		textEntry("model", "latest"),
	}
}

func ejectionManager(contextLimit int) *Manager {
	return NewManager(ManagerConfig{
		Enabled:                  true,
		ContextLimit:             contextLimit,
		EjectionEnabled:          true,
		EjectionMinAgeTurns:      1,
		EjectionMinTokensPerCall: 100,
		WriteFileToolName:        writeFileTool,
	})
}

// TestManagerEjectionUnderPressureLeavesNoCopyableMarker is the regression for
// the write_file rejection loop (AD-067): when ejection fires under budget
// pressure it must drop the content arg entirely, leaving no marker string the
// model could copy into its next write_file call.
func TestManagerEjectionUnderPressureLeavesNoCopyableMarker(t *testing.T) {
	t.Parallel()
	history := ejectionHistory()
	// Small limit so historyTokens >= 0.6*limit -> ejection fires.
	m := ejectionManager(200)
	got, err := m.PrepareTurn(context.Background(), cloneHistory(history), 3)
	if err != nil {
		t.Fatalf("PrepareTurn: %v", err)
	}
	// The write_file call's content arg must be gone.
	if _, present := got[2].Parts[0].FunctionCall.Args["content"]; present {
		t.Errorf("content arg still present after ejection; want it dropped")
	}
	// No copyable marker anywhere in the outbound history.
	for _, bad := range []string{"[sagittarius omitted", "<file_written", "omitted write_file content"} {
		if historyContains(got, bad) {
			t.Errorf("outbound history contains copyable marker %q", bad)
		}
	}
}

// TestManagerEjectionSkippedWithHeadroom proves Fix A: with plenty of context
// budget, ejection does not fire and the model keeps its written content.
func TestManagerEjectionSkippedWithHeadroom(t *testing.T) {
	t.Parallel()
	history := ejectionHistory()
	m := ejectionManager(1_000_000) // 0.6*1e6 >> history tokens -> no ejection
	got, err := m.PrepareTurn(context.Background(), cloneHistory(history), 3)
	if err != nil {
		t.Fatalf("PrepareTurn: %v", err)
	}
	if callContent(got[2]) != bigContent() {
		t.Errorf("write_file content was ejected despite headroom; want it preserved")
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
