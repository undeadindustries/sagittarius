package config

import (
	"strings"
	"testing"
)

func TestLookupProviderPreset(t *testing.T) {
	if _, ok := LookupProviderPreset("nope"); ok {
		t.Fatal("unknown preset should not resolve")
	}
	for _, id := range []string{"openai", "OpenAI", "  openrouter  ", "openai-responses"} {
		if _, ok := LookupProviderPreset(id); !ok {
			t.Errorf("preset %q should resolve (case/space-insensitive)", id)
		}
	}
}

func TestProviderPresetsWellFormed(t *testing.T) {
	seen := make(map[string]bool)
	responsesCount := 0
	for _, p := range ProviderPresets {
		if p.ID == "" || p.DisplayName == "" || p.BaseURL == "" {
			t.Errorf("preset %+v has an empty required field", p)
		}
		if seen[p.ID] {
			t.Errorf("duplicate preset id %q", p.ID)
		}
		seen[p.ID] = true
		if p.WireFormat != WireFormatOpenAIChat && p.WireFormat != WireFormatOpenAIResponses {
			t.Errorf("preset %q has unexpected wire format %q", p.ID, p.WireFormat)
		}
		if p.WireFormat == WireFormatOpenAIResponses {
			responsesCount++
		}
		// The base URL must already carry the completions/responses path so the
		// normalizer honors it verbatim (AD-072).
		if !strings.HasSuffix(p.BaseURL, "/chat/completions") && !strings.HasSuffix(p.BaseURL, "/responses") {
			t.Errorf("preset %q base URL %q lacks a completions/responses suffix", p.ID, p.BaseURL)
		}
	}
	// The two former built-ins must ship as presets.
	for _, id := range []string{"openai", "openai-responses", "openrouter", "anthropic"} {
		if !seen[id] {
			t.Errorf("expected preset %q to be present", id)
		}
	}
	if responsesCount != 1 {
		t.Errorf("expected exactly one openai-responses preset, got %d", responsesCount)
	}
}

func TestProviderPresetToCustomDefinition(t *testing.T) {
	p, ok := LookupProviderPreset("openai")
	if !ok {
		t.Fatal("openai preset missing")
	}
	def := p.ToCustomProviderDefinition()
	if def.BaseURL != p.BaseURL || def.APIKeyEnvVar != p.APIKeyEnvVar || def.WireFormat != p.WireFormat {
		t.Errorf("definition did not mirror preset: %+v", def)
	}
	if def.DefaultContextLimit == nil || *def.DefaultContextLimit != 128_000 {
		t.Errorf("openai default context limit not carried: %+v", def.DefaultContextLimit)
	}
	// A preset without a context limit yields a nil pointer.
	groq, _ := LookupProviderPreset("groq")
	if groq.ToCustomProviderDefinition().DefaultContextLimit != nil {
		t.Error("groq should have no default context limit")
	}
}

func TestProviderDefaultsResolvesPresets(t *testing.T) {
	def, ok := ProviderDefaults("openrouter")
	if !ok {
		t.Fatal("openrouter should resolve via ProviderDefaults")
	}
	if def.WireFormat != WireFormatOpenAIChat || !def.RequiresAPIKey {
		t.Errorf("unexpected openrouter defaults: %+v", def)
	}
	if _, ok := ProviderDefaults("definitely-not-a-provider"); ok {
		t.Fatal("unknown id should not resolve")
	}
}
