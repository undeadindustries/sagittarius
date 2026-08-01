package provider

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/genai"
)

// staticStreamer returns a fixed sequence of responses for testing.
type staticStreamer struct {
	resps []*genai.GenerateContentResponse
	// capturedConfig is set on each call so tests can inspect what was sent.
	capturedConfig *genai.GenerateContentConfig
}

func (s *staticStreamer) GenerateContentStream(
	_ context.Context,
	_ string,
	_ []*genai.Content,
	cfg *genai.GenerateContentConfig,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	s.capturedConfig = cfg
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for _, r := range s.resps {
			if !yield(r, nil) {
				return
			}
		}
	}
}

// makeResp builds a minimal GenerateContentResponse with the given parts.
func makeResp(parts ...*genai.Part) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Role:  "model",
					Parts: parts,
				},
			},
		},
	}
}

// TestGeminiThoughtPartsEmitReasoningDelta verifies that when Gemini returns
// thought parts (p.Thought == true) the stream emits ReasoningDelta, not
// TextDelta, and that answer text still arrives as TextDelta.
func TestGeminiThoughtPartsEmitReasoningDelta(t *testing.T) {
	t.Parallel()

	thoughtText := "I should consider the problem carefully."
	answerText := "The answer is 42."

	streamer := &staticStreamer{
		resps: []*genai.GenerateContentResponse{
			makeResp(&genai.Part{Text: thoughtText, Thought: true}),
			makeResp(&genai.Part{Text: answerText}),
		},
	}

	gen := &GeminiGenerator{streamer: streamer, model: "gemini-test"}
	ch, err := gen.GenerateContentStream(context.Background(), &GenerateRequest{
		Messages:        []Message{{Role: RoleUser, Parts: []Part{{Text: "hello"}}}},
		IncludeThoughts: true,
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}

	var reasoning, text string
	for resp := range ch {
		if resp.Error != nil {
			t.Fatalf("stream error: %v", resp.Error)
		}
		reasoning += resp.ReasoningDelta
		text += resp.TextDelta
	}

	if reasoning != thoughtText {
		t.Errorf("ReasoningDelta = %q, want %q", reasoning, thoughtText)
	}
	if text != answerText {
		t.Errorf("TextDelta = %q, want %q", text, answerText)
	}
}

// TestGeminiThoughtPartsNotInModelParts verifies that thought parts are
// excluded from the ModelParts emitted for history replay, so they are
// never sent back to the API.
func TestGeminiThoughtPartsNotInModelParts(t *testing.T) {
	t.Parallel()

	streamer := &staticStreamer{
		resps: []*genai.GenerateContentResponse{
			makeResp(
				&genai.Part{Text: "thinking step", Thought: true},
				&genai.Part{Text: "final answer"},
			),
		},
	}

	gen := &GeminiGenerator{streamer: streamer, model: "gemini-test"}
	ch, err := gen.GenerateContentStream(context.Background(), &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "q"}}}},
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}

	var modelParts []Part
	for resp := range ch {
		if resp.Error != nil {
			t.Fatalf("stream error: %v", resp.Error)
		}
		if len(resp.ModelParts) > 0 {
			modelParts = resp.ModelParts
		}
	}

	for _, p := range modelParts {
		if p.Text == "thinking step" {
			t.Error("ModelParts contains thought text that should have been excluded")
		}
	}
	if len(modelParts) != 1 || modelParts[0].Text != "final answer" {
		t.Errorf("ModelParts = %#v, want [{Text: \"final answer\"}]", modelParts)
	}
}

// TestBuildGenerateContentConfigIncludeThoughts verifies that IncludeThoughts
// on the request causes ThinkingConfig to be set on the Gemini config.
func TestBuildGenerateContentConfigIncludeThoughts(t *testing.T) {
	t.Parallel()

	cfg := BuildGenerateContentConfig(&GenerateRequest{IncludeThoughts: true})
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig is nil, want non-nil")
	}
	if !cfg.ThinkingConfig.IncludeThoughts {
		t.Error("IncludeThoughts = false, want true")
	}
}

// TestBuildGenerateContentConfigNoIncludeThoughts verifies that when
// IncludeThoughts is false (default), ThinkingConfig is not set.
func TestBuildGenerateContentConfigNoIncludeThoughts(t *testing.T) {
	t.Parallel()

	cfg := BuildGenerateContentConfig(&GenerateRequest{IncludeThoughts: false})
	if cfg.ThinkingConfig != nil {
		t.Errorf("ThinkingConfig = %+v, want nil", cfg.ThinkingConfig)
	}
}

// TestGeminiStreamerReceivesIncludeThoughtsConfig verifies that when
// IncludeThoughts is set the underlying streamer call gets a config with
// ThinkingConfig.IncludeThoughts=true.
func TestGeminiStreamerReceivesIncludeThoughtsConfig(t *testing.T) {
	t.Parallel()

	streamer := &staticStreamer{
		resps: []*genai.GenerateContentResponse{makeResp(&genai.Part{Text: "hi"})},
	}

	gen := &GeminiGenerator{streamer: streamer, model: "gemini-test"}
	ch, err := gen.GenerateContentStream(context.Background(), &GenerateRequest{
		Messages:        []Message{{Role: RoleUser, Parts: []Part{{Text: "hello"}}}},
		IncludeThoughts: true,
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}
	for range ch {
	}

	if streamer.capturedConfig == nil {
		t.Fatal("streamer capturedConfig is nil")
	}
	if streamer.capturedConfig.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig is nil in streamer call, want non-nil")
	}
	if !streamer.capturedConfig.ThinkingConfig.IncludeThoughts {
		t.Error("IncludeThoughts = false in streamer call, want true")
	}
}

