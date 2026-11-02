package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveCustomPrompt(t *testing.T) {
	t.Parallel()
	withCustom := func(providers *ProvidersSettings) *Settings {
		return &Settings{
			Providers:   providers,
			Sagittarius: &SagittariusSettings{SystemPrompt: &SagittariusSystemPromptConfig{Custom: "reviewer"}},
		}
	}
	tests := []struct {
		name     string
		settings *Settings
		provider string
		model    string
		want     string
	}{
		{name: "nil settings", settings: nil, want: ""},
		{name: "no system prompt", settings: &Settings{}, want: ""},
		{name: "builtin selection", settings: &Settings{Sagittarius: &SagittariusSettings{SystemPrompt: &SagittariusSystemPromptConfig{Personality: PersonalitySysadmin}}}, want: ""},
		{name: "custom selected", settings: withCustom(nil), provider: "openai", model: "gpt-4o", want: "reviewer"},
		{
			name:     "provider personality wins",
			settings: withCustom(&ProvidersSettings{OpenAI: &ProviderInstanceConfig{Personality: PersonalitySysadmin}}),
			provider: "openai", model: "gpt-4o", want: "",
		},
		{
			name: "model personality wins",
			settings: withCustom(&ProvidersSettings{OpenAI: &ProviderInstanceConfig{
				Models: map[string]ProviderModelConfig{"gpt-4o": {Personality: PersonalityProgrammer}},
			}}),
			provider: "openai", model: "gpt-4o", want: "",
		},
		{
			name: "other model on same provider keeps custom",
			settings: withCustom(&ProvidersSettings{OpenAI: &ProviderInstanceConfig{
				Models: map[string]ProviderModelConfig{"gpt-4o": {Personality: PersonalityProgrammer}},
			}}),
			provider: "openai", model: "gpt-5", want: "reviewer",
		},
		{
			name:     "provider prompt mode alone keeps custom",
			settings: withCustom(&ProvidersSettings{OpenAI: &ProviderInstanceConfig{PromptMode: PromptModeLite}}),
			provider: "openai", model: "gpt-4o", want: "reviewer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveCustomPrompt(tt.settings, tt.provider, tt.model); got != tt.want {
				t.Fatalf("ResolveCustomPrompt = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeSystemPromptConfigSelectionUnit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		global  *SagittariusSystemPromptConfig
		project *SagittariusSystemPromptConfig
		want    SagittariusSystemPromptConfig
	}{
		{
			name:    "project builtin replaces global custom",
			global:  &SagittariusSystemPromptConfig{Custom: "reviewer"},
			project: &SagittariusSystemPromptConfig{Personality: PersonalitySysadmin, Variant: VariantLite},
			want:    SagittariusSystemPromptConfig{Personality: PersonalitySysadmin, Variant: VariantLite},
		},
		{
			name:    "project custom replaces global builtin",
			global:  &SagittariusSystemPromptConfig{Personality: PersonalitySysadmin, Variant: VariantLite},
			project: &SagittariusSystemPromptConfig{Custom: "reviewer"},
			want:    SagittariusSystemPromptConfig{Custom: "reviewer"},
		},
		{
			name:    "empty project keeps global",
			global:  &SagittariusSystemPromptConfig{Custom: "reviewer"},
			project: &SagittariusSystemPromptConfig{},
			want:    SagittariusSystemPromptConfig{Custom: "reviewer"},
		},
		{
			name:    "project variant alone replaces the whole selection",
			global:  &SagittariusSystemPromptConfig{Personality: PersonalitySysadmin, Variant: VariantFull},
			project: &SagittariusSystemPromptConfig{Variant: VariantLite},
			want:    SagittariusSystemPromptConfig{Variant: VariantLite},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mergeSystemPromptConfig(tt.global, tt.project)
			if got.Custom != tt.want.Custom || got.Personality != tt.want.Personality || got.Variant != tt.want.Variant {
				t.Fatalf("merge = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestSystemPromptConfigCustomRoundTrip(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"custom":"reviewer","futureKey":true}`)
	cfg, err := unmarshalSystemPromptConfig(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Custom != "reviewer" {
		t.Fatalf("Custom = %q", cfg.Custom)
	}
	out, err := marshalSystemPromptConfig(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"custom":"reviewer"`) || !strings.Contains(s, `"futureKey":true`) {
		t.Fatalf("round trip = %s", s)
	}
	if _, err := unmarshalSystemPromptConfig(json.RawMessage(`{"custom":5}`)); err == nil {
		t.Fatal("expected error for non-string custom")
	}
}

func TestProjectSystemPromptPresetIDCustom(t *testing.T) {
	t.Parallel()
	s := &Settings{Sagittarius: &SagittariusSettings{SystemPrompt: &SagittariusSystemPromptConfig{Custom: "reviewer"}}}
	if got := ProjectSystemPromptPresetID(s); got != "reviewer" {
		t.Fatalf("ProjectSystemPromptPresetID = %q, want reviewer", got)
	}
}
