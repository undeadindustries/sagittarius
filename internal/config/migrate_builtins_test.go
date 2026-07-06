package config

import "testing"

func TestMigrateLegacyBuiltins_ActiveOpenAI(t *testing.T) {
	s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInOpenAI)}}
	if !MigrateLegacyBuiltins(s) {
		t.Fatal("expected migration to report a change")
	}
	def, ok := s.Providers.Custom[string(BuiltInOpenAI)]
	if !ok {
		t.Fatalf("custom[openai] not created: %+v", s.Providers.Custom)
	}
	if def.BaseURL != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("baseURL = %q", def.BaseURL)
	}
	if def.APIKeyEnvVar != "OPENAI_API_KEY" {
		t.Errorf("apiKeyEnvVar = %q", def.APIKeyEnvVar)
	}
	if def.WireFormat != WireFormatOpenAIChat {
		t.Errorf("wireFormat = %q", def.WireFormat)
	}
}

func TestMigrateLegacyBuiltins_Idempotent(t *testing.T) {
	s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInOpenAI)}}
	if !MigrateLegacyBuiltins(s) {
		t.Fatal("first pass should migrate")
	}
	if MigrateLegacyBuiltins(s) {
		t.Fatal("second pass should be a no-op")
	}
}

func TestMigrateLegacyBuiltins_TypedResponsesBlock(t *testing.T) {
	s := &Settings{Providers: &ProvidersSettings{
		Active:          string(BuiltInGeminiAPIKey),
		OpenAIResponses: &ProviderInstanceConfig{Model: "gpt-5-codex"},
	}}
	if !MigrateLegacyBuiltins(s) {
		t.Fatal("expected migration from typed instance block")
	}
	def, ok := s.Providers.Custom[string(BuiltInOpenAIResponses)]
	if !ok {
		t.Fatalf("custom[openai-responses] not created")
	}
	if def.WireFormat != WireFormatOpenAIResponses {
		t.Errorf("wireFormat = %q, want openai-responses", def.WireFormat)
	}
}

func TestMigrateLegacyBuiltins_NotReferenced(t *testing.T) {
	s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInGeminiAPIKey)}}
	if MigrateLegacyBuiltins(s) {
		t.Fatal("no openai reference — should not migrate")
	}
	if _, ok := s.Providers.Custom[string(BuiltInOpenAI)]; ok {
		t.Fatal("custom[openai] should not be created when unreferenced")
	}
}

func TestMigrateLegacyBuiltins_PreservesExistingCustom(t *testing.T) {
	existing := CustomProviderDefinition{
		DisplayName: "My OpenAI",
		BaseURL:     "https://proxy.internal/v1/chat/completions",
		WireFormat:  WireFormatOpenAIChat,
	}
	s := &Settings{Providers: &ProvidersSettings{
		Active: string(BuiltInOpenAI),
		Custom: map[string]CustomProviderDefinition{string(BuiltInOpenAI): existing},
	}}
	if MigrateLegacyBuiltins(s) {
		t.Fatal("existing custom[openai] should be left untouched")
	}
	if got := s.Providers.Custom[string(BuiltInOpenAI)].BaseURL; got != existing.BaseURL {
		t.Errorf("baseURL overwritten: %q", got)
	}
}
