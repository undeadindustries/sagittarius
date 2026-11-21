package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func newReasoningTestRunner(t *testing.T, model string) *Runner {
	t.Helper()
	gen := &fakeGenerator{}
	runner, err := NewRunner(RunnerConfig{
		Generator: gen,
		Model:     model,
		WorkDir:   t.TempDir(),
		Settings: &config.Settings{
			Providers: &config.ProvidersSettings{
				Active:          string(config.BuiltInOpenAIResponses),
				OpenAIResponses: &config.ProviderInstanceConfig{},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

// TestRunnerBuildGenerateRequestUsesPersistedModelEffort verifies that
// per-model reasoningEffort in settings drives buildGenerateRequest.
func TestRunnerBuildGenerateRequestUsesPersistedModelEffort(t *testing.T) {
	t.Parallel()

	r := newReasoningTestRunner(t, "gpt-5")
	s := r.settingsSnapshot()
	s.Providers.OpenAIResponses.Models = map[string]config.ProviderModelConfig{
		"gpt-5": {ReasoningEffort: "high"},
	}
	r.SetSettings(s)

	req := r.buildGenerateRequest()
	if req.Reasoning == nil {
		t.Fatal("Reasoning is nil, want non-nil")
	}
	if req.Reasoning.Effort != "high" || !req.Reasoning.Enabled {
		t.Errorf("Reasoning = %+v, want {Effort: high, Enabled: true}", req.Reasoning)
	}

	s2 := r.settingsSnapshot()
	s2.Providers.OpenAIResponses.Models = nil
	r.SetSettings(s2)
	req = r.buildGenerateRequest()
	if req.Reasoning != nil {
		t.Errorf("Reasoning = %+v, want nil after clearing per-model effort", req.Reasoning)
	}
}

// TestRunnerBuildGenerateRequestPerModelEffortDoesNotBleedAcrossModels verifies
// that switching A -> B -> A resolves each model's own persisted effort cleanly.
func TestRunnerBuildGenerateRequestPerModelEffortDoesNotBleedAcrossModels(t *testing.T) {
	t.Parallel()

	r := newReasoningTestRunner(t, "gpt-5")
	s := r.settingsSnapshot()
	s.Providers.OpenAIResponses.Models = map[string]config.ProviderModelConfig{
		"gpt-5":     {ReasoningEffort: "high"},
		"gpt-5-pro": {ReasoningEffort: "low"},
	}
	r.SetSettings(s)

	if req := r.buildGenerateRequest(); req.Reasoning == nil || req.Reasoning.Effort != "high" {
		t.Fatalf("gpt-5 Reasoning = %+v, want high", req.Reasoning)
	}
	r.SetProviderDefaultModel("gpt-5-pro")
	if req := r.buildGenerateRequest(); req.Reasoning == nil || req.Reasoning.Effort != "low" {
		t.Fatalf("gpt-5-pro Reasoning = %+v, want low", req.Reasoning)
	}
	r.SetProviderDefaultModel("gpt-5")
	if req := r.buildGenerateRequest(); req.Reasoning == nil || req.Reasoning.Effort != "high" {
		t.Fatalf("back to gpt-5 Reasoning = %+v, want high", req.Reasoning)
	}
}
