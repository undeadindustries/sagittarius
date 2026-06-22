package provider

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// TestOpenAIChatUsageParsed verifies that a standard OpenAI-chat stream with a
// trailing usage object emits a StreamResponse with non-zero token counts.
func TestOpenAIChatUsageParsed(t *testing.T) {
	t.Parallel()

	// Simulate a typical OpenAI stream: a text delta, a done chunk, then a
	// usage-carrying final frame with no choices.
	sse := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]," +
		"\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n" +
		"data: [DONE]\n\n"

	var usageResp *StreamResponse
	_, err := parseSSEStream(
		strings.NewReader(sse),
		config.ToolCallParsingLenient,
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
	if usageResp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", usageResp.Usage.InputTokens)
	}
	if usageResp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", usageResp.Usage.OutputTokens)
	}
	if usageResp.Usage.CostKnown {
		t.Error("CostKnown should be false for plain OpenAI (no cost field)")
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
	if usageChunk.Usage.CostKnown {
		t.Error("CostKnown should be false for OpenAI Responses (no cost field)")
	}
}
