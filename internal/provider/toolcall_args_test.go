package provider

import (
	"encoding/json"
	"testing"
)

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

// TestAnthropicNoArgToolUseKeepsInput pins that every replayed tool_use block
// carries "input". Anthropic rejects a block without it ("tool_use.input: Field
// required"), and because the summarizer replays the same history, one
// no-argument call poisons compression and every later turn.
func TestAnthropicNoArgToolUseKeepsInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil args", args: nil},
		{name: "empty args", args: map[string]any{}},
		{name: "only internal call id", args: map[string]any{InternalCallIDArg: "toolu_1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			history := []Message{
				{Role: RoleUser, Parts: []Part{{Text: "list background processes"}}},
				{Role: RoleModel, Parts: []Part{{FunctionCall: &ToolCall{
					ID: "toolu_1", Name: "list_background_processes", Args: tc.args,
				}}}},
				{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
					CallID: "toolu_1", Name: "list_background_processes", Response: map[string]any{"output": "none"},
				}}}},
			}
			plan := BuildAnthropicRequestPlan(&GenerateRequest{Messages: history}, AnthropicPlanOptions{ToolsEnabled: true})
			raw, err := json.Marshal(plan.Body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			var body struct {
				Messages []struct {
					Content []map[string]json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("unmarshal body: %v", err)
			}
			found := false
			for _, msg := range body.Messages {
				for _, blk := range msg.Content {
					if string(blk["type"]) != `"tool_use"` {
						continue
					}
					found = true
					input, ok := blk["input"]
					if !ok {
						t.Fatalf("tool_use block missing input: %s", raw)
					}
					if string(input) != "{}" {
						t.Fatalf("tool_use input = %s, want {}", input)
					}
				}
			}
			if !found {
				t.Fatalf("no tool_use block in request: %s", raw)
			}
		})
	}
}

func TestInternalCallIDStrippedOnBothWirePaths(t *testing.T) {
	t.Parallel()

	const want = `{"file_path":"main.go"}`
	original := map[string]any{"file_path": "main.go", InternalCallIDArg: "call_1"}
	history := []Message{
		{Role: RoleModel, Parts: []Part{{FunctionCall: &ToolCall{
			ID:   "call_1",
			Name: "read_file",
			Args: original,
		}}}},
		{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
			CallID: "call_1", Name: "read_file", Response: map[string]any{"output": "package main"},
		}}}},
	}

	msgs := MessagesToOpenAIMessages(history, "gpt-4o")
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.Function.Arguments != want {
				t.Fatalf("openai-chat arguments = %q, want %q", tc.Function.Arguments, want)
			}
		}
	}
	plan := BuildResponsesRequestPlan(&GenerateRequest{Messages: history}, false)
	found := false
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
	planAnth := BuildAnthropicRequestPlan(&GenerateRequest{Messages: history}, AnthropicPlanOptions{ToolsEnabled: true})
	if got := string(planAnth.Body.Messages[0].Content[0].Input); got != want {
		t.Fatalf("anthropic tool_use input = %s, want %s", got, want)
	}
	if original[InternalCallIDArg] != "call_1" {
		t.Fatalf("history args mutated: %#v", original)
	}
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
