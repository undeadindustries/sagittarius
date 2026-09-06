package provider

import "testing"

func TestMarshalToolCallArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "nil map", args: nil, want: "{}"},
		{name: "empty map", args: map[string]any{}, want: "{}"},
		{name: "populated", args: map[string]any{"path": "a.txt"}, want: `{"path":"a.txt"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := marshalToolCallArgs(tc.args); got != tc.want {
				t.Fatalf("marshalToolCallArgs = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNoArgToolCallSerializesAsObject pins the wire shape of a Gemini-native
// no-argument tool call replayed to an OpenAI-compatible endpoint. json.Marshal
// of a nil map is "null", which LiteLLM rejects with "Assistant tool call
// function.arguments must be a JSON object", poisoning every later request that
// replays the turn — including the summarizer's, so compression latches and the
// session cannot recover.
func TestNoArgToolCallSerializesAsObject(t *testing.T) {
	t.Parallel()

	history := []Message{
		{Role: RoleUser, Parts: []Part{{Text: "list the wings"}}},
		{Role: RoleModel, Parts: []Part{{FunctionCall: &ToolCall{
			ID:   "call_1",
			Name: "mcp_mempalace_mempalace_list_wings",
			Args: nil,
		}}}},
		{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
			CallID:   "call_1",
			Name:     "mcp_mempalace_mempalace_list_wings",
			Response: map[string]any{"wings": []string{"sagittarius"}},
		}}}},
	}

	t.Run("openai-chat", func(t *testing.T) {
		t.Parallel()
		msgs := MessagesToOpenAIMessages(history, "qwen3.8-27b-nothink")
		got := ""
		for _, m := range msgs {
			for _, tc := range m.ToolCalls {
				got = tc.Function.Arguments
			}
		}
		if got != "{}" {
			t.Fatalf("arguments = %q, want %q", got, "{}")
		}
	})

	t.Run("openai-responses", func(t *testing.T) {
		t.Parallel()
		plan := BuildResponsesRequestPlan(&GenerateRequest{Messages: history}, false)
		got := ""
		for _, item := range plan.Input {
			if item.Type == "function_call" {
				got = item.Arguments
			}
		}
		if got != "{}" {
			t.Fatalf("arguments = %q, want %q", got, "{}")
		}
	})
}

func TestToolCallArgsPreservedOnBothWirePaths(t *testing.T) {
	t.Parallel()

	const want = `{"file_path":"main.go"}`
	history := []Message{
		{Role: RoleModel, Parts: []Part{{FunctionCall: &ToolCall{
			ID:   "call_1",
			Name: "read_file",
			Args: map[string]any{"file_path": "main.go"},
		}}}},
		{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
			CallID: "call_1", Name: "read_file", Response: map[string]any{"output": "package main"},
		}}}},
	}

	msgs := MessagesToOpenAIMessages(history, "gpt-4o")
	found := false
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			found = true
			if tc.Function.Arguments != want {
				t.Fatalf("openai-chat arguments = %q, want %q", tc.Function.Arguments, want)
			}
		}
	}
	if !found {
		t.Fatal("openai-chat: no tool call in mapped messages")
	}

	plan := BuildResponsesRequestPlan(&GenerateRequest{Messages: history}, false)
	found = false
	for _, item := range plan.Input {
		if item.Type != "function_call" {
			continue
		}
		found = true
		if item.Arguments != want {
			t.Fatalf("openai-responses arguments = %q, want %q", item.Arguments, want)
		}
	}
	if !found {
		t.Fatal("openai-responses: no function_call in plan input")
	}
}
