package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
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
	runner.metrics.recordTurnUsage("openai", "gpt-4o", "agent", "main", 7500, 100, 0, false)
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

func TestEnforceRequestBudgetTruncatesAndNotifies(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("alpha ", 80)
	var history []provider.Message
	for i := 0; i < 12; i++ {
		history = append(history, provider.Message{
			Role:  provider.RoleUser,
			Parts: []provider.Part{{Text: body}},
		})
		history = append(history, provider.Message{
			Role:  provider.RoleModel,
			Parts: []provider.Part{{Text: body}},
		})
	}

	summarize := func(ctx context.Context, contents []contextmgmt.Message, systemInstruction string) (string, error) {
		return "", fmt.Errorf("gemini api error (400 INVALID_ARGUMENT): exceeds 1048576")
	}
	mgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:              true,
		ContextLimit:         400,
		CompressionThreshold: 0.1,
		PreserveFraction:     0.3,
		Summarize:            summarize,
	})

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "ok"}, {Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.ReplaceHistory(history, nil)
	runner.SetContextManager(mgr)

	events, err := runner.RunTurn(testContext(t), "continue")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)
	info := false
	for _, ev := range got {
		if ev.Type == ui.StreamInfo && strings.Contains(ev.Text, "Dropped") {
			info = true
		}
	}
	if !info {
		t.Fatalf("events = %#v, want a StreamInfo about dropped messages", got)
	}
	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected a generate request")
	}
	if tok := estimateMessageTokens(req.Messages); tok > mgr.BudgetLimit() {
		t.Fatalf("request tokens = %d, want <= BudgetLimit %d", tok, mgr.BudgetLimit())
	}
}

// TestEnforceRequestBudgetKeepsPromptAfterCompression is the reported Opus 5.5
// failure: after compression the history opens with the summary and the canned
// ack, and an over-budget recent turn used to be truncated down to exactly
// those two, so the request ended on the assistant ack ("does not support
// assistant message prefill") and the user's prompt was never sent.
func TestEnforceRequestBudgetKeepsPromptAfterCompression(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("alpha ", 80)
	history := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "summary of earlier work"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "Got it. Thanks for the additional context!"}}},
	}
	for i := 0; i < 6; i++ {
		history = append(history,
			provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Text: body}}},
			provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{Text: body}}},
		)
	}
	summarize := func(ctx context.Context, contents []contextmgmt.Message, systemInstruction string) (string, error) {
		return "", fmt.Errorf("summarizer unavailable")
	}
	mgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:              true,
		ContextLimit:         400,
		CompressionThreshold: 0.1,
		PreserveFraction:     0.3,
		Summarize:            summarize,
	})
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "ok"}, {Done: true}}}}
	runner, err := NewRunner(RunnerConfig{Generator: gen, Model: "test-model", WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.ReplaceHistory(history, nil)
	runner.SetContextManager(mgr)

	const prompt = "what should we do next?"
	got := collectEvents(t, mustRunTurn(t, runner, prompt))
	dropped := false
	for _, ev := range got {
		if ev.Type == ui.StreamInfo && strings.Contains(ev.Text, "Dropped") {
			dropped = true
		}
	}
	if !dropped {
		t.Fatalf("events = %#v, want a StreamInfo about dropped messages", got)
	}
	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected a generate request")
		return
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != provider.RoleUser || last.Parts[0].Text != prompt {
		t.Fatalf("request ends on %+v, want the user's prompt", last)
	}
}

func TestEmptyModelReplyEmitsError(t *testing.T) {
	t.Parallel()
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	events, err := runner.RunTurn(testContext(t), "hello")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)
	found := false
	for _, ev := range got {
		if ev.Type == ui.StreamError && strings.Contains(ev.Text, "returned no content") {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %#v, want StreamError for empty model reply", got)
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

func TestEnforceRequestBudgetOverheadTruncation(t *testing.T) {
	t.Parallel()
	var history []provider.Message
	body := strings.Repeat("hello world ", 20) // ~60 tokens
	for i := 0; i < 8; i++ {
		history = append(history, provider.Message{
			Role:  provider.RoleUser,
			Parts: []provider.Part{{Text: body}},
		})
		history = append(history, provider.Message{
			Role:  provider.RoleModel,
			Parts: []provider.Part{{Text: body}},
		})
	}

	// ContextLimit = 4000, BudgetLimit = 4000*0.85 = 3400.
	// Tool schemas + system prompt overhead is ~2500 tokens.
	// Message budget = 3400 - 2500 = ~900 tokens.
	// History + new prompt has ~1000 tokens, which exceeds 900 tokens.
	mgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:      true,
		ContextLimit: 4000,
	})

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "ok"}, {Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:            gen,
		Model:                "test-model",
		WorkDir:              t.TempDir(),
		Interactive:          false,
		SystemPromptOverride: strings.Repeat("system instruction line ", 40), // ~200 tokens
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.ReplaceHistory(history, nil)
	runner.SetContextManager(mgr)

	events, err := runner.RunTurn(testContext(t), "next turn")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)
	info := false
	for _, ev := range got {
		if ev.Type == ui.StreamInfo && (strings.Contains(ev.Text, "Context exceeded") || strings.Contains(ev.Text, "Context budget exceeded")) {
			info = true
		}
	}
	if !info {
		t.Fatalf("events = %#v, want a StreamInfo about dropped/capped messages", got)
	}
	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected request")
	}
	overhead := contextmgmt.EstimateRequestOverhead(req.SystemInstruction, req.Tools)
	budget := mgr.MessageBudget(overhead)
	if tok := estimateMessageTokens(req.Messages); tok > budget {
		t.Fatalf("message tokens = %d, want <= budget %d (overhead = %d)", tok, budget, overhead)
	}
}

