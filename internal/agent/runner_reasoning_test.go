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

// TestRunnerReasoningOverrideDoesNotBleedAcrossInstances is the Runner-level
// regression for AD-077's move of the reasoning override off a process-global
// (provider.SessionReasoningOverride) and onto Runner: two independent Runner
// instances (mirroring two logical sessions) must never share an override.
func TestRunnerReasoningOverrideDoesNotBleedAcrossInstances(t *testing.T) {
	t.Parallel()

	r1 := newReasoningTestRunner(t, "gpt-5")
	r2 := newReasoningTestRunner(t, "gpt-5")

	r1.SetReasoningOverride("high")
	if got := r2.ReasoningOverride(); got != "" {
		t.Errorf("r2 leaked r1's override: got %q, want empty", got)
	}
	if got := r1.ReasoningOverride(); got != "high" {
		t.Errorf("r1 override = %q, want high", got)
	}

	r2.SetReasoningOverride("low")
	if got := r1.ReasoningOverride(); got != "high" {
		t.Errorf("r1 override mutated by r2: got %q, want high", got)
	}
	if got := r2.ReasoningOverride(); got != "low" {
		t.Errorf("r2 override = %q, want low", got)
	}

	// The error/clear path only clears the owning instance.
	r1.ClearReasoningOverride()
	if got := r1.ReasoningOverride(); got != "" {
		t.Errorf("r1 override after clear = %q, want empty", got)
	}
	if got := r2.ReasoningOverride(); got != "low" {
		t.Errorf("clearing r1 affected r2: got %q, want low", got)
	}
}

// TestRunnerReasoningOverrideSelfInvalidatesOnModelChange verifies the
// override is scoped to the (provider, model) it was set for: once the live
// model changes (e.g. mode routing or /model), a stale override for the
// previous model is no longer returned, without requiring an explicit clear
// at every model-switch call site.
func TestRunnerReasoningOverrideSelfInvalidatesOnModelChange(t *testing.T) {
	t.Parallel()

	r := newReasoningTestRunner(t, "gpt-5")
	r.SetReasoningOverride("high")
	if got := r.ReasoningOverride(); got != "high" {
		t.Fatalf("override = %q, want high", got)
	}

	r.SetProviderDefaultModel("gpt-5-pro")
	r.RefreshModelFromMode()

	if got := r.ReasoningOverride(); got != "" {
		t.Errorf("override should self-invalidate after a model change, got %q", got)
	}
}

// TestRunnerBuildGenerateRequestUsesReasoningOverride verifies the override
// flows through buildGenerateRequest into GenerateRequest.Reasoning for the
// very next round (no rebuild required), matching /reasoning <level>'s
// "live on the next request" contract.
func TestRunnerBuildGenerateRequestUsesReasoningOverride(t *testing.T) {
	t.Parallel()

	r := newReasoningTestRunner(t, "gpt-5")
	r.SetReasoningOverride("high")

	req := r.buildGenerateRequest()
	if req.Reasoning == nil {
		t.Fatal("Reasoning is nil, want non-nil")
	}
	if req.Reasoning.Effort != "high" || !req.Reasoning.Enabled {
		t.Errorf("Reasoning = %+v, want {Effort: high, Enabled: true}", req.Reasoning)
	}

	r.ClearReasoningOverride()
	req = r.buildGenerateRequest()
	// gpt-5 (base family) has a static rule but its default omits the field
	// (let the provider apply its own default), so clearing the override
	// should leave Reasoning nil again.
	if req.Reasoning != nil {
		t.Errorf("Reasoning = %+v, want nil after clearing override", req.Reasoning)
	}
}
