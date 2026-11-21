package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIResponsesUsesRequestTemperatureAndOmitsOnGPT5(t *testing.T) {
	t.Parallel()

	genTemp := 0.2
	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:     "http://127.0.0.1:1/v1/responses",
		Model:       "gpt-4o",
		Temperature: &genTemp,
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}

	reqTemp := 0.7
	body, err := EncodeResponsesRequestBody(gen, &GenerateRequest{
		Model:       "gpt-4o",
		Temperature: &reqTemp,
		Messages:    []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	}, "gpt-4o")
	if err != nil {
		t.Fatalf("EncodeResponsesRequestBody: %v", err)
	}
	if body["temperature"] != 0.7 {
		t.Fatalf("temperature = %v, want 0.7 from req.Temperature", body["temperature"])
	}

	// Switching to gpt-5 must not inherit gpt-4o's construction-time 0.2.
	bodyGPT5, err := EncodeResponsesRequestBody(gen, &GenerateRequest{
		Model:    "gpt-5",
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	}, "gpt-5")
	if err != nil {
		t.Fatalf("EncodeResponsesRequestBody gpt-5: %v", err)
	}
	if _, hasTemp := bodyGPT5["temperature"]; hasTemp {
		t.Fatalf("gpt-5 leaked construction-time temperature: %v", bodyGPT5["temperature"])
	}
}

func TestAnthropicGeneratorPerRequestModelState(t *testing.T) {
	t.Parallel()

	gen, err := NewAnthropicGenerator(AnthropicConfig{
		BaseURL:          "https://api.anthropic.com/v1/messages",
		Model:            "claude-opus-5-5",
		DefaultMaxTokens: 128_000,
		ThinkingTypes:    []string{"adaptive"},
		SupportedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
	})
	if err != nil {
		t.Fatalf("NewAnthropicGenerator: %v", err)
	}

	// Switching req.Model to claude-sonnet-4-5 without passing Opus's caps must
	// not leak Opus's "adaptive" thinking type onto Sonnet 4.5.
	body, err := EncodeAnthropicRequestBody(gen, &GenerateRequest{
		Model:     "claude-sonnet-4-5",
		Reasoning: &ReasoningRequest{Enabled: true, Effort: "high"},
		Messages:  []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	}, "claude-sonnet-4-5")
	if err != nil {
		t.Fatalf("EncodeAnthropicRequestBody: %v", err)
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Fatalf("claude-sonnet-4-5 thinking.type = %v, want enabled (leaked adaptive?)", thinking["type"])
	}
	if int(body["max_tokens"].(float64)) != anthropicDefaultMaxTokens {
		t.Fatalf("max_tokens = %v, want %d (leaked opus 128k?)", body["max_tokens"], anthropicDefaultMaxTokens)
	}

	// Per-request caps override defaults cleanly.
	bodyCustom, err := EncodeAnthropicRequestBody(gen, &GenerateRequest{
		Model:                "claude-custom",
		ModelMaxOutputTokens: 64_000,
		ModelThinkingTypes:   []string{"adaptive", "disabled"},
		Reasoning:            &ReasoningRequest{Enabled: true, Effort: "none"},
		Messages:             []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	}, "claude-custom")
	if err != nil {
		t.Fatalf("EncodeAnthropicRequestBody custom: %v", err)
	}
	if int(bodyCustom["max_tokens"].(float64)) != 64_000 {
		t.Fatalf("custom max_tokens = %v, want 64000", bodyCustom["max_tokens"])
	}
	if bodyCustom["thinking"].(map[string]any)["type"] != "disabled" {
		t.Fatalf("custom thinking = %#v, want disabled", bodyCustom["thinking"])
	}
}

func TestOpenAIResponsesResetChainClearsPreviousResponseID(t *testing.T) {
	t.Parallel()

	var seenPrevIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		prev, _ := body["previous_response_id"].(string)
		seenPrevIDs = append(seenPrevIDs, prev)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))
	}))
	t.Cleanup(srv.Close)

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:             srv.URL + "/v1/responses",
		Model:               "gpt-5",
		UseResponseChaining: true,
		HTTPClient:          srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}
	run := func() {
		ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
			Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		})
		if err != nil {
			t.Fatalf("GenerateContentStream: %v", err)
		}
		for range ch {
		}
	}
	run()
	gen.ResetResponseChain()
	run()

	if len(seenPrevIDs) != 2 || seenPrevIDs[0] != "" || seenPrevIDs[1] != "" {
		t.Fatalf("seen previous_response_id = %v, want [\"\" \"\"] after reset", seenPrevIDs)
	}
}
