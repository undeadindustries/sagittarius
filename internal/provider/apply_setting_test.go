package provider

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func openAISettings() *config.Settings {
	return &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{},
		},
	}
}

func TestApplyProviderSettingTypedFields(t *testing.T) {
	t.Parallel()
	s := openAISettings()

	if err := ApplyProviderSetting(s, "openai", "temperature", "0.25"); err != nil {
		t.Fatalf("temperature: %v", err)
	}
	if s.Providers.OpenAI.Temperature == nil || *s.Providers.OpenAI.Temperature != 0.25 {
		t.Fatalf("temperature not applied: %+v", s.Providers.OpenAI.Temperature)
	}

	if err := ApplyProviderSetting(s, "openai", "enableTools", "false"); err != nil {
		t.Fatalf("enableTools: %v", err)
	}
	if s.Providers.OpenAI.EnableTools == nil || *s.Providers.OpenAI.EnableTools {
		t.Fatalf("enableTools not applied: %+v", s.Providers.OpenAI.EnableTools)
	}

	if err := ApplyProviderSetting(s, "openai", "contextLimit", "64000"); err != nil {
		t.Fatalf("contextLimit: %v", err)
	}
	if s.Providers.OpenAI.ContextLimit == nil || *s.Providers.OpenAI.ContextLimit != 64000 {
		t.Fatalf("contextLimit not applied: %+v", s.Providers.OpenAI.ContextLimit)
	}
}

func TestApplyProviderSettingRejectsUnknownKey(t *testing.T) {
	t.Parallel()
	s := openAISettings()
	err := ApplyProviderSetting(s, "openai", "reasoningEffort", "high")
	if err == nil {
		t.Fatal("expected reasoningEffort to be rejected for openai-chat")
	}
	if !strings.Contains(err.Error(), "not editable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyProviderSettingRejectsBadValue(t *testing.T) {
	t.Parallel()
	s := openAISettings()
	if err := ApplyProviderSetting(s, "openai", "temperature", "warm"); err == nil {
		t.Fatal("expected parse error for non-numeric temperature")
	}
}

func TestApplyProviderSettingGeminiHasNoEditableKeys(t *testing.T) {
	t.Parallel()
	s := &config.Settings{Providers: &config.ProvidersSettings{
		Active:       string(config.BuiltInGeminiAPIKey),
		GeminiAPIKey: &config.ProviderInstanceConfig{},
	}}
	err := ApplyProviderSetting(s, "gemini", "temperature", "0.5")
	if err == nil || !strings.Contains(err.Error(), "no editable settings") {
		t.Fatalf("expected no-editable-settings error, got %v", err)
	}
}

func TestUpdateCustomProviderDefinition(t *testing.T) {
	t.Parallel()
	s := &config.Settings{Providers: &config.ProvidersSettings{
		Custom: map[string]config.CustomProviderDefinition{
			"my-vllm": {DisplayName: "old", BaseURL: "http://x/v1", WireFormat: config.WireFormatOpenAIChat},
		},
	}}

	if err := UpdateCustomProviderDefinition(s, "my-vllm", "displayName", "Local vLLM"); err != nil {
		t.Fatalf("displayName: %v", err)
	}
	if s.Providers.Custom["my-vllm"].DisplayName != "Local vLLM" {
		t.Fatalf("displayName not updated: %q", s.Providers.Custom["my-vllm"].DisplayName)
	}

	if err := UpdateCustomProviderDefinition(s, "my-vllm", "wireFormat", "openai-responses"); err != nil {
		t.Fatalf("wireFormat: %v", err)
	}
	if s.Providers.Custom["my-vllm"].WireFormat != config.WireFormatOpenAIResponses {
		t.Fatalf("wireFormat not updated: %q", s.Providers.Custom["my-vllm"].WireFormat)
	}

	if err := UpdateCustomProviderDefinition(s, "my-vllm", "wireFormat", "bogus"); err == nil {
		t.Fatal("expected invalid wireFormat to be rejected")
	}
}

func TestResolveEndpointForProviderAnyTarget(t *testing.T) {
	t.Parallel()
	// Active is gemini, but we resolve openai's endpoint without switching.
	s := &config.Settings{Providers: &config.ProvidersSettings{
		Active:       string(config.BuiltInGeminiAPIKey),
		GeminiAPIKey: &config.ProviderInstanceConfig{},
	}}
	endpoint, err := ResolveEndpointForProvider(s, "openai")
	if err != nil {
		t.Fatalf("resolve openai: %v", err)
	}
	if endpoint.ProviderID != string(config.BuiltInOpenAI) {
		t.Fatalf("ProviderID = %q, want openai", endpoint.ProviderID)
	}
	if endpoint.WireFormat != config.WireFormatOpenAIChat {
		t.Fatalf("wire format = %q", endpoint.WireFormat)
	}
	// The active provider in the original settings is untouched.
	if s.ActiveProvider() != string(config.BuiltInGeminiAPIKey) {
		t.Fatalf("active mutated to %q", s.ActiveProvider())
	}
}
