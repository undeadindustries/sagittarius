package provider

import "testing"

func TestDeltaSplitsReasoningFromContent(t *testing.T) {
	t.Parallel()
	content := "answer"
	reasoning := "thinking"

	// Content and reasoning arrive on separate channels, never merged.
	if got := deltaContent(openAIStreamDelta{Content: &content, ReasoningContent: &reasoning}); got != "answer" {
		t.Fatalf("deltaContent = %q, want %q", got, "answer")
	}
	if got := deltaReasoning(openAIStreamDelta{Content: &content, ReasoningContent: &reasoning}); got != "thinking" {
		t.Fatalf("deltaReasoning = %q, want %q", got, "thinking")
	}

	// The OpenRouter "reasoning" field is also treated as reasoning, not content.
	r := "via reasoning field"
	if got := deltaReasoning(openAIStreamDelta{Reasoning: &r}); got != r {
		t.Fatalf("deltaReasoning(reasoning) = %q, want %q", got, r)
	}
	if got := deltaContent(openAIStreamDelta{Reasoning: &r}); got != "" {
		t.Fatalf("deltaContent should be empty when only reasoning present, got %q", got)
	}
}
