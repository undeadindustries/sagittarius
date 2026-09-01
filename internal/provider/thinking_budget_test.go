package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func chatBody(t *testing.T, req *GenerateRequest, model string) openAIChatRequest {
	t.Helper()
	return BuildOpenAIChatRequest(req, model, config.ToolCallParsingLenient, nil)
}

func TestBuildOpenAIChatRequestThinkingBudget(t *testing.T) {
	t.Parallel()

	base := func() *GenerateRequest {
		return &GenerateRequest{Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}}}
	}

	t.Run("no budget omits the fields", func(t *testing.T) {
		t.Parallel()
		body := chatBody(t, base(), "qwen3")
		if body.ReasoningBudgetTokens != nil {
			t.Errorf("ReasoningBudgetTokens = %v, want nil", body.ReasoningBudgetTokens)
		}
		if body.ReasoningBudgetMessage != "" {
			t.Errorf("ReasoningBudgetMessage = %q, want empty", body.ReasoningBudgetMessage)
		}
		if body.ChatTemplateKwargs != nil {
			t.Errorf("ChatTemplateKwargs = %+v, want nil", body.ChatTemplateKwargs)
		}
	})

	t.Run("budget is advertised with a wrap-up message", func(t *testing.T) {
		t.Parallel()
		req := base()
		req.ThinkingBudgetTokens = 2048
		body := chatBody(t, req, "qwen3")
		if body.ReasoningBudgetTokens == nil || *body.ReasoningBudgetTokens != 2048 {
			t.Fatalf("ReasoningBudgetTokens = %v, want 2048", body.ReasoningBudgetTokens)
		}
		// The message is what keeps a native cut from degrading quality; an
		// empty one would be worse than no budget at all.
		if body.ReasoningBudgetMessage != ThinkingBudgetMessage {
			t.Errorf("ReasoningBudgetMessage = %q, want the default wrap-up text", body.ReasoningBudgetMessage)
		}
	})

	t.Run("suppression sends every key and drops the budget", func(t *testing.T) {
		t.Parallel()
		req := base()
		req.ThinkingBudgetTokens = 2048
		req.SuppressThinking = true
		body := chatBody(t, req, "qwen3")
		if body.ReasoningBudgetTokens == nil || *body.ReasoningBudgetTokens != 0 {
			t.Errorf("ReasoningBudgetTokens = %v, want 0", body.ReasoningBudgetTokens)
		}
		if body.Reasoning == nil || body.Reasoning.Enabled {
			t.Errorf("Reasoning = %+v, want enabled=false", body.Reasoning)
		}
		if body.ChatTemplateKwargs == nil || body.ChatTemplateKwargs.EnableThinking == nil ||
			*body.ChatTemplateKwargs.EnableThinking {
			t.Errorf("ChatTemplateKwargs = %+v, want enable_thinking=false", body.ChatTemplateKwargs)
		}
	})

	t.Run("suppression overrides an enabled reasoning ask", func(t *testing.T) {
		t.Parallel()
		req := base()
		req.Reasoning = &ReasoningRequest{Enabled: true, Effort: "high"}
		req.SuppressThinking = true
		body := chatBody(t, req, "qwen3")
		if body.Reasoning == nil || body.Reasoning.Enabled || body.Reasoning.Effort != "" {
			t.Errorf("Reasoning = %+v, want a bare enabled=false", body.Reasoning)
		}
	})
}

// TestSuppressThinkingWireJSON pins the serialized shape: enabled=false and
// enable_thinking=false must survive marshalling, which omitempty on a plain
// bool would silently drop.
func TestSuppressThinkingWireJSON(t *testing.T) {
	t.Parallel()

	body := chatBody(t, &GenerateRequest{
		Messages:         []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		SuppressThinking: true,
	}, "qwen3")

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		`"reasoning_budget_tokens":0`,
		`"enabled":false`,
		`"enable_thinking":false`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("wire body missing %s:\n%s", want, raw)
		}
	}
}

