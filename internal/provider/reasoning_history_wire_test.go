package provider

import (
	"reflect"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestBuildOpenAIChatRequestReasoningHistoryAndTemplateKwargs(t *testing.T) {
	t.Parallel()

	history := []Message{
		{Role: RoleUser, Parts: []Part{{Text: "find the bug"}}},
		{Role: RoleModel, Parts: []Part{
			{Text: "Let me read the file.", Reasoning: "step 1: inspect main.go"},
			{FunctionCall: &ToolCall{ID: "call_1", Name: "read_file", Args: map[string]any{"path": "main.go"}}},
		}},
		{Role: RoleUser, Parts: []Part{
			{FunctionResponse: &FunctionResponse{Name: "read_file", CallID: "call_1", Response: map[string]any{"output": "package main"}}},
		}},
		{Role: RoleModel, Parts: []Part{
			{Text: "No reasoning on this turn."},
		}},
	}

	t.Run("keep=false omits reasoning_content on every assistant message", func(t *testing.T) {
		t.Parallel()
		req := &GenerateRequest{
			Model:                "Qwen/Qwen3.8-32B",
			Messages:             history,
			KeepReasoningHistory: false,
		}
		body := BuildOpenAIChatRequest(req, req.Model, config.ToolCallParsingLenient, nil)
		for i, m := range body.Messages {
			if m.ReasoningContent != nil {
				t.Errorf("msg[%d] ReasoningContent = %q, want nil", i, *m.ReasoningContent)
			}
		}
	})

	t.Run("keep=true populates reasoning_content only on turns with Part.Reasoning", func(t *testing.T) {
		t.Parallel()
		req := &GenerateRequest{
			Model:                "Qwen/Qwen3.8-32B",
			Messages:             history,
			KeepReasoningHistory: true,
		}
		body := BuildOpenAIChatRequest(req, req.Model, config.ToolCallParsingLenient, nil)
		if len(body.Messages) != 4 {
			t.Fatalf("len(body.Messages) = %d, want 4", len(body.Messages))
		}
		if body.Messages[1].ReasoningContent == nil || *body.Messages[1].ReasoningContent != "step 1: inspect main.go" {
			t.Errorf("msg[1] ReasoningContent = %v, want 'step 1: inspect main.go'", body.Messages[1].ReasoningContent)
		}
		if body.Messages[3].ReasoningContent != nil {
			t.Errorf("msg[3] ReasoningContent = %q, want nil on turn without reasoning", *body.Messages[3].ReasoningContent)
		}
	})

	t.Run("auto table end-to-end with BuildOpenAIChatRequest", func(t *testing.T) {
		t.Parallel()
		s := config.Settings{
			Providers: &config.ProvidersSettings{
				OpenAI: &config.ProviderInstanceConfig{
					BaseURL: "http://localhost:8000/v1",
					Models: map[string]config.ProviderModelConfig{
						"Qwen/Qwen3.6-custom": {
							ChatTemplateKwargs: map[string]any{
								"preserve_thinking": false,
							},
						},
					},
				},
				Custom: map[string]config.CustomProviderDefinition{
					"openrouter": {
						DisplayName: "OpenRouter",
						BaseURL:     "https://openrouter.ai/api/v1",
					},
				},
			},
		}

		cases := []struct {
			name           string
			providerID     string
			model          string
			wantReasoning  bool
			wantChatKwargs map[string]any
		}{
			{
				name:           "auto + Qwen/Qwen3.8-32B",
				providerID:     "openai",
				model:          "Qwen/Qwen3.8-32B",
				wantReasoning:  true,
				wantChatKwargs: nil,
			},
			{
				name:           "auto + Qwen/Qwen3.6-35B-A3B",
				providerID:     "openai",
				model:          "Qwen/Qwen3.6-35B-A3B",
				wantReasoning:  true,
				wantChatKwargs: map[string]any{"preserve_thinking": true},
			},
			{
				name:           "auto + zai-org/GLM-4.7",
				providerID:     "openai",
				model:          "zai-org/GLM-4.7",
				wantReasoning:  true,
				wantChatKwargs: map[string]any{"clear_thinking": false},
			},
			{
				name:           "auto + openrouter provider omits reasoning even for qwen3.8",
				providerID:     "openrouter",
				model:          "qwen/qwen3.8-32b",
				wantReasoning:  false,
				wantChatKwargs: nil,
			},
			{
				name:           "user chatTemplateKwargs overrides auto value for Qwen3.6",
				providerID:     "openai",
				model:          "Qwen/Qwen3.6-custom",
				wantReasoning:  true,
				wantChatKwargs: map[string]any{"preserve_thinking": false},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				keep, _, kwargs := config.ResolveReasoningHistory(&s, tc.providerID, tc.model)
				req := &GenerateRequest{
					Model:                tc.model,
					Messages:             history,
					KeepReasoningHistory: keep,
					ChatTemplateKwargs:   kwargs,
				}
				body := BuildOpenAIChatRequest(req, req.Model, config.ToolCallParsingLenient, nil)
				gotReasoning := body.Messages[1].ReasoningContent != nil &&
					*body.Messages[1].ReasoningContent == "step 1: inspect main.go"
				if gotReasoning != tc.wantReasoning {
					t.Errorf("gotReasoning = %v, want %v", gotReasoning, tc.wantReasoning)
				}
				if !reflect.DeepEqual(body.ChatTemplateKwargs, tc.wantChatKwargs) {
					t.Errorf("ChatTemplateKwargs = %#v, want %#v", body.ChatTemplateKwargs, tc.wantChatKwargs)
				}
			})
		}
	})
}
