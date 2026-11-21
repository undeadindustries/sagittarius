package provider

import "testing"

func TestToolCallPairerResolve(t *testing.T) {
	t.Parallel()

	type step struct {
		call   bool
		name   string
		id     string
		wantID string
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{
			name: "id-less result pairs with same-turn call",
			steps: []step{
				{call: true, name: "shell", id: "a"},
				{name: "shell", wantID: "a"},
			},
		},
		{
			name: "answered call from earlier turn is not reused",
			steps: []step{
				{call: true, name: "shell", id: "a"},
				{name: "shell", id: "a", wantID: "a"},
				{call: true, name: "shell", id: "b"},
				{name: "shell", wantID: "b"},
			},
		},
		{
			name: "unanswered call from earlier turn is not reused",
			steps: []step{
				{call: true, name: "shell", id: "a"},
				{call: true, name: "read", id: "r"},
				{name: "read", id: "r", wantID: "r"},
				{call: true, name: "shell", id: "b"},
				{name: "shell", wantID: "b"},
			},
		},
		{
			name: "parallel same-name calls pair in order around an id-bearing result",
			steps: []step{
				{call: true, name: "shell", id: "a"},
				{call: true, name: "shell", id: "b"},
				{call: true, name: "shell", id: "c"},
				{name: "shell", id: "b", wantID: "b"},
				{name: "shell", wantID: "a"},
				{name: "shell", wantID: "c"},
			},
		},
		{
			name: "no candidate falls back",
			steps: []step{
				{call: true, name: "shell", id: "a"},
				{name: "read", wantID: "fallback"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newToolCallPairer()
			for i, s := range tc.steps {
				if s.call {
					p.addCall(s.name, s.id)
					continue
				}
				if got := p.resolve(s.name, s.id, "fallback"); got != s.wantID {
					t.Fatalf("step %d: resolve(%q, %q) = %q, want %q", i, s.name, s.id, got, s.wantID)
				}
			}
		})
	}
}

// TestIDLessToolResultPairsWithPrecedingTurn reproduces a history where an
// earlier same-name call was answered with its ID and a later result lost its
// CallID (as context masking used to do). The later result must reference the
// call in the immediately preceding assistant turn; pairing it with the first
// call made Anthropic reject the request ("unexpected tool_use_id found in
// tool_result blocks").
func TestIDLessToolResultPairsWithPrecedingTurn(t *testing.T) {
	t.Parallel()

	history := []Message{
		{Role: RoleUser, Parts: []Part{{Text: "start"}}},
		{Role: RoleModel, Parts: []Part{{FunctionCall: &ToolCall{ID: "call_first", Name: "run_shell_command", Args: map[string]any{"command": "ls"}}}}},
		{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{CallID: "call_first", Name: "run_shell_command", Response: map[string]any{"output": "a"}}}}},
		{Role: RoleModel, Parts: []Part{{FunctionCall: &ToolCall{ID: "call_second", Name: "run_shell_command", Args: map[string]any{"command": "pwd"}}}}},
		{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{Name: "run_shell_command", Response: map[string]any{"output": "masked"}}}}},
	}
	const want = "call_second"

	t.Run("anthropic", func(t *testing.T) {
		t.Parallel()
		plan := BuildAnthropicRequestPlan(&GenerateRequest{Messages: history}, AnthropicPlanOptions{ToolsEnabled: true})
		msgs := plan.Body.Messages
		last := msgs[len(msgs)-1].Content[0]
		if last.Type != "tool_result" || last.ToolUseID != want {
			t.Fatalf("last block = %s %q, want tool_result %q", last.Type, last.ToolUseID, want)
		}
	})

	t.Run("openai-chat", func(t *testing.T) {
		t.Parallel()
		msgs := MessagesToOpenAIMessages(history, "gpt-4o")
		last := msgs[len(msgs)-1]
		if last.Role != OpenAIRoleTool || last.ToolCallID != want {
			t.Fatalf("last message = %s %q, want tool %q", last.Role, last.ToolCallID, want)
		}
	})

	t.Run("openai-responses", func(t *testing.T) {
		t.Parallel()
		plan := BuildResponsesRequestPlan(&GenerateRequest{Messages: history}, false)
		last := plan.Input[len(plan.Input)-1]
		if last.Type != "function_call_output" || last.CallID != safeResponsesCallID(want) {
			t.Fatalf("last item = %s %q, want function_call_output %q", last.Type, last.CallID, want)
		}
	})
}
