package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// contextLimitRunner builds a runner on the openai-chat path whose provider pins
// a context window per model, plus a plan-mode override routing to planModel.
func contextLimitRunner(t *testing.T, agentModel, planModel string, pins map[string]int) *Runner {
	t.Helper()

	models := make(map[string]config.ProviderModelConfig, len(pins))
	for model, limit := range pins {
		limit := limit
		models[model] = config.ProviderModelConfig{ContextLimit: &limit}
	}
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{Models: models},
		},
		Sagittarius: &config.SagittariusSettings{
			Modes: &config.SagittariusModes{
				Plan: &config.SagittariusModeConfig{Model: planModel},
			},
		},
	}

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       agentModel,
		WorkDir:     t.TempDir(),
		Interactive: false,
		Settings:    settings,
		InitialMode: modes.ModeAgent,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetProviderDefaultModel(agentModel)

	mgr := NewContextManager(settings, nil, runner.CompressionModel,
		runner.ActiveProviderID, func() string { return runner.InteractionMode().String() },
		"sess-ctx-limit", nil, nil)
	if mgr == nil {
		t.Fatal("expected a context manager for the openai-chat provider")
	}
	runner.SetContextManager(mgr)
	return runner
}

// TestContextLimitFollowsSameProviderModeOverride is the reported failure: a
// mode override that changes only the model leaves the provider unchanged, so
// nothing rebuilds the context manager. A window captured at construction meant
// a 262k conversation kept budgeting for 262k after routing to a 131k model,
// and the model rejected the request.
func TestContextLimitFollowsSameProviderModeOverride(t *testing.T) {
	t.Parallel()

	runner := contextLimitRunner(t, "big-model", "small-model", map[string]int{
		"big-model":   262_144,
		"small-model": 131_000,
	})

	if got := runner.contextManager().ContextLimit(); got != 262_144 {
		t.Fatalf("agent-mode ContextLimit = %d, want 262144", got)
	}

	if model := runner.SetInteractionMode(modes.ModePlan); model != "small-model" {
		t.Fatalf("plan-mode model = %q, want small-model", model)
	}
	if got := runner.contextManager().ContextLimit(); got != 131_000 {
		t.Fatalf("plan-mode ContextLimit = %d, want 131000", got)
	}
	if got := runner.contextManager().BudgetLimit(); got != 111_350 {
		t.Fatalf("plan-mode BudgetLimit = %d, want 111350", got)
	}

	if model := runner.SetInteractionMode(modes.ModeAgent); model != "big-model" {
		t.Fatalf("model back in agent mode = %q, want big-model", model)
	}
	if got := runner.contextManager().ContextLimit(); got != 262_144 {
		t.Fatalf("ContextLimit back in agent mode = %d, want 262144", got)
	}
}

// TestPrepareContextSurfacesCompressionFailure pins that a rejected summarizer
// request reaches the user. A malformed replayed tool call made every
// summarizer request 400, and because the failure was a log line only, the
// session looked like it had simply stopped compressing.
func TestPrepareContextSurfacesCompressionFailure(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "small-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	summarize := func(context.Context, []provider.Message, string) (string, error) {
		return "", errors.New("HTTP 400: Assistant tool call function.arguments must be a JSON object")
	}
	runner.SetContextManager(contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:              true,
		ContextLimit:         50,
		CompressionThreshold: 0.4,
		PreserveFraction:     0.3,
		Summarize:            summarize,
	}))
	runner.history = []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: strings.Repeat("alpha ", 8)}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: strings.Repeat("beta ", 8)}}},
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: strings.Repeat("gamma ", 8)}}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: strings.Repeat("delta ", 8)}}},
	}

	out := make(chan ui.StreamEvent, 4)
	runner.prepareContext(context.Background(), out)
	close(out)

	var texts []string
	for ev := range out {
		if ev.Type == ui.StreamInfo {
			texts = append(texts, ev.Text)
		}
	}
	if len(texts) != 1 {
		t.Fatalf("StreamInfo events = %d (%q), want 1", len(texts), texts)
	}
	for _, want := range []string{"Context compression failed", "JSON object", "truncation"} {
		if !strings.Contains(texts[0], want) {
			t.Fatalf("event = %q, want it to mention %q", texts[0], want)
		}
	}
}

// TestContextFitNotice covers the three outcomes a switch can have: a history
// that no longer fits (warn), one that fits (silent), and a provider with no
// client-side context management (silent).
func TestContextFitNotice(t *testing.T) {
	t.Parallel()

	runner := contextLimitRunner(t, "big-model", "small-model", map[string]int{
		"big-model":   262_144,
		"small-model": 4_000,
	})
	runner.history = []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: strings.Repeat("alpha ", 20_000)}}},
	}

	if got := runner.ContextFitNotice(); got != "" {
		t.Fatalf("notice while history fits = %q, want empty", got)
	}

	runner.SetInteractionMode(modes.ModePlan)
	notice := runner.ContextFitNotice()
	for _, want := range []string{"small-model", "30.0k", "3.4k"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice = %q, want it to mention %q", notice, want)
		}
	}

	runner.SetContextManager(nil)
	if got := runner.ContextFitNotice(); got != "" {
		t.Fatalf("notice without a context manager = %q, want empty", got)
	}
}

func TestContextFitNoticeEmptyHistory(t *testing.T) {
	t.Parallel()

	runner := contextLimitRunner(t, "small-model", "small-model", map[string]int{"small-model": 4_000})
	if got := runner.ContextFitNotice(); got != "" {
		t.Fatalf("notice with empty history = %q, want empty", got)
	}
}

// TestContextLimitIgnoresConstructionTimeModel covers the cross-provider
// ordering: a provider-changing mode switch rebuilds the manager *before* the
// new mode is applied, so construction sees the origin mode's model. Here that
// model has no pin on the destination provider and resolves to the preset's
// 128k, which would over-truncate a conversation bound for a 65k model.
func TestContextLimitIgnoresConstructionTimeModel(t *testing.T) {
	t.Parallel()

	runner := contextLimitRunner(t, "gemini-3.8-flash", "small-model", map[string]int{
		"small-model": 65_000,
	})

	if got := runner.contextManager().ContextLimit(); got != 128_000 {
		t.Fatalf("construction-time ContextLimit = %d, want the openai preset 128000", got)
	}

	runner.SetInteractionMode(modes.ModePlan)
	if got := runner.contextManager().ContextLimit(); got != 65_000 {
		t.Fatalf("ContextLimit after mode switch = %d, want the destination model's 65000", got)
	}
}
