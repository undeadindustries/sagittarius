package config

import (
	"reflect"
	"testing"
)

func TestModelReasoningHistoryRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		providerID string
		baseURL    string
		model      string
		wantKeep   bool
		wantKwargs map[string]any
	}{
		{
			name:       "qwen3.8 keeps reasoning with no extra kwargs",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "Qwen/Qwen3.8-32B-Instruct",
			wantKeep:   true,
			wantKwargs: nil,
		},
		{
			name:       "qwen3.6 keeps reasoning and sets preserve_thinking=true",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "Qwen/Qwen3.6-35B-A3B",
			wantKeep:   true,
			wantKwargs: map[string]any{"preserve_thinking": true},
		},
		{
			name:       "qwen3.5 keeps reasoning",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "qwen3.5-27b",
			wantKeep:   true,
			wantKwargs: nil,
		},
		{
			name:       "glm-4.7 keeps reasoning and sets clear_thinking=false",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "zai-org/GLM-4.7-Flash",
			wantKeep:   true,
			wantKwargs: map[string]any{"clear_thinking": false},
		},
		{
			name:       "glm-4.5 keeps reasoning without extra kwargs",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "THUDM/GLM-4.5",
			wantKeep:   true,
			wantKwargs: nil,
		},
		{
			name:       "kimi-k2 keeps reasoning",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "moonshotai/Kimi-K2-Thinking",
			wantKeep:   true,
			wantKwargs: nil,
		},
		{
			name:       "minimax-m2.5 keeps reasoning",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "MiniMaxAI/MiniMax-M2.5",
			wantKeep:   true,
			wantKwargs: nil,
		},
		{
			name:       "gemma-4 keeps reasoning",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "google/gemma-4-31b-it",
			wantKeep:   true,
			wantKwargs: nil,
		},
		{
			name:       "deepseek direct API keeps reasoning",
			providerID: "deepseek",
			baseURL:    "https://api.deepseek.com/v1",
			model:      "deepseek-reasoner",
			wantKeep:   true,
			wantKwargs: nil,
		},
		{
			name:       "openrouter never keeps reasoning by default even for qwen3.8",
			providerID: "openrouter",
			baseURL:    "https://openrouter.ai/api/v1",
			model:      "qwen/qwen3.8-32b",
			wantKeep:   false,
			wantKwargs: nil,
		},
		{
			name:       "custom provider pointing at openrouter.ai never keeps by default",
			providerID: "my-or",
			baseURL:    "https://openrouter.ai/api/v1",
			model:      "deepseek/deepseek-r1",
			wantKeep:   false,
			wantKwargs: nil,
		},
		{
			name:       "gpt-oss strips prior thinking",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "openai/gpt-oss-120b",
			wantKeep:   false,
			wantKwargs: nil,
		},
		{
			name:       "unknown model defaults to none",
			providerID: "local-vllm",
			baseURL:    "http://localhost:8000/v1",
			model:      "meta-llama/Llama-3.3-70B-Instruct",
			wantKeep:   false,
			wantKwargs: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotKeep, gotKwargs := ModelReasoningHistoryRule(tc.providerID, tc.baseURL, tc.model)
			if gotKeep != tc.wantKeep {
				t.Errorf("wireKeep = %v, want %v", gotKeep, tc.wantKeep)
			}
			if !reflect.DeepEqual(gotKwargs, tc.wantKwargs) {
				t.Errorf("templateKwargs = %#v, want %#v", gotKwargs, tc.wantKwargs)
			}
		})
	}
}

func TestResolveReasoningHistoryPrecedenceAndKwargsMerge(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "providers": {
    "openai": {
      "baseUrl": "http://localhost:8000/v1",
      "reasoningHistory": "none",
      "models": {
        "Qwen/Qwen3.6-35B-A3B": {
          "reasoningHistory": "keep-all",
          "chatTemplateKwargs": {
            "preserve_thinking": false,
            "custom_flag": "yes"
          }
        },
        "zai-org/GLM-4.7": {
          "reasoningHistory": "auto"
        }
      }
    }
  }
}`)
	doc, err := decodeSettingsDocument(raw)
	if err != nil {
		t.Fatalf("decodeSettingsDocument: %v", err)
	}

	// 1. Model with explicit keep-all + custom kwargs overriding preserve_thinking.
	wire, prune, kwargs := ResolveReasoningHistory(doc, "openai", "Qwen/Qwen3.6-35B-A3B")
	if !wire || prune {
		t.Errorf("Qwen3.6 keep-all: wire=%v prune=%v, want true/false", wire, prune)
	}
	wantKwargs := map[string]any{"preserve_thinking": false, "custom_flag": "yes"}
	if !reflect.DeepEqual(kwargs, wantKwargs) {
		t.Errorf("Qwen3.6 kwargs = %#v, want %#v", kwargs, wantKwargs)
	}

	// 2. Model with explicit auto on GLM-4.7 beats provider-level "none".
	wire, prune, kwargs = ResolveReasoningHistory(doc, "openai", "zai-org/GLM-4.7")
	if !wire || !prune {
		t.Errorf("GLM-4.7 auto: wire=%v prune=%v, want true/true", wire, prune)
	}
	wantGLM := map[string]any{"clear_thinking": false}
	if !reflect.DeepEqual(kwargs, wantGLM) {
		t.Errorf("GLM-4.7 kwargs = %#v, want %#v", kwargs, wantGLM)
	}

	// 3. Unlisted model inherits provider-level "none" even if family would match auto.
	wire, prune, kwargs = ResolveReasoningHistory(doc, "openai", "Qwen/Qwen3.8-32B")
	if wire || prune || len(kwargs) != 0 {
		t.Errorf("Qwen3.8 inheriting provider none: wire=%v prune=%v kwargs=%#v, want false/false/empty", wire, prune, kwargs)
	}
}