func TestEnforceRequestBudgetCapsOversizedMessages(t *testing.T) {
	t.Parallel()
	// When leading message is huge (~5000 tokens) but history length is small (len=2),
	// TruncateHistoryToFit cannot drop the protected leading user message (DroppedCount == 0).
	// CapOversizedMessages must cap the large message and offload it.
	hugeText := strings.Repeat("massive payload line in user prompt\n", 400) // ~14000 chars, ~3500 tokens
	history := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: hugeText}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "short ack"}}},
	}

	mgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:      true,
		ContextLimit: 400, // BudgetLimit = 340 tokens
	})

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "ok"}, {Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.ReplaceHistory(history, nil)
	runner.SetContextManager(mgr)

	events, err := runner.RunTurn(testContext(t), "short follow up")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)
	infoFound := false
	for _, ev := range got {
		if ev.Type == ui.StreamInfo && (strings.Contains(ev.Text, "capped") || strings.Contains(ev.Text, "Context budget exceeded")) {
			infoFound = true
		}
	}
	if !infoFound {
		t.Fatalf("events = %#v, want StreamInfo about capped message", got)
	}
}

type overflowRetryGenerator struct {
	calls int
	reqs  []*provider.GenerateRequest
}

func (g *overflowRetryGenerator) GenerateContentStream(ctx context.Context, req *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	g.calls++
	g.reqs = append(g.reqs, req)
	if g.calls == 1 {
		return nil, fmt.Errorf("%w: prompt is too long: context length exceeded", provider.ErrContextOverflow)
	}

	ch := make(chan provider.StreamResponse, 2)
	ch <- provider.StreamResponse{TextDelta: "recovered reply"}
	ch <- provider.StreamResponse{Done: true}
	close(ch)
	return ch, nil
}

func (g *overflowRetryGenerator) Close() error { return nil }

func TestRunnerContextOverflowRetry(t *testing.T) {
	t.Parallel()
	gen := &overflowRetryGenerator{}
	var history []provider.Message
	body := strings.Repeat("text ", 40)
	for i := 0; i < 10; i++ {
		history = append(history, provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Text: body}}})
		history = append(history, provider.Message{Role: provider.RoleModel, Parts: []provider.Part{{Text: body}}})
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.ReplaceHistory(history, nil)

	events, err := runner.RunTurn(testContext(t), "trigger turn")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got := collectEvents(t, events)

	if gen.calls != 2 {
		t.Fatalf("expected generator to be called twice (initial + retry), got %d", gen.calls)
	}

	retryInfoFound := false
	textFound := false
	for _, ev := range got {
		if ev.Type == ui.StreamInfo && strings.Contains(ev.Text, "Context length overflow reported by provider") {
			retryInfoFound = true
		}
		if ev.Type == ui.StreamTextDelta && strings.Contains(ev.Text, "recovered reply") {
			textFound = true
		}
	}
	if !retryInfoFound {
		t.Errorf("expected StreamInfo about overflow retry, got events: %#v", got)
	}
	if !textFound {
		t.Errorf("expected recovered response in events, got: %#v", got)
	}
	assertRetryShape(t, gen.reqs[len(gen.reqs)-1], "trigger turn")
}

// TestRunnerContextOverflowRetryCapsWhenNothingDroppable pins the fallback for
// a history whose only large message is the current prompt. The old fallback
// sliced history in half, which here started it on the canned model ack.
func TestRunnerContextOverflowRetryCapsWhenNothingDroppable(t *testing.T) {
	t.Parallel()
	gen := &overflowRetryGenerator{}
	runner, err := NewRunner(RunnerConfig{Generator: gen, Model: "test-model", WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.ReplaceHistory([]provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "summary"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "Got it."}}},
	}, nil)

	prompt := "analyze this:\n" + strings.Repeat("payload line that is far too large\n", 400)
	drainEvents(t, mustRunTurn(t, runner, prompt))
	if gen.calls != 2 {
		t.Fatalf("generator calls = %d, want 2 (initial + retry)", gen.calls)
	}
	retry := gen.reqs[1]
	if retry.Messages[0].Role != provider.RoleUser {
		t.Fatalf("retry starts on role %q, want user", retry.Messages[0].Role)
	}
	last := retry.Messages[len(retry.Messages)-1]
	if last.Role != provider.RoleUser || !strings.Contains(last.Parts[0].Text, "analyze this:") {
		t.Fatalf("retry ends on role %q, want the (capped) user prompt", last.Role)
	}
	if len(last.Parts[0].Text) >= len(prompt) {
		t.Errorf("retry prompt len = %d, want it capped below %d", len(last.Parts[0].Text), len(prompt))
	}
}

func assertRetryShape(t *testing.T, req *provider.GenerateRequest, prompt string) {
	t.Helper()
	if req.Messages[0].Role != provider.RoleUser {
		t.Errorf("retry starts on role %q, want user", req.Messages[0].Role)
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != provider.RoleUser || last.Parts[0].Text != prompt {
		t.Errorf("retry ends on %+v, want the user's prompt %q", last, prompt)
	}
}
