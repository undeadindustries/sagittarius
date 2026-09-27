package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// subagentHarness builds a parent runner on the openai-chat path with per-model
// context pins, capturing the settings each child launch passes to the
// generator factory.
type subagentHarness struct {
	parent   *Runner
	captured []*config.Settings
}

func newSubagentHarness(t *testing.T, settings *config.Settings) *subagentHarness {
	t.Helper()
	h := &subagentHarness{}
	runner, err := NewRunner(RunnerConfig{
		Generator:    &fakeGenerator{},
		Model:        "parent-model",
		WorkDir:      t.TempDir(),
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
		Settings:     settings,
		InitialMode:  modes.ModeAgent,
		SubagentGenerator: func(_ context.Context, s *config.Settings) (provider.ContentGenerator, error) {
			h.captured = append(h.captured, s)
			return &fakeGenerator{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	runner.SetProviderDefaultModel("parent-model")
	h.parent = runner
	return h
}

func openAISettingsWithModelPins(pins map[string]int) *config.Settings {
	models := make(map[string]config.ProviderModelConfig, len(pins))
	for model, limit := range pins {
		limit := limit
		models[model] = config.ProviderModelConfig{ContextLimit: &limit}
	}
	return &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{Models: models},
		},
		Sagittarius: &config.SagittariusSettings{},
	}
}

// TestSubagentCrossProviderClone pins a child to a different provider and
// asserts the generator factory receives a clone with that pair forced active
// — not the parent's settings, and not the parent's model.
func TestSubagentCrossProviderClone(t *testing.T) {
	t.Parallel()

	on := true
	settings := openAISettingsWithModelPins(map[string]int{"parent-model": 100_000})
	settings.Sagittarius.Subagents = &config.SagittariusSubagents{
		Coding: &config.SagittariusSubagentClass{Enabled: &on, Provider: "openrouter", Model: "qwen/qwen3-coder"},
	}
	h := newSubagentHarness(t, settings)

	child, err := h.parent.newSubagent(context.Background(), subagentSpec{
		description: "cross-provider",
		mode:        modes.ModeAgent,
		class:       config.SubagentCoding,
		approval:    ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("newSubagent: %v", err)
	}
	if len(h.captured) != 1 {
		t.Fatalf("generator factory called %d times, want 1", len(h.captured))
	}
	clone := h.captured[0]
	if got := clone.ActiveProvider(); got != "openrouter" {
		t.Errorf("clone active provider = %q, want openrouter", got)
	}
	if got := child.runner.Model(); got != "qwen/qwen3-coder" {
		t.Errorf("child model = %q, want qwen/qwen3-coder", got)
	}
	if got := child.runner.ActiveProviderID(); got != "openrouter" {
		t.Errorf("child provider = %q, want openrouter", got)
	}
	if settings.ActiveProvider() != string(config.BuiltInOpenAI) {
		t.Errorf("parent settings mutated: active = %q", settings.ActiveProvider())
	}
}

// TestSubagentChildContextLimitFollowsPin is the AD-137 child case: a child
// pinned to a smaller-window model must get that model's window, not the
// parent's and not zero. Against the old bare-NewManager construction the
// child's ContextLimit is 0 because masking and compression never run.
func TestSubagentChildContextLimitFollowsPin(t *testing.T) {
	t.Parallel()

	on := true
	settings := openAISettingsWithModelPins(map[string]int{
		"parent-model": 100_000,
		"child-model":  50_000,
	})
	settings.Sagittarius.Subagents = &config.SagittariusSubagents{
		Research: &config.SagittariusSubagentClass{
			Enabled:  &on,
			Provider: string(config.BuiltInOpenAI),
			Model:    "child-model",
		},
	}
	h := newSubagentHarness(t, settings)

	child, err := h.parent.newSubagent(context.Background(), subagentSpec{
		description: "pinned window",
		mode:        modes.ModeAsk,
		class:       config.SubagentResearch,
		approval:    ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("newSubagent: %v", err)
	}
	mgr := child.runner.contextManager()
	if mgr == nil {
		t.Fatal("child has no context manager")
	}
	if got := mgr.ContextLimit(); got != 50_000 {
		t.Errorf("child ContextLimit = %d, want 50000 (the pinned model's window)", got)
	}
}

// TestSubagentGeneratorErrorIsFailLoud pins a child to a pair whose generator
// cannot be built and asserts the launch fails naming the slot and /subagents
// — it never falls back to the parent's model.
func TestSubagentGeneratorErrorIsFailLoud(t *testing.T) {
	t.Parallel()

	on := true
	settings := openAISettingsWithModelPins(nil)
	settings.Sagittarius.Subagents = &config.SagittariusSubagents{
		Coding: &config.SagittariusSubagentClass{Enabled: &on, Provider: "openrouter", Model: "qwen/qwen3-coder"},
	}
	h := newSubagentHarness(t, settings)
	h.parent.newSubagentGenerator = func(context.Context, *config.Settings) (provider.ContentGenerator, error) {
		return nil, errors.New("no credential for pinned provider")
	}

	_, err := h.parent.newSubagent(context.Background(), subagentSpec{
		description: "broken pin",
		mode:        modes.ModeAgent,
		class:       config.SubagentCoding,
		approval:    ApprovalYolo,
	})
	if err == nil {
		t.Fatal("expected a launch error, got nil")
	}
	for _, want := range []string{"coding", "openrouter", "qwen/qwen3-coder", "/subagents"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if len(h.captured) != 0 {
		t.Errorf("failing factory still recorded %d calls", len(h.captured))
	}
}
