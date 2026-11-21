package provider

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestFilterHistoryForTarget(t *testing.T) {
	t.Parallel()

	geminiTurn := Message{
		Role:     RoleModel,
		Provider: string(config.BuiltInGeminiAPIKey),
		Model:    "gemini-4-carbon",
		Wire:     config.WireFormatGemini,
		Parts: []Part{
			{Text: "checking", ThoughtSignature: []byte("gem-text-sig")},
			{FunctionCall: &ToolCall{ID: "c1", Name: "read_file", Args: map[string]any{"file_path": "a.go"}}, ThoughtSignature: []byte("gem-call-sig")},
		},
	}
	anthropicTurn := Message{
		Role:     RoleModel,
		Provider: "anthropic",
		Model:    "claude-opus-5-5",
		Wire:     config.WireFormatAnthropic,
		Parts: []Part{
			{Reasoning: "secret reasoning", ThoughtSignature: []byte("anth-sig")},
			{RedactedThinking: "opaque-redacted"},
			{Text: "done"},
		},
	}
	legacyNoProvenance := Message{
		Role: RoleModel,
		Parts: []Part{
			{Reasoning: "legacy reasoning", ThoughtSignature: []byte("stale-sig")},
			{RedactedThinking: "stale-redacted"},
			{Text: "legacy answer"},
		},
	}

	t.Run("gemini signatures dropped when target is anthropic", func(t *testing.T) {
		t.Parallel()
		filtered := FilterHistoryForTarget([]Message{geminiTurn}, "anthropic", "claude-opus-5-5", config.WireFormatAnthropic)
		if len(filtered) != 1 || len(filtered[0].Parts) != 2 {
			t.Fatalf("filtered = %#v", filtered)
		}
		if len(filtered[0].Parts[0].ThoughtSignature) != 0 || len(filtered[0].Parts[1].ThoughtSignature) != 0 {
			t.Fatalf("gemini signatures leaked to anthropic: %#v", filtered[0].Parts)
		}
		// Original slice must not be mutated.
		if len(geminiTurn.Parts[0].ThoughtSignature) == 0 {
			t.Fatal("input geminiTurn mutated in place")
		}
	})

	t.Run("anthropic thinking and redacted_thinking never reach gemini or openai-chat", func(t *testing.T) {
		t.Parallel()
		for _, target := range []struct {
			prov string
			wire config.WireFormat
		}{
			{string(config.BuiltInGeminiAPIKey), config.WireFormatGemini},
			{"openai", config.WireFormatOpenAIChat},
		} {
			filtered := FilterHistoryForTarget([]Message{anthropicTurn}, target.prov, "m", target.wire)
			if len(filtered) != 1 || len(filtered[0].Parts) != 1 || filtered[0].Parts[0].Text != "done" {
				t.Fatalf("target %s: filtered = %#v, want only text part", target.prov, filtered)
			}
		}
	})

	t.Run("same-provider model change preserves signatures", func(t *testing.T) {
		t.Parallel()
		anthFiltered := FilterHistoryForTarget([]Message{anthropicTurn}, "anthropic", "claude-sonnet-5-5", config.WireFormatAnthropic)
		if len(anthFiltered[0].Parts) != 3 || string(anthFiltered[0].Parts[0].ThoughtSignature) != "anth-sig" || anthFiltered[0].Parts[1].RedactedThinking != "opaque-redacted" {
			t.Fatalf("same-provider anthropic switch dropped parts: %#v", anthFiltered)
		}
		gemFiltered := FilterHistoryForTarget([]Message{geminiTurn}, "gemini", "gemini-4-hydrogen", config.WireFormatGemini)
		if string(gemFiltered[0].Parts[1].ThoughtSignature) != "gem-call-sig" {
			t.Fatalf("same-provider gemini switch dropped signature: %#v", gemFiltered)
		}
	})

	t.Run("resumed legacy turn with no provenance drops provider-bound data", func(t *testing.T) {
		t.Parallel()
		filtered := FilterHistoryForTarget([]Message{legacyNoProvenance}, "anthropic", "claude-opus-5-5", config.WireFormatAnthropic)
		if len(filtered) != 1 || len(filtered[0].Parts) != 1 || filtered[0].Parts[0].Text != "legacy answer" {
			t.Fatalf("legacy filtered = %#v, want only text part", filtered)
		}
	})

	t.Run("anthropic mapper ignores attached gemini signatures even directly", func(t *testing.T) {
		t.Parallel()
		plan := BuildAnthropicRequestPlan(&GenerateRequest{
			Model:    "claude-opus-5-5",
			Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}, geminiTurn},
		}, AnthropicPlanOptions{ToolsEnabled: true})
		asst := plan.Body.Messages[1].Content
		for _, blk := range asst {
			if blk.Type == "thinking" || blk.Type == "redacted_thinking" {
				t.Fatalf("attached gemini signature became anthropic thinking block: %#v", asst)
			}
		}
	})
}
