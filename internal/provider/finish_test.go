package provider

import (
	"context"
	"iter"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestFinishNotice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reason     string
		wantSubstr []string
	}{
		{"recitation suggests paraphrasing", "RECITATION", []string{"RECITATION", "paraphrase"}},
		{"gemini max tokens", "MAX_TOKENS", []string{"MAX_TOKENS", "output limit"}},
		{"openai length", "length", []string{"length", "output limit"}},
		{"safety", "SAFETY", []string{"SAFETY", "content filter"}},
		{"content filter", "content_filter", []string{"content_filter", "content filter"}},
		{"prompt blocked", "PROMPT_BLOCKED_SAFETY", []string{"PROMPT_BLOCKED_SAFETY", "content filter"}},
		{"malformed call", "MALFORMED_FUNCTION_CALL", []string{"MALFORMED_FUNCTION_CALL", "tool call"}},
		{"unknown reason is still named", "SOMETHING_NEW", []string{"SOMETHING_NEW"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FinishNotice(tt.reason)
			for _, want := range tt.wantSubstr {
				if !strings.Contains(got, want) {
					t.Errorf("FinishNotice(%q) = %q, want it to contain %q", tt.reason, got, want)
				}
			}
		})
	}

	if got := FinishNotice(""); got != "" {
		t.Errorf("FinishNotice(\"\") = %q, want empty (a normal finish is silent)", got)
	}
}

func TestGeminiFinishIsAbnormal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason genai.FinishReason
		want   bool
	}{
		{genai.FinishReasonStop, false},
		{genai.FinishReasonUnspecified, false},
		{"", false},
		{genai.FinishReasonMaxTokens, true},
		{genai.FinishReasonSafety, true},
		{genai.FinishReasonRecitation, true},
		{genai.FinishReasonOther, true},
		{genai.FinishReasonMalformedFunctionCall, true},
	}
	for _, tt := range tests {
		if got := geminiFinishIsAbnormal(tt.reason); got != tt.want {
			t.Errorf("geminiFinishIsAbnormal(%q) = %v, want %v", tt.reason, got, tt.want)
		}
	}
}

func TestOpenAIFinishIsAbnormal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason string
		want   bool
	}{
		{"", false},
		{"stop", false},
		{"tool_calls", false},
		{"function_call", false},
		{"length", true},
		{"content_filter", true},
		{"error", true},
		// Unknown vocabularies stay quiet: a false alarm on every reply from an
		// unfamiliar server is worse than a missed notice.
		{"end_turn", false},
	}
	for _, tt := range tests {
		if got := openAIFinishIsAbnormal(tt.reason); got != tt.want {
			t.Errorf("openAIFinishIsAbnormal(%q) = %v, want %v", tt.reason, got, tt.want)
		}
	}
}

// scriptedStreamer replays fixed Gemini responses.
type scriptedStreamer struct {
	responses []*genai.GenerateContentResponse
}

func (s scriptedStreamer) GenerateContentStream(
	_ context.Context,
	_ string,
	_ []*genai.Content,
	_ *genai.GenerateContentConfig,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for _, r := range s.responses {
			if !yield(r, nil) {
				return
			}
		}
	}
}

func geminiChunk(text string, finish genai.FinishReason) *genai.GenerateContentResponse {
	c := &genai.Candidate{FinishReason: finish}
	if text != "" {
		c.Content = &genai.Content{Parts: []*genai.Part{{Text: text}}}
	}
	return &genai.GenerateContentResponse{Candidates: []*genai.Candidate{c}}
}

// finishReasonsFromGemini runs the adapter over the scripted responses and
// returns every non-empty FinishReason it emitted.
func finishReasonsFromGemini(t *testing.T, responses ...*genai.GenerateContentResponse) []string {
	t.Helper()
	gen, err := NewGeminiGenerator(context.Background(), GeminiConfig{
		Model:    "test-model",
		Streamer: scriptedStreamer{responses: responses},
	})
	if err != nil {
		t.Fatalf("NewGeminiGenerator: %v", err)
	}
	ch, err := gen.GenerateContentStream(context.Background(), &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}
	var reasons []string
	for sr := range ch {
		if sr.FinishReason != "" {
			reasons = append(reasons, sr.FinishReason)
		}
	}
	return reasons
}

func TestGeminiStreamReportsAbnormalFinish(t *testing.T) {
	t.Parallel()

	t.Run("recitation mid-answer is reported", func(t *testing.T) {
		t.Parallel()
		got := finishReasonsFromGemini(t,
			geminiChunk("if (x) {", genai.FinishReasonUnspecified),
			geminiChunk("", genai.FinishReasonRecitation),
		)
		if len(got) != 1 || got[0] != "RECITATION" {
			t.Errorf("finish reasons = %v, want [RECITATION]", got)
		}
	})

	t.Run("normal stop is silent", func(t *testing.T) {
		t.Parallel()
		got := finishReasonsFromGemini(t, geminiChunk("done", genai.FinishReasonStop))
		if len(got) != 0 {
			t.Errorf("finish reasons = %v, want none for STOP", got)
		}
	})

	t.Run("blocked prompt is reported", func(t *testing.T) {
		t.Parallel()
		got := finishReasonsFromGemini(t, &genai.GenerateContentResponse{
			PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
				BlockReason: genai.BlockedReasonSafety,
			},
		})
		if len(got) != 1 || !strings.HasPrefix(got[0], "PROMPT_BLOCKED") {
			t.Errorf("finish reasons = %v, want a PROMPT_BLOCKED reason", got)
		}
	})
}

func TestFlushSSEStateReportsAbnormalFinish(t *testing.T) {
	t.Parallel()

	run := func(finish string) []string {
		state := &sseStreamState{lastFinish: finish}
		var reasons []string
		_, err := flushSSEState(state, config.ToolCallParsingMode(""), func(sr StreamResponse) bool {
			if sr.FinishReason != "" {
				reasons = append(reasons, sr.FinishReason)
			}
			return true
		})
		if err != nil {
			t.Fatalf("flushSSEState: %v", err)
		}
		return reasons
	}

	if got := run("length"); len(got) != 1 || got[0] != "length" {
		t.Errorf("length: finish reasons = %v, want [length]", got)
	}
	if got := run("stop"); len(got) != 0 {
		t.Errorf("stop: finish reasons = %v, want none", got)
	}
}