// TestBuildGenerateContentConfigReasoningDynamic verifies an empty-effort,
// enabled ReasoningRequest sets ThinkingBudget=-1 (adaptive/dynamic), the
// Gemini-family default per config.ResolveReasoningRequest.
func TestBuildGenerateContentConfigReasoningDynamic(t *testing.T) {
	t.Parallel()

	cfg := BuildGenerateContentConfig(&GenerateRequest{
		Model:     "gemini-3-pro",
		Reasoning: &ReasoningRequest{Enabled: true},
	})
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig is nil, want non-nil")
	}
	if cfg.ThinkingConfig.ThinkingBudget == nil || *cfg.ThinkingConfig.ThinkingBudget != -1 {
		t.Errorf("ThinkingBudget = %v, want -1 (dynamic)", cfg.ThinkingConfig.ThinkingBudget)
	}
	if cfg.ThinkingConfig.ThinkingLevel != "" {
		t.Errorf("ThinkingLevel = %q, want empty when dynamic", cfg.ThinkingConfig.ThinkingLevel)
	}
}

// TestBuildGenerateContentConfigReasoningDisabled verifies "none"/"off"
// disables thinking via ThinkingBudget=0.
func TestBuildGenerateContentConfigReasoningDisabled(t *testing.T) {
	t.Parallel()

	for _, effort := range []string{"none", "off"} {
		cfg := BuildGenerateContentConfig(&GenerateRequest{
			Model:     "gemini-3-pro",
			Reasoning: &ReasoningRequest{Enabled: true, Effort: effort},
		})
		if cfg.ThinkingConfig == nil {
			t.Fatalf("effort=%q: ThinkingConfig is nil, want non-nil", effort)
		}
		if cfg.ThinkingConfig.ThinkingBudget == nil || *cfg.ThinkingConfig.ThinkingBudget != 0 {
			t.Errorf("effort=%q: ThinkingBudget = %v, want 0 (disabled)", effort, cfg.ThinkingConfig.ThinkingBudget)
		}
	}
}

// TestBuildGenerateContentConfigReasoningPinnedLevelGemini3 verifies a pinned
// level (e.g. "high") on a Gemini 3 model maps to ThinkingLevel, not a raw
// ThinkingBudget.
func TestBuildGenerateContentConfigReasoningPinnedLevelGemini3(t *testing.T) {
	t.Parallel()

	cfg := BuildGenerateContentConfig(&GenerateRequest{
		Model:     "gemini-3-flash",
		Reasoning: &ReasoningRequest{Enabled: true, Effort: "high"},
	})
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig is nil, want non-nil")
	}
	if cfg.ThinkingConfig.ThinkingLevel != genai.ThinkingLevelHigh {
		t.Errorf("ThinkingLevel = %q, want %q", cfg.ThinkingConfig.ThinkingLevel, genai.ThinkingLevelHigh)
	}
	if cfg.ThinkingConfig.ThinkingBudget != nil {
		t.Errorf("ThinkingBudget = %v, want nil when ThinkingLevel is set", cfg.ThinkingConfig.ThinkingBudget)
	}
}

// TestBuildGenerateContentConfigReasoningPinnedLevelGemini25FallsBackDynamic
// verifies a pinned level on a Gemini 2.5 model (no ThinkingLevel support)
// falls back to dynamic ThinkingBudget=-1 rather than guessing a raw budget.
func TestBuildGenerateContentConfigReasoningPinnedLevelGemini25FallsBackDynamic(t *testing.T) {
	t.Parallel()

	cfg := BuildGenerateContentConfig(&GenerateRequest{
		Model:     "gemini-2.5-flash",
		Reasoning: &ReasoningRequest{Enabled: true, Effort: "high"},
	})
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig is nil, want non-nil")
	}
	if cfg.ThinkingConfig.ThinkingLevel != "" {
		t.Errorf("ThinkingLevel = %q, want empty on Gemini 2.5", cfg.ThinkingConfig.ThinkingLevel)
	}
	if cfg.ThinkingConfig.ThinkingBudget == nil || *cfg.ThinkingConfig.ThinkingBudget != -1 {
		t.Errorf("ThinkingBudget = %v, want -1 (dynamic fallback)", cfg.ThinkingConfig.ThinkingBudget)
	}
}

// TestBuildGenerateContentConfigReasoningDisabledIgnored verifies a
// non-enabled ReasoningRequest never sets ThinkingConfig at all (no
// IncludeThoughts either).
func TestBuildGenerateContentConfigReasoningDisabledIgnored(t *testing.T) {
	t.Parallel()

	cfg := BuildGenerateContentConfig(&GenerateRequest{
		Model:     "gemini-3-pro",
		Reasoning: &ReasoningRequest{Enabled: false, Effort: "high"},
	})
	if cfg.ThinkingConfig != nil {
		t.Errorf("ThinkingConfig = %+v, want nil when Reasoning.Enabled=false", cfg.ThinkingConfig)
	}
}
