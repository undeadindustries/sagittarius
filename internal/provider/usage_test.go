package provider

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// TestGeneratedTokens verifies Gemini adds separate thought tokens while an
// OpenAI-style usage leaves them inside OutputTokens.
func TestGeneratedTokens(t *testing.T) {
	t.Parallel()

	gemini := Usage{
		OutputTokens:      20,
		ReasoningTokens:   80,
		ReasoningSeparate: true,
	}
	if got := gemini.GeneratedTokens(); got != 100 {
		t.Errorf("gemini GeneratedTokens = %d, want 100", got)
	}

	openai := Usage{OutputTokens: 20, ReasoningTokens: 80}
	if got := openai.GeneratedTokens(); got != 20 {
		t.Errorf("openai GeneratedTokens = %d, want 20 (reasoning already in OutputTokens)", got)
	}
}

// TestOpenAIChatUsageParsed verifies that a standard OpenAI-chat stream with a
// trailing usage object emits a StreamResponse with non-zero token counts.
func TestOpenAIChatUsageParsed(t *testing.T) {
	t.Parallel()

	// Simulate a typical OpenAI stream: a text delta, a done chunk, then a
	// usage-carrying final frame with no choices.
	sse := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]," +
		"\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":2}}}\n\n" +
		"data: [DONE]\n\n"

	var usageResp *StreamResponse
	_, err := parseSSEStream(
		strings.NewReader(sse),
		config.ToolCallParsingLenient,
		nil,
		func(r StreamResponse) bool {
			if r.Usage != nil {
				usageResp = &r
			}
			return true
		},
	)
	if err != nil {
		t.Fatalf("parseSSEStream: %v", err)
	}
	if usageResp == nil {
		t.Fatal("expected a StreamResponse with Usage set, got none")
		return
	}
	if usageResp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", usageResp.Usage.InputTokens)
	}
	if usageResp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", usageResp.Usage.OutputTokens)
	}
	if usageResp.Usage.CachedTokens != 4 {
		t.Errorf("CachedTokens = %d, want 4", usageResp.Usage.CachedTokens)
	}
	if !usageResp.Usage.CachedReported {
		t.Error("CachedReported = false, want true when prompt_tokens_details is present")
	}
	if usageResp.Usage.ReasoningTokens != 2 {
		t.Errorf("ReasoningTokens = %d, want 2", usageResp.Usage.ReasoningTokens)
	}
	if usageResp.Usage.CostKnown {
		t.Error("CostKnown should be false for plain OpenAI (no cost field)")
	}
}

// TestOpenAIChatCachedReportedTruthTable verifies that CachedReported distinguishes
// an omitted prompt_tokens_details (unreported) from an explicit 0 cached_tokens.
func TestOpenAIChatCachedReportedTruthTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		usageJSON    string
		wantCached   int
		wantReported bool
	}{
		{
			name:         "no-details-unreported",
			usageJSON:    `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`,
			wantCached:   0,
			wantReported: false,
		},
		{
			name:         "zero-cached-reported",
			usageJSON:    `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":0}}`,
			wantCached:   0,
			wantReported: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sse := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":" + tt.usageJSON + "}\n\ndata: [DONE]\n\n"
			var u *Usage
			_, err := parseSSEStream(strings.NewReader(sse), config.ToolCallParsingLenient, nil, func(r StreamResponse) bool {
				if r.Usage != nil {
					u = r.Usage
				}
				return true
			})
			if err != nil {
				t.Fatalf("parseSSEStream: %v", err)
			}
			if u == nil {
				t.Fatal("expected Usage, got nil")
				return
			}
			if u.CachedTokens != tt.wantCached || u.CachedReported != tt.wantReported {
				t.Errorf("got CachedTokens=%d CachedReported=%v, want %d %v",
					u.CachedTokens, u.CachedReported, tt.wantCached, tt.wantReported)
			}
		})
	}
}

// TestOpenRouterCostParsed verifies that an OpenRouter final frame that carries a
// "cost" field is reflected in CostUSD and CostKnown.
func TestOpenRouterCostParsed(t *testing.T) {
	t.Parallel()

	// OpenRouter sends a usage-only final frame after [DONE] equivalent:
	// a chunk with no choices and a "cost" field in the usage object.
	sse := "data: {\"id\":\"r1\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"r1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":8,\"total_tokens\":28,\"cost\":0.0042}}\n\n" +
		"data: [DONE]\n\n"

	var usageResp *StreamResponse
	_, err := parseSSEStream(
		strings.NewReader(sse),
		config.ToolCallParsingLenient,
		nil,
		func(r StreamResponse) bool {
			if r.Usage != nil {
				usageResp = &r
			}
			return true
		},
	)
	if err != nil {
		t.Fatalf("parseSSEStream: %v", err)
	}
	if usageResp == nil {
		t.Fatal("expected a StreamResponse with Usage set, got none")
	}
	if usageResp.Usage.InputTokens != 20 {
		t.Errorf("InputTokens = %d, want 20", usageResp.Usage.InputTokens)
	}
	if usageResp.Usage.OutputTokens != 8 {
		t.Errorf("OutputTokens = %d, want 8", usageResp.Usage.OutputTokens)
	}
	if !usageResp.Usage.CostKnown {
		t.Error("CostKnown should be true when cost field is present")
	}
	if usageResp.Usage.CostUSD != 0.0042 {
		t.Errorf("CostUSD = %f, want 0.0042", usageResp.Usage.CostUSD)
	}
}

// TestOpenAIResponsesUsageParsed verifies that the Responses API "response.completed"
// event maps usage tokens correctly.
func TestOpenAIResponsesUsageParsed(t *testing.T) {
	t.Parallel()

	event := ResponsesSseEvent{
		Type: "response.completed",
		Response: &responsesSseResponse{
			ID: "resp-1",
			Usage: &responsesSseUsage{
				InputTokens:  30,
				OutputTokens: 12,
				TotalTokens:  42,
				InputTokenDetails: &responsesSseInputTokenDetails{
					CachedTokens: 8,
				},
				OutputTokenDetails: &responsesSseOutputTokenDetails{
					ReasoningTokens: 6,
				},
			},
		},
	}
	state := NewResponsesSseMapperState()
	chunks, err := MapResponsesSseEvent(event, state)
	if err != nil {
		t.Fatalf("MapResponsesSseEvent: %v", err)
	}

	var usageChunk *StreamResponse
	for i := range chunks {
		if chunks[i].Usage != nil {
			usageChunk = &chunks[i]
			break
		}
	}
	if usageChunk == nil {
		t.Fatal("expected a chunk with Usage from response.completed")
	}
	if usageChunk.Usage.InputTokens != 30 {
		t.Errorf("InputTokens = %d, want 30", usageChunk.Usage.InputTokens)
	}
	if usageChunk.Usage.OutputTokens != 12 {
		t.Errorf("OutputTokens = %d, want 12", usageChunk.Usage.OutputTokens)
	}
	if usageChunk.Usage.CachedTokens != 8 {
		t.Errorf("CachedTokens = %d, want 8", usageChunk.Usage.CachedTokens)
	}
	if !usageChunk.Usage.CachedReported {
		t.Error("CachedReported = false, want true when input_token_details is present")
	}
	if usageChunk.Usage.ReasoningTokens != 6 {
		t.Errorf("ReasoningTokens = %d, want 6", usageChunk.Usage.ReasoningTokens)
	}
	if usageChunk.Usage.CostKnown {
		t.Error("CostKnown should be false for OpenAI Responses (no cost field)")
	}
}
