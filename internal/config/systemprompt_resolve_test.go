package config

import "testing"

// TestResolvePersonalityPrecedence asserts the per-model → provider → global →
// builtin tier order. (Migrated from internal/prompt when the prompt-package
// resolution wrappers were retired in favor of calling config directly.)
func TestResolvePersonalityPrecedence(t *testing.T) {
	t.Parallel()

	settings := &Settings{
		Providers: &ProvidersSettings{
			Active: "openai",
			OpenAI: &ProviderInstanceConfig{
				Personality: PersonalitySysadmin,
				PromptMode:  PromptModeLite,
				Models: map[string]ProviderModelConfig{
					// Legacy "assistant" id resolves to canonical personal-assistant.
					"gpt-4o": {Personality: PersonalityAssistant, PromptMode: PromptModeFull},
				},
			},
		},
		Sagittarius: &SagittariusSettings{
			SystemPrompt: &SagittariusSystemPromptConfig{
				Personality: PersonalityProgrammer,
				Variant:     VariantFull,
			},
		},
	}

	cases := []struct {
		name     string
		provider string
		model    string
		wantPers string
		wantVrnt string
	}{
		{"model override wins", "openai", "gpt-4o", PersonalityPersonalAssistant, VariantFull},
		{"provider override next", "openai", "other-model", PersonalitySysadmin, VariantLite},
		{"global default fallback", "gemini-apikey", "gemini-2.5", PersonalityProgrammer, VariantFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolvePersonality(settings, tc.provider, tc.model); got != tc.wantPers {
				t.Errorf("ResolvePersonality = %q, want %q", got, tc.wantPers)
			}
			if got := ResolveVariant(settings, tc.provider, tc.model); got != tc.wantVrnt {
				t.Errorf("ResolveVariant = %q, want %q", got, tc.wantVrnt)
			}
		})
	}
}

// TestResolveBuiltinDefaultsWhenUnset asserts the builtin fallbacks when nothing
// is configured.
func TestResolveBuiltinDefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	for _, settings := range []*Settings{nil, {}, {Sagittarius: &SagittariusSettings{}}} {
		if got := ResolvePersonality(settings, "openai", "gpt-4o"); got != PersonalityProgrammer {
			t.Errorf("ResolvePersonality = %q, want builtin %q", got, PersonalityProgrammer)
		}
		if got := ResolveVariant(settings, "openai", "gpt-4o"); got != VariantFull {
			t.Errorf("ResolveVariant = %q, want builtin %q", got, VariantFull)
		}
	}
}
