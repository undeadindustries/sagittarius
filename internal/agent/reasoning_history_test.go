package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

type reasoningStreamGen struct {
	mu       sync.Mutex
	requests []*provider.GenerateRequest
	rounds   []provider.StreamResponse
	idx      int
}

func (g *reasoningStreamGen) GenerateContentStream(_ context.Context, req *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	g.mu.Lock()
	cloned := *req
	cloned.Messages = make([]provider.Message, len(req.Messages))
	for i := range req.Messages {
		cloned.Messages[i] = provider.Message{
			Role:  req.Messages[i].Role,
			Parts: append([]provider.Part(nil), req.Messages[i].Parts...),
		}
	}
	g.requests = append(g.requests, &cloned)
	var chunk provider.StreamResponse
	if g.idx < len(g.rounds) {
		chunk = g.rounds[g.idx]
		g.idx++
	} else {
		chunk = provider.StreamResponse{TextDelta: "done", Done: true}
	}
	g.mu.Unlock()

	ch := make(chan provider.StreamResponse, 2)
	ch <- chunk
	if !chunk.Done {
		ch <- provider.StreamResponse{Done: true}
	}
	close(ch)
	return ch, nil
}

func TestRunnerDropsReasoningTheWireWillNotReplay(t *testing.T) {
	t.Parallel()

	settings := config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "openai",
			OpenAI: &config.ProviderInstanceConfig{
				BaseURL:          "http://localhost:8000/v1",
				Model:            "Qwen/Qwen3.8-32B",
				ReasoningHistory: "none",
			},
		},
	}
	gen := &reasoningStreamGen{
		rounds: []provider.StreamResponse{
			{ReasoningDelta: strings.Repeat("r", 3200), TextDelta: "ans 1", Done: true},
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Settings:    &settings,
		Model:       "Qwen/Qwen3.8-32B",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	// Reasoning restored by --resume (or kept under a model that replays it)
	// must not survive into a round whose model does not receive it.
	runner.historyMu.Lock()
	runner.history = []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "earlier"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Reasoning: strings.Repeat("o", 3200), Text: "earlier answer"}}},
	}
	runner.historyMu.Unlock()

	events, err := runner.RunTurn(testContext(t), "u1")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	for _, ev := range collectEvents(t, events) {
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
	}

	runner.historyMu.RLock()
	defer runner.historyMu.RUnlock()
	for i, msg := range runner.history {
		for _, p := range msg.Parts {
			if p.Reasoning != "" {
				t.Errorf("history[%d] carries %d chars of reasoning the wire never sends", i, len(p.Reasoning))
			}
		}
	}
	if n := len(runner.history); n == 0 || runner.history[n-1].Parts[0].Text != "ans 1" {
		t.Fatalf("model reply missing from history: %+v", runner.history)
	}
}

func TestRunnerReasoningHistoryReplayAndBatchedPrunePrefixStability(t *testing.T) {
	t.Parallel()

	assertCache := true
	settings := config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "openai",
			OpenAI: &config.ProviderInstanceConfig{
				BaseURL: "http://localhost:8000/v1",
				Model:   "Qwen/Qwen3.8-32B",
			},
		},
		Sagittarius: &config.SagittariusSettings{
			Debug: &config.SagittariusDebugConfig{
				AssertPromptCache: &assertCache,
			},
		},
	}

	// Each 3200-char reasoning string is 800 tokens (at 4 chars/token).
	// With ProtectionThresholdTokens=2000 and MinPrunableThresholdTokens=1000:
	// - Turns 1..4 stay below the prunable buffer (turn 4 has 800 unprotected < 1000) -> 0 breaks.
	// - Turn 5 reaches 1600 unprotected tokens (turns 1+2) >= 1000 -> 1 batch prune break!
	think800 := strings.Repeat("r", 3200)
	gen := &reasoningStreamGen{
		rounds: []provider.StreamResponse{
			{ReasoningDelta: think800, TextDelta: "ans 1", Done: true},
			{ReasoningDelta: think800, TextDelta: "ans 2", Done: true},
			{ReasoningDelta: think800, TextDelta: "ans 3", Done: true},
			{ReasoningDelta: think800, TextDelta: "ans 4", Done: true},
			{ReasoningDelta: think800, TextDelta: "ans 5", Done: true},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Settings:    &settings,
		Model:       "Qwen/Qwen3.8-32B",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetContextManager(contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:                   true,
		ContextLimit:              20000,
		EstimateSafetyFactor:      1.0,
		MaskingEnabled:            true,
		MaskingProtectionFraction: 0.10, // 2000 tokens
		MaskingPrunableFraction:   0.05, // 1000 tokens
		MaskingProtectLatestTurn:  true,
		ReasoningPruneEnabled:     true,
	}))

	runOnce := func(prompt string) {
		t.Helper()
		events, err := runner.RunTurn(testContext(t), prompt)
		if err != nil {
			t.Fatalf("RunTurn(%q): %v", prompt, err)
		}
		for _, ev := range collectEvents(t, events) {
			if ev.Err != nil {
				t.Fatalf("RunTurn(%q) stream error: %v", prompt, ev.Err)
			}
		}
	}

	runOnce("u1")
	runOnce("u2")
	runOnce("u3")
	runOnce("u4")
	statsBeforePrune := runner.Stats()
	if len(statsBeforePrune.CacheBreaks) != 0 {
		t.Fatalf("CacheBreaks before batch threshold = %+v, want empty", statsBeforePrune.CacheBreaks)
	}

	// Turn 5: turns 3+4 reasoning (1600 tok) protected; turns 1+2 reasoning (1600 tok >= 1000) pruned in one batch!
	// assertPromptCache is enabled and must pass because "reasoning-prune" is an expected break.
	runOnce("u5")
	statsAfterPrune := runner.Stats()
	if len(statsAfterPrune.CacheBreaks) != 1 {
		t.Fatalf("CacheBreaks after batch prune = %+v, want 1", statsAfterPrune.CacheBreaks)
	}
	if statsAfterPrune.CacheBreaks[0].Reason != "reasoning-prune" {
		t.Fatalf("CacheBreaks[0] = %+v, want reason=reasoning-prune", statsAfterPrune.CacheBreaks[0])
	}
}
