package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestSystemPromptStatusDetailPresetLabel(t *testing.T) {
	t.Parallel()
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{
				Personality: config.PersonalityProgrammer,
				PromptMode:  config.PromptMode(config.VariantLite),
			},
		},
	}
	got := systemPromptStatusDetail(nil, settings)
	if got != "System Prompt: Programmer (low context)" {
		t.Fatalf("got %q, want System Prompt: Programmer (low context)", got)
	}
}
