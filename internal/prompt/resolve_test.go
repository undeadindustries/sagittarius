package prompt

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestResolvePersonalityPrecedence(t *testing.T) {
	t.Parallel()

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: "openai",
			OpenAI: &config.ProviderInstanceConfig{
				Personality: config.PersonalitySysadmin,
				PromptMode:  config.PromptModeLite,
				Models: map[string]config.ProviderModelConfig{
					// Legacy "assistant" id resolves to canonical personal-assistant.
					"gpt-4o": {Personality: config.PersonalityAssistant, PromptMode: config.PromptModeFull},
				},
			},
		},
		Sagittarius: &config.SagittariusSettings{
			SystemPrompt: &config.SagittariusSystemPromptConfig{
				Personality: config.PersonalityProgrammer,
				Variant:     "full",
			},
		},
	}

	cases := []struct {
		name      string
		provider  string
		model     string
		wantPers  Personality
		wantVrnt  Variant
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

func TestResolveBuiltinDefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	for _, settings := range []*config.Settings{nil, {}, {Sagittarius: &config.SagittariusSettings{}}} {
		if got := ResolvePersonality(settings, "openai", "gpt-4o"); got != DefaultPersonality {
			t.Errorf("ResolvePersonality = %q, want builtin %q", got, DefaultPersonality)
		}
		if got := ResolveVariant(settings, "openai", "gpt-4o"); got != DefaultVariant {
			t.Errorf("ResolveVariant = %q, want builtin %q", got, DefaultVariant)
		}
	}
}
