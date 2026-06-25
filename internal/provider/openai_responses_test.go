package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func responsesSSEResponse(chunks ...string) string {
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString("data: ")
		b.WriteString(chunk)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func TestResponsesTextDelta(t *testing.T) {
	t.Parallel()

	sseBody := responsesSSEResponse(
		`{"type":"response.output_text.delta","delta":"Hello"}`,
		`{"type":"response.output_text.delta","delta":" world"}`,
		`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want /v1/responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseBody))
	}))
	t.Cleanup(srv.Close)

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:    srv.URL + "/v1/responses",
		Model:      "gpt-5-codex",
		Bearer:     "test-key",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}

	ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}

	var text strings.Builder
	var done bool
	for resp := range ch {
		if resp.Error != nil {
			t.Fatalf("stream error: %v", resp.Error)
		}
		text.WriteString(resp.TextDelta)
		if resp.Done {
			done = true
		}
	}
	if got, want := text.String(), "Hello world"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if !done {
		t.Error("expected Done chunk")
	}
}

func TestResponsesFunctionCall(t *testing.T) {
	t.Parallel()

	sseBody := responsesSSEResponse(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c1","name":"shell"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"cm"}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"d\":\"ls\"}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"cmd\":\"ls\"}"}}`,
		`{"type":"response.completed","response":{"id":"resp_2"}}`,
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseBody))
	}))
	t.Cleanup(srv.Close)

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:    srv.URL,
		Model:      "gpt-5-codex",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}

	ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "run ls"}}}},
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}

	var calls []ToolCall
	for resp := range ch {
		if resp.Error != nil {
			t.Fatalf("stream error: %v", resp.Error)
		}
		calls = append(calls, resp.ToolCalls...)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if calls[0].Name != "shell" {
		t.Errorf("name = %q, want shell", calls[0].Name)
	}
	if got := calls[0].Args["cmd"]; got != "ls" {
		t.Errorf("args[cmd] = %v, want ls", got)
	}
}

func TestReasoningEffortInRequest(t *testing.T) {
	t.Parallel()

	ClearSessionReasoningOverride()
	t.Cleanup(ClearSessionReasoningOverride)

	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(responsesSSEResponse(`{"type":"response.completed","response":{"id":"r"}}`)))
	}))
	t.Cleanup(srv.Close)

	gen, err := NewOpenAIResponsesGenerator(OpenAIResponsesConfig{
		BaseURL:         srv.URL,
		Model:           "gpt-5-codex",
		ReasoningEffort: "low",
		HTTPClient:      srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesGenerator: %v", err)
	}

	ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("GenerateContentStream: %v", err)
	}
	for range ch {
	}

	reasoning, ok := captured["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning missing from body: %#v", captured)
	}
	if got := reasoning["effort"]; got != "low" {
		t.Errorf("effort = %v, want low", got)
	}

	SetSessionReasoningOverride("high")
	ch2, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "again"}}}},
	})
	if err != nil {
		t.Fatalf("second stream: %v", err)
	}
	for range ch2 {
	}
	reasoning, ok = captured["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning missing on second request: %#v", captured)
	}
	if got := reasoning["effort"]; got != "high" {
		t.Errorf("session effort = %v, want high", got)
	}
}

func TestNoLocalMaskingOnResponsesPath(t *testing.T) {
	t.Parallel()

	responsesSettings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAIResponses),
		},
	}
	if IsOpenAIChatMode(responsesSettings) {
		t.Error("IsOpenAIChatMode should be false for openai-responses")
	}
	if !IsOpenAIResponsesMode(responsesSettings) {
		t.Error("IsOpenAIResponsesMode should be true for openai-responses")
	}

	chatSettings := &config.Settings{
		Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI)},
	}
	if !IsOpenAIChatMode(chatSettings) {
		t.Error("IsOpenAIChatMode should be true for openai")
	}
	if IsOpenAIResponsesMode(chatSettings) {
		t.Error("IsOpenAIResponsesMode should be false for openai chat")
	}
}

func TestFactorySelectsOpenAIResponses(t *testing.T) {
	// Not parallel: t.Setenv mutates the process environment.
	ctx := testContext(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAIResponses),
		},
	}

	gen, err := NewContentGenerator(ctx, settings)
	if err != nil {
		t.Fatalf("NewContentGenerator: %v", err)
	}
	if _, ok := gen.(*OpenAIResponsesGenerator); !ok {
		t.Fatalf("generator type = %T, want *OpenAIResponsesGenerator", gen)
	}
}

func TestMapResponsesSseEventTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		events        []ResponsesSseEvent
		wantText      string
		wantReasoning string
		wantCalls     int
		wantDone      bool
		wantErr       bool
	}{
		{
			name: "text delta",
			events: []ResponsesSseEvent{
				{Type: "response.output_text.delta", Delta: "hi"},
			},
			wantText: "hi",
		},
		{
			name: "reasoning delta",
			events: []ResponsesSseEvent{
				{Type: "response.reasoning_summary_text.delta", Delta: "thinking"},
			},
			wantReasoning: "thinking",
		},
		{
			name: "completed marks done",
			events: []ResponsesSseEvent{
				{Type: "response.completed", Response: &responsesSseResponse{ID: "resp_123"}},
			},
			wantDone: true,
		},
		{
			name: "failed throws",
			events: []ResponsesSseEvent{
				{Type: "response.failed", Response: &responsesSseResponse{Error: &responsesSseErrorField{Message: "boom"}}},
			},
			wantErr: true,
		},
		{
			name: "unknown ignored",
			events: []ResponsesSseEvent{
				{Type: "response.future_event_42"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := NewResponsesSseMapperState()
			var text strings.Builder
			var reasoning strings.Builder
			var calls int
			var done bool
			for _, ev := range tt.events {
				chunks, err := MapResponsesSseEvent(ev, state)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error")
					}
					return
				}
				if err != nil {
					t.Fatalf("MapResponsesSseEvent: %v", err)
				}
				for _, chunk := range chunks {
					text.WriteString(chunk.TextDelta)
					reasoning.WriteString(chunk.ReasoningDelta)
					calls += len(chunk.ToolCalls)
					if chunk.Done {
						done = true
					}
				}
			}
			if got := text.String(); got != tt.wantText {
				t.Errorf("text = %q, want %q", got, tt.wantText)
			}
			if got := reasoning.String(); got != tt.wantReasoning {
				t.Errorf("reasoning = %q, want %q", got, tt.wantReasoning)
			}
			if calls != tt.wantCalls {
				t.Errorf("tool calls = %d, want %d", calls, tt.wantCalls)
			}
			if done != tt.wantDone {
				t.Errorf("done = %v, want %v", done, tt.wantDone)
			}
		})
	}
}

func TestTrimInputForChainingCases(t *testing.T) {
	t.Parallel()

	input := []responsesInputItem{
		{Type: "message", Role: "user", Content: []responsesInputContent{{Type: "input_text", Text: "old"}}},
		{Type: "message", Role: "assistant", Content: []responsesInputContent{{Type: "output_text", Text: "older"}}},
		{Type: "function_call", CallID: "a", Name: "noop", Arguments: "{}"},
		{Type: "function_call_output", CallID: "a", Output: "{}"},
		{Type: "message", Role: "user", Content: []responsesInputContent{{Type: "input_text", Text: "new turn"}}},
	}
	out := TrimInputForChaining(input)
	if len(out) != 1 || out[0].Role != "user" {
		t.Fatalf("trim with user = %#v", out)
	}

	toolOnly := []responsesInputItem{
		{Type: "message", Role: "assistant", Content: []responsesInputContent{{Type: "output_text", Text: "prev"}}},
		{Type: "function_call_output", CallID: "a", Output: "result"},
	}
	out = TrimInputForChaining(toolOnly)
	if len(out) != 1 || out[0].Type != "function_call_output" {
		t.Fatalf("tool-only trim = %#v", out)
	}
}

func TestResponsesURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"https://api.openai.com/v1/responses", "https://api.openai.com/v1/responses"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/responses"},
		{"http://127.0.0.1:8000", "http://127.0.0.1:8000/v1/responses"},
	}
	for _, tt := range tests {
		if got := ResponsesURL(tt.in); got != tt.want {
			t.Errorf("ResponsesURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildResponsesRequestPlan(t *testing.T) {
	t.Parallel()

	plan := BuildResponsesRequestPlan(&GenerateRequest{
		SystemInstruction: "be concise",
		Messages: []Message{
			{Role: RoleUser, Parts: []Part{{Text: "hi"}}},
			{Role: RoleModel, Parts: []Part{{Text: "hello"}}},
		},
		Tools: []ToolDeclaration{{Name: "noop", Parameters: emptyObjectSchema}},
	}, true)
	if plan.Instructions != "be concise" {
		t.Errorf("instructions = %q", plan.Instructions)
	}
	if len(plan.Input) != 2 {
		t.Fatalf("input len = %d, want 2", len(plan.Input))
	}
	if len(plan.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(plan.Tools))
	}
}
