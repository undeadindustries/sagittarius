package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestForceCompressRefreshesContextTokens(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("alpha ", 400)
	history := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: big}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: big}}},
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: big}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: big}}},
	}

	summarize := (&queuedTestSummarizer{responses: []string{"summary", "summary"}}).fn
	mgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:          true,
		ContextLimit:     8000,
		PreserveFraction: 0.3,
		Summarize:        summarize,
	})

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.ReplaceHistory(history, nil)
	runner.SetContextManager(mgr)

	// Stale high gauge from the last API turn (footer stuck at ~100%).
	runner.metrics.recordTurnUsage("openai", "gpt-4o", "agent", 7500, 100, 0, false)
	before := runner.Stats()
	if before.ContextLimit != 8000 {
		t.Fatalf("ContextLimit = %d, want 8000", before.ContextLimit)
	}
	if pct := before.ContextPercent(); pct < 90 {
		t.Fatalf("ContextPercent before compress = %d, want >= 90", pct)
	}

	info, err := runner.ForceCompress(context.Background())
	if err != nil {
		t.Fatalf("ForceCompress: %v", err)
	}
	if info.Status != contextmgmt.Compressed {
		t.Fatalf("status = %v, want Compressed", info.Status)
	}

	after := runner.Stats()
	if after.ContextTokens >= before.ContextTokens {
		t.Errorf("ContextTokens after = %d, want < %d", after.ContextTokens, before.ContextTokens)
	}
	if pct := after.ContextPercent(); pct >= before.ContextPercent() {
		t.Errorf("ContextPercent after = %d, want < %d", pct, before.ContextPercent())
	}
	if after.ContextTokens != info.NewTokenCount {
		t.Errorf("ContextTokens = %d, want compression NewTokenCount %d", after.ContextTokens, info.NewTokenCount)
	}
}

type queuedTestSummarizer struct {
	responses []string
}

func (q *queuedTestSummarizer) fn(ctx context.Context, contents []contextmgmt.Message, systemInstruction string) (string, error) {
	if len(q.responses) == 0 {
		return "summary", nil
	}
	out := q.responses[0]
	q.responses = q.responses[1:]
	return out, nil
}