// TestEnabledReasoningStillSerializesTrue guards the omitempty removal on
// openAIReasoning.Enabled against changing existing behavior.
func TestEnabledReasoningStillSerializesTrue(t *testing.T) {
	t.Parallel()

	body := chatBody(t, &GenerateRequest{
		Messages:  []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
		Reasoning: &ReasoningRequest{Enabled: true, Effort: "low"},
	}, "openrouter/model")

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"reasoning":{"effort":"low","enabled":true}`) {
		t.Errorf("unexpected reasoning object:\n%s", raw)
	}
}

func TestSetAndClearModelThinkingBudget(t *testing.T) {
	t.Parallel()
	s := openAISettings()

	if err := SetModelConfig(s, "openai", "gpt-4o", "thinkingBudgetTokens", "3000"); err != nil {
		t.Fatalf("set thinkingBudgetTokens: %v", err)
	}
	if err := SetModelConfig(s, "openai", "gpt-4o", "hardThinkingBudget", "true"); err != nil {
		t.Fatalf("set hardThinkingBudget: %v", err)
	}
	got := ModelConfigValues(s, "openai", "gpt-4o")
	if got["thinkingBudgetTokens"] != "3000" || got["hardThinkingBudget"] != "true" {
		t.Fatalf("ModelConfigValues = %v, want the budget and hard flag", got)
	}

	tokens, hard := config.ResolveThinkingBudget(s, "openai", "gpt-4o")
	if tokens != 3000 || !hard {
		t.Fatalf("ResolveThinkingBudget = (%d, %v), want (3000, true)", tokens, hard)
	}

	// A negative budget is a user error, not a silent no-op: it would otherwise
	// read as "unset" while looking configured in the editor.
	if err := SetModelConfig(s, "openai", "gpt-4o", "thinkingBudgetTokens", "-1"); err == nil {
		t.Error("a negative budget should be rejected")
	}
	if err := SetModelConfig(s, "openai", "gpt-4o", "thinkingBudgetTokens", "abc"); err == nil {
		t.Error("a non-numeric budget should be rejected")
	}

	for _, key := range []string{"thinkingBudgetTokens", "hardThinkingBudget"} {
		if err := ClearModelConfig(s, "openai", "gpt-4o", key); err != nil {
			t.Fatalf("clear %s: %v", key, err)
		}
	}
	if left := ModelConfigValues(s, "openai", "gpt-4o"); len(left) != 0 {
		t.Fatalf("ModelConfigValues = %v, want empty after clearing both", left)
	}
	// Clearing the last pinned field must drop the entry rather than persist
	// an empty object into settings.json.
	if _, ok := s.Providers.OpenAI.Models["gpt-4o"]; ok {
		t.Error("an emptied model entry should be deleted")
	}
}

func TestBuildGenerateContentConfigThinkingBudget(t *testing.T) {
	t.Parallel()

	t.Run("numeric budget maps onto Gemini 2.5 ThinkingBudget", func(t *testing.T) {
		t.Parallel()
		cfg := BuildGenerateContentConfig(&GenerateRequest{
			Model:                "gemini-2.5-pro",
			Reasoning:            &ReasoningRequest{Enabled: true},
			ThinkingBudgetTokens: 4096,
		})
		if cfg.ThinkingConfig == nil {
			t.Fatal("ThinkingConfig is nil, want non-nil")
		}
		if cfg.ThinkingConfig.ThinkingBudget == nil || *cfg.ThinkingConfig.ThinkingBudget != 4096 {
			t.Errorf("ThinkingBudget = %v, want 4096", cfg.ThinkingConfig.ThinkingBudget)
		}
	})

	t.Run("Gemini 3 keeps its level rather than a rejected numeric budget", func(t *testing.T) {
		t.Parallel()
		cfg := BuildGenerateContentConfig(&GenerateRequest{
			Model:                "gemini-3-pro",
			Reasoning:            &ReasoningRequest{Enabled: true, Effort: "high"},
			ThinkingBudgetTokens: 4096,
		})
		if cfg.ThinkingConfig == nil {
			t.Fatal("ThinkingConfig is nil, want non-nil")
		}
		if cfg.ThinkingConfig.ThinkingBudget != nil {
			t.Errorf("ThinkingBudget = %v, want nil on Gemini 3", cfg.ThinkingConfig.ThinkingBudget)
		}
		if cfg.ThinkingConfig.ThinkingLevel == "" {
			t.Error("ThinkingLevel is empty, want the pinned level preserved")
		}
	})

	t.Run("suppression disables thinking and thoughts", func(t *testing.T) {
		t.Parallel()
		cfg := BuildGenerateContentConfig(&GenerateRequest{
			Model:                "gemini-2.5-pro",
			Reasoning:            &ReasoningRequest{Enabled: true},
			IncludeThoughts:      true,
			ThinkingBudgetTokens: 4096,
			SuppressThinking:     true,
		})
		if cfg.ThinkingConfig == nil {
			t.Fatal("ThinkingConfig is nil, want non-nil")
		}
		if cfg.ThinkingConfig.ThinkingBudget == nil || *cfg.ThinkingConfig.ThinkingBudget != 0 {
			t.Errorf("ThinkingBudget = %v, want 0", cfg.ThinkingConfig.ThinkingBudget)
		}
		if cfg.ThinkingConfig.IncludeThoughts {
			t.Error("IncludeThoughts = true, want false when thinking is suppressed")
		}
		if cfg.ThinkingConfig.ThinkingLevel != "" {
			t.Errorf("ThinkingLevel = %q, want empty when suppressed", cfg.ThinkingConfig.ThinkingLevel)
		}
	})

	t.Run("suppression alone still emits a zero budget", func(t *testing.T) {
		t.Parallel()
		cfg := BuildGenerateContentConfig(&GenerateRequest{
			Model:            "gemini-2.5-pro",
			SuppressThinking: true,
		})
		if cfg.ThinkingConfig == nil {
			t.Fatal("ThinkingConfig is nil, want a zero budget even with no reasoning ask")
		}
		if cfg.ThinkingConfig.ThinkingBudget == nil || *cfg.ThinkingConfig.ThinkingBudget != 0 {
			t.Errorf("ThinkingBudget = %v, want 0", cfg.ThinkingConfig.ThinkingBudget)
		}
	})
}
