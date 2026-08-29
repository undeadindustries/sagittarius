package contextmgmt

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
	if len(q.calls) == 0 {
		t.Fatal("expected summarizer to run on the first turn")
	}
	firstCalls := len(q.calls)

	if _, err := m.PrepareTurn(context.Background(), cloneHistory(history), 1); err != nil {
		t.Fatalf("second PrepareTurn: %v", err)
	}
	if len(q.calls) != firstCalls {
		t.Fatalf("summarizer calls after second turn = %d, want %d (no re-summarization)", len(q.calls), firstCalls)
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

func TestManagerBudgetLimitUsesSafetyFactor(t *testing.T) {
	t.Parallel()
	m := NewManager(ManagerConfig{Enabled: true, ContextLimit: 1000})
	if m.ContextLimit() != 1000 {
		t.Fatalf("ContextLimit = %d, want 1000", m.ContextLimit())
	}
	if got := m.BudgetLimit(); got != 850 {
		t.Fatalf("BudgetLimit = %d, want 850", got)
	}
	m.cfg.EstimateSafetyFactor = 0.5
	if got := m.BudgetLimit(); got != 500 {
		t.Fatalf("BudgetLimit with 0.5 = %d, want 500", got)
	}
}

func TestManagerLatchesSummarizerError(t *testing.T) {
	t.Parallel()
	calls := 0
	summarize := func(ctx context.Context, contents []Message, systemInstruction string) (string, error) {
		calls++
		return "", fmt.Errorf("gemini api error (400 INVALID_ARGUMENT): exceeds 1048576")
	}
	history := []Message{
		msg("user", strings.Repeat("alpha ", 8)),
		msg("model", strings.Repeat("beta ", 8)),
		msg("user", strings.Repeat("gamma ", 8)),
		msg("model", strings.Repeat("delta ", 8)),
	}
	m := NewManager(ManagerConfig{
		Enabled:              true,
		ContextLimit:         50,
		CompressionThreshold: 0.4,
		PreserveFraction:     0.3,
		Summarize:            summarize,
	})
	if _, err := m.PrepareTurn(context.Background(), cloneHistory(history), 0); err == nil {
		t.Fatal("expected summarizer error")
	}
	if !m.hasFailedCompression {
		t.Fatal("expected hasFailedCompression to latch after a provider error")
	}
	firstCalls := calls
	if _, err := m.PrepareTurn(context.Background(), cloneHistory(history), 1); err != nil {
		t.Fatalf("second PrepareTurn: %v", err)
	}
	if calls != firstCalls {
		t.Fatalf("summarizer calls after latch = %d, want %d (truncation only)", calls, firstCalls)
	}
	m.ResetCompressionFailure()
	if m.hasFailedCompression {
		t.Fatal("ResetCompressionFailure left the latch set")
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

func TestManagerLatchesEmptySummary(t *testing.T) {
	t.Parallel()
	// When summarizer returns an empty summary (e.g. thinking-only model),
	// the compression status is CompressionFailedEmptySummary and must latch.
	calls := 0
	summarize := func(ctx context.Context, contents []Message, systemInstruction string) (string, error) {
		calls++
		return "<scratchpad>thinking</scratchpad>", nil // extractSnapshot yields empty summary
	}
	history := []Message{
		msg("user", strings.Repeat("alpha ", 10)),
		msg("model", strings.Repeat("beta ", 10)),
		msg("user", strings.Repeat("gamma ", 10)),
		msg("model", strings.Repeat("delta ", 10)),
	}
	m := NewManager(ManagerConfig{
		Enabled:              true,
		ContextLimit:         50,
		CompressionThreshold: 0.4,
		PreserveFraction:     0.3,
		Summarize:            summarize,
	})
	if _, err := m.PrepareTurn(context.Background(), cloneHistory(history), 0); err != nil {
		t.Fatalf("PrepareTurn: %v", err)
	}
	if !m.hasFailedCompression {
		t.Fatal("expected hasFailedCompression to latch on empty summary")
	}
}

func TestManagerLatchesSub10PercentReduction(t *testing.T) {
	t.Parallel()
	// OpenCode's loop guard: a compression that succeeds but achieves <10% reduction
	// must latch hasFailedCompression to prevent burning summarizer calls repeatedly.
	history := []Message{
		msg("user", strings.Repeat("alpha ", 10)),
		msg("model", strings.Repeat("beta ", 10)),
		msg("user", strings.Repeat("gamma ", 10)),
		msg("model", strings.Repeat("delta ", 10)),
	}
	origTok := EstimateTokens(flattenParts(history))
	// Return a summary that is barely smaller (95% of original tokens -> only 5% reduction).
	hugeSummary := "<state_snapshot>" + strings.Repeat("a", int(float64(origTok*4)*0.95)) + "</state_snapshot>"
	q := &queuedSummarizer{responses: []string{hugeSummary}}

	m := NewManager(ManagerConfig{
		Enabled:              true,
		ContextLimit:         origTok * 2,
		CompressionThreshold: 0.4,
		PreserveFraction:     0.2,
		Summarize:            q.fn,
	})
	if _, err := m.PrepareTurn(context.Background(), cloneHistory(history), 0); err != nil {
		t.Fatalf("PrepareTurn: %v", err)
	}
	if !m.hasFailedCompression {
		t.Fatal("expected hasFailedCompression to latch when compression achieves <10% reduction")
	}
}

func TestManagerLatchesRespectedByBudgetTriggered(t *testing.T) {
	t.Parallel()
	// A proactive budget-triggered compression (BudgetTriggered: true) must respect
	// the failure latch and not invoke the summarizer when a prior failure occurred.
	calls := 0
	summarize := func(ctx context.Context, contents []Message, systemInstruction string) (string, error) {
		calls++
		return "<state_snapshot>ok</state_snapshot>", nil
	}
	history := []Message{
		msg("user", strings.Repeat("alpha ", 20)),
		msg("model", strings.Repeat("beta ", 20)),
	}
	m := NewManager(ManagerConfig{
		Enabled:                true,
		ContextLimit:           100,
		BudgetEnabled:          true,
		ProactiveCompressAt:    0.3, // triggers budget compression
		ReservedResponseTokens: 20,
		Summarize:              summarize,
	})
	m.hasFailedCompression = true // latch already set

	got, err := m.PrepareTurn(context.Background(), cloneHistory(history), 0)
	if err != nil {
		t.Fatalf("PrepareTurn: %v", err)
	}
	if calls != 0 {
		t.Fatalf("summarizer called %d times; want 0 (budget-triggered must respect latch)", calls)
	}
	_ = got
}

func TestManagerConcurrentResetVsPrepare(t *testing.T) {
	t.Parallel()
	// Race check: ResetCompressionFailure from UI thread vs PrepareTurn on runner thread.
	q := &queuedSummarizer{responses: []string{"<state_snapshot>summary</state_snapshot>"}}
	history := []Message{
		msg("user", "hello"),
		msg("model", "world"),
	}
	m := NewManager(ManagerConfig{
		Enabled:              true,
		ContextLimit:         100,
		CompressionThreshold: 0.1,
		Summarize:            q.fn,
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			m.ResetCompressionFailure()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = m.PrepareTurn(context.Background(), cloneHistory(history), i)
		}
	}()

	wg.Wait()
}

func TestManagerMessageBudget(t *testing.T) {
	t.Parallel()
	m := NewManager(ManagerConfig{
		Enabled:                true,
		ContextLimit:           1000,
		EstimateSafetyFactor:   0.85, // budget limit = 850
		ReservedResponseTokens: 200,
	})

	// Normal overhead: 850 - 150 - 200 = 500
	if got := m.MessageBudget(150); got != 500 {
		t.Errorf("MessageBudget(150) = %d, want 500", got)
	}

	// Overhead exceeds budget: 850 - 700 - 200 = -50 -> clamped to 1
	if got := m.MessageBudget(700); got != 1 {
		t.Errorf("MessageBudget(700) = %d, want 1 (clamped)", got)
	}

	// Nil / disabled manager returns 0
	var nilMgr *Manager
	if got := nilMgr.MessageBudget(100); got != 0 {
		t.Errorf("nilMgr.MessageBudget = %d, want 0", got)
	}
}
