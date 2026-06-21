package config

import (
	"encoding/json"
	"testing"
)

func TestSystemPromptRoundTrip(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"systemPrompt":{"personality":"sysadmin","variant":"lite","futureKnob":1}}`)
	s, err := unmarshalSagittarius(raw)
	if err != nil {
		t.Fatalf("unmarshalSagittarius: %v", err)
	}
	if s.SystemPrompt == nil {
		t.Fatal("SystemPrompt is nil")
	}
	if s.SystemPrompt.Personality != "sysadmin" || s.SystemPrompt.Variant != "lite" {
		t.Errorf("systemPrompt = %+v", s.SystemPrompt)
	}
	if _, ok := s.SystemPrompt.Extra["futureKnob"]; !ok {
		t.Error("unknown systemPrompt key should round-trip via Extra")
	}

	out, err := marshalSagittarius(s)
	if err != nil {
		t.Fatalf("marshalSagittarius: %v", err)
	}
	reloaded, err := unmarshalSagittarius(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.SystemPrompt == nil || reloaded.SystemPrompt.Personality != "sysadmin" || reloaded.SystemPrompt.Variant != "lite" {
		t.Errorf("reloaded systemPrompt = %+v", reloaded.SystemPrompt)
	}
}

func TestSystemPromptRejectsBadVariant(t *testing.T) {
	t.Parallel()

	if _, err := unmarshalSagittarius(json.RawMessage(`{"systemPrompt":{"variant":"medium"}}`)); err == nil {
		t.Fatal("expected validation error for bad systemPrompt.variant")
	}
}

func TestProviderPersonalityAndModelsRoundTrip(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
      "model": "gpt-4o",
      "personality": "sysadmin",
      "models": {
        "gpt-4o": { "personality": "assistant", "promptMode": "lite", "futureKnob": true }
      }
    }`)
	cfg, err := unmarshalProviderInstance(raw)
	if err != nil {
		t.Fatalf("unmarshalProviderInstance: %v", err)
	}
	if cfg.Personality != "sysadmin" {
		t.Errorf("personality = %q", cfg.Personality)
	}
	mc, ok := cfg.Models["gpt-4o"]
	if !ok {
		t.Fatal("models[gpt-4o] missing")
	}
	if mc.Personality != "assistant" || mc.PromptMode != PromptModeLite {
		t.Errorf("model config = %+v", mc)
	}
	if _, ok := mc.Extra["futureKnob"]; !ok {
		t.Error("unknown per-model key should round-trip via Extra")
	}

	out, err := marshalProviderInstance(cfg)
	if err != nil {
		t.Fatalf("marshalProviderInstance: %v", err)
	}
	reloaded, err := unmarshalProviderInstance(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Personality != "sysadmin" {
		t.Errorf("reloaded personality = %q", reloaded.Personality)
	}
	if rmc := reloaded.Models["gpt-4o"]; rmc.Personality != "assistant" || rmc.PromptMode != PromptModeLite {
		t.Errorf("reloaded model config = %+v", rmc)
	}
}

func TestProviderModelsOmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	out, err := marshalProviderInstance(&ProviderInstanceConfig{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("marshalProviderInstance: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := obj["models"]; ok {
		t.Error("empty models map should be omitted")
	}
	if _, ok := obj["personality"]; ok {
		t.Error("empty personality should be omitted")
	}
}
