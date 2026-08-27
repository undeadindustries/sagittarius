package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// newThoughtsTestRunner builds a Runner pinned to the Gemini-native provider,
// the only adapter that acts on GenerateRequest.IncludeThoughts.
func newThoughtsTestRunner(t *testing.T, model string, showThinking *bool) *Runner {
	t.Helper()
	runner, err := NewRunner(RunnerConfig{
		Generator: &fakeGenerator{},
		Model:     model,
		WorkDir:   t.TempDir(),
		Settings: &config.Settings{
			Providers: &config.ProvidersSettings{
				Active:       string(config.BuiltInGeminiAPIKey),
				GeminiAPIKey: &config.ProviderInstanceConfig{ShowThinking: showThinking},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

// TestIncludeThoughtsWhenReasoningEnabledWithBoxHidden is the regression for the
// reported bug: the TUI shows "Working…" while the model is reasoning, and
// Ctrl+T reveals an already-streaming thinking box. Thought text used to be
// requested only when the box was visible, so with it collapsed no reasoning
// delta ever arrived and the status label had nothing to switch on. Requesting
// thoughts is free — both Gemini and OpenAI bill the full reasoning trace either
// way — so it must not depend on a display setting.
func TestIncludeThoughtsWhenReasoningEnabledWithBoxHidden(t *testing.T) {
	t.Parallel()

	off := false
	r := newThoughtsTestRunner(t, "gemini-2.5-pro", &off)

	req := r.buildGenerateRequest()
	if req.Reasoning == nil || !req.Reasoning.Enabled {
		t.Fatalf("Reasoning = %+v, want enabled (Gemini 2.5 defaults to dynamic thinking)", req.Reasoning)
	}
	if !req.IncludeThoughts {
		t.Error("IncludeThoughts = false with reasoning enabled and the thinking box hidden, want true")
	}
}

// TestIncludeThoughtsFollowsShowThinkingWithoutReasoningRule pins the additive
// half of the resolution. config.ModelReasoningRule matches model families, not
// every id, so a model with no rule resolves to a nil ReasoningRequest; such a
// model must still receive thoughts when the box is on, exactly as before.
func TestIncludeThoughtsFollowsShowThinkingWithoutReasoningRule(t *testing.T) {
	t.Parallel()

	on := true
	r := newThoughtsTestRunner(t, "some-unmatched-model", &on)

	req := r.buildGenerateRequest()
	if req.Reasoning != nil {
		t.Fatalf("Reasoning = %+v, want nil for a model with no family rule", req.Reasoning)
	}
	if !req.IncludeThoughts {
		t.Error("IncludeThoughts = false with the thinking box on, want true")
	}
}

// TestIncludeThoughtsOffWhenReasoningDisabled confirms /reasoning none remains
// the way to stop paying for reasoning: no reasoning ask and no visible box
// means no thought text is requested either.
func TestIncludeThoughtsOffWhenReasoningDisabled(t *testing.T) {
	t.Parallel()

	off := false
	r := newThoughtsTestRunner(t, "gemini-2.5-pro", &off)
	r.SetReasoningOverride("none")

	// "none" stays Enabled on the wire: the effort has to reach the provider to
	// actively turn thinking off (Gemini maps it to ThinkingBudget=0). What must
	// not happen is asking for a summary of reasoning we just suppressed.
	req := r.buildGenerateRequest()
	if req.Reasoning == nil || req.Reasoning.Effort != "none" {
		t.Fatalf("Reasoning = %+v, want effort none after /reasoning none", req.Reasoning)
	}
	if req.Reasoning.ProducesReasoning() {
		t.Error("ProducesReasoning() = true for effort none, want false")
	}
	if req.IncludeThoughts {
		t.Error("IncludeThoughts = true with reasoning suppressed and the box hidden, want false")
	}
}
